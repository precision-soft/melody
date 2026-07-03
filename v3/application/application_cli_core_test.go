package application

import (
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/http"
)

func TestRegisterCoreCliCommandIfAbsent_RegistersWhenNamePartitionIsFree(t *testing.T) {
    instance := &Application{}

    instance.registerCoreCliCommandIfAbsent(http.NewRouteManifestCommand())

    if 1 != len(instance.cliCommands) {
        t.Fatalf("expected the core command to be registered, got %d", len(instance.cliCommands))
    }
}

func TestRegisterCoreCliCommandIfAbsent_SkipsWhenAppAlreadyRegisteredSameName(t *testing.T) {
    instance := &Application{}

    appCommand := http.NewRouteManifestCommand()
    instance.cliCommands = []clicontract.Command{appCommand}

    /* @important a framework-registered core command must not panic on a duplicate name when the application still wires the same command by hand; it is skipped so the upgrade stays backward-compatible */
    instance.registerCoreCliCommandIfAbsent(http.NewRouteManifestCommand())

    if 1 != len(instance.cliCommands) {
        t.Fatalf("expected the duplicate core command to be skipped, got %d commands", len(instance.cliCommands))
    }

    if appCommand != instance.cliCommands[0] {
        t.Fatalf("expected the application's own command instance to be kept")
    }
}
