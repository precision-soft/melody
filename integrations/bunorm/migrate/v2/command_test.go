package migrate

import (
    "bytes"
    "context"
    "database/sql/driver"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v2"
    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/runtime"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

type fakeDatabaseProvider struct {
    database *bun.DB
}

func (instance *fakeDatabaseProvider) Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.database, nil
}

func newRuntimeWithDatabase(t *testing.T, database *bun.DB) runtimecontract.Runtime {
    t.Helper()

    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "primary", Provider: &fakeDatabaseProvider{database: database}, IsDefault: true},
    )
    if nil != registryErr {
        t.Fatalf("failed to build manager registry: %s", registryErr.Error())
    }

    serviceContainer := container.NewContainer()
    container.MustRegister[*bunorm.ManagerRegistry](
        serviceContainer,
        DefaultOptions().ManagerRegistryServiceId,
        func(resolver containercontract.Resolver) (*bunorm.ManagerRegistry, error) {
            return registry, nil
        },
    )

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

/*
runMigrationCommand drives a migration command exactly like the CLI kernel
does: the command metadata is mounted on a command context, the arguments are
parsed and the command's Run receives the parsed context.
*/
func runMigrationCommand(
    t *testing.T,
    runtimeInstance runtimecontract.Runtime,
    command clicontract.Command,
    arguments ...string,
) (string, error) {
    t.Helper()

    buffer := &bytes.Buffer{}

    var runErr error
    commandContext := &clicontract.CommandContext{
        Name:   command.Name(),
        Flags:  command.Flags(),
        Writer: buffer,
        Action: func(ctx context.Context, innerContext *clicontract.CommandContext) error {
            runErr = command.Run(runtimeInstance, innerContext)

            return nil
        },
    }

    if parseErr := commandContext.Run(context.Background(), append([]string{command.Name()}, arguments...)); nil != parseErr {
        t.Fatalf("failed to parse command arguments: %s", parseErr.Error())
    }

    return buffer.String(), runErr
}

func newSingleMigrationSet(name string, comment string, upCalls *int, downCalls *int) *migrate.Migrations {
    migrations := migrate.NewMigrations()

    migrations.Add(migrate.Migration{
        Name:    name,
        Comment: comment,
        Up: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            if nil != upCalls {
                *upCalls = *upCalls + 1
            }

            return nil
        },
        Down: func(ctx context.Context, migrator *migrate.Migrator, migration *migrate.Migration) error {
            if nil != downCalls {
                *downCalls = *downCalls + 1
            }

            return nil
        },
    })

    return migrations
}

func appliedMigrationRowsHook(appliedNames ...string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.HasPrefix(query, "SELECT") && true == strings.Contains(query, "bun_migrations") {
            rows := make([][]driver.Value, 0, len(appliedNames))
            for index, appliedName := range appliedNames {
                rows = append(rows, []driver.Value{int64(index + 1), appliedName, int64(1), time.Now().UTC()})
            }

            return []string{"id", "name", "group_id", "migrated_at"}, rows, nil
        }

        return []string{}, nil, nil
    }
}

func isLockInsert(query string) bool {
    return strings.HasPrefix(query, "INSERT") && strings.Contains(query, "bun_migration_locks")
}

func isUnlockDelete(query string) bool {
    return strings.HasPrefix(query, "DELETE") && strings.Contains(query, "bun_migration_locks")
}

func isMigrationStatusSelect(query string) bool {
    return strings.HasPrefix(query, "SELECT") && strings.Contains(query, "bun_migrations")
}

func TestInitCommand_CreatesMigrationTables(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewInitCommand(migrate.NewMigrations(), DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "migrations tables initialized") {
        t.Fatalf("missing success message in %q", rendered)
    }

    migrationsTableIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "bun_migrations")
    })
    locksTableIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "CREATE TABLE") && strings.Contains(query, "bun_migration_locks")
    })

    if 0 > migrationsTableIndex {
        t.Fatalf("migrations table was not created: %v", recorder.recordedQueries())
    }

    if 0 > locksTableIndex {
        t.Fatalf("locks table was not created: %v", recorder.recordedQueries())
    }
}

func TestInitCommand_UnknownManagerFails(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewInitCommand(migrate.NewMigrations(), DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color", "--manager", "unknown")
    if nil == runErr {
        t.Fatal("expected an error for an unknown manager name")
    }

    if false == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("error was not printed to the command output: %q", rendered)
    }
}

