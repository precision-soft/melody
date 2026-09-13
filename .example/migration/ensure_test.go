package migration

import (
    "context"
    "database/sql/driver"
    "errors"
    "sync"
    "testing"

    melodyexception "github.com/precision-soft/melody/exception"
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

    if createCount := recorder.countMatching(isExampleCreateTable); 4 != createCount {
        t.Fatalf("expected all four catalog tables to be created, got %d", createCount)
    }
}

func TestEnsureMigratedRunsOncePerHandle(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    /* the first resolution must have DONE the work: without this the test cannot tell once-then-skipped apart from never-at-all, and a guard inverted to skip the first run answers both calls with silence */
    if createCount := recorder.countMatching(isExampleCreateTable); 4 != createCount {
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

    var refusal *melodyexception.Error
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
    if createCount := recorder.countMatching(isExampleCreateTable); 4 != createCount {
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
    if createCount := recorder.countMatching(isExampleCreateTable); 4 != createCount {
        t.Fatalf("expected the set to be applied exactly once, got %d creates", createCount)
    }
}

func TestEnsureJournalMigratedAppliesTheJournalSet(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureJournalMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the journal migration run to succeed, got %v", ensureErr)
    }

    if journalCount := recorder.countMatching(isJournalCreateTable); 1 != journalCount {
        t.Fatalf("expected exactly the journal table to be created, got %d", journalCount)
    }
    if createCount := recorder.countMatching(isExampleCreateTable); 1 != createCount {
        t.Fatalf("expected no catalog table beside the journal one, got %d creates", createCount)
    }
}

func TestEnsureJournalMigratedRefusesNamingItsOwnRemedy(t *testing.T) {
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

    ensureErr := EnsureJournalMigrated(context.Background(), database)
    if nil == ensureErr {
        t.Fatal("expected the exhausted retry window to refuse")
    }

    var refusal *melodyexception.Error
    if false == errors.As(ensureErr, &refusal) {
        t.Fatalf("expected a melody exception, got %T", ensureErr)
    }
    if journalMigrationUnlockCommand != refusal.Context()["unlockCommand"] {
        t.Fatalf("expected the refusal to name the journal unlock command, got %v", refusal.Context())
    }
}

func TestEnsureMigratedSetsAreMemoizedIndependentlyOnOneHandle(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the catalog migration run to succeed, got %v", ensureErr)
    }

    /* a memoization keyed by handle alone would answer the journal set with the catalog set's recorded success and never apply it */
    recorder.reset()

    if ensureErr := EnsureJournalMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the journal migration run to succeed, got %v", ensureErr)
    }
    if journalCount := recorder.countMatching(isJournalCreateTable); 1 != journalCount {
        t.Fatalf("expected the journal set to be applied on the already-memoized handle, got %d", journalCount)
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

    schemaDropIndex := indexOfQueryContaining(queries, "DROP TABLE IF EXISTS `melody_example_v1_category`")
    bookkeepingDropIndex := indexOfQueryContaining(queries, "DROP TABLE IF EXISTS bun_migrations")
    schemaCreateIndex := indexOfQueryContaining(queries, "CREATE TABLE IF NOT EXISTS `melody_example_v1_category`")

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

    ensureMutex.Lock()
    migratedDatabaseList[migratedSetKey{database: database, migrationSet: Migrations}] = struct{}{}
    ensureMutex.Unlock()

    if resetErr := Reset(context.Background(), database); nil != resetErr {
        t.Fatalf("expected the reset to succeed, got %v", resetErr)
    }

    ensureMutex.Lock()
    _, stillMigrated := migratedDatabaseList[migratedSetKey{database: database, migrationSet: Migrations}]
    ensureMutex.Unlock()

    if true == stillMigrated {
        t.Fatalf("expected the reset to clear the migrated memo for the handle")
    }
}

/* the journal is a set and a database of its own, so its reset is a door of its own; the memo is keyed by
   handle AND set, which is what keeps one from answering for the other. */
func TestResetJournalDropsAndReappliesTheJournalSchemaAlone(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    ensureMutex.Lock()
    migratedDatabaseList[migratedSetKey{database: database, migrationSet: JournalMigrations}] = struct{}{}
    ensureMutex.Unlock()

    if resetErr := ResetJournal(context.Background(), database); nil != resetErr {
        t.Fatalf("expected the journal reset to succeed, got %v", resetErr)
    }

    queries := recorder.recordedQueries()

    dropIndex := indexOfQueryContaining(queries, "DROP TABLE IF EXISTS melody_example_v1_catalog_journal")
    createIndex := indexOfQueryContaining(queries, "CREATE TABLE IF NOT EXISTS melody_example_v1_catalog_journal")

    if 0 > dropIndex || 0 > createIndex || dropIndex > createIndex {
        t.Fatalf("expected the journal table to be dropped and created again, recorded: %v", queries)
    }

    if 0 <= indexOfQueryContaining(queries, "melody_example_v1_category") {
        t.Fatalf("expected the journal reset to leave the catalog set alone, recorded: %v", queries)
    }

    ensureMutex.Lock()
    _, stillMigrated := migratedDatabaseList[migratedSetKey{database: database, migrationSet: JournalMigrations}]
    ensureMutex.Unlock()

    if true == stillMigrated {
        t.Fatalf("expected the journal reset to clear the memo for its own set")
    }
}
