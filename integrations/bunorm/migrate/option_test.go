package migrate

import (
    "testing"
)

/* @info the defaults are the contract every zero-value Options field resolves to; a drift here silently re-roots every registration that relied on them */
func TestDefaultOptions(t *testing.T) {
    options := DefaultOptions()

    if "service.database.manager.registry" != options.ManagerRegistryServiceId {
        t.Fatalf("unexpected default registry service id: %q", options.ManagerRegistryServiceId)
    }

    if "manager" != options.ManagerFlagName {
        t.Fatalf("unexpected default manager flag name: %q", options.ManagerFlagName)
    }

    if "db" != options.CommandPrefix {
        t.Fatalf("unexpected default command prefix: %q", options.CommandPrefix)
    }

    if "" != options.ManagerName {
        t.Fatalf("expected no default manager pin, got %q", options.ManagerName)
    }
}
