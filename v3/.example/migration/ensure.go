package migration

import (
    "context"
    "errors"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
    "github.com/uptrace/bun/driver/pgdriver"
)

const (
    migrationLocksTable           = "bun_migration_locks"
    migrationUnlockCommand        = "db:unlock"
    archiveMigrationUnlockCommand = "db:archive:unlock"
)

const migrationUnlockTimeout = 5 * time.Second

var (
    migrationLockRetryWindow   = 30 * time.Second
    migrationLockRetryInterval = 250 * time.Millisecond
)

type migratedSetKey struct {
    database     *bun.DB
    migrationSet *migrate.Migrations
}

var (
    ensureMutex          sync.Mutex
    migratedDatabaseList = map[migratedSetKey]struct{}{}
    refusedDatabaseList  = map[migratedSetKey]refusedMigrationAttempt{}
)

type refusedMigrationAttempt struct {
    refusal   error
    refusedAt time.Time
}

/* EnsureMigrated applies the Migrations set to the example's database, once per handle and per process. The repository constructors the generated wiring fills call it at first resolution, and the two-factor build step calls it before it publishes its store, which is what keeps a freshly recreated volume usable without an operator step: the tables appear when the first request reaches a repository, exactly as they did when each repository owned its own create statement.

   A success is recorded for good. A refusal that spent the retry window waiting for another process is recorded for as long as that window, so the resolutions arriving inside it are answered with it instead of each waiting again; every other failure is recorded not at all and is retried at the next resolution. The mutex serializes the callers of one process, and the bun migration lock serializes processes sharing the database — several instances of this example race here whenever a volume starts empty. */
func EnsureMigrated(ctx context.Context, database *bun.DB) error {
    return ensureMigratedSet(ctx, database, Migrations, catalogMigrationSetName, migrationUnlockCommand)
}

/* EnsureArchiveMigrated applies the ArchiveMigrations set to the archive database, through the same funnel EnsureMigrated runs — only the set and the unlock remedy differ, because the archive's lock lives in the archive's own database and is cleared by db:archive:unlock, not db:unlock. */
func EnsureArchiveMigrated(ctx context.Context, database *bun.DB) error {
    return ensureMigratedSet(ctx, database, ArchiveMigrations, archiveMigrationSetName, archiveMigrationUnlockCommand)
}

const (
    catalogMigrationSetName = "catalogue"
    archiveMigrationSetName = "archive"
)

func ensureMigratedSet(ctx context.Context, database *bun.DB, migrationSet *migrate.Migrations, setName string, unlockCommand string) error {
    if nil == database {
        return exception.NewError("migration: bun database is nil", nil, nil)
    }

    ensureMutex.Lock()
    defer ensureMutex.Unlock()

    memoizationKey := migratedSetKey{database: database, migrationSet: migrationSet}

    if _, alreadyMigrated := migratedDatabaseList[memoizationKey]; true == alreadyMigrated {
        return nil
    }

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

    if initErr := initializeMigrationBookkeeping(ctx, migrator); nil != initErr {
        return migrationStepFailure(setName, "initialising the bookkeeping", unlockCommand, initErr)
    }

    lockStartedAt := time.Now()

    locked, lockErr := acquireMigrationLock(ctx, migrator, unlockCommand)
    if nil != lockErr {

        if migrationLockRetryWindow <= time.Since(lockStartedAt) && nil == ctx.Err() {
            refusedDatabaseList[memoizationKey] = refusedMigrationAttempt{refusal: lockErr, refusedAt: time.Now()}
        }

        return lockErr
    }

    if true == locked {
        if migrateErr := migrateWhileLocked(ctx, migrator, unlockCommand); nil != migrateErr {
            return migrationStepFailure(setName, "applying the set", unlockCommand, migrateErr)
        }
    }

    migratedDatabaseList[memoizationKey] = struct{}{}

    return nil
}

func migrationStepFailure(setName string, step string, unlockCommand string, cause error) error {
    var ownException *exception.Error
    if true == errors.As(cause, &ownException) {
        return cause
    }

    return exception.NewError(
        "migration: "+step+" did not complete on the "+setName+" set",
        exceptioncontract.Context{
            "set":           setName,
            "step":          step,
            "unlockCommand": unlockCommand,
        },
        cause,
    )
}

func acquireMigrationLock(ctx context.Context, migrator *migrate.Migrator, unlockCommand string) (bool, error) {
    startedAt := time.Now()

    for {
        lockErr := migrator.Lock(ctx)
        if nil == lockErr {
            return true, nil
        }

        pending, statusErr := hasUnappliedMigration(ctx, migrator)
        if nil == statusErr && false == pending {
            return false, nil
        }

        if migrationLockRetryWindow <= time.Since(startedAt) {

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

/* Reset drops and recreates the declared schema and migration bookkeeping. It invalidates process memoization before attempting changes. The caller must serialize this destructive development operation; no migration lock can span dropping its own lock table. */
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

    memoizationKey := migratedSetKey{database: database, migrationSet: migrationSet}
    delete(migratedDatabaseList, memoizationKey)
    delete(refusedDatabaseList, memoizationKey)

    migrator := migrate.NewMigrator(database, migrationSet, migrate.WithMarkAppliedOnSuccess(true))

    if initErr := initializeMigrationBookkeeping(ctx, migrator); nil != initErr {
        return initErr
    }

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

    return nil
}

func initializeMigrationBookkeeping(ctx context.Context, migrator *migrate.Migrator) error {
    for attempt := 0; ; attempt++ {
        initErr := migrator.Init(ctx)
        if nil == initErr || 3 <= attempt {
            return initErr
        }
        var databaseErr pgdriver.Error
        if false == errors.As(initErr, &databaseErr) {
            return initErr
        }
        constraint := databaseErr.Field('n')
        code := databaseErr.Field('C')
        if "42P07" != code && ("23505" != code || ("pg_class_relname_nsp_index" != constraint && "pg_type_typname_nsp_index" != constraint)) {
            return initErr
        }
        if nil != ctx.Err() {
            return ctx.Err()
        }
    }
}
