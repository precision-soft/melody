package migration

import (
    "context"
    "database/sql"
    "fmt"
    "os"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
    "github.com/uptrace/bun/migrate"
    "strings"
    "database/sql/driver"
    "errors"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

func TestEnsureMigratedRefusesANilDatabase(t *testing.T) {
    ensureErr := EnsureMigrated(context.Background(), nil)
    if nil == ensureErr {
        t.Fatal("expected a nil database to be refused")
    }
}

func TestEnsureMigratedRunsInitLockMigrateUnlockInOrder(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    lockIndex := recorder.firstIndexMatching(isMigrationLockInsert)
    createIndex := recorder.firstIndexMatching(isExampleCreateTable)
    unlockIndex := recorder.firstIndexMatching(isMigrationLockDelete)

    if -1 == lockIndex || -1 == createIndex || -1 == unlockIndex {
        t.Fatalf("expected lock, create and unlock statements, got %v", recorder.recordedQueries())
    }
    if false == (lockIndex < createIndex && createIndex < unlockIndex) {
        t.Fatalf(
            "expected lock (%d) before create (%d) before unlock (%d)",
            lockIndex,
            createIndex,
            unlockIndex,
        )
    }

    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
        t.Fatalf("expected all six tables to be created, got %d", createCount)
    }
}

func TestEnsureMigratedRunsOncePerHandle(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
        t.Fatalf("expected the first resolution to apply the set, got %d creates", createCount)
    }

    recorder.reset()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the recorded success to answer, got %v", ensureErr)
    }
    if queries := recorder.recordedQueries(); 0 != len(queries) {
        t.Fatalf("expected no statement on the second resolution, got %v", queries)
    }
}

func TestEnsureMigratedSkipsWhenTheLockIsHeldAndNothingIsPending(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    lockHeld := errors.New("lock row exists")
    recorder.execHook = func(query string) error {
        if true == isMigrationLockInsert(query) {
            return lockHeld
        }

        return nil
    }
    recorder.queryHook = func(query string) ([]string, [][]driver.Value, error) {
        if true == isMigrationStatusSelect(query) {
            columns, rows := appliedStatusRows()

            return columns, rows, nil
        }

        return []string{}, nil, nil
    }

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected a finished competitor to answer success, got %v", ensureErr)
    }

    if createCount := recorder.countMatching(isExampleCreateTable); 0 != createCount {
        t.Fatalf("expected no example table statement, got %d", createCount)
    }
    if unlockCount := recorder.countMatching(isMigrationLockDelete); 0 != unlockCount {
        t.Fatal("expected the never-held lock to never be released")
    }

    recorder.reset()
    recorder.execHook = nil
    recorder.queryHook = nil

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the recorded success to answer, got %v", ensureErr)
    }
    if queries := recorder.recordedQueries(); 0 != len(queries) {
        t.Fatalf("expected no statement on the second resolution, got %v", queries)
    }
}

func TestEnsureMigratedRefusesAfterTheRetryWindowNamingTheRemedy(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    previousWindow := migrationLockRetryWindow
    migrationLockRetryWindow = 0
    defer func() {
        migrationLockRetryWindow = previousWindow
    }()

    lockHeld := errors.New("lock row exists")
    recorder.execHook = func(query string) error {
        if true == isMigrationLockInsert(query) {
            return lockHeld
        }

        return nil
    }

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr {
        t.Fatal("expected the exhausted retry window to refuse")
    }

    var refusal *exception.Error
    if false == errors.As(ensureErr, &refusal) {
        t.Fatalf("expected a melody exception, got %T", ensureErr)
    }
    if migrationLocksTable != refusal.Context()["locksTable"] {
        t.Fatalf("expected the refusal to name the locks table, got %v", refusal.Context())
    }
    if migrationUnlockCommand != refusal.Context()["unlockCommand"] {
        t.Fatalf("expected the refusal to name the unlock command, got %v", refusal.Context())
    }
    if false == errors.Is(ensureErr, lockHeld) {
        t.Fatal("expected the bun lock error to stay reachable as the cause")
    }

    recorder.reset()
    recorder.execHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("expected the retried resolution to succeed, got %v", retryErr)
    }
    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
        t.Fatalf("expected the retried resolution to migrate, got %d creates", createCount)
    }
}

