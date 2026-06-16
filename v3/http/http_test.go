package http

import (
    "context"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

func newTestRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    scope := serviceContainer.NewScope()

    scope.MustOverrideProtectedInstance(logging.ServiceLogger, logging.NewNopLogger())

    return runtime.New(context.Background(), scope, serviceContainer)
}

type testEnvironmentSource struct {
    values map[string]string
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func newHttpTestContainer() containercontract.Container {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            environment, err := config.NewEnvironment(
                &testEnvironmentSource{
                    values: map[string]string{
                        config.EnvKey: config.EnvDevelopment,
                    },
                },
            )
            if nil != err {
                return nil, err
            }

            return config.NewConfiguration(environment, "/tmp/melody")
        },
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            storage := session.NewInMemoryStorage()
            return session.NewManager(storage, 30*time.Minute), nil
        },
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(clock.NewSystemClock()), nil
        },
    )

    return serviceContainer
}

/** @info fakes */

/* closeRecordingScope wraps a real scope to record Close calls and optionally force OverrideProtectedInstance to fail */
type closeRecordingScope struct {
    containercontract.Scope
    failOverride bool
    closed       bool
}

func (instance *closeRecordingScope) OverrideProtectedInstance(serviceName string, value any) error {
    if true == instance.failOverride {
        return exception.NewError("forced override failure", nil, nil)
    }

    return instance.Scope.OverrideProtectedInstance(serviceName, value)
}

func (instance *closeRecordingScope) Close() error {
    instance.closed = true

    return instance.Scope.Close()
}

/* scopeRecordingContainer wraps a real container and hands out a closeRecordingScope so a test can observe scope lifecycle */
type scopeRecordingContainer struct {
    containercontract.Container
    failOverride bool
    scope        *closeRecordingScope
}

func (instance *scopeRecordingContainer) NewScope() containercontract.Scope {
    instance.scope = &closeRecordingScope{
        Scope:        instance.Container.NewScope(),
        failOverride: instance.failOverride,
    }

    return instance.scope
}
