package migrate

import (
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "testing"

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

    /* the command no longer pre-prints the failure it returns: the cli runner's [error] line and the full log record already report it */
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
