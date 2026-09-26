package migration

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "os"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
    "github.com/uptrace/bun/migrate"
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

    if createCount := recorder.countMatching(isExampleCreateTable); 7 != createCount {
        t.Fatalf("expected the six tables and the fingerprint table to be created, got %d", createCount)
    }
}

func TestEnsureMigratedRunsOncePerHandle(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    /* the first resolution must have DONE the work: without this the test cannot tell once-then-skipped apart from never-at-all, and a guard inverted to skip the first run answers both calls with silence */
    if createCount := recorder.countMatching(isExampleCreateTable); 7 != createCount {
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

    /* the observed competitor success is recorded like an own one: the next resolution asks nothing */
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

    /* a refusal is not a success: the next resolution tries again */
    recorder.reset()
    recorder.execHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("expected the retried resolution to succeed, got %v", retryErr)
    }
    if createCount := recorder.countMatching(isExampleCreateTable); 7 != createCount {
        t.Fatalf("expected the retried resolution to migrate, got %d creates", createCount)
    }
}

func TestEnsureMigratedReleasesTheLockOnACancelledContext(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    cancellableContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    /* the fake connection refuses a cancelled context before recording, so the unlock delete can appear below only by riding a context detached from the cancelled one */
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
    if createCount := recorder.countMatching(isExampleCreateTable); 7 != createCount {
        t.Fatalf("expected the set to be applied exactly once, got %d creates", createCount)
    }
}

/* the journal table is the one this major keeps in the same set instead of a context of its own, so the set applying without it would leave catalog:journal reading a table nothing creates */
func TestEnsureMigratedCreatesTheJournalTableWithTheCatalogue(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    if journalCount := recorder.countMatching(isJournalCreateTable); 1 != journalCount {
        t.Fatalf("expected the journal table to be created by the single set, got %d", journalCount)
    }
}

/* the released lock is the whole point of the deferred unlock, so a release that fails becomes the verdict rather than being dropped: a lock row that survives refuses every later migration on every process, and a resolution that answered success would leave the operator with a database nothing can migrate and no error saying why. */
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

    /* the migration ran: the failure is about the release, not about the set */
    if createCount := recorder.countMatching(isExampleCreateTable); 7 != createCount {
        t.Fatalf("expected the set to have been applied before the release failed, got %d creates", createCount)
    }

    /* and it is not recorded as a success: the next resolution tries again */
    recorder.reset()
    recorder.execHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("expected the retried resolution to succeed, got %v", retryErr)
    }
}

/* the two-factor enrollment table is this major's own: neither frozen major carries it, and the set applying without it would leave the enrollment flow reading a table nothing creates */
func TestEnsureMigratedCreatesTheTwoFactorTableWithTheCatalogue(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    if twoFactorCount := recorder.countMatching(isTwoFactorCreateTable); 1 != twoFactorCount {
        t.Fatalf("expected the two-factor table to be created by the single set, got %d", twoFactorCount)
    }
}

/* the wait is paid once, not once per resolution: the whole protocol runs under one process mutex, so without the memo a lock nobody releases would cost the window to every caller in turn. The refusal is the same value, handed back, so the assertion is on the cost and on the identity of what is returned, the two things that separate a remembered refusal from a repeated one. */
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

/* the memory is not a verdict: once the window it is recorded for has passed, the next resolution asks the database again, so a released lock heals the process without a restart. */
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
    if createCount := recorder.countMatching(isExampleCreateTable); 7 != createCount {
        t.Fatalf("expected the healed resolution to migrate, got %d creates", createCount)
    }
}

/* Reset is the door an operator reaches for when a volume was provisioned by an older build, so what it
   has to do is more than re-run the set: it drops the schema, drops the BOOKKEEPING with it — which is
   where an older set's rows live — and applies the schema again. The order is the assertion, because a
   reset that dropped the bookkeeping before the schema would leave the tables standing with no record of
   them. */
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

/* the memo is what would otherwise answer for a state the reset has just taken away: a resolution later in
   the same process reads "already migrated" and finds no tables. */
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

