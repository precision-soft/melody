package config

import (
    "bytes"
    "errors"
    "strings"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
)

/* the hub is never resolved through the container here except by ResolveBootServices, so the journal record proves its provider ran at boot: the module's own hub is the one broadcast on, as the handlers hold it */
func TestResolveBootServices_HandsTheHubItsJournalBeforeAnyWrite(t *testing.T) {
    journal := &bytes.Buffer{}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewJsonLogger(journal, melodylogging.JsonLoggerConfig{MinLevel: melodyloggingcontract.LevelDebug}), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildServerSentEvent()
    moduleInstance.buildInternalAuth()
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerInternalAuthSignerService(containerRegistrar{Container: containerInstance})

    if resolveErr := ResolveBootServices(t.Context(), containerInstance); nil != resolveErr {
        t.Fatalf("unexpected boot resolution error: %v", resolveErr)
    }

    hub := moduleInstance.serverSentEventHub
    hub.SetBackplane(refusingBackplane{})
    hub.Broadcast("catalog", melodyhttp.ServerSentEvent{Event: "notification", Data: "changed"})
    hub.Shutdown()

    if false == strings.Contains(journal.String(), "server sent event backplane publish failed") {
        t.Fatalf("expected the refused publish in the container's journal, got %q", journal.String())
    }
}

func TestResolveBootServices_RefusesAHubThatIsNotRegistered(t *testing.T) {
    resolveErr := ResolveBootServices(t.Context(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}))
    if nil == resolveErr {
        t.Fatal("expected the boot resolution to refuse a missing hub")
    }

    if false == strings.Contains(resolveErr.Error(), "not registered") {
        t.Fatalf("expected the container's refusal, got %v", resolveErr)
    }

}

func bootContainerWithInternalAuthSigner(t *testing.T, moduleInstance *Module) melodycontainercontract.Container {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance.buildServerSentEvent()
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})
    moduleInstance.registerInternalAuthSignerService(containerRegistrar{Container: containerInstance})

    return containerInstance
}

func TestResolveBootServices_BuildsTheInternalAuthSignerOnce(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildInternalAuth()
    containerInstance := bootContainerWithInternalAuthSigner(t, moduleInstance)

    if resolveErr := ResolveBootServices(t.Context(), containerInstance); nil != resolveErr {
        t.Fatalf("unexpected boot resolution error: %v", resolveErr)
    }

    first := melodycontainer.MustFromResolver[*melodysecurity.HmacEnvelopeSigner](t.Context(), containerInstance, ServiceInternalAuthSigner)
    second := melodycontainer.MustFromResolver[*melodysecurity.HmacEnvelopeSigner](t.Context(), containerInstance, ServiceInternalAuthSigner)
    if first != second {
        t.Fatal("expected one signer for the process")
    }
}

func TestResolveBootServices_ACurrentKeyNotBoundToTheSignerApplicationFailsTheBoot(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.buildInternalAuth()
    moduleInstance.internalAuthApplication = "another-service"
    containerInstance := bootContainerWithInternalAuthSigner(t, moduleInstance)

    resolveErr := ResolveBootServices(t.Context(), containerInstance)
    for cause := resolveErr; nil != cause; cause = errors.Unwrap(cause) {
        if true == strings.Contains(cause.Error(), "not bound to the signer application") {
            return
        }
    }

    t.Fatalf("expected the boot refused over the unbound key, got %v", resolveErr)
}