func TestEnsureMigratedReleasesTheLockOnACancelledContext(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    cancellableContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    recorder.execHook = func(query string) error {
        if true == isExampleCreateTable(query) {
            cancel()
        }

        return nil
    }

    ensureErr := EnsureMigrated(cancellableContext, database)
    if nil == ensureErr {
        t.Fatal("expected the cancelled migration to fail")
    }

    if unlockCount := recorder.countMatching(isMigrationLockDelete); 1 != unlockCount {
        t.Fatalf("expected the unlock to reach the database despite the cancellation, got %d", unlockCount)
    }
}

func TestEnsureMigratedSerializesConcurrentResolutions(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    waitGroup := sync.WaitGroup{}
    failures := make(chan error, 8)

    for index := 0; index < 8; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
                failures <- ensureErr
            }
        }()
    }

    waitGroup.Wait()
    close(failures)

    for ensureErr := range failures {
        t.Fatalf("expected every concurrent resolution to succeed, got %v", ensureErr)
    }

    if lockCount := recorder.countMatching(isMigrationLockInsert); 1 != lockCount {
        t.Fatalf("expected exactly one lock acquisition across the resolutions, got %d", lockCount)
    }
    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
        t.Fatalf("expected the set to be applied exactly once, got %d creates", createCount)
    }
}

func TestEnsureMigratedCreatesTheJournalTableWithTheCatalogue(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    if journalCount := recorder.countMatching(isJournalCreateTable); 1 != journalCount {
        t.Fatalf("expected the journal table to be created by the single set, got %d", journalCount)
    }
}

func TestEnsureMigratedReportsAFailedUnlockAsTheVerdict(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    unlockRefused := errors.New("the delete could not run")
    recorder.execHook = func(query string) error {
        if true == isMigrationLockDelete(query) {
            return unlockRefused
        }

        return nil
    }

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr {
        t.Fatal("expected the failed release to become the verdict")
    }

    var refusal *exception.Error
    if false == errors.As(ensureErr, &refusal) {
        t.Fatalf("expected a melody exception, got %T", ensureErr)
    }
    if migrationLocksTable != refusal.Context()["locksTable"] {
        t.Fatalf("expected the refusal to name the locks table, got %v", refusal.Context())
    }
    if migrationUnlockCommand != refusal.Context()["unlockCommand"] {
        t.Fatalf("expected the refusal to name the unlock command, got %v", refusal.Context())
    }
    if false == errors.Is(ensureErr, unlockRefused) {
        t.Fatal("expected the release failure to stay reachable as the cause")
    }

    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
        t.Fatalf("expected the set to have been applied before the release failed, got %d creates", createCount)
    }

    recorder.reset()
    recorder.execHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("expected the retried resolution to succeed, got %v", retryErr)
    }
}

func TestEnsureMigratedCreatesTheTwoFactorTableWithTheCatalogue(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    if twoFactorCount := recorder.countMatching(isTwoFactorCreateTable); 1 != twoFactorCount {
        t.Fatalf("expected the two-factor table to be created by the single set, got %d", twoFactorCount)
    }
}

func TestEnsureMigratedAnswersARememberedRefusalWithoutWaitingAgain(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    previousWindow := migrationLockRetryWindow
    migrationLockRetryWindow = 150 * time.Millisecond
    defer func() {
        migrationLockRetryWindow = previousWindow
    }()

    lockHeld := errors.New("lock row exists")
    recorder.execHook = func(query string) error {
        if true == isMigrationLockInsert(query) {
            return lockHeld
        }

        return nil
    }

    startedAt := time.Now()
    firstErr := EnsureMigrated(context.Background(), database)
    firstCost := time.Since(startedAt)

    if nil == firstErr {
        t.Fatal("expected the held lock to refuse the first resolution")
    }
    if migrationLockRetryWindow > firstCost {
        t.Fatalf("expected the first resolution to wait out the window, got %v", firstCost)
    }

    startedAt = time.Now()
    secondErr := EnsureMigrated(context.Background(), database)
    secondCost := time.Since(startedAt)

    if secondErr != firstErr {
        t.Fatalf("expected the remembered refusal itself, got %v", secondErr)
    }
    if migrationLockRetryWindow <= secondCost {
        t.Fatalf("expected the second resolution to be answered without waiting again, got %v", secondCost)
    }
}

