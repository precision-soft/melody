package application

import (
    "errors"
    "io"
    "os"
    "path/filepath"
    "syscall"
    "time"

    applicationcontract "github.com/precision-soft/melody/v3/application/contract"
    "github.com/precision-soft/melody/v3/cache"
    cachecontract "github.com/precision-soft/melody/v3/cache/contract"
    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/event"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/messagebus"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    "github.com/precision-soft/melody/v3/serializer"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
    "github.com/precision-soft/melody/v3/session"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
    "github.com/precision-soft/melody/v3/validation"
)

func (instance *Application) RegisterService(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    instance.MustRegister(serviceName, provider, options...)
}

/* Register makes the application a container registrar, so a module may reach for the container's own registration helpers — container.MustRegisterType and the generated wiring built on it — instead of only the name-based RegisterService. A duplicate is absorbed into the aggregated boot report rather than returned, so a module that registers a service the framework already provides reports it the same way whichever entry point it used. */
func (instance *Application) Register(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) error {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register services after boot", nil, nil))
    }

    registerErr := instance.kernel.ServiceContainer().Register(serviceName, provider, options...)
    if nil == registerErr {
        return nil
    }

    if true == errors.Is(registerErr, container.ErrServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindService, serviceName)
        return nil
    }

    if true == errors.Is(registerErr, container.ErrServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindServiceType, serviceName)
        return nil
    }

    if true == errors.Is(registerErr, container.ErrScopedServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedService, serviceName)
        return nil
    }

    if true == errors.Is(registerErr, container.ErrScopedServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedServiceType, serviceName)
        return nil
    }

    return registerErr
}

func (instance *Application) MustRegister(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    registerErr := instance.Register(serviceName, provider, options...)
    if nil != registerErr {
        exception.Panic(exception.FromError(registerErr))
    }
}

/* RegisterScopedService declares a service the application's scopes own: one instance per scope — one http request, one command run — closed with it. It mirrors RegisterService in everything but lifetime, collisions included — a name claimed at both lifetimes is absorbed into the aggregated boot report, so a module that scopes a name the framework registers later hears about it beside every other collision instead of one panic per boot attempt.

   In console the run's scope spans the whole command, so for a one-shot command "scoped" and "per run" are the same thing — but a long-running command that processes many units of work holds one scope for all of them, and a scoped transaction or identity quietly becomes a process singleton. Such a command creates a child runtime per unit, the way the cron runner does around each scheduled run: a fresh scope from Container().NewScope(), a runtime.New over it, and a Close whose error is joined onto the unit's own when the unit ends. */
func (instance *Application) RegisterScopedService(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    instance.MustRegisterScoped(serviceName, provider, options...)
}

/* RegisterScoped declares a service the application's scopes own: one instance per request, closed with the request. It mirrors Register in everything but lifetime, collisions included — a name claimed at both lifetimes is absorbed into the aggregated boot report, so a module that scopes a name the framework registers later hears about it beside every other collision instead of one panic per boot attempt. */
func (instance *Application) RegisterScoped(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) error {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register scoped services after boot", nil, nil))
    }

    registerScopedErr := instance.kernel.ServiceContainer().RegisterScoped(serviceName, provider, options...)
    if nil == registerScopedErr {
        return nil
    }

    if true == errors.Is(registerScopedErr, container.ErrScopedServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedService, serviceName)
        return nil
    }

    if true == errors.Is(registerScopedErr, container.ErrScopedServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedServiceType, serviceName)
        return nil
    }

    if true == errors.Is(registerScopedErr, container.ErrServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedService, serviceName)
        return nil
    }

    if true == errors.Is(registerScopedErr, container.ErrServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedServiceType, serviceName)
        return nil
    }

    return registerScopedErr
}

func (instance *Application) MustRegisterScoped(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    registerScopedErr := instance.RegisterScoped(serviceName, provider, options...)
    if nil != registerScopedErr {
        exception.Panic(exception.FromError(registerScopedErr))
    }
}

