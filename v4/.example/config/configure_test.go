package config

import (
    "context"
    "testing"
    "time"

    melodyotlp "github.com/precision-soft/melody/integrations/opentelemetry/v4/otlp"
    "github.com/precision-soft/melody/v4/.example/migration"
    "github.com/precision-soft/melody/v4/bound"
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

type countingBackplane struct {
    hub    *melodyhttp.ServerSentEventHub
    closes int
}

func (instance *countingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    return nil
}

func (instance *countingBackplane) CloseWithContext(closeContext context.Context) error {
    instance.closes++
    instance.hub.SetBackplane(nil)

    return nil
}

type recordingShutdownRegistrar struct {
    hookList []func()
}

func (instance *recordingShutdownRegistrar) OnHttpShutdown(hook func()) {
    instance.hookList = append(instance.hookList, hook)
}

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

    if closeErr := moduleInstance.serverSentEventHub.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected the teardown's second close of the hub to be a no-op, got %v", closeErr)
    }

    if 1 != backplane.closes {
        t.Fatalf("expected the teardown's close of the hub not to reach the backplane again, got %d closes", backplane.closes)
    }
}

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

func TestWebsocketModuleConfig_ServesTheModulesHubWithAThirtySecondKeepalive(t *testing.T) {
    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()

    moduleConfig := moduleInstance.websocketModuleConfig()
    if 1 != len(moduleConfig.Streams) {
        t.Fatalf("expected one stream, got %d", len(moduleConfig.Streams))
    }

    config := moduleConfig.Streams[0]

    if moduleInstance.serverSentEventHub != config.Hub || nil == config.Hub {
        t.Fatal("expected the feed to serve the module's hub")
    }

    if "/ws" != config.Path || "example.websocket" != config.RouteName {
        t.Fatalf("expected the feed at /ws named example.websocket, got %q named %q", config.Path, config.RouteName)
    }

    if bound.Of(30*time.Second) != config.Options.IdleTimeout {
        t.Fatalf("expected a thirty-second keepalive, got %s", config.Options.IdleTimeout)
    }
}

func TestOtlpModuleConfig_ReportsTheStampedApplicationVersion(t *testing.T) {
    moduleConfig := (&Module{applicationVersion: "9.9.9"}).otlpModuleConfig("otel-collector:4317")

    if "9.9.9" != moduleConfig.Config.ServiceVersion || "melody.example" != moduleConfig.Config.ServiceName {
        t.Fatalf("expected the stamped version under the example's service name, got %+v", moduleConfig.Config)
    }
}

func TestOtlpModuleConfig_CarriesTheEndpointAndGrpc(t *testing.T) {
    moduleConfig := (&Module{applicationVersion: "9.9.9"}).otlpModuleConfig("otel-collector:4317")

    if "otel-collector:4317" != moduleConfig.Config.Endpoint || melodyotlp.ProtocolGrpc != moduleConfig.Config.Protocol || false == moduleConfig.Config.Insecure {
        t.Fatalf("expected the endpoint over plaintext grpc, got %+v", moduleConfig.Config)
    }
}