/* the memo key is the handle and the set together, so one handle asked for both sets runs both; keyed by the handle alone, the first set applied would answer for the second and the archive's table would never be created on an application that keeps both on one connection. It is driven through the funnel rather than by writing the map, because the key is computed inside the funnel and a test that built it would assert its own arithmetic. The two sets share one handle deliberately: over two handles the memo separates them under either key. */
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

    /* the archive's own table is what says the second set RAN. With the memo keyed on the handle alone the
       catalogue's entry answers for the archive, EnsureArchiveMigrated returns nil having done nothing,
       and this statement never reaches the recorder. */
    sawArchiveTable := 0 < recorder.countMatching(func(query string) bool {
        return strings.Contains(query, CatalogReadingTableName)
    })

    if false == sawArchiveTable {
        t.Fatalf("the archive set did not run over a handle the catalogue set had already migrated: the memo let one set answer for the other")
    }
}

/* a failure of the database on the archive's first resolution is handed back as this application's exception naming the set and the step, with the driver's error as the cause, so the headline says which database refused rather than the container's by-type relabelling "service not registered in resolver", and errors.Is still reaches the cause. */
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

/* the lock refusal already names its remedy; wrapping it again would bury the remedy under a second headline, so an exception of this application's own is handed back as it is */
func TestMigrationStepFailureLeavesAnOwnExceptionUntouched(t *testing.T) {
    own := exception.NewError("migration: the migration lock is held", nil, nil)

    if own != migrationStepFailure("archive", "applying the set", "db:archive:unlock", own) {
        t.Fatal("expected the application's own exception to be handed back unwrapped")
    }
}

/* a lock wait that ends because the process is going away hands back the set's own exception, not a bare context.Canceled, which a by-type resolution would wrap under "service resolution failed in resolver". errors.Is still reaches the cancellation. */
func TestEnsureMigratedNamesTheSetWhenTheLockWaitIsCancelled(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    recorder.execHook = func(query string) error {
        if true == isMigrationLockInsert(query) {
            return errors.New("lock row exists")
        }

        return nil
    }
    recorder.queryHook = func(query string) ([]string, [][]driver.Value, error) {
        if true == isMigrationStatusSelect(query) {
            columns, rows := pendingStatusRows()

            return columns, rows, nil
        }

        return []string{}, nil, nil
    }

    ctx, cancel := context.WithCancel(context.Background())
    go func() {
        time.Sleep(50 * time.Millisecond)
        cancel()
    }()

    ensureErr := EnsureMigrated(ctx, database)
    if nil == ensureErr {
        t.Fatal("expected the cancelled wait to refuse")
    }

    if false == errors.Is(ensureErr, context.Canceled) {
        t.Fatalf("expected the cancellation to stay the cause, got %v", ensureErr)
    }

    var ownException *exception.Error
    if false == errors.As(ensureErr, &ownException) {
        t.Fatalf("expected the application's own exception around the cancellation, got %T: %v", ensureErr, ensureErr)
    }

    if "waiting for the migration lock" != exception.LogContext(ensureErr)["step"] {
        t.Fatalf("expected the step named, got %v", exception.LogContext(ensureErr))
    }
}

func TestResetThatFailsHalfWayLeavesTheHandleToBeMigratedAgain(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    memoizationKey := migratedSetKey{database: database, migrationSet: Migrations}

    ensureMutex.Lock()
    migratedDatabaseList[memoizationKey] = struct{}{}
    ensureMutex.Unlock()
    defer func() {
        ensureMutex.Lock()
        delete(migratedDatabaseList, memoizationKey)
        ensureMutex.Unlock()
    }()

    dropRefused := errors.New("drop refused")
    recorder.execHook = func(query string) error {
        if "DROP TABLE" == query[:min(len(query), len("DROP TABLE"))] {
            return dropRefused
        }

        return nil
    }

    if resetErr := Reset(context.Background(), database); false == errors.Is(resetErr, dropRefused) {
        t.Fatalf("expected the reset to fail on the refused drop, got %v", resetErr)
    }

    ensureMutex.Lock()
    _, stillMigrated := migratedDatabaseList[memoizationKey]
    ensureMutex.Unlock()

    if true == stillMigrated {
        t.Fatalf("expected a reset that failed half way to clear the migrated memo for the handle")
    }
}