func TestEnsureMigratedForgetsTheRefusalOnceItsWindowHasPassed(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    previousWindow := migrationLockRetryWindow
    migrationLockRetryWindow = 50 * time.Millisecond
    defer func() {
        migrationLockRetryWindow = previousWindow
    }()

    recorder.execHook = func(query string) error {
        if true == isMigrationLockInsert(query) {
            return errors.New("lock row exists")
        }

        return nil
    }

    if firstErr := EnsureMigrated(context.Background(), database); nil == firstErr {
        t.Fatal("expected the held lock to refuse the first resolution")
    }

    time.Sleep(2 * migrationLockRetryWindow)

    recorder.reset()
    recorder.execHook = nil

    if healedErr := EnsureMigrated(context.Background(), database); nil != healedErr {
        t.Fatalf("expected the resolution after the window to try the database again, got %v", healedErr)
    }
    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
        t.Fatalf("expected the healed resolution to migrate, got %d creates", createCount)
    }
}

func TestResetDropsTheSchemaAndTheBookkeepingThenAppliesTheSchemaAgain(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if resetErr := Reset(context.Background(), database); nil != resetErr {
        t.Fatalf("expected the reset to succeed, got %v", resetErr)
    }

    queries := recorder.recordedQueries()

    schemaDropIndex := indexOfQueryContaining(queries, "DROP TABLE IF EXISTS `melody_example_v3_category`")
    bookkeepingDropIndex := indexOfQueryContaining(queries, "DROP TABLE IF EXISTS bun_migrations")
    schemaCreateIndex := indexOfQueryContaining(queries, "CREATE TABLE IF NOT EXISTS `melody_example_v3_category`")

    if 0 > schemaDropIndex || 0 > bookkeepingDropIndex || 0 > schemaCreateIndex {
        t.Fatalf(
            "expected the reset to drop the schema (%d), drop the bookkeeping (%d) and create the schema again (%d), recorded: %v",
            schemaDropIndex,
            bookkeepingDropIndex,
            schemaCreateIndex,
            queries,
        )
    }

    if schemaDropIndex > bookkeepingDropIndex {
        t.Fatalf("expected the schema to be dropped before the bookkeeping, recorded: %v", queries)
    }

    if bookkeepingDropIndex > schemaCreateIndex {
        t.Fatalf("expected the schema to be created after the bookkeeping went, recorded: %v", queries)
    }
}

func TestResetClearsTheMemoForTheHandle(t *testing.T) {
    database, _ := newFakeBunDatabase()
    memoizationKey := migratedSetKey{database: database, migrationSet: Migrations}

    ensureMutex.Lock()
    migratedDatabaseList[memoizationKey] = struct{}{}
    refusedDatabaseList[memoizationKey] = refusedMigrationAttempt{refusal: context.Canceled, refusedAt: time.Now()}
    ensureMutex.Unlock()

    if resetErr := Reset(context.Background(), database); nil != resetErr {
        t.Fatalf("expected the reset to succeed, got %v", resetErr)
    }

    ensureMutex.Lock()
    _, stillMigrated := migratedDatabaseList[memoizationKey]
    _, stillRefused := refusedDatabaseList[memoizationKey]
    ensureMutex.Unlock()

    if true == stillMigrated {
        t.Fatalf("expected the reset to clear the migrated memo for the handle")
    }
    if true == stillRefused {
        t.Fatalf("expected the reset to clear the refusal memo for the handle")
    }
}

func TestTheMemoDoesNotLetOneSetAnswerForTheOther(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    defer func() {
        ensureMutex.Lock()
        delete(migratedDatabaseList, migratedSetKey{database: database, migrationSet: Migrations})
        delete(migratedDatabaseList, migratedSetKey{database: database, migrationSet: ArchiveMigrations})
        ensureMutex.Unlock()
    }()

    if catalogErr := EnsureMigrated(context.Background(), database); nil != catalogErr {
        t.Fatalf("catalogue set: %v", catalogErr)
    }

    recorder.reset()

    if archiveErr := EnsureArchiveMigrated(context.Background(), database); nil != archiveErr {
        t.Fatalf("archive set: %v", archiveErr)
    }

    sawArchiveTable := 0 < recorder.countMatching(func(query string) bool {
        return strings.Contains(query, CatalogReadingTableName)
    })

    if false == sawArchiveTable {
        t.Fatalf("the archive set did not run over a handle the catalogue set had already migrated: the memo let one set answer for the other")
    }
}