func TestMigrateCommand_TakesLockBeforeMigratingAndReleasesIt(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color", "--verbose")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 1 != upCalls {
        t.Fatalf("expected the up migration to run once, ran %d times", upCalls)
    }

    lockIndex := recorder.firstIndexMatching(isLockInsert)
    statusIndex := recorder.firstIndexMatching(isMigrationStatusSelect)
    markAppliedIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "INSERT") &&
            strings.Contains(query, "bun_migrations") &&
            false == strings.Contains(query, "bun_migration_locks")
    })
    unlockIndex := recorder.firstIndexMatching(isUnlockDelete)

    if 0 > lockIndex || 0 > statusIndex || 0 > markAppliedIndex || 0 > unlockIndex {
        t.Fatalf("missing expected statements: %v", recorder.recordedQueries())
    }

    if false == (lockIndex < statusIndex) {
        t.Fatalf("migration lock was not taken before computing the pending set: %v", recorder.recordedQueries())
    }

    if false == (markAppliedIndex < unlockIndex) {
        t.Fatalf("migration lock was released before the migration was marked applied: %v", recorder.recordedQueries())
    }

    if len(recorder.recordedQueries())-1 != unlockIndex {
        t.Fatalf("unlock is not the final statement: %v", recorder.recordedQueries())
    }

    if false == strings.Contains(rendered, "| manager | <default>") {
        t.Fatalf("missing manager detail in %q", rendered)
    }

    if false == strings.Contains(rendered, "| applied | 1") {
        t.Fatalf("missing applied count detail in %q", rendered)
    }

    if false == strings.Contains(rendered, "APPLIED MIGRATIONS") {
        t.Fatalf("missing applied migrations block in %q", rendered)
    }

    if false == strings.Contains(rendered, "20240101000000") {
        t.Fatalf("missing applied migration name in %q", rendered)
    }
}

func TestMigrateCommand_LockFailureAbortsWithoutMigratingOrUnlocking(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.execHook = func(query string) error {
        if true == isLockInsert(query) {
            return context.DeadlineExceeded
        }

        return nil
    }

    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatal("expected an error when the migration lock cannot be taken")
    }

    if false == strings.Contains(runErr.Error(), "already locked") {
        t.Fatalf("error = %q, want the bun lock failure", runErr.Error())
    }

    if 0 != upCalls {
        t.Fatalf("migration ran despite the lock failure (%d times)", upCalls)
    }

    if 0 <= recorder.firstIndexMatching(isUnlockDelete) {
        t.Fatalf("a never-acquired lock was released: %v", recorder.recordedQueries())
    }

    if false == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("lock failure was not printed to the command output: %q", rendered)
    }
}

func TestMigrateCommand_NoPendingMigrationsWarns(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    upCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", &upCalls, nil)

    command := NewMigrateCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 0 != upCalls {
        t.Fatalf("an already applied migration ran again (%d times)", upCalls)
    }

    if false == strings.Contains(rendered, "WARNING: no pending migrations") {
        t.Fatalf("missing warning in %q", rendered)
    }

    if 0 > recorder.firstIndexMatching(isUnlockDelete) {
        t.Fatalf("migration lock was not released: %v", recorder.recordedQueries())
    }
}

func TestRollbackCommand_RollsBackLastGroupUnderLock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    downCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", nil, &downCalls)

    command := NewRollbackCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 1 != downCalls {
        t.Fatalf("expected the down migration to run once, ran %d times", downCalls)
    }

    if false == strings.Contains(rendered, "migrations rolled back successfully") {
        t.Fatalf("missing success message in %q", rendered)
    }

    lockIndex := recorder.firstIndexMatching(isLockInsert)
    markUnappliedIndex := recorder.firstIndexMatching(func(query string) bool {
        return strings.HasPrefix(query, "DELETE") &&
            strings.Contains(query, "bun_migrations") &&
            false == strings.Contains(query, "bun_migration_locks")
    })
    unlockIndex := recorder.firstIndexMatching(isUnlockDelete)

    if 0 > lockIndex || 0 > markUnappliedIndex || 0 > unlockIndex {
        t.Fatalf("missing expected statements: %v", recorder.recordedQueries())
    }

    if false == (lockIndex < markUnappliedIndex && markUnappliedIndex < unlockIndex) {
        t.Fatalf("rollback did not run inside the migration lock: %v", recorder.recordedQueries())
    }
}

func TestRollbackCommand_NoMigrationsWarns(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    downCalls := 0
    migrations := newSingleMigrationSet("20240101000000", "create_users", nil, &downCalls)

    command := NewRollbackCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if 0 != downCalls {
        t.Fatalf("a never-applied migration was rolled back (%d times)", downCalls)
    }

    if false == strings.Contains(rendered, "WARNING: no migrations to rollback") {
        t.Fatalf("missing warning in %q", rendered)
    }
}

