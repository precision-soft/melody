package http

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

type phaseRecorder struct {
    mutex sync.Mutex
    kinds map[string]containercontract.Kind
}

func (instance *phaseRecorder) record(serviceName string, resolver containercontract.Resolver) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.kinds[serviceName] = resolver.Runtime().Kind()
}

func (instance *phaseRecorder) kindOf(serviceName string) containercontract.Kind {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.kinds[serviceName]
}

func TestKernel_TheSingletonsTheRequestPathBuildsFirstReadKindRequest(t *testing.T) {
    recorder := &phaseRecorder{kinds: map[string]containercontract.Kind{}}

    serviceContainer := container.NewContainer(container.ContainerConfig{})

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            recorder.record(logging.ServiceLogger, resolver)

            /* a logger whose provider pulls a collaborator in, as a real one reads the configuration */
            _ = config.ConfigMustFromResolver(resolver.Runtime().Context(), resolver)

            return logging.NewNopLogger(), nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            recorder.record(config.ServiceConfig, resolver)

            environment, err := config.NewEnvironment(context.Background(),
                &testEnvironmentSource{
                    values: map[string]string{config.EnvironmentNameKey: config.EnvironmentDevelopment},
                },
            )
            if nil != err {
                return nil, err
            }

            return config.NewConfiguration(environment, "/tmp/melody", config.ConfigurationConfig{})
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            recorder.record(session.ServiceSessionManager, resolver)

            return session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{Ttl: bound.Of(30 * time.Minute)}), nil
        },
        container.RegisterOptions{},
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(event.EventDispatcherConfig{Clock: clock.NewSystemClock()}), nil
        },
        container.RegisterOptions{},
    )

    router := NewRouter()
    router.Handle(
        "/phase",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(serviceContainer)

    responseRecorder := httptest.NewRecorder()
    handler.ServeHTTP(responseRecorder, httptest.NewRequest(nethttp.MethodGet, "/phase", nil))

    if nethttp.StatusOK != responseRecorder.Code {
        t.Fatalf("expected status 200, got %d", responseRecorder.Code)
    }

    for _, serviceName := range []string{logging.ServiceLogger, config.ServiceConfig, session.ServiceSessionManager} {
        if containercontract.KindRequest != recorder.kindOf(serviceName) {
            t.Fatalf("expected %s, first built on the request path, to read kind request, got %s", serviceName, recorder.kindOf(serviceName))
        }
    }
}
