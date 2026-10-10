package migrate

import (
    "strings"
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

/* the empty configuration is refused at registration by name: registering the commands is this module's only purpose, so an empty config is a wiring mistake with no legal reading — accepted, the operator discovered it as "unknown command" at the first db:migrate. */
func TestModule_RegisterCliCommandsRefusesTheEmptyConfigurationByName(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatal("expected the empty configuration to be refused at registration")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError || false == strings.Contains(recoveredErr.Error(), "requires migrations or contexts") {
            t.Fatalf("expected the refusal to name the rule, got %v", recoveredValue)
        }
    }()

    NewModule(ModuleConfig{}).RegisterCliCommands(nil)
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

func TestModule_RegisterCliCommandsComposesLegacyAndContexts(t *testing.T) {
    module := NewModule(ModuleConfig{
        Migrations: migrate.NewMigrations(),
        Contexts: []ContextConfig{
            {Name: "payment", Migrations: migrate.NewMigrations()},
        },
    })

    commands := module.RegisterCliCommands(nil)

    names := make(map[string]bool, len(commands))
    for _, command := range commands {
        names[command.Name()] = true
    }

    if false == names["db:migrate"] {
        t.Fatalf("expected the legacy unprefixed family to survive, got: %v", names)
    }

    if false == names["db:payment:migrate"] {
        t.Fatalf("expected the per-context family, got: %v", names)
    }

    if 12 != len(commands) {
        t.Fatalf("expected both families (12 commands), got %d", len(commands))
    }
}

func TestModule_RegisterCliCommandsContextsOnly(t *testing.T) {
    module := NewModule(ModuleConfig{
        Contexts: []ContextConfig{
            {Name: "platform", Migrations: migrate.NewMigrations()},
        },
    })

    commands := module.RegisterCliCommands(nil)

    if 6 != len(commands) {
        t.Fatalf("expected one context family, got %d commands", len(commands))
    }

    for _, command := range commands {
        if false == strings.HasPrefix(command.Name(), "db:platform:") {
            t.Fatalf("expected the context prefix, got: %s", command.Name())
        }
    }
}
