package migrate

import (
    "testing"
)

func TestOptions_TheDefaultsAreTheExportedConstants(t *testing.T) {
    if "service.database.manager.registry" != DefaultManagerRegistryServiceId {
        t.Fatalf("unexpected default registry service id: %q", DefaultManagerRegistryServiceId)
    }

    if "manager" != DefaultManagerFlagName {
        t.Fatalf("unexpected default manager flag name: %q", DefaultManagerFlagName)
    }

    if "db" != DefaultCommandPrefix {
        t.Fatalf("unexpected default command prefix: %q", DefaultCommandPrefix)
    }
}
