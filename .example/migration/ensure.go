package migration

import (
    "context"
    "sync"
    "time"

    melodyexception "github.com/precision-soft/melody/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

const (
    migrationLocksTable           = "bun_migration_locks"
    migrationUnlockCommand        = "db:unlock"
    journalMigrationUnlockCommand = "db:journal:unlock"
)

/* the unlock must not ride the caller's context: an interrupted resolution cancels it, the delete never reaches the database and the lock row survives, refusing every later migration until someone runs the unlock command */
const migrationUnlockTimeout = 5 * time.Second

/* the window bounds how long a resolution waits for another process's migration before refusing; both are variables so the tests can shorten the wait instead of holding a test binary for half a minute */
var (
    migrationLockRetryWindow   = 30 * time.Second
    migrationLockRetryInterval = 250 * time.Millisecond
)

/* the memoization is keyed by handle and set together: the catalog and the journal database are distinct handles, but nothing in the funnel forbids one handle carrying both sets, and a shared key would let the first set answer for the second */
type migratedSetKey struct {
    database     *bun.DB
    migrationSet *migrate.Migrations
}

var (
    ensureMutex          sync.Mutex
    migratedDatabaseList = map[migratedSetKey]struct{}{}
)

/* EnsureMigrated applies the Migrations set to the catalog database, once per handle and per process. The catalog repository providers call it at first resolution, which is what keeps a freshly recreated volume usable without an operator step: the tables appear when the first request reaches a repository, exactly as they did when each repository owned its own create statement.

   Only a success is recorded; a failed attempt is retried at the next resolution. The mutex serializes the providers of one process, and the bun migration lock serializes processes sharing the database — several example applications race here whenever a volume starts empty. */
func EnsureMigrated(ctx context.Context, database *bun.DB) error {
    return ensureMigratedSet(ctx, database, Migrations, migrationUnlockCommand)
}

/* EnsureJournalMigrated applies the JournalMigrations set to the journal database, through the same funnel EnsureMigrated runs — only the set and the unlock remedy differ, because the journal's lock lives in the journal's own database and is cleared by db:journal:unlock, not db:unlock. */
func EnsureJournalMigrated(ctx context.Context, database *bun.DB) error {
    return ensureMigratedSet(ctx, database, JournalMigrations, journalMigrationUnlockCommand)
}

func ensureMigratedSet(ctx context.Context, database *bun.DB, migrationSet *migrate.Migrations, unlockCommand string) error {
    if nil == database {
        return melodyexception.NewError("migration: bun database is nil", nil, nil)
    }

    ensureMutex.Lock()
    defer ensureMutex.Unlock()

    memoizationKey := migratedSetKey{database: database, migrationSet: migrationSet}
    if _, alreadyMigrated := migratedDatabaseList[memoizationKey]; true == alreadyMigrated {
        return nil
    }

    migrator := migrate.NewMigrator(
        database,
        migrationSet,
        migrate.WithMarkAppliedOnSuccess(true),
    )

    if initErr := migrator.Init(ctx); nil != initErr {
        return initErr
    }

    locked, lockErr := acquireMigrationLock(ctx, migrator, unlockCommand)
    if nil != lockErr {
        return lockErr
    }

    if true == locked {
        if migrateErr := migrateWhileLocked(ctx, migrator, unlockCommand); nil != migrateErr {
            return migrateErr
        }
    }

    migratedDatabaseList[memoizationKey] = struct{}{}

    return nil
}

/* acquireMigrationLock answers whether the lock was taken. A false with a nil error means another process applied the whole set while this one waited, so there is nothing left to run and the lock was never held here. */
func acquireMigrationLock(ctx context.Context, migrator *migrate.Migrator, unlockCommand string) (bool, error) {
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
            return false, melodyexception.NewError(
                "migration: the migration lock is held; another migration is running, or a crashed one left it behind",
                melodyexceptioncontract.Context{
                    "locksTable":    migrationLocksTable,
                    "unlockCommand": unlockCommand,
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
func migrateWhileLocked(ctx context.Context, migrator *migrate.Migrator, unlockCommand string) (migrateErr error) {
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
                    "unlockCommand": unlockCommand,
                },
                unlockErr,
            )
        }
    }()

    _, migrateErr = migrator.Migrate(ctx)

    return migrateErr
}
