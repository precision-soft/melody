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

    /* duplicates are recorded for the aggregated boot report instead of panicking one at a time (the first registration wins until the guaranteed panic ends the boot); any other registration failure stays fail-fast */
    if true == errors.Is(registerErr, container.ErrServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindService, serviceName)
        return
    }

    if true == errors.Is(registerErr, container.ErrServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindServiceType, serviceName)
        return
    }

    /* the name — or the type — is already claimed at the scoped lifetime. The collision is the same wiring mistake reported from the other side, so it joins the same report rather than ending the boot on its own */
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

/* RegisterScopedService declares a service the application's scopes own: one instance per scope — one http request, one command run — closed with it. It mirrors RegisterService in everything but lifetime, collisions included — a name claimed at both lifetimes is absorbed into the aggregated boot report, so a module that scopes a name the framework registers later hears about it beside every other collision instead of one panic per boot attempt.

In console the run's scope spans the whole command, so for a one-shot command "scoped" and "per run" are the same thing — but a long-running command that processes many units of work holds one scope for all of them, and a scoped transaction or identity quietly becomes a process singleton. Such a command creates a child runtime per unit, the way the cron runner does around each scheduled run: a fresh scope from Container().NewScope(), a runtime.New over it, and a Close whose error is joined onto the unit's own when the unit ends. */
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

    /* gated like the cache, session and firewall registrations below: the logger was the one default service the application or a module could never substitute, because its second registration was a guaranteed boot collision */
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

    /* the router, the dispatcher, the clock, the config, the route registry and the process role are deliberately NOT gated: they hand back objects the kernel owns and the request path reads directly, so a gate would promise a substitution the framework would then ignore — the honest answer for those is the document, not a door that lies. The gates below stand where a replacement built outside is a whole answer: the logger, the cache, the session, the firewall manager, and the three the boot used to make unsubstitutable. */
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

    /* the manager is the door content negotiation reads, and it was registered unconditionally: a module registering the same id to add a media type — xml, msgpack, cbor, application/vnd.api+json — got the boot's duplicate-registration exit, code 1, instead of a substitution, so the whole negotiation was closed to everything but a fork. NewSerializerManager is exported and takes the map, so a replacement built outside is a whole answer */
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

    /* the default serializer answers the two documented resolvers, SerializerFromRuntime and SerializerMustFromRuntime, which named an id nothing ever registered: the Must door panicked for every caller and the soft one answered nil, both by construction. This is the default serializer alone — content negotiation runs through the manager above, and registering another serializer here changes what those two resolvers answer, not what a request is served */
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

    /* the logger is resolved once, eagerly, whoever registered it: its provider is the one whose failure bootLogger otherwise swallows — the container recovers the provider panic into an error, the fallback answers the emergency logger, and the boot reported success for a process whose next logger resolution could only panic, attributed to the run instead of to the configuration that broke it. Failing here names the boot step that owns the failure, and the container memoizes the built logger, so nothing opens the log file a second time. */
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

                return session.NewManagerWithTombstoneRetention(
                    storage,
                    instance.configuration.Http().SessionTtl(),
                    instance.configuration.Http().SessionTombstoneRetention(),
                ), nil
            },
        )
    }
}

/* registerSecurity wires what a compiled security configuration means for this process. The firewall manager is registered whatever the mode: it is a plain view of the compiled configuration with no request in it, and gating it on the mode sent a console process asking for it to a "service is not registered" panic that reads as a wiring mistake when the actual difference was the process shape — configured means resolvable. The two kernel listeners remain http-only: they are the enforcement, they listen for requests, and a console process has no request to guard. */
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
