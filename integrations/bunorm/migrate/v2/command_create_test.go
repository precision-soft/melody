package migrate

import (
    "encoding/json"
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
