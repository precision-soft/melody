package middleware

import (
    "context"
    "errors"
    nethttp "net/http"
    "testing"

    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/runtime"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

type rateLimitKernelEnvironmentSource struct{}

func (instance *rateLimitKernelEnvironmentSource) Load() (map[string]string, error) {
    return map[string]string{config.EnvironmentNameKey: config.EnvironmentProduction}, nil
}

/* the real kernel over a minimal container, so a pin reads the header the renderer writes on the wire */
func servedRateLimitKernel(t *testing.T, configure func(router *http.Router, dispatcher eventcontract.EventDispatcher)) nethttp.Handler {
    t.Helper()

    dispatcher := event.NewEventDispatcher(clock.NewSystemClock())
    router := http.NewRouter()
    configure(router, dispatcher)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, environmentErr := config.NewEnvironment(&rateLimitKernelEnvironmentSource{})
            if nil != environmentErr {
                return nil, environmentErr
            }

            return config.NewConfiguration(environment, t.TempDir())
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return session.NewManager(session.NewInMemoryStorage(session.InMemoryStorageConfig{}), session.ManagerConfig{}), nil
        },
        container.RegisterOptions{},
    )
    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return dispatcher, nil
        },
        container.RegisterOptions{},
    )

    return http.NewKernel(router, http.KernelConfig{}).ServeHttp(serviceContainer)
}

func rateLimitTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func allowedBy(limiter httpcontract.RateLimiter, key string) bool {
    decision, _ := limiter.Allow(rateLimitTestRuntime(), key)

    return decision.Allowed
}

func resetBy(limiter httpcontract.RateLimiter, key string) {
    _ = limiter.Reset(rateLimitTestRuntime(), key)
}

func assertPanicsNamingField(t *testing.T, callback func(), expectedMessage string, field string) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic with the refusal %q", expectedMessage)
        }

        var refusal *exception.Error
        if false == errors.As(recoveredErr, &refusal) {
            t.Fatalf("expected the refusal %q, got %v", expectedMessage, recoveredErr)
        }

        if expectedMessage != refusal.Message() {
            t.Fatalf("expected the message %q, got %q", expectedMessage, refusal.Message())
        }

        if field != refusal.Context()["field"] {
            t.Fatalf("expected the refusal to name %q, got %v", field, refusal.Context()["field"])
        }
    }()

    callback()
}

func assertPanicsNamingArgument(t *testing.T, callback func(), expectedMessage string, argument string, door string) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a panic with the refusal %q", expectedMessage)
        }

        var refusal *exception.Error
        if false == errors.As(recoveredErr, &refusal) {
            t.Fatalf("expected the refusal %q, got %v", expectedMessage, recoveredErr)
        }

        if expectedMessage != refusal.Message() {
            t.Fatalf("expected the message %q, got %q", expectedMessage, refusal.Message())
        }

        if argument != refusal.Context()["argument"] || door != refusal.Context()["door"] {
            t.Fatalf("expected the refusal to name the argument %q on %q, got %v", argument, door, refusal.Context())
        }
    }()

    callback()
}

func assertPanicsNamingContextKey(t *testing.T, callback func(), expectedMessage string, key string, value string) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a panic %q naming %s %q", expectedMessage, key, value)
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected an error panic, got %T", recovered)
        }

        var melodyErr *exception.Error
        if false == errors.As(recoveredErr, &melodyErr) {
            t.Fatalf("expected a melody error, got %T", recoveredErr)
        }

        if expectedMessage != melodyErr.Message() || value != melodyErr.Context()[key] {
            t.Fatalf("expected %q naming %s %q, got %q %v", expectedMessage, key, value, melodyErr.Message(), melodyErr.Context())
        }
    }()

    callback()
}
