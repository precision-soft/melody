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

func TestModule_RegisterCliCommandsHonorsConfiguredOptions(t *testing.T) {
    module := NewModule(ModuleConfig{
        Migrations: migrate.NewMigrations(),
        Options:    Options{CommandPrefix: "orm"},
    })

    commands := module.RegisterCliCommands(nil)

    expectedNames := map[string]bool{
        "orm:init":     false,
        "orm:migrate":  false,
        "orm:rollback": false,
        "orm:status":   false,
        "orm:unlock":   false,
        "orm:create":   false,
    }

    if len(expectedNames) != len(commands) {
        t.Fatalf("expected %d commands, got %d", len(expectedNames), len(commands))
    }

    for _, command := range commands {
        seen, expected := expectedNames[command.Name()]
        if false == expected {
            t.Fatalf("unexpected command %q registered", command.Name())
        }

        if true == seen {
            t.Fatalf("command %q registered twice", command.Name())
        }

        expectedNames[command.Name()] = true
    }
}
