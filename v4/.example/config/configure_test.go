package config

import (
    "bytes"
    "context"
    "strings"
    "testing"
    "time"

    melodyotlp "github.com/precision-soft/melody/integrations/opentelemetry/v4/otlp"
    "github.com/precision-soft/melody/v4/.example/migration"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
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
    _ = instance.hub.ClearBackplane(closeContext)

    return nil
}

type recordingShutdownRegistrar struct {
    hookList []func(shutdownContext context.Context)
}

func (instance *recordingShutdownRegistrar) OnHttpShutdown(hook func(shutdownContext context.Context)) {
    instance.hookList = append(instance.hookList, hook)
}

func TestRegisterHubShutdown_RegistersTheHubsShutdownAloneAndItClosesTheBackplaneOnce(t *testing.T) {
    moduleInstance := &Module{}
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    moduleInstance.builtServerSentEventHub.Store(hub)

    backplane := &countingBackplane{hub: hub}
    hub.SetBackplane(backplane)

    registrar := &recordingShutdownRegistrar{}
    moduleInstance.registerHubShutdown(registrar)

    if 1 != len(registrar.hookList) {
        t.Fatalf("expected the composition root to register one http shutdown hook for the hub and its backplane, got %d", len(registrar.hookList))
    }

    for _, hook := range registrar.hookList {
        hook(context.Background())
    }

    if false == hub.IsClosed() {
        t.Fatal("expected the hook to shut the hub down")
    }

    if 1 != backplane.closes {
        t.Fatalf("expected the backplane closed exactly once by the hub that owns it, got %d", backplane.closes)
    }

    if closeErr := hub.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected the teardown's second close of the hub to be a no-op, got %v", closeErr)
    }

    if 1 != backplane.closes {
        t.Fatalf("expected the teardown's close of the hub not to reach the backplane again, got %d closes", backplane.closes)
    }
}

/* blockingBackplane holds a publish inside until released, the shape of a backplane whose network round trip outlives the shutdown */
type blockingBackplane struct {
    entered  chan struct{}
    released chan struct{}
    closed   chan struct{}
}

func (instance *blockingBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    close(instance.entered)
    <-instance.released

    return nil
}

func (instance *blockingBackplane) CloseWithContext(closeContext context.Context) error {
    close(instance.closed)

    return nil
}

/* the hook closes the hub under the shutdown's context: a publish still inside the backplane when the budget is gone does not hold the hook, the hub hands the backplane to its own closer and answers the error, which the hook journals at warning */
func TestRegisterHubShutdown_ReturnsWithinTheShutdownsBudgetWhileAPublishIsInside(t *testing.T) {
    journal := &bytes.Buffer{}
    previous := hubShutdownWarningLogger
    hubShutdownWarningLogger = func() melodyloggingcontract.Logger {
        return melodylogging.NewJsonLogger(journal, melodylogging.JsonLoggerConfig{MinLevel: melodyloggingcontract.LevelDebug})
    }
    t.Cleanup(func() {
        hubShutdownWarningLogger = previous
    })

    moduleInstance := &Module{}
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    moduleInstance.builtServerSentEventHub.Store(hub)

    backplane := &blockingBackplane{entered: make(chan struct{}), released: make(chan struct{}), closed: make(chan struct{})}
    hub.SetBackplane(backplane)

    go hub.Broadcast("catalog", melodyhttp.ServerSentEvent{Data: "changed"})
    <-backplane.entered

    registrar := &recordingShutdownRegistrar{}
    moduleInstance.registerHubShutdown(registrar)

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    hookReturned := make(chan struct{})
    go func() {
        registrar.hookList[0](doneContext)
        close(hookReturned)
    }()

    select {
    case <-hookReturned:
    case <-time.After(5 * time.Second):
        close(backplane.released)
        t.Fatal("expected the hook to return within the shutdown's budget while a publish is inside the backplane")
    }

    if false == hub.IsClosed() {
        t.Fatal("expected the hook to close the hub")
    }

    if false == strings.Contains(journal.String(), "the server sent event hub did not close within the http shutdown budget") {
        t.Fatalf("expected the hub's close error journaled at warning, got %q", journal.String())
    }

    close(backplane.released)

    select {
    case <-backplane.closed:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the hub's own closer to close the backplane once the publish ended")
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

/* the hook closes the hub the provider built: before the provider ran there is none, and the hook returns without one */
func TestRegisterHubShutdown_ClosesNothingBeforeTheProviderBuiltTheHub(t *testing.T) {
    moduleInstance := &Module{}

    registrar := &recordingShutdownRegistrar{}
    moduleInstance.registerHubShutdown(registrar)

    registrar.hookList[0](context.Background())

    if nil != moduleInstance.builtServerSentEventHub.Load() {
        t.Fatal("expected the hook to build no hub of its own")
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

    if "otel-collector:4317" != moduleConfig.Endpoint || melodyotlp.ProtocolGrpc != moduleConfig.Config.Protocol || false == moduleConfig.Config.Insecure {
        t.Fatalf("expected the endpoint over plaintext grpc, got %+v", moduleConfig.Config)
    }
}
