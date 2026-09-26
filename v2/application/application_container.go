package application

import (
    "errors"
    "os"
    "path/filepath"

    applicationcontract "github.com/precision-soft/melody/v2/application/contract"
    "github.com/precision-soft/melody/v2/cache"
    cachecontract "github.com/precision-soft/melody/v2/cache/contract"
    "github.com/precision-soft/melody/v2/clock"
    clockcontract "github.com/precision-soft/melody/v2/clock/contract"
    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/precision-soft/melody/v2/security"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
    "github.com/precision-soft/melody/v2/serializer"
    serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
    "github.com/precision-soft/melody/v2/session"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
    "github.com/precision-soft/melody/v2/validation"
)

func (instance *Application) RegisterService(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register services after boot", nil, nil))
    }

    registerErr := instance.kernel.ServiceContainer().Register(serviceName, provider, options...)
    if nil == registerErr {
        return
    }

    /* duplicates are recorded for the aggregated boot report, the first registration winning until the report ends the boot; any other registration failure stays fail-fast */
    if true == errors.Is(registerErr, container.ErrServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindService, serviceName)
        return
    }

    if true == errors.Is(registerErr, container.ErrServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindServiceType, serviceName)
        return
    }

    /* the name or type is already claimed at the scoped lifetime, the same wiring mistake from the other side, so it joins the same report */
    if true == errors.Is(registerErr, container.ErrScopedServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedService, serviceName)
        return
    }

    if true == errors.Is(registerErr, container.ErrScopedServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedServiceType, serviceName)
        return
    }

    exception.Panic(exception.FromError(registerErr))
}

/* RegisterScopedService declares a service the application's scopes own: one instance per scope, one http request or one command run, closed with it. It mirrors RegisterService in everything but lifetime; a name claimed at both lifetimes joins the aggregated boot report. In console the run's scope spans the whole command, so a long-running command that processes many units creates a child runtime per unit, as the cron runner does: a scope from Container().NewScope(), a runtime.New over it, and a Close whose error joins the unit's own. */
func (instance *Application) RegisterScopedService(
    serviceName string,
    provider any,
    options ...containercontract.RegisterOption,
) {
    if true == instance.booted {
        exception.Panic(exception.NewError("may not register scoped services after boot", nil, nil))
    }

    registerScopedErr := instance.kernel.ServiceContainer().RegisterScoped(serviceName, provider, options...)
    if nil == registerScopedErr {
        return
    }

    if true == errors.Is(registerScopedErr, container.ErrScopedServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedService, serviceName)
        return
    }

    if true == errors.Is(registerScopedErr, container.ErrScopedServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedServiceType, serviceName)
        return
    }

    if true == errors.Is(registerScopedErr, container.ErrServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedService, serviceName)
        return
    }

    if true == errors.Is(registerScopedErr, container.ErrServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindScopedServiceType, serviceName)
        return
    }

    exception.Panic(exception.FromError(registerScopedErr))
}

func (instance *Application) bootContainer() {
    kernelInstance := instance.kernel
    configuration := instance.configuration

    serviceContainer := kernelInstance.ServiceContainer()

    /* gated like the cache, session and firewall registrations below, so the application or a module can substitute the logger */
    if false == serviceContainer.Has(logging.ServiceLogger) {
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

    /* the router, the dispatcher, the clock, the config, the route registry and the process role are not gated: the kernel owns them and reads them directly, so a substitute would be ignored. The gates stand where a replacement built outside is a whole answer. */
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

    /* gated so a module can substitute the manager content negotiation reads, to add a media type; NewSerializerManager takes the map, so a replacement built outside is a whole answer */
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

    /* the default serializer SerializerFromRuntime and SerializerMustFromRuntime answer; content negotiation runs through the manager above, so replacing it changes what those two resolvers answer, not what a request is served */
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

    /* the logger is resolved once, eagerly, whoever registered it, so a failing provider fails the boot step that owns it instead of the first run that resolves it; the container memoizes the built logger, so the log file is opened once */
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

/* newContainerLogger builds the logger the container serves. The module configuration is read before the descriptor is opened, because it panics on a configuration of the wrong type and a file opened before it would have no owner: the container stores only what a provider returns and does not memoize a creation failure. */
func newContainerLogger(
    logPath string,
    logLevel loggingcontract.Level,
    moduleConfigurations map[string]any,
) loggingcontract.Logger {
    loggingConfigurationInstance := logging.LoggingConfigurationFromModules(moduleConfigurations)

    writer := os.Stdout

    if "" != logPath {
        /* the parent directory is created as ensureRuntimeDirectories creates the logs directory, since MELODY_LOG_PATH may point elsewhere */
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
        /* the fallback backend is left without an item ceiling or an expiry, since either would drop the application's entries behind its back; its cost is reported on the http path as a warning */
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
        /* the fallback storage keeps its entries in this process, where nothing outside it can expire them; its cost is reported on the http path as a warning, since a lifetime chosen here would end sessions the application never agreed to end */
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

                return session.NewManagerWithTombstoneRetention(
                    storage,
                    instance.configuration.Http().SessionTtl(),
                    instance.configuration.Http().SessionTombstoneRetention(),
                ), nil
            },
        )
    }
}

/* registerSecurity wires what a compiled security configuration means for this process. The firewall manager is registered in every mode, since it is a plain view of the configuration; the two kernel listeners are the enforcement and stay http-only. */
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

var _ applicationcontract.ServiceRegistrar = (*Application)(nil)
var _ applicationcontract.ScopedServiceRegistrar = (*Application)(nil)
