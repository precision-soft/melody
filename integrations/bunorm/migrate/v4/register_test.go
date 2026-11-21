package migrate

import (
    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "strings"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/uptrace/bun/migrate"
)

func flagsContainName(flags []clicontract.Flag, name string) bool {
    for _, flag := range flags {
        if name == flag.Names()[0] {
            return true
        }
    }

    return false
}

func commandNames(commands []clicontract.Command) []string {
    names := make([]string, 0, len(commands))
    for _, command := range commands {
        names = append(names, command.Name())
    }

    return names
}

func TestRegisterCommands_NilMigrationsIsRefused(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected nil migrations to be refused")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected an error panic, got %#v", recovered)
        }

        if false == strings.Contains(recoveredErr.Error(), "migrations are nil") {
            t.Fatalf("expected the refusal to name the nil migrations, got %q", recoveredErr.Error())
        }
    }()

    RegisterCommands(nil, Options{})
}

/* the module gates its own optional set before calling the registrar, so a binary that registers only migration contexts never reaches the registrar's nil-migrations refusal — the context-only shape is the one legal reading of absent Migrations. */
func TestModule_WithContextsOnlyDoesNotReachTheRegistrarsRefusal(t *testing.T) {
    module := NewModule(ModuleConfig{
        Migrations: nil,
        Options:    resolvedDefaultOptions,
        Contexts: []ContextConfig{
            {Name: "reporting", Migrations: migrate.NewMigrations()},
        },
    })

    commands := module.RegisterCliCommands(nil)
    if 0 == len(commands) {
        t.Fatal("expected the context commands to be registered")
    }
}

func TestRegisterCommands_EmptyOptionsResolveToDefaults(t *testing.T) {
    commands := RegisterCommands(migrate.NewMigrations(), Options{})

    expectedNames := []string{"db:init", "db:migrate", "db:rollback", "db:status", "db:unlock", "db:create"}
    names := commandNames(commands)

    if len(expectedNames) != len(names) {
        t.Fatalf("expected %d commands, got %d (%v)", len(expectedNames), len(names), names)
    }

    for index, expectedName := range expectedNames {
        if expectedName != names[index] {
            t.Fatalf("command at index %d = %q, want %q (all: %v)", index, names[index], expectedName, names)
        }
    }

    for _, command := range commands {
        if false == flagsContainName(command.Flags(), "manager") {
            t.Fatalf("command %q is missing the default manager flag", command.Name())
        }
    }

    if migrateCommand := commands[1].(*MigrateCommand); DefaultManagerRegistryServiceId != migrateCommand.base.options.ManagerRegistryServiceId {
        t.Fatalf("expected the registry service id to default to %q, got %q", DefaultManagerRegistryServiceId, migrateCommand.base.options.ManagerRegistryServiceId)
    }
}

func TestRegisterCommands_CustomPrefixAndManagerFlagName(t *testing.T) {
    options := Options{
        ManagerRegistryServiceId: "service.custom.registry",
        ManagerFlagName:          "connection",
        CommandPrefix:            "database",
    }

    commands := RegisterCommands(migrate.NewMigrations(), options)

    if 6 != len(commands) {
        t.Fatalf("expected 6 commands, got %d", len(commands))
    }

    for _, command := range commands {
        if false == strings.HasPrefix(command.Name(), "database:") {
            t.Fatalf("command %q is not prefixed with the custom prefix", command.Name())
        }

        if false == flagsContainName(command.Flags(), "connection") {
            t.Fatalf("command %q is missing the custom manager flag", command.Name())
        }

        if true == flagsContainName(command.Flags(), "manager") {
            t.Fatalf("command %q still exposes the default manager flag", command.Name())
        }
    }
}

func TestRegisterCommands_MetadataIsComplete(t *testing.T) {
    commands := RegisterCommands(migrate.NewMigrations(), Options{})

    for _, command := range commands {
        if "" == command.Name() {
            t.Fatal("command with an empty name registered")
        }

        if false == strings.HasPrefix(command.Name(), "db:") {
            t.Fatalf("command %q is not prefixed with the default prefix", command.Name())
        }

        if "" == command.Description() {
            t.Fatalf("command %q has an empty description", command.Name())
        }

        if 0 == len(command.Flags()) {
            t.Fatalf("command %q has no flags", command.Name())
        }

        for _, standardFlagName := range []string{"no-color", "verbose", "format"} {
            if false == flagsContainName(command.Flags(), standardFlagName) {
                t.Fatalf("command %q is missing the standard %q flag", command.Name(), standardFlagName)
            }
        }
    }
}

func TestRegisterContextCommands_BuildsOneFamilyPerContext(t *testing.T) {
    commands := RegisterContextCommands(
        []ContextConfig{
            {Name: "platform", Migrations: migrate.NewMigrations()},
            {Name: "payment", Migrations: migrate.NewMigrations()},
        },
        Options{},
    )

    expectedNames := map[string]bool{
        "db:platform:init":     false,
        "db:platform:migrate":  false,
        "db:platform:rollback": false,
        "db:platform:status":   false,
        "db:platform:unlock":   false,
        "db:platform:create":   false,
        "db:payment:init":      false,
        "db:payment:migrate":   false,
        "db:payment:rollback":  false,
        "db:payment:status":    false,
        "db:payment:unlock":    false,
        "db:payment:create":    false,
    }

    if len(expectedNames) != len(commands) {
        t.Fatalf("expected %d commands, got %d", len(expectedNames), len(commands))
    }

    for _, command := range commands {
        seen, expected := expectedNames[command.Name()]
        if false == expected {
            t.Fatalf("unexpected command name: %s", command.Name())
        }

        if true == seen {
            t.Fatalf("duplicate command name: %s", command.Name())
        }

        expectedNames[command.Name()] = true
    }
}

func TestRegisterContextCommands_EmptyContextsBuildNothing(t *testing.T) {
    if commands := RegisterContextCommands(nil, Options{}); 0 != len(commands) {
        t.Fatalf("expected no commands, got %d", len(commands))
    }
}

func TestRegisterCommands_RefusesAnUnlockTimeoutOutsideItsRuleNamingTheField(t *testing.T) {
    for name, unlockTimeout := range map[string]bound.Duration{
        "zero":              bound.Of(time.Duration(0)),
        "below the minimum": bound.Of(time.Microsecond),
        "unlimited":         bound.UnlimitedDuration(),
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatal("expected the unlock timeout refused")
                }

                if "Options.UnlockTimeout" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
                    t.Fatalf("expected the refusal to name Options.UnlockTimeout, got %v", recoveredErr)
                }
            }()

            RegisterCommands(migrate.NewMigrations(), Options{UnlockTimeout: unlockTimeout})
        })
    }
}