func TestStatusCommand_ListsAppliedAndPendingMigrations(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = appliedMigrationRowsHook("20240101000000")

    runtimeInstance := newRuntimeWithDatabase(t, database)

    migrations := migrate.NewMigrations()
    migrations.Add(migrate.Migration{Name: "20240101000000", Comment: "create_users"})
    migrations.Add(migrate.Migration{Name: "20240202000000", Comment: "create_orders"})

    command := NewStatusCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "| manager | <default>") {
        t.Fatalf("missing manager detail in %q", rendered)
    }

    if false == strings.Contains(rendered, "| applied | 1") {
        t.Fatalf("missing applied count in %q", rendered)
    }

    if false == strings.Contains(rendered, "| status  | 1 pending") {
        t.Fatalf("missing pending count in %q", rendered)
    }

    appliedBlockIndex := strings.Index(rendered, "APPLIED")
    pendingBlockIndex := strings.Index(rendered, "PENDING")

    if 0 > appliedBlockIndex || 0 > pendingBlockIndex {
        t.Fatalf("missing APPLIED/PENDING blocks in %q", rendered)
    }

    if false == strings.Contains(rendered[appliedBlockIndex:pendingBlockIndex], "20240101000000") {
        t.Fatalf("applied migration not listed in the APPLIED block: %q", rendered)
    }

    if false == strings.Contains(rendered[pendingBlockIndex:], "20240202000000") {
        t.Fatalf("pending migration not listed in the PENDING block: %q", rendered)
    }
}

func TestStatusCommand_NoMigrationsWarns(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewStatusCommand(migrate.NewMigrations(), DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "WARNING: no migrations found") {
        t.Fatalf("missing warning in %q", rendered)
    }
}

func TestUnlockCommand_DeletesMigrationLock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewUnlockCommand(migrate.NewMigrations(), DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color", "--verbose")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "migrations table unlocked") {
        t.Fatalf("missing success message in %q", rendered)
    }

    if false == strings.Contains(rendered, "| status  | unlocked") {
        t.Fatalf("missing verbose status detail in %q", rendered)
    }

    if 0 > recorder.firstIndexMatching(isUnlockDelete) {
        t.Fatalf("no delete on the locks table was issued: %v", recorder.recordedQueries())
    }
}

func TestCreateCommand_WritesMigrationFileTemplate(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    directory := t.TempDir()
    migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(directory))

    command := NewCreateGoCommand(migrations, DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color", "create_users")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    if false == strings.Contains(rendered, "migration file created") {
        t.Fatalf("missing success message in %q", rendered)
    }

    if false == strings.Contains(rendered, "FILES") {
        t.Fatalf("missing FILES block in %q", rendered)
    }

    entries, readErr := os.ReadDir(directory)
    if nil != readErr {
        t.Fatalf("failed to read migrations directory: %s", readErr.Error())
    }

    if 1 != len(entries) {
        t.Fatalf("expected exactly one generated file, got %d", len(entries))
    }

    fileName := entries[0].Name()
    if false == regexp.MustCompile(`^\d{14}_create_users\.go$`).MatchString(fileName) {
        t.Fatalf("generated file name %q does not match <timestamp>_<name>.go", fileName)
    }

    if false == strings.Contains(rendered, fileName) {
        t.Fatalf("generated file %q not listed in the FILES block: %q", fileName, rendered)
    }

    content, contentErr := os.ReadFile(filepath.Join(directory, fileName))
    if nil != contentErr {
        t.Fatalf("failed to read generated migration: %s", contentErr.Error())
    }

    skeleton := string(content)
    for _, expected := range []string{"package migrations", "func init()", "Migrations.MustRegister", "up migration", "down migration"} {
        if false == strings.Contains(skeleton, expected) {
            t.Fatalf("generated migration is missing %q:\n%s", expected, skeleton)
        }
    }
}

func TestCreateCommand_MissingNameFails(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    command := NewCreateGoCommand(migrate.NewMigrations(), DefaultOptions())

    rendered, runErr := runMigrationCommand(t, runtimeInstance, command, "--no-color")
    if nil == runErr {
        t.Fatal("expected an error when the migration name is missing")
    }

    if false == strings.Contains(runErr.Error(), "migration name is required") {
        t.Fatalf("error = %q, want the missing-name message", runErr.Error())
    }

    if false == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("error was not printed to the command output: %q", rendered)
    }
}