/* postgresFieldError answers Field the way pgdriver.Error does, so the classification is exercised without a server */
type postgresFieldError struct {
    fields map[byte]string
}

func (instance postgresFieldError) Error() string {
    return "ERROR: " + instance.fields['C']
}

func (instance postgresFieldError) Field(field byte) string {
    return instance.fields[field]
}

func TestInitializeMigrationBookkeepingRetriesALostCreationRace(t *testing.T) {
    for _, raceError := range []postgresFieldError{
        {fields: map[byte]string{'C': "23505", 'n': "pg_class_relname_nsp_index"}},
        {fields: map[byte]string{'C': "23505", 'n': "pg_type_typname_nsp_index"}},
        {fields: map[byte]string{'C': "42P07"}},
        {fields: map[byte]string{'C': "42710"}},
    } {
        database, recorder := newFakeBunDatabase()
        creations := 0
        recorder.execHook = func(query string) error {
            if false == strings.HasPrefix(query, "CREATE TABLE") {
                return nil
            }

            creations++
            if 1 == creations {
                return raceError
            }

            return nil
        }

        if initErr := initializeMigrationBookkeeping(context.Background(), migrate.NewMigrator(database, migrate.NewMigrations())); nil != initErr {
            t.Fatalf("expected the lost race %v to be retried, got %v", raceError.fields, initErr)
        }
        if 2 > creations {
            t.Fatalf("expected the creation to run again after the lost race %v, got %d", raceError.fields, creations)
        }
    }
}

func TestInitializeMigrationBookkeepingAnswersAnyOtherFailureAtOnce(t *testing.T) {
    for _, otherError := range []error{
        postgresFieldError{fields: map[byte]string{'C': "42501"}},
        postgresFieldError{fields: map[byte]string{'C': "23505", 'n': "an_application_index"}},
        errors.New("connection refused"),
    } {
        database, recorder := newFakeBunDatabase()
        creations := 0
        recorder.execHook = func(query string) error {
            if true == strings.HasPrefix(query, "CREATE TABLE") {
                creations++

                return otherError
            }

            return nil
        }

        initErr := initializeMigrationBookkeeping(context.Background(), migrate.NewMigrator(database, migrate.NewMigrations()))
        if nil == initErr || otherError.Error() != initErr.Error() || 1 != creations {
            t.Fatalf("expected %v answered at once, got %v after %d creations", otherError, initErr, creations)
        }
    }
}

func TestInitializeMigrationBookkeepingSurvivesConcurrentCreatorsOnPostgres(t *testing.T) {
    dsn := os.Getenv("POSTGRES_DSN")
    if "" == dsn {
        t.Skip("POSTGRES_DSN is not set")
    }

    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()

    root := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
    defer root.Close()

    schemaName := fmt.Sprintf("melody_init_%d", time.Now().UnixNano())
    if _, createErr := root.ExecContext(ctx, "CREATE SCHEMA "+schemaName); nil != createErr {
        t.Fatalf("create schema: %v", createErr)
    }
    defer func() {
        cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancelCleanup()

        if _, dropErr := root.ExecContext(cleanupContext, "DROP SCHEMA "+schemaName+" CASCADE"); nil != dropErr {
            t.Errorf("drop schema: %v", dropErr)
        }
    }()

    const workers = 12
    databases := make([]*bun.DB, workers)
    for index := range databases {
        sqlDatabase := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
        sqlDatabase.SetMaxOpenConns(1)
        sqlDatabase.SetMaxIdleConns(1)
        if _, searchPathErr := sqlDatabase.ExecContext(ctx, "SET search_path TO "+schemaName); nil != searchPathErr {
            _ = sqlDatabase.Close()
            t.Fatalf("search path: %v", searchPathErr)
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
        if initErr := <-results; nil != initErr {
            t.Errorf("expected every concurrent init to succeed, got %v", initErr)
        }
    }
}
