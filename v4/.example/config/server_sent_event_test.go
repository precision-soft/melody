package config

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* with redis the hub's provider installs the cross-node backplane on the hub it builds, over the connection it resolves: a second backplane is refused, which only an installed one makes it */
func TestHub_TheRedisBackplaneIsInstalledByTheHubsProvider(t *testing.T) {
    client := redisTestClient(t)

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    containerInstance.MustRegister(
        melodyrueidis.ServiceConnection,
        func(resolver melodycontainercontract.Resolver) (*melodyrueidis.Connection, error) {
            return melodyrueidis.NewConnection(client, "melody-example-v4"), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance := &Module{}
    moduleInstance.redisProvider = melodyrueidis.NewProvider("melody-example-v4", melodyrueidis.ProviderConfig{})
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})

    hub := melodycontainer.MustFromResolver[*melodyhttp.ServerSentEventHub](t.Context(), containerInstance, subscriber.ServiceCatalogNotificationHub)

    defer func() {
        recovered := recover()
        if nil == recovered || false == strings.Contains(fmt.Sprint(recovered), "already carries a backplane") {
            t.Fatalf("expected the hub to carry the redis backplane, got %v", recovered)
        }
    }()

    hub.SetBackplane(refusingBackplane{})
}

/* without redis the hub carries no backplane: one installed by hand is taken */
func TestHub_WithoutRedisTheProviderInstallsNoBackplane(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    containerInstance.MustRegister(
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewNopLogger(), nil
        },
        melodycontainer.RegisterOptions{},
    )

    moduleInstance := &Module{}
    moduleInstance.registerServerSentEventHubService(containerRegistrar{Container: containerInstance})

    hub := melodycontainer.MustFromResolver[*melodyhttp.ServerSentEventHub](t.Context(), containerInstance, subscriber.ServiceCatalogNotificationHub)
    hub.SetBackplane(refusingBackplane{})
}

func TestWebsocketStreamHandlerConfig_KeepsAThirtySecondKeepalive(t *testing.T) {
    if bound.Of(30*time.Second) != websocketStreamHandlerConfig().IdleTimeout {
        t.Fatalf("expected a thirty-second keepalive, got %s", websocketStreamHandlerConfig().IdleTimeout)
    }
}

/* the /ws handler serves the hub the container publishes, resolved at each request: a closed one is refused by the integration's handler with 503 before any upgrade */
func TestWebsocketStreamHandler_ServesTheHubTheContainerPublishes(t *testing.T) {
    registeredHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    _ = registeredHub.CloseWithContext(t.Context())

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return registeredHub, nil
        },
        melodycontainer.RegisterOptions{},
    )

    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/ws", nil), nil, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("websocket-test", time.Now())})

    _, handlerErr := websocketStreamHandler(websocketStreamHandlerConfig())(runtimeInstance, httptest.NewRecorder(), request)

    var httpException *melodyexception.HttpException
    if false == errors.As(handlerErr, &httpException) || nethttp.StatusServiceUnavailable != httpException.StatusCode() {
        t.Fatalf("expected the closed hub refused with 503, got %v", handlerErr)
    }
}

/* without the hub service the request answers the resolution's refusal, never a nil hub dereferenced */
func TestWebsocketStreamHandler_AnswersTheRefusalWhenTheHubIsNotRegistered(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/ws", nil), nil, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("websocket-test", time.Now())})

    if _, handlerErr := websocketStreamHandler(websocketStreamHandlerConfig())(runtimeInstance, httptest.NewRecorder(), request); nil == handlerErr {
        t.Fatal("expected the missing hub service refused")
    }
}
