package application

import (
    "errors"
    "os"
    "path/filepath"

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

    /* duplicates are recorded for the aggregated boot report instead of panicking one at a time (the first registration wins until the guaranteed panic ends the boot); any other registration failure stays fail-fast */
    if true == errors.Is(registerErr, container.ErrServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindService, serviceName)
        return nil
    }

    if true == errors.Is(registerErr, container.ErrServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindServiceType, serviceName)
        return nil
    }

    /* the name — or the type — is already claimed at the scoped lifetime. The collision is the same wiring mistake reported from the other side, so it joins the same report rather than ending the boot on its own */
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

    instance.RegisterService(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return newContainerLogger(
                resolveRuntimePath(configuration.Kernel().ProjectDir(), configuration.Kernel().LogPath()),
                configuration.Kernel().LogLevel(),
                instance.moduleConfigurations,
            ), nil
        },
    )

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

    instance.RegisterService(
        http.ServiceUrlGenerator,
        func(resolver containercontract.Resolver) (httpcontract.UrlGenerator, error) {
            return http.NewUrlGenerator(instance.routeRegistry), nil
        },
    )

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

    instance.RegisterService(
        validation.ServiceValidator,
        func(resolver containercontract.Resolver) (*validation.Validator, error) {
            return validation.NewValidator(), nil
        },
    )

    instance.RegisterService(
        clock.ServiceClock,
        func(resolver containercontract.Resolver) (clockcontract.Clock, error) {
            return kernelInstance.Clock(), nil
        },
    )

    instance.registerCache()

    instance.registerHttpSession()

    httpSecurityErr := instance.registerHttpSecurity()
    if nil != httpSecurityErr {
        exception.Panic(exception.FromError(httpSecurityErr))
    }
}

/* newContainerLogger builds the logger the container serves. The module configuration is read before the descriptor is acquired, because it panics on a configuration registered under the supported name with a type that does not implement the interface: a file opened above that would be left with no owner at all — the container stores only what a provider returned, so nothing would ever close it, and a creation failure is not memoized, so each later resolution opens another one. */
func newContainerLogger(
    logPath string,
    logLevel loggingcontract.Level,
    moduleConfigurations map[string]any,
) loggingcontract.Logger {
    loggingConfigurationInstance := logging.LoggingConfigurationFromModules(moduleConfigurations)

    writer := os.Stdout

    if "" != logPath {
        /* the directory is guaranteed the way ensureRuntimeDirectories guarantees the logs directory: only the default log path lives inside it, and an operator-supplied MELODY_LOG_PATH pointing anywhere else had no owner to create its parent */
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

        file, openFileErr := os.OpenFile(
            logPath,
            os.O_CREATE|os.O_APPEND|os.O_WRONLY,
            0o644,
        )
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

        writer = file
    }

    return logging.NewJsonLoggerWithLabels(writer, logLevel, loggingConfigurationInstance.LevelLabels())
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
        /* the fallback backend is deliberately left unarmed in both dimensions — an item ceiling melody picked would evict an application's entries behind its back, and an expiry melody picked would drop them early — so what it costs is carried to the http path as a warning instead of being decided here */
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
        /* the fallback storage keeps its entries in this process and nothing outside it can expire them, so what it costs is carried to the http path as a warning rather than being decided here — the same shape the fallback cache backend uses, and for the same reason: a lifetime melody picked would end sessions the application never agreed to end */
        instance.defaultInMemorySessionStorage = true

        instance.RegisterService(
            session.ServiceSessionStorage,
            func(resolver containercontract.Resolver) (sessioncontract.Storage, error) {
                return session.NewInMemoryStorage(), nil
            },
        )
    }

    if false == serviceContainer.Has(session.ServiceSessionManager) {
        instance.RegisterService(
            session.ServiceSessionManager,
            func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
                storage := session.SessionStorageMustFromResolver(resolver)

                return session.NewManager(storage, instance.configuration.Http().SessionTtl()), nil
            },
        )
    }
}

func (instance *Application) registerHttpSecurity() error {
    if config.ModeHttp != instance.runtimeFlags.Mode() {
        return nil
    }

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

    registry := security.NewFirewallRegistry(instance.securityConfiguration)

    kernelInstance := instance.kernel

    security.RegisterKernelSecurityResolutionListener(kernelInstance, registry)
    security.RegisterKernelAccessControlListener(kernelInstance, registry)

    return nil
}

var _ applicationcontract.ServiceRegistrar = (*Application)(nil)
var _ applicationcontract.ScopedServiceRegistrar = (*Application)(nil)