func (instance *Application) bootContainer() {
    kernelInstance := instance.kernel
    configuration := instance.configuration

    serviceContainer := kernelInstance.ServiceContainer()

    if false == serviceContainer.Has(logging.ServiceLogger) {
        instance.RegisterService(
            logging.ServiceLogger,
            func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
                return newContainerLogger(
                    resolveRuntimePath(configuration.Kernel().ProjectDir(), configuration.Kernel().LogPath()),
                    configuration.Kernel().LogLevel(),
                    instance.moduleConfigurations,
                    kernelInstance.Clock(),
                    config.ModeHttp == instance.runtimeFlags.Mode(),
                ), nil
            },
        )
    }

    instance.RegisterService(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
    )

    instance.RegisterService(
        ServiceProcessRole,
        func(resolver containercontract.Resolver) (string, error) {
            return instance.runtimeFlags.Role(), nil
        },
    )

    instance.RegisterService(
        http.ServiceRouteRegistry,
        func(resolver containercontract.Resolver) (httpcontract.RouteRegistry, error) {
            return instance.routeRegistry, nil
        },
    )

    if false == serviceContainer.Has(http.ServiceUrlGenerator) {
        instance.RegisterService(
            http.ServiceUrlGenerator,
            func(resolver containercontract.Resolver) (httpcontract.UrlGenerator, error) {
                return http.NewUrlGenerator(instance.routeRegistry), nil
            },
        )
    }

    instance.RegisterService(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return kernelInstance.HttpRouter(), nil
        },
    )

    instance.RegisterService(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return kernelInstance.EventDispatcher(), nil
        },
    )

    if false == serviceContainer.Has(serializer.ServiceSerializerManager) {
        instance.RegisterService(
            serializer.ServiceSerializerManager,
            func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
                return serializer.NewSerializerManager(
                    map[string]serializercontract.Serializer{
                        "application/json": serializer.NewJsonSerializer(),
                        "text/plain":       serializer.NewPlainTextSerializer(),
                    },
                )
            },
        )
    }

    if false == serviceContainer.Has(serializer.ServiceSerializer) {
        instance.RegisterService(
            serializer.ServiceSerializer,
            func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
                return serializer.NewJsonSerializer(), nil
            },
        )
    }

    if false == serviceContainer.Has(validation.ServiceValidator) {
        instance.RegisterService(
            validation.ServiceValidator,
            func(resolver containercontract.Resolver) (*validation.Validator, error) {
                return validation.NewValidator(), nil
            },
        )
    }

    instance.RegisterService(
        clock.ServiceClock,
        func(resolver containercontract.Resolver) (clockcontract.Clock, error) {
            return kernelInstance.Clock(), nil
        },
    )

    instance.registerCache()

    instance.registerHttpSession()

    securityErr := instance.registerSecurity()
    if nil != securityErr {
        exception.Panic(exception.FromError(securityErr))
    }

    _, loggerProbeErr := logging.LoggerFromContainer(serviceContainer)
    if nil != loggerProbeErr {
        exception.Panic(
            exception.NewError(
                "the configured logger cannot be built",
                nil,
                loggerProbeErr,
            ),
        )
    }
}

func newContainerLogger(
    logPath string,
    logLevel loggingcontract.Level,
    moduleConfigurations map[string]any,
    clockInstance clockcontract.Clock,
    armRotationReopen bool,
) loggingcontract.Logger {
    loggingConfigurationInstance := logging.LoggingConfigurationFromModules(moduleConfigurations)

    var writer io.Writer = os.Stdout

    if "" != logPath {

        mkdirErr := os.MkdirAll(filepath.Dir(logPath), 0o755)
        if nil != mkdirErr {
            exception.Panic(
                exception.NewError(
                    "failed to create the log directory",
                    exceptioncontract.Context{
                        "path": logPath,
                    },
                    mkdirErr,
                ),
            )
        }

        fileWriter, openFileErr := logging.NewReopenableFileWriter(logPath)
        if nil != openFileErr {
            exception.Panic(
                exception.NewError(
                    "failed to open log file",
                    exceptioncontract.Context{
                        "path": logPath,
                    },
                    openFileErr,
                ),
            )
        }

        if true == armRotationReopen {
            armErr := fileWriter.ArmReopenOnSignal(syscall.SIGHUP)
            if nil != armErr {

                _ = fileWriter.Close()

                exception.Panic(
                    exception.NewError(
                        "failed to arm the log rotation signal watcher",
                        exceptioncontract.Context{
                            "path": logPath,
                        },
                        armErr,
                    ),
                )
            }
        }

        writer = fileWriter
    }

    return logging.NewJsonLoggerWithClock(writer, logLevel, loggingConfigurationInstance.LevelLabels(), clockInstance)
}

