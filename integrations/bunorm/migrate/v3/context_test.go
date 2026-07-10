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

/** @info Every zero-value field of a per-context Options inherits from the base options — the ContextConfig doc says so, and CommandPrefix, ManagerFlagName and ManagerRegistryServiceId all do. ManagerName jumped straight to the context name, so a base pin was silently ignored and each context's commands targeted a manager merely named after it. */
func TestEffectiveOptions_ManagerNameInheritsTheBasePin(t *testing.T) {
    resolved := effectiveOptions(
        ContextConfig{Name: "erp"},
        Options{ManagerName: "primary"},
    )

    if "primary" != resolved.ManagerName {
        t.Fatalf("expected the base ManagerName pin to be inherited, got %q", resolved.ManagerName)
    }
}

/** @info With neither the context nor the base pinning it, the context name remains the default. */
func TestEffectiveOptions_ManagerNameFallsBackToTheContextName(t *testing.T) {
    resolved := effectiveOptions(ContextConfig{Name: "reporting"}, Options{})

    if "reporting" != resolved.ManagerName {
        t.Fatalf("expected the context name as the default manager, got %q", resolved.ManagerName)
    }
}

/** @info A per-context pin still beats the base pin. */
func TestEffectiveOptions_ContextManagerNameBeatsTheBasePin(t *testing.T) {
    resolved := effectiveOptions(
        ContextConfig{Name: "erp", Options: Options{ManagerName: "erp_database"}},
        Options{ManagerName: "primary"},
    )

    if "erp_database" != resolved.ManagerName {
        t.Fatalf("expected the per-context pin to win, got %q", resolved.ManagerName)
    }
}
