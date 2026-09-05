package migration

import (
    "context"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

const (
    migrationLocksTable    = "bun_migration_locks"
    migrationUnlockCommand = "db:unlock"
)

/* the unlock must not ride the caller's context: an interrupted resolution cancels it, the delete never reaches the database and the lock row survives, refusing every later migration until someone runs the unlock command */
const migrationUnlockTimeout = 5 * time.Second

/* the window bounds how long a resolution waits for another process's migration before refusing; both are variables so the tests can shorten the wait instead of holding a test binary for half a minute */
var (
    migrationLockRetryWindow   = 30 * time.Second
    migrationLockRetryInterval = 250 * time.Millisecond
)

/* the memoization is keyed by handle alone, because this major carries a single set: the catalogue, the journal and the two-factor enrollment share one connection, so one handle can only ever be asked for Migrations */
var (
    ensureMutex          sync.Mutex
    migratedDatabaseList = map[*bun.DB]struct{}{}
)

/* EnsureMigrated applies the Migrations set to the example's database, once per handle and per process. The repository constructors the generated wiring fills call it at first resolution, and the two-factor build step calls it before it publishes its store, which is what keeps a freshly recreated volume usable without an operator step: the tables appear when the first request reaches a repository, exactly as they did when each repository owned its own create statement.

   Only a success is recorded; a failed attempt is retried at the next resolution. The mutex serializes the callers of one process, and the bun migration lock serializes processes sharing the database — several instances of this example race here whenever a volume starts empty. */
func EnsureMigrated(ctx context.Context, database *bun.DB) error {
    if nil == database {
        return exception.NewError("migration: bun database is nil", nil, nil)
    }

    ensureMutex.Lock()
    defer ensureMutex.Unlock()

    if _, alreadyMigrated := migratedDatabaseList[database]; true == alreadyMigrated {
        return nil
    }

    migrator := migrate.NewMigrator(
        database,
        Migrations,
        migrate.WithMarkAppliedOnSuccess(true),
    )

    if initErr := migrator.Init(ctx); nil != initErr {
        return initErr
    }

    locked, lockErr := acquireMigrationLock(ctx, migrator)
    if nil != lockErr {
        return lockErr
    }

    if true == locked {
        if migrateErr := migrateWhileLocked(ctx, migrator); nil != migrateErr {
            return migrateErr
        }
    }

    migratedDatabaseList[database] = struct{}{}

    return nil
}

/* acquireMigrationLock answers whether the lock was taken. A false with a nil error means another process applied the whole set while this one waited, so there is nothing left to run and the lock was never held here. */
func acquireMigrationLock(ctx context.Context, migrator *migrate.Migrator) (bool, error) {
    startedAt := time.Now()

    for {
        lockErr := migrator.Lock(ctx)
        if nil == lockErr {
            return true, nil
        }

        /* the status read can fail while the lock holder is mid-migration; an unreadable status keeps the wait going instead of concluding anything from it */
        pending, statusErr := hasUnappliedMigration(ctx, migrator)
        if nil == statusErr && false == pending {
            return false, nil
        }

        if migrationLockRetryWindow <= time.Since(startedAt) {
            /* the refusal names the resource and the remedy: on its own bun's error states that a lock exists and nothing else — not that the db:unlock command exists to clear a lock a crashed process left behind. The bun error stays the cause, so errors.Is still reaches it. */
            return false, exception.NewError(
                "migration: the migration lock is held; another migration is running, or a crashed one left it behind",
                exceptioncontract.Context{
                    "locksTable":    migrationLocksTable,
                    "unlockCommand": migrationUnlockCommand,
                },
                lockErr,
            )
        }

        select {
        case <-ctx.Done():
            return false, ctx.Err()
        case <-time.After(migrationLockRetryInterval):
        }
    }
}

func hasUnappliedMigration(ctx context.Context, migrator *migrate.Migrator) (bool, error) {
    status, statusErr := migrator.MigrationsWithStatus(ctx)
    if nil != statusErr {
        return true, statusErr
    }

    return 0 < len(status.Unapplied()), nil
}

/* migrateWhileLocked releases the lock whatever the migration answered: a lock row that survives refuses every later migration on every process. The unlock failure becomes the verdict only when the migration itself succeeded; a failed migration keeps its own error. */
func migrateWhileLocked(ctx context.Context, migrator *migrate.Migrator) (migrateErr error) {
    defer func() {
        unlockContext, cancelUnlock := context.WithTimeout(
            context.WithoutCancel(ctx),
            migrationUnlockTimeout,
        )
        defer cancelUnlock()

        unlockErr := migrator.Unlock(unlockContext)
        if nil == migrateErr && nil != unlockErr {
            migrateErr = exception.NewError(
                "migration: the migration lock could not be released",
                exceptioncontract.Context{
                    "locksTable":    migrationLocksTable,
                    "unlockCommand": migrationUnlockCommand,
                },
                unlockErr,
            )
        }
    }()

    _, migrateErr = migrator.Migrate(ctx)

    return migrateErr
}
