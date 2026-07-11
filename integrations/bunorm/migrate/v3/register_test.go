package migrate

import (
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/uptrace/bun/migrate"
)

func flagsContainName(flags []clicontract.Flag, name string) bool {
    for _, flag := range flags {
        for _, flagName := range flag.Names() {
            if name == flagName {
                return true
            }
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

func TestDefaultOptions_Values(t *testing.T) {
    options := DefaultOptions()

    if "service.database.manager.registry" != options.ManagerRegistryServiceId {
        t.Fatalf("ManagerRegistryServiceId = %q, want %q", options.ManagerRegistryServiceId, "service.database.manager.registry")
    }

    if "manager" != options.ManagerFlagName {
        t.Fatalf("ManagerFlagName = %q, want %q", options.ManagerFlagName, "manager")
    }

    if "db" != options.CommandPrefix {
        t.Fatalf("CommandPrefix = %q, want %q", options.CommandPrefix, "db")
    }
}

func TestRegisterCommands_NilMigrationsReturnsEmptySet(t *testing.T) {
    commands := RegisterCommands(nil, DefaultOptions())

    if 0 != len(commands) {
        t.Fatalf("expected no commands for nil migrations, got %d", len(commands))
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
    commands := RegisterCommands(migrate.NewMigrations(), DefaultOptions())

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
