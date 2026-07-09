package migrate

import (
    "testing"

    "github.com/uptrace/bun/migrate"
)

func assertPanics(t *testing.T, callback func()) {
    t.Helper()

    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatalf("expected a panic")
        }
    }()

    callback()
}

func TestEffectiveOptions_DerivesPrefixAndManagerFromTheContextName(t *testing.T) {
    resolved := effectiveOptions(ContextConfig{Name: "payment"}, Options{})

    if "db:payment" != resolved.CommandPrefix {
        t.Fatalf("expected the derived prefix, got: %s", resolved.CommandPrefix)
    }

    if "payment" != resolved.ManagerName {
        t.Fatalf("expected the manager to default to the context name, got: %s", resolved.ManagerName)
    }

    defaults := DefaultOptions()
    if defaults.ManagerRegistryServiceId != resolved.ManagerRegistryServiceId {
        t.Fatalf("expected the default registry service id, got: %s", resolved.ManagerRegistryServiceId)
    }

    if defaults.ManagerFlagName != resolved.ManagerFlagName {
        t.Fatalf("expected the default manager flag name, got: %s", resolved.ManagerFlagName)
    }
}

func TestEffectiveOptions_InheritsFromBaseOptions(t *testing.T) {
    base := Options{
        ManagerRegistryServiceId: "service.database.registry.custom",
        ManagerFlagName:          "database",
        CommandPrefix:            "orm",
    }

    resolved := effectiveOptions(ContextConfig{Name: "apiaccess"}, base)

    if "service.database.registry.custom" != resolved.ManagerRegistryServiceId {
        t.Fatalf("expected the base registry service id, got: %s", resolved.ManagerRegistryServiceId)
    }

    if "database" != resolved.ManagerFlagName {
        t.Fatalf("expected the base manager flag name, got: %s", resolved.ManagerFlagName)
    }

    if "orm:apiaccess" != resolved.CommandPrefix {
        t.Fatalf("expected the base-derived prefix, got: %s", resolved.CommandPrefix)
    }
}

func TestEffectiveOptions_ExplicitContextOptionsWin(t *testing.T) {
    contextConfig := ContextConfig{
        Name: "platform",
        Options: Options{
            ManagerRegistryServiceId: "service.database.registry.platform",
            CommandPrefix:            "platform-db",
            ManagerName:              "platform-primary",
        },
    }

    resolved := effectiveOptions(contextConfig, Options{CommandPrefix: "orm"})

    if "service.database.registry.platform" != resolved.ManagerRegistryServiceId {
        t.Fatalf("expected the explicit registry service id, got: %s", resolved.ManagerRegistryServiceId)
    }

    if "platform-db" != resolved.CommandPrefix {
        t.Fatalf("expected the explicit prefix, got: %s", resolved.CommandPrefix)
    }

    if "platform-primary" != resolved.ManagerName {
        t.Fatalf("expected the explicit manager name, got: %s", resolved.ManagerName)
    }
}

func TestValidateContexts_PanicsOnEmptyName(t *testing.T) {
    assertPanics(t, func() {
        validateContexts([]ContextConfig{{Name: "", Migrations: migrate.NewMigrations()}})
    })
}

func TestValidateContexts_PanicsOnNilMigrations(t *testing.T) {
    assertPanics(t, func() {
        validateContexts([]ContextConfig{{Name: "payment"}})
    })
}

func TestValidateContexts_PanicsOnDuplicateName(t *testing.T) {
    assertPanics(t, func() {
        validateContexts([]ContextConfig{
            {Name: "payment", Migrations: migrate.NewMigrations()},
            {Name: "payment", Migrations: migrate.NewMigrations()},
        })
    })
}

func TestValidateContexts_AcceptsDistinctContexts(t *testing.T) {
    validateContexts([]ContextConfig{
        {Name: "platform", Migrations: migrate.NewMigrations()},
        {Name: "payment", Migrations: migrate.NewMigrations()},
    })
}
