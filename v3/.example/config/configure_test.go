package config

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/migration"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

/* countingBackplane closes the way the shipped ones do, clearing itself from the hub as the first step, and counts the calls, so a test sees a second Close. */
type countingBackplane struct {
    hub    *melodyhttp.ServerSentEventHub
    closes int
}

func (instance *countingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    return nil
}

func (instance *countingBackplane) Close() error {
    instance.closes++
    instance.hub.SetBackplane(nil)

    return nil
}

/* recordingShutdownRegistrar keeps the hooks a module hands the application's http shutdown, so a test runs
   them the way the server does */
type recordingShutdownRegistrar struct {
    hookList []func()
}

func (instance *recordingShutdownRegistrar) OnHttpShutdown(hook func()) {
    instance.hookList = append(instance.hookList, hook)
}

/* The hub owns the backplane and closes it, and the composition root registers exactly one http shutdown hook for the pair, the hub's own Shutdown: the http shutdown hooks run on their own goroutines, so a second Close of the backplane would race the hub's drain. The hook is run here over the hub the module built, and the container's teardown then closes the hub again through its Close; the pair is idempotent. */
func TestRegisterHubShutdown_RegistersTheHubsShutdownAloneAndItClosesTheBackplaneOnce(t *testing.T) {
    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()

    backplane := &countingBackplane{hub: moduleInstance.serverSentEventHub}
    moduleInstance.serverSentEventHub.SetBackplane(backplane)

    registrar := &recordingShutdownRegistrar{}
    moduleInstance.registerHubShutdown(registrar)

    if 1 != len(registrar.hookList) {
        t.Fatalf("expected the composition root to register one http shutdown hook for the hub and its backplane, got %d", len(registrar.hookList))
    }

    for _, hook := range registrar.hookList {
        hook()
    }

    if false == moduleInstance.serverSentEventHub.IsClosed() {
        t.Fatal("expected the hook to shut the hub down")
    }

    if 1 != backplane.closes {
        t.Fatalf("expected the backplane closed exactly once by the hub that owns it, got %d", backplane.closes)
    }

    if closeErr := moduleInstance.serverSentEventHub.Close(); nil != closeErr {
        t.Fatalf("expected the teardown's second close of the hub to be a no-op, got %v", closeErr)
    }

    if 1 != backplane.closes {
        t.Fatalf("expected the teardown's close of the hub not to reach the backplane again, got %d closes", backplane.closes)
    }
}

/* The base family is pinned to the catalogue's manager, and the archive is a context named after its own manager, carrying its own set. Unpinned, the base family would take the registry's default, which in an environment that armed the archive alone is the archive, and an unqualified db:migrate would aim the catalogue's mysql DDL at postgres. The registry is named by the service the database wiring publishes. */
func TestMigrateModuleConfig_PinsTheBaseFamilyToTheCatalogueAndTheArchiveToItsContext(t *testing.T) {
    config := migrateModuleConfig()

    if migration.Migrations != config.Migrations {
        t.Fatal("expected the base family to carry the catalogue's migration set")
    }

    if "default" != config.Options.ManagerName {
        t.Fatalf("expected the base family pinned to the catalogue's manager, got %q", config.Options.ManagerName)
    }

    if serviceDatabaseRegistry != config.Options.ManagerRegistryServiceId {
        t.Fatalf("expected the registry named by the service the wiring publishes, got %q", config.Options.ManagerRegistryServiceId)
    }

    if 1 != len(config.Contexts) {
        t.Fatalf("expected one context beside the base family, got %d", len(config.Contexts))
    }

    archiveContext := config.Contexts[0]
    if "archive" != archiveContext.Name {
        t.Fatalf("expected the context named after the archive's manager, got %q", archiveContext.Name)
    }

    if migration.ArchiveMigrations != archiveContext.Migrations {
        t.Fatal("expected the archive context to carry the archive's migration set")
    }
}

/* the websocket feed serves the module's own hub, and its keepalive interval is the application's choice — the
   module supplies none and a zero fails the route at boot: the ping is the only thing that reaps a tab that
   went away without a fin */
func TestWebsocketModuleConfig_ServesTheModulesHubWithAThirtySecondKeepalive(t *testing.T) {
    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()

    config := moduleInstance.websocketModuleConfig()

    if moduleInstance.serverSentEventHub != config.Hub || nil == config.Hub {
        t.Fatal("expected the feed to serve the module's hub")
    }

    if "/ws" != config.Path || "example.websocket" != config.RouteName {
        t.Fatalf("expected the feed at /ws named example.websocket, got %q named %q", config.Path, config.RouteName)
    }

    if 30*time.Second != config.Options.IdleTimeout {
        t.Fatalf("expected a thirty-second keepalive, got %s", config.Options.IdleTimeout)
    }
}
