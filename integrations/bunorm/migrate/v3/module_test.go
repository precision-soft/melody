package migrate

import (
    "testing"

    "github.com/uptrace/bun/migrate"
)

func TestModule_NameAndDescription(t *testing.T) {
    module := NewModule(ModuleConfig{})

    if "bunorm.migrate" != module.Name() {
        t.Fatalf("Name() = %q, want %q", module.Name(), "bunorm.migrate")
    }

    if "" == module.Description() {
        t.Fatal("Description() must not be empty")
    }
}

func TestModule_RegisterCliCommandsReturnsNilWithoutMigrations(t *testing.T) {
    if commands := NewModule(ModuleConfig{}).RegisterCliCommands(nil); nil != commands {
        t.Fatalf("expected no commands without migrations, got %d", len(commands))
    }
}

func TestModule_RegisterCliCommandsExposesMigrationCommands(t *testing.T) {
    commands := NewModule(ModuleConfig{Migrations: migrate.NewMigrations()}).RegisterCliCommands(nil)

    if 0 == len(commands) {
        t.Fatal("expected the migration commands to be registered")
    }
}
