package migrate

import (
    "context"
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/migrate"
)

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

    /* the command does not pre-print the failure it returns: the cli runner's [error] line and the full log record report it */
    if true == strings.Contains(rendered, "ERROR:") {
        t.Fatalf("the returned failure must not be pre-printed by the command, got: %q", rendered)
    }
}

/* the machine document names the argument the command ran on: built without the arguments it answered an empty list for every command, db:create included, whose one argument is the migration the document reports on */
func TestCreateCommand_TheMachineDocumentCarriesTheArguments(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)
    migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(t.TempDir()))

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--format=json", "create_users")
    if nil != runErr {
        t.Fatalf("unexpected error: %s", runErr.Error())
    }

    var document struct {
        Meta struct {
            Arguments []string `json:"arguments"`
        } `json:"meta"`
    }
    if decodeErr := json.Unmarshal([]byte(rendered), &document); nil != decodeErr {
        t.Fatalf("the output is not one json document: %v; got %q", decodeErr, rendered)
    }

    if 1 != len(document.Meta.Arguments) || "create_users" != document.Meta.Arguments[0] {
        t.Fatalf("meta.arguments = %v, want [create_users]", document.Meta.Arguments)
    }
}

/* the name is held to the generator's grammar before it reaches bun or the database: a parent reference or a separator in it names a path outside the migrations directory, and the confinement must be this command's rather than the pinned dependency's */
func TestCreateCommand_RefusesANameOutsideTheGrammarBeforeTouchingAnything(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)
    directory := t.TempDir()
    migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(directory))

    _, runErr := runMigrationCommand(t, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--no-color", "../escape")
    if nil == runErr {
        t.Fatal("expected the name to be refused")
    }

    if false == strings.Contains(runErr.Error(), "migration name must match") {
        t.Fatalf("expected the refusal to name the grammar, got %q", runErr.Error())
    }

    if 0 != len(recorder.recordedQueries()) {
        t.Fatalf("the refusal reached the database: %v", recorder.recordedQueries())
    }

    entries, _ := os.ReadDir(directory)
    if 0 != len(entries) {
        t.Fatalf("the refusal reached the filesystem: %d entries", len(entries))
    }
}

func TestCreateCommand_FormatMigrationFilesSurvivesANilFile(t *testing.T) {
    command := &CreateCommand{}

    lines := command.formatMigrationFiles(nil)
    if 1 != len(lines) || "<unknown>" != lines[0] {
        t.Fatalf("lines = %v, want [<unknown>]", lines)
    }
}

/* a directory fsync that fails AFTER the rename leaves a whole file in place: the run succeeds with a warning naming what could not be guaranteed, because a failure verdict sent the operator to run the command again, which created a second migration under a new timestamp beside a perfectly good first one */
func TestCreateCommand_ADirectorySyncFailureAfterTheRenameIsAWarningNotAFailure(t *testing.T) {
    previous := syncDirectoryAfterRename
    t.Cleanup(func() { syncDirectoryAfterRename = previous })
    syncDirectoryAfterRename = func(path string) error {
        return errors.New("fsync refused by the filesystem")
    }

    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)
    directory := t.TempDir()
    migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(directory))

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--no-color", "create_users")
    if nil != runErr {
        t.Fatalf("expected the run to succeed with a warning, got %v", runErr)
    }

    if false == strings.Contains(rendered, "WARNING: migration file ") || false == strings.Contains(rendered, "could not be fsynced and may not survive a crash: the directory entry could not be fsynced after the rename: fsync refused by the filesystem") {
        t.Fatalf("expected the warning to name the file and the cause, got %q", rendered)
    }

    if false == strings.Contains(rendered, "migration file created") {
        t.Fatalf("expected the success line beside the warning, got %q", rendered)
    }

    entries, _ := os.ReadDir(directory)
    if 1 != len(entries) {
        t.Fatalf("expected exactly one migration file, got %d", len(entries))
    }
}

/* refusingCountingProvider refuses every open and counts them — the provider of a host that is down, where an open costs the whole retry budget */
type refusingCountingProvider struct {
    opens int
}

func (instance *refusingCountingProvider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    instance.opens++

    return nil, errors.New("database connection failed")
}

/* newRuntimeWithProvider wires a registry whose one manager opens through the given provider, the way newRuntimeWithDatabase does for a database already opened */
func newRuntimeWithProvider(t *testing.T, provider bunorm.Provider) runtimecontract.Runtime {
    t.Helper()

    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "primary", Provider: provider, IsDefault: true},
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

/* the file is written from the migrations collection alone: bun's generator never touches the database, so none is opened, and a host that is down costs no dial */
func TestCreateCommand_WritesTheFileWithoutOpeningTheDatabase(t *testing.T) {
    provider := &refusingCountingProvider{}
    runtimeInstance := newRuntimeWithProvider(t, provider)

    directory := t.TempDir()
    migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(directory))

    rendered, runErr := runMigrationCommand(t, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--no-color", "create_users")
    if nil != runErr {
        t.Fatalf("expected the file written without a database, got: %v", runErr)
    }

    if 0 != provider.opens {
        t.Fatalf("expected no database open for a file the generator writes offline, got %d", provider.opens)
    }

    entries, readErr := os.ReadDir(directory)
    if nil != readErr || 1 != len(entries) {
        t.Fatalf("expected exactly one generated file, got %d (%v)", len(entries), readErr)
    }

    if false == strings.Contains(rendered, "migration file created") {
        t.Fatalf("missing success message in %q", rendered)
    }
}

