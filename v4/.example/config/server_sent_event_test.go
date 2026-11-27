package config

import (
    "context"
    "fmt"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    "github.com/precision-soft/melody/v4/.example/persistence"
    "github.com/precision-soft/melody/v4/.example/repository"
    handlerevent "github.com/precision-soft/melody/v4/.example/handler/event"
    "github.com/precision-soft/melody/v4/.example/subscriber"
    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
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

func TestWebsocketModuleConfig_ServesSlashWsUnderItsCountedRouteWithAThirtySecondKeepalive(t *testing.T) {
    streams := websocketModuleConfig().Streams
    if 1 != len(streams) || "/ws" != streams[0].Path || websocketRouteName != streams[0].RouteName {
        t.Fatalf("expected one /ws stream named %s, got %+v", websocketRouteName, streams)
    }

    if bound.Of(30*time.Second) != streams[0].Handler.IdleTimeout {
        t.Fatalf("expected a thirty-second keepalive, got %s", streams[0].Handler.IdleTimeout)
    }
}

/* the module's hub provider answers the hub the container publishes, resolved through the connection's runtime */
func TestWebsocketModuleConfig_TheHubIsTheOneTheContainerPublishes(t *testing.T) {
    registeredHub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    t.Cleanup(func() { _ = registeredHub.CloseWithContext(context.Background()) })

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    containerInstance.MustRegister(
        subscriber.ServiceCatalogNotificationHub,
        func(resolver melodycontainercontract.Resolver) (*melodyhttp.ServerSentEventHub, error) {
            return registeredHub, nil
        },
        melodycontainer.RegisterOptions{},
    )

    hub, hubErr := websocketModuleConfig().Streams[0].Hub(melodyruntime.NewRun(context.Background(), containerInstance.NewScope()))
    if nil != hubErr || registeredHub != hub {
        t.Fatalf("expected the registered hub, got %p %v", hub, hubErr)
    }
}

/* without the hub service the provider answers the resolution's refusal, never a nil hub */
func TestWebsocketModuleConfig_AnswersTheRefusalWhenTheHubIsNotRegistered(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    hub, hubErr := websocketModuleConfig().Streams[0].Hub(melodyruntime.NewRun(context.Background(), containerInstance.NewScope()))
    if nil == hubErr || nil != hub {
        t.Fatalf("expected the missing hub service refused, got %p %v", hub, hubErr)
    }
}

/* the /ws connection subscribes to the topic its query names, as the event stream reads it, the catalogue's when it names none */
func TestWebsocketModuleConfig_ResolvesTheTopicFromTheQuery(t *testing.T) {
    resolver := websocketModuleConfig().Streams[0].Handler.TopicResolver
    if nil == resolver {
        t.Fatal("expected the /ws stream to resolve its topic")
    }

    runtimeInstance := melodyruntime.NewRun(context.Background(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
    for target, expected := range map[string]string{
        "/ws?topic=user-directory": handlerevent.UserDirectoryTopic,
        "/ws":                      handlerevent.CatalogTopic,
    } {
        request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, target, nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("topic-test", time.Now())})
        if topic := resolver(request); expected != topic {
            t.Fatalf("%s: expected the topic %q, got %q", target, expected, topic)
        }
    }
}

/* the re-read reads the account through the directory and the session through the session storage: a deleted account reads absent and a deleted session reads ended */
func TestEventStreamRevision_ReReadsTheDirectoryAndTheSessionStorage(t *testing.T) {
    userRepository, repositoryErr := repository.NewUserRepository(persistence.NewCatalogStorage(nil).WithAccountSeed(), &persistence.CatalogSchema{})
    if nil != repositoryErr {
        t.Fatalf("new user repository: %v", repositoryErr)
    }

    sessionStorage := melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })
    melodycontainer.MustRegister[repository.UserRepository](containerInstance, repository.ServiceUserRepository, func(resolver melodycontainercontract.Resolver) (repository.UserRepository, error) {
        return userRepository, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister[melodysessioncontract.Storage](containerInstance, melodysession.ServiceSessionStorage, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
        return sessionStorage, nil
    }, melodycontainer.RegisterOptions{})
    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    if saveErr := sessionStorage.Save(runtimeInstance, "session-live", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("saving the session failed: %v", saveErr)
    }

    revision := moduleWithEnvironment(t, map[string]string{}).eventStreamRevision()

    if user, found, lookupErr := revision.LookupUser(runtimeInstance, "user-2"); nil != lookupErr || false == found || "user-2" != user.Id {
        t.Fatalf("expected the seeded account read from the directory, got %v, %v, %v", user, found, lookupErr)
    }

    if _, found, lookupErr := revision.LookupUser(runtimeInstance, "user-absent"); nil != lookupErr || true == found {
        t.Fatalf("expected an absent account read absent, got %v, %v", found, lookupErr)
    }

    if live, liveErr := revision.SessionLive(runtimeInstance, "session-live"); nil != liveErr || false == live {
        t.Fatalf("expected the stored session read live, got %v, %v", live, liveErr)
    }

    if deleteErr := sessionStorage.Delete(runtimeInstance, "session-live"); nil != deleteErr {
        t.Fatalf("deleting the session failed: %v", deleteErr)
    }

    if live, liveErr := revision.SessionLive(runtimeInstance, "session-live"); nil != liveErr || true == live {
        t.Fatalf("expected the deleted session read ended, got %v, %v", live, liveErr)
    }
}

func TestEventStreamRevision_ReadsTheIntervalFromItsKeyAndKeepsTheReReadOn(t *testing.T) {
    for value, expected := range map[string]time.Duration{"": 15 * time.Second, "2s": 2 * time.Second} {
        revision := moduleWithEnvironment(t, map[string]string{environmentKeyEventStreamRevisionInterval: value}).eventStreamRevision()
        if expected != revision.Interval || true == revision.Disabled || nil == revision.LookupUser || nil == revision.SessionLive {
            t.Errorf("expected %q to give an enabled re-read every %s, got %+v", value, expected, revision)
        }
    }
}