func (instance *Application) registerCache() {
    serviceContainer := instance.kernel.ServiceContainer()

    if false == serviceContainer.Has(cache.ServiceCacheSerializer) {
        instance.RegisterService(
            cache.ServiceCacheSerializer,
            func(resolver containercontract.Resolver) (cachecontract.Serializer, error) {
                return cache.NewJsonSerializer(), nil
            },
        )
    }

    if false == serviceContainer.Has(cache.ServiceCacheBackend) {

        instance.unboundedDefaultCacheBackend = true

        instance.RegisterService(
            cache.ServiceCacheBackend,
            func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
                clockInstance := clock.ClockMustFromResolver(resolver)

                return cache.NewInMemoryBackend(
                    0,
                    0,
                    clockInstance,
                ), nil
            },
        )
    }

    if false == serviceContainer.Has(cache.ServiceCache) {
        instance.RegisterService(
            cache.ServiceCache,
            func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
                backend := cache.CacheBackendMustFromResolver(resolver)
                serializerInstance := cache.CacheSerializerMustFromResolver(resolver)

                return cache.NewManager(
                    backend,
                    serializerInstance,
                ), nil
            },
        )
    }
}

func (instance *Application) registerHttpSession() {
    serviceContainer := instance.kernel.ServiceContainer()

    if false == serviceContainer.Has(session.ServiceSessionStorage) {

        instance.defaultInMemorySessionStorage = true

        instance.RegisterService(
            session.ServiceSessionStorage,
            func(resolver containercontract.Resolver) (sessioncontract.Storage, error) {
                return session.NewInMemoryStorageWithClock(time.Minute, instance.kernel.Clock()), nil
            },
        )
    }

    if false == serviceContainer.Has(session.ServiceSessionManager) {
        instance.RegisterService(
            session.ServiceSessionManager,
            func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
                storage := session.SessionStorageMustFromResolver(resolver)

                return session.NewManagerWithClock(
                    storage,
                    instance.configuration.Http().SessionTtl(),
                    instance.configuration.Http().SessionTombstoneRetention(),
                    instance.kernel.Clock(),
                ), nil
            },
        )
    }
}

func (instance *Application) registerSecurity() error {
    if nil == instance.securityConfiguration {
        return nil
    }

    serviceContainer := instance.kernel.ServiceContainer()

    if false == serviceContainer.Has(security.ServiceFirewallManager) {
        instance.RegisterService(
            security.ServiceFirewallManager,
            func(resolver containercontract.Resolver) (securitycontract.FirewallManager, error) {
                return security.NewFirewallManager(instance.securityConfiguration), nil
            },
        )
    }

    if config.ModeHttp != instance.runtimeFlags.Mode() {
        return nil
    }

    registry := security.NewFirewallRegistry(instance.securityConfiguration)

    kernelInstance := instance.kernel

    security.RegisterKernelSecurityResolutionListener(kernelInstance, registry)
    security.RegisterKernelAccessControlListener(kernelInstance, registry)

    return nil
}

func (instance *Application) buildMessageBusTransportsCloser() {
    serviceContainer := instance.kernel.ServiceContainer()

    if false == serviceContainer.Has(messagebus.ServiceTransportsCloser) {
        return
    }

    _, closerErr := container.FromResolver[*messagebus.TransportsCloser](
        serviceContainer,
        messagebus.ServiceTransportsCloser,
    )
    if nil != closerErr {
        instance.bootLogger().Warning(
            "could not build the message bus transports closer; the registered transports will not be closed on shutdown",
            exceptioncontract.Context{
                "serviceName": messagebus.ServiceTransportsCloser,
                "error":       closerErr.Error(),
            },
        )
    }
}

var _ applicationcontract.ServiceRegistrar = (*Application)(nil)
var _ applicationcontract.ScopedServiceRegistrar = (*Application)(nil)