/* the usage in the missing-name refusal names the command's own family: the archive family of an application printed the other family's usage */
func TestCreateCommand_MissingNameNamesTheCommandsOwnFamily(t *testing.T) {
    database, _ := newFakeBunDatabase()
    runtimeInstance := newRuntimeWithDatabase(t, database)

    options := DefaultOptions()
    options.CommandPrefix = "db:archive"

    _, runErr := runMigrationCommand(t, runtimeInstance, NewCreateGoCommand(migrate.NewMigrations(), options), "--no-color")
    if nil == runErr || false == strings.Contains(runErr.Error(), "usage: db:archive:create <name>") {
        t.Fatalf("expected the usage to name db:archive:create, got %v", runErr)
    }
}

/* the open this command stopped paying was also the only thing that validated --manager: a misspelt name wrote the file, labelled it with the misspelling, and was discovered at the first db:migrate. The registry answers the name without opening, through the door that reads what it was built with. */
func TestCreateCommand_RefusesAManagerTheRegistryDoesNotKnowWithoutOpeningIt(t *testing.T) {
    provider := &refusingCountingProvider{}
    runtimeInstance := newRuntimeWithProvider(t, provider)

    directory := t.TempDir()
    migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(directory))

    _, runErr := runMigrationCommand(t, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--no-color", "--manager=nope", "create_users")
    if false == errors.Is(runErr, bunorm.ErrProviderDefinitionNotFound) {
        t.Fatalf("expected the unknown manager refused by the registry's own class, got: %v", runErr)
    }

    if 0 != provider.opens {
        t.Fatalf("expected the refusal without a database open, got %d", provider.opens)
    }

    entries, readErr := os.ReadDir(directory)
    if nil != readErr || 0 != len(entries) {
        t.Fatalf("expected no file written for a refused manager, got %d (%v)", len(entries), readErr)
    }

    _, runErr = runMigrationCommand(t, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--no-color", "--manager=primary", "create_users")
    if nil != runErr {
        t.Fatalf("expected the registered manager admitted, got: %v", runErr)
    }

    entries, _ = os.ReadDir(directory)
    if 1 != len(entries) || 0 != provider.opens {
        t.Fatalf("expected the file written without an open for the registered manager, got %d files and %d opens", len(entries), provider.opens)
    }
}

/* journalRecorder is the application's logger as the create command resolves it, keeping the warnings it receives */
type journalRecorder struct {
    loggingcontract.Logger
    warnings []string
    paths    []string
}

func (instance *journalRecorder) Warning(message string, context loggingcontract.Context) {
    instance.warnings = append(instance.warnings, message)

    path, _ := context["path"].(string)
    instance.paths = append(instance.paths, path)
}

/* the result of db:create is the file it writes, not its report: a report the writer lost goes to the journal and the run answers nil, since a failed run would invite a re-run that creates a second migration beside the first. db:migrate keeps the refusal, its report being its result. */
func TestCreateCommand_ALostReportWriteIsAWarningInTheJournalNotAFailure(t *testing.T) {
    journal := &journalRecorder{Logger: logging.NewNopLogger()}

    registry, registryErr := bunorm.NewManagerRegistry(
        logging.NewNopLogger(),
        bunorm.ProviderDefinition{Name: "primary", Provider: &refusingCountingProvider{}, IsDefault: true},
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
    container.MustRegister[loggingcontract.Logger](
        serviceContainer,
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return journal, nil
        },
    )
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    directory := t.TempDir()
    migrations := migrate.NewMigrations(migrate.WithMigrationsDirectory(directory))

    banner := &failingOnWriter{marker: "migration file created"}
    runErr := runMigrationCommandTo(t, banner, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--no-color", "create_users")
    if nil != runErr {
        t.Fatalf("expected the run to succeed with its file in place, got: %v", runErr)
    }

    entries, readErr := os.ReadDir(directory)
    if nil != readErr || 1 != len(entries) {
        t.Fatalf("expected the one generated file in place, got %d (%v)", len(entries), readErr)
    }

    if 1 != len(journal.warnings) || "the report could not be written in full; the command's result is in place" != journal.warnings[0] {
        t.Fatalf("expected the lost report recorded once in the journal, got %v", journal.warnings)
    }

    /* the warning is the one place the operator can read the file's path from when the document on stdout was cut */
    if filepath.Join(directory, entries[0].Name()) != journal.paths[0] {
        t.Fatalf("expected the warning to name the file the run wrote, got %q", journal.paths[0])
    }

    document := &failingOnWriter{marker: "\"meta\""}
    runErr = runMigrationCommandTo(t, document, runtimeInstance, NewCreateGoCommand(migrations, DefaultOptions()), "--format=json", "create_orders")
    if nil != runErr || 2 != len(journal.warnings) {
        t.Fatalf("expected the lost json document journaled the same way, got err=%v warnings=%v", runErr, journal.warnings)
    }
}
