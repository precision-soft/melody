package migration

import (
    "context"
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

    /* the first resolution must have DONE the work: without this the test cannot tell once-then-skipped apart from never-at-all, and a guard inverted to skip the first run answers both calls with silence */
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
    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
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
    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
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

/* the released lock is the whole point of the deferred unlock, so a release that FAILED has to become the verdict rather than be dropped: a lock row that survives refuses every later migration on every process, and a resolution that answered success would leave the operator with a database nothing can migrate and no error saying why. Neither frozen major pins it — this is the assertion added here. */
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
    if createCount := recorder.countMatching(isExampleCreateTable); 6 != createCount {
        t.Fatalf("expected the set to have been applied before the release failed, got %d creates", createCount)
    }

    /* and it is not recorded as a success: the next resolution tries again */
    recorder.reset()
    recorder.execHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("expected the retried resolution to succeed, got %v", retryErr)
    }
}

/* the two-factor enrollment table is this major's own: neither frozen major carries it, and the set applying without it would leave the enrollment flow unwired at every boot, silently — the build step swallows a schema failure rather than aborting the application */
func TestEnsureMigratedCreatesTheTwoFactorTableWithTheCatalogue(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if ensureErr := EnsureMigrated(context.Background(), database); nil != ensureErr {
        t.Fatalf("expected the migration run to succeed, got %v", ensureErr)
    }

    if twoFactorCount := recorder.countMatching(isTwoFactorCreateTable); 1 != twoFactorCount {
        t.Fatalf("expected the two-factor table to be created by the single set, got %d", twoFactorCount)
    }
}

/* the wait is paid once, not once per resolution. The whole protocol runs under one process mutex, so a
   lock nobody releases used to cost the window to every caller in turn: measured on a 300ms window, three
   concurrent resolutions took 1.5s and each later request added its own. What the refusal says does not
   change — it is the same value, handed back — so the assertion is on the COST and on the identity of what
   is returned, the two things that separate a remembered refusal from a repeated one. */
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

/* the memory is not a verdict: once the window it was recorded for has passed, the next resolution asks the
   database again, so a lock that was released heals the process without a restart. */
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
