package migration

import (
    "context"
    "sync"
    "time"

    melodyexception "github.com/precision-soft/melody/v2/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
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

/* the memoization is keyed by handle alone, because this major carries a single set: the catalogue and the journal share one connection, so one handle can only ever be asked for Migrations */
var (
    ensureMutex          sync.Mutex
    migratedDatabaseList = map[*bun.DB]struct{}{}
)

/* EnsureMigrated applies the Migrations set to the example's database, once per handle and per process; the repository providers call it at first resolution, so a freshly recreated volume needs no operator step. Only a success is recorded. The mutex serializes the providers of one process, and the bun migration lock serializes processes sharing the database. */
func EnsureMigrated(ctx context.Context, database *bun.DB) error {
    if nil == database {
        return melodyexception.NewError("migration: bun database is nil", nil, nil)
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
            /* the refusal names the resource and the remedy, the unlock command that clears a lock a crashed process left, which bun's error does not; the bun error stays the cause, so errors.Is still reaches it */
            return false, melodyexception.NewError(
                "migration: the migration lock is held; another migration is running, or a crashed one left it behind",
                melodyexceptioncontract.Context{
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
            migrateErr = melodyexception.NewError(
                "migration: the migration lock could not be released",
                melodyexceptioncontract.Context{
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

/* Reset brings the database back to the schema this application declares, whatever shape it was left in: the tables the set owns are dropped, the bookkeeping is dropped and recreated with them, the single migration is applied again, and this package's memo for the handle is cleared before the first of them, so a reset that fails half way leaves the handle to be migrated again rather than answered as migrated. It is how a volume in an older shape reaches the present one, the bookkeeping drop removing the rows of steps this schema does not have. No migration lock is taken: the reset drops the very table the lock lives in, so it is an operator command over a development volume, serialized by its caller. */
func Reset(ctx context.Context, database *bun.DB) error {
    if nil == database {
        return melodyexception.NewError("migration: bun database is nil", nil, nil)
    }

    ensureMutex.Lock()
    defer ensureMutex.Unlock()

    delete(migratedDatabaseList, database)

    migrator := migrate.NewMigrator(database, Migrations, migrate.WithMarkAppliedOnSuccess(true))

    if initErr := migrator.Init(ctx); nil != initErr {
        return initErr
    }

    /* the down of the set is run before the bookkeeping goes, rather than through the migrator's rollback: a rollback reverts the last group, so a volume whose rows name steps this schema does not have would keep its tables. The set is one migration, so its down is the whole schema. */
    for _, migrationInstance := range Migrations.Sorted() {
        if nil == migrationInstance.Down {
            continue
        }

        if downErr := migrationInstance.Down(ctx, migrator, &migrationInstance); nil != downErr {
            return downErr
        }
    }

    if resetErr := migrator.Reset(ctx); nil != resetErr {
        return resetErr
    }

    if _, migrateErr := migrator.Migrate(ctx); nil != migrateErr {
        return migrateErr
    }

    return nil
}
