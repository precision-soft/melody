package config

import (
    "bytes"
    "strings"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* the hub is never resolved through the container here except by ResolveBootServices, so the journal record proves its provider ran at boot: the module's own hub is the one broadcast on, as the handlers hold it */
func TestResolveBootServices_HandsTheHubItsJournalBeforeAnyWrite(t *testing.T) {
    journal := &bytes.Buffer{}

    containerInstance := melodycontainer.NewContainer()
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug), nil
        },
    )

    moduleInstance := &Module{}
    moduleInstance.buildServerSentEvent()
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})

    if resolveErr := ResolveBootServices(containerInstance); nil != resolveErr {
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
    resolveErr := ResolveBootServices(melodycontainer.NewContainer())
    if nil == resolveErr {
        t.Fatal("expected the boot resolution to refuse a missing hub")
    }

    if false == strings.Contains(resolveErr.Error(), "not registered") {
        t.Fatalf("expected the container's refusal, got %v", resolveErr)
    }

}