func TestEnsureArchiveMigratedNamesTheArchiveSetAndTheStepOverADatabaseThatRefuses(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    refusal := errors.New("ERROR: permission denied for schema public (SQLSTATE 42501)")
    recorder.execHook = func(query string) error {
        if true == strings.Contains(query, "CREATE TABLE") && true == strings.Contains(query, "bun_migrations") {
            return refusal
        }

        return nil
    }

    ensureErr := EnsureArchiveMigrated(context.Background(), database)
    if nil == ensureErr {
        t.Fatal("expected the refused init to fail the resolution")
    }

    if false == strings.Contains(ensureErr.Error(), "initialising the bookkeeping did not complete on the archive set") {
        t.Fatalf("expected the failure to name the archive set and the step, got %q", ensureErr.Error())
    }

    if false == errors.Is(ensureErr, refusal) {
        t.Fatalf("expected the driver's refusal to stay the cause, got %v", ensureErr)
    }

    if "archive" != exception.LogContext(ensureErr)["set"] || "db:archive:unlock" != exception.LogContext(ensureErr)["unlockCommand"] {
        t.Fatalf("expected the context to carry the set and its unlock command, got %v", exception.LogContext(ensureErr))
    }
}

func TestMigrationStepFailureLeavesAnOwnExceptionUntouched(t *testing.T) {
    own := exception.NewError("migration: the migration lock is held", nil, nil)

    if own != migrationStepFailure("archive", "applying the set", "db:archive:unlock", own) {
        t.Fatal("expected the application's own exception to be handed back unwrapped")
    }
}

func TestInitializeMigrationBookkeepingConcurrentPostgres(t *testing.T) {
    dsn := os.Getenv("POSTGRES_DSN")
    if "" == dsn {
        t.Skip("POSTGRES_DSN is not set")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()
    root := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
    defer root.Close()
    schemaName := fmt.Sprintf("melody_init_%d", time.Now().UnixNano())
    if _, err := root.ExecContext(ctx, "CREATE SCHEMA "+schemaName); nil != err {
        t.Fatal(err)
    }
    defer func() {
        cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancelCleanup()
        if _, err := root.ExecContext(cleanupContext, "DROP SCHEMA "+schemaName+" CASCADE"); nil != err {
            t.Error(err)
        }
    }()

    const workers = 12
    databases := make([]*bun.DB, workers)
    for index := range databases {
        sqlDatabase := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
        sqlDatabase.SetMaxOpenConns(1)
        sqlDatabase.SetMaxIdleConns(1)
        if _, err := sqlDatabase.ExecContext(ctx, "SET search_path TO "+schemaName); nil != err {
            sqlDatabase.Close()
            t.Fatal(err)
        }
        databases[index] = bun.NewDB(sqlDatabase, pgdialect.New())
        defer databases[index].Close()
    }
    start := make(chan struct{})
    results := make(chan error, workers)
    for _, database := range databases {
        go func() {
            <-start
            results <- initializeMigrationBookkeeping(ctx, migrate.NewMigrator(database, migrate.NewMigrations()))
        }()
    }
    close(start)
    for range workers {
        if err := <-results; nil != err {
            t.Errorf("concurrent init: %v", err)
        }
    }
}

func TestResetFailureInvalidatesSuccessfulMigrationMemo(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    defer database.Close()
    if err := EnsureMigrated(context.Background(), database); nil != err {
        t.Fatal(err)
    }
    failure := errors.New("drop refused")
    recorder.execHook = func(query string) error {
        if strings.HasPrefix(query, "DROP TABLE") {
            return failure
        }
        return nil
    }
    if err := Reset(context.Background(), database); false == errors.Is(err, failure) {
        t.Fatalf("reset failure lost: %v", err)
    }
    recorder.execHook = nil
    recorder.reset()
    if err := EnsureMigrated(context.Background(), database); nil != err {
        t.Fatal(err)
    }
    if 0 == len(recorder.recordedQueries()) {
        t.Fatal("failed reset retained success memo")
    }
}

func TestInitializeMigrationBookkeepingReturnsNonCatalogErrors(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    defer database.Close()
    failure := errors.New("create refused")
    calls := 0
    recorder.execHook = func(query string) error {
        calls++
        return failure
    }
    err := initializeMigrationBookkeeping(context.Background(), migrate.NewMigrator(database, migrate.NewMigrations()))
    if false == errors.Is(err, failure) || 1 != calls {
        t.Fatalf("unexpected retry or cause: calls=%d error=%v", calls, err)
    }
}
