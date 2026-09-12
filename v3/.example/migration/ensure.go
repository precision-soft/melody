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
    migrationLocksTable           = "bun_migration_locks"
    migrationUnlockCommand        = "db:unlock"
    archiveMigrationUnlockCommand = "db:archive:unlock"
)

/* the unlock must not ride the caller's context: an interrupted resolution cancels it, the delete never reaches the database and the lock row survives, refusing every later migration until someone runs the unlock command */
const migrationUnlockTimeout = 5 * time.Second

/* the window bounds how long a resolution waits for another process's migration before refusing; both are variables so the tests can shorten the wait instead of holding a test binary for half a minute */
var (
    migrationLockRetryWindow   = 30 * time.Second
    migrationLockRetryInterval = 250 * time.Millisecond
)

/* the memoization is keyed by handle AND set together. This major carries two of them — the catalogue schema on mysql and the reading archive on postgres — so a key of the handle alone would let whichever set ran first answer for the other; and nothing in the funnel forbids one handle carrying both, so the pair is the honest key even where the two handles happen to differ. */
type migratedSetKey struct {
    database     *bun.DB
    migrationSet *migrate.Migrations
}

var (
    ensureMutex          sync.Mutex
    migratedDatabaseList = map[migratedSetKey]struct{}{}
    refusedDatabaseList  = map[migratedSetKey]refusedMigrationAttempt{}
)

/* refusedMigrationAttempt is what an attempt that waited out the whole window leaves behind, so the ones
   after it are told what it learned instead of waiting for it again. */
type refusedMigrationAttempt struct {
    refusal   error
    refusedAt time.Time
}

/* EnsureMigrated applies the Migrations set to the example's database, once per handle and per process. The repository constructors the generated wiring fills call it at first resolution, and the two-factor build step calls it before it publishes its store, which is what keeps a freshly recreated volume usable without an operator step: the tables appear when the first request reaches a repository, exactly as they did when each repository owned its own create statement.

   A success is recorded for good. A refusal that spent the retry window waiting for another process is recorded for as long as that window, so the resolutions arriving inside it are answered with it instead of each waiting again; every other failure is recorded not at all and is retried at the next resolution. The mutex serializes the callers of one process, and the bun migration lock serializes processes sharing the database — several instances of this example race here whenever a volume starts empty. */
func EnsureMigrated(ctx context.Context, database *bun.DB) error {
    return ensureMigratedSet(ctx, database, Migrations, migrationUnlockCommand)
}

/* EnsureArchiveMigrated applies the ArchiveMigrations set to the archive database, through the same funnel EnsureMigrated runs — only the set and the unlock remedy differ, because the archive's lock lives in the archive's own database and is cleared by db:archive:unlock, not db:unlock. */
func EnsureArchiveMigrated(ctx context.Context, database *bun.DB) error {
    return ensureMigratedSet(ctx, database, ArchiveMigrations, archiveMigrationUnlockCommand)
}

func ensureMigratedSet(ctx context.Context, database *bun.DB, migrationSet *migrate.Migrations, unlockCommand string) error {
    if nil == database {
        return exception.NewError("migration: bun database is nil", nil, nil)
    }

    ensureMutex.Lock()
    defer ensureMutex.Unlock()

    memoizationKey := migratedSetKey{database: database, migrationSet: migrationSet}

    if _, alreadyMigrated := migratedDatabaseList[memoizationKey]; true == alreadyMigrated {
        return nil
    }

    /* an attempt that was refused is remembered for as long as the wait that produced it, and the callers
       that arrive inside that span are answered with it rather than made to repeat it.

       Without this the cost of one lock nobody releases is paid per resolution and serially, because the
       whole protocol runs under this mutex: measured on a window shortened to 300ms, three concurrent
       resolutions took 1.5s — five windows, not one — and at the real window that is two and a half minutes
       of requests holding on a refusal already known, each of them answering 500 afterwards. The refusal is
       the same value, so nothing about what a caller is told changes; only how long it takes to be told. */
    if refused, wasRefused := refusedDatabaseList[memoizationKey]; true == wasRefused {
        if migrationLockRetryWindow > time.Since(refused.refusedAt) {
            return refused.refusal
        }

        delete(refusedDatabaseList, memoizationKey)
    }

    migrator := migrate.NewMigrator(
        database,
        migrationSet,
        migrate.WithMarkAppliedOnSuccess(true),
    )

    if initErr := migrator.Init(ctx); nil != initErr {
        return initErr
    }

    lockStartedAt := time.Now()

    locked, lockErr := acquireMigrationLock(ctx, migrator, unlockCommand)
    if nil != lockErr {
        /* only a refusal that COST the wait is remembered, and that is the whole of the harm: a refusal
           that came back at once — a failed init, a lock that could not be released after the set was
           applied — costs nothing to reach again, so the next resolution reaches it again and heals as soon
           as the database does. A caller that walked away is not evidence about the database either. */
        if migrationLockRetryWindow <= time.Since(lockStartedAt) && nil == ctx.Err() {
            refusedDatabaseList[memoizationKey] = refusedMigrationAttempt{refusal: lockErr, refusedAt: time.Now()}
        }

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
            /* the refusal names the resource and the remedy: on its own bun's error states that a lock exists and nothing else — not that an unlock command exists to clear a lock a crashed process left behind. The remedy is the one for THIS set: each set's lock lives in its own database, so an operator told to run db:unlock over a held archive lock would clear the wrong one and find the refusal unchanged. The bun error stays the cause, so errors.Is still reaches it. */
            return false, exception.NewError(
                "migration: the migration lock is held; another migration is running, or a crashed one left it behind",
                exceptioncontract.Context{
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
            migrateErr = exception.NewError(
                "migration: the migration lock could not be released",
                exceptioncontract.Context{
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

/* Reset brings the database back to the schema this application declares, whatever shape it was left in:
   the tables the set owns are dropped, the bookkeeping is dropped and recreated with them, the single
   migration is applied again, and the memo this package keeps for the handle is cleared so a resolution
   later in the same process does not answer from a state that no longer exists.

   It is the answer this example gives to a volume provisioned by an older build. An example is not a
   project with a past: it has one state, the present one, so it carries no migration that repairs its
   history — the reset is where a database in an older shape is brought to the present one, and dropping
   the bookkeeping is the half that matters there, because a volume migrated by an older set still holds
   the rows of steps this schema no longer has.

   No migration lock is taken, and that is not an omission: the reset drops the very table the lock lives
   in, so no lock could span it. It is an operator command over a development volume, run deliberately,
   and the caller is what serializes it. */
func Reset(ctx context.Context, database *bun.DB) error {
    return resetSet(ctx, database, Migrations)
}

/* ResetArchive is the same door for the archive set on its own database: the reset command drives both, and a set whose database this environment never wired is left alone rather than failing over a connection nobody asked for. */
func ResetArchive(ctx context.Context, database *bun.DB) error {
    return resetSet(ctx, database, ArchiveMigrations)
}

func resetSet(ctx context.Context, database *bun.DB, migrationSet *migrate.Migrations) error {
    if nil == database {
        return exception.NewError("migration: bun database is nil", nil, nil)
    }

    ensureMutex.Lock()
    defer ensureMutex.Unlock()

    migrator := migrate.NewMigrator(database, migrationSet, migrate.WithMarkAppliedOnSuccess(true))

    if initErr := migrator.Init(ctx); nil != initErr {
        return initErr
    }

    /* the down of the set is run before the bookkeeping goes, rather than through the migrator's own
       rollback: a rollback reverts the last GROUP, so a volume whose rows name steps this schema no
       longer has would leave its tables standing. The set is one migration, so its down is the whole
       schema. */
    for _, migrationInstance := range migrationSet.Sorted() {
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

    memoizationKey := migratedSetKey{database: database, migrationSet: migrationSet}
    delete(migratedDatabaseList, memoizationKey)
    delete(refusedDatabaseList, memoizationKey)

    return nil
}
