package application

import (
    "errors"
    "io"
    "os"
    "path/filepath"
    "syscall"

    applicationcontract "github.com/precision-soft/melody/v4/application/contract"
    "github.com/precision-soft/melody/v4/cache"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/event"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/precision-soft/melody/v4/messagebus"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/serializer"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
    "github.com/precision-soft/melody/v4/validation"
)

func (instance *Application) RegisterService(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
) {
    instance.MustRegister(serviceName, provider, options)
}

/* Register makes the application a container registrar; a duplicate joins the aggregated boot report rather than being returned. */
func (instance *Application) Register(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
) error {
    if true == instance.registrationClosed() {
        exception.Panic(exception.NewError("may not register services after boot", exceptioncontract.Context{"serviceName": serviceName}, nil))
    }

    registerErr := instance.kernel.ServiceContainer().Register(serviceName, provider, options)
    if nil == registerErr {
        return nil
    }

    /* a duplicate joins the aggregated boot report; any other registration failure stays fail-fast */
    if true == errors.Is(registerErr, container.ErrServiceIdAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindService, serviceName)
        return nil
    }

    if true == errors.Is(registerErr, container.ErrServiceTypeAlreadyRegistered) {
        instance.recordBootCollision(bootCollisionKindServiceType, serviceName)
        return nil
    }

    /* a name claimed at the scoped lifetime joins the same report */
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
    options containercontract.RegisterOptions,
) {
    registerErr := instance.Register(serviceName, provider, options)
    if nil != registerErr {
        exception.Panic(exception.FromError(registerErr))
    }
}

/* ReservePhaseService reserves an application's own name for the phase that installs it; the framework's phase names are reserved by the application itself and refused here. */
func (instance *Application) ReservePhaseService(serviceName string, kind containercontract.Kind) error {
    if http.ServiceRequestContext == serviceName || securitycontract.ServiceSecurityContext == serviceName || ServiceProcessContext == serviceName {
        return exception.NewError("the framework reserves this phase service", exceptioncontract.Context{"serviceName": serviceName}, nil)
    }

    return instance.kernel.ServiceContainer().ReservePhaseService(serviceName, kind)
}

/* RegisterScopedService declares a service the application's scopes own, one instance per http request or command run, closed with it. */
func (instance *Application) RegisterScopedService(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
) {
    instance.MustRegisterScoped(serviceName, provider, options)
}

/* RegisterScoped declares a service the application's scopes own, one instance per http request or command run, closed with it. */
func (instance *Application) RegisterScoped(
    serviceName string,
    provider any,
    options containercontract.RegisterOptions,
) error {
    if true == instance.registrationClosed() {
        exception.Panic(exception.NewError("may not register scoped services after boot", exceptioncontract.Context{"serviceName": serviceName}, nil))
    }

    registerScopedErr := instance.kernel.ServiceContainer().RegisterScoped(serviceName, provider, options)
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
    options containercontract.RegisterOptions,
) {
    registerScopedErr := instance.RegisterScoped(serviceName, provider, options)
    if nil != registerScopedErr {
        exception.Panic(exception.FromError(registerScopedErr))
    }
}

func (instance *Application) buildServicesBuiltAtBoot(bootRuntime containercontract.Runtime) {
    lister, lists := instance.kernel.ServiceContainer().(interface{ BuiltAtBootServiceNames() []string })
    if false == lists {
        return
    }

    for _, serviceName := range lister.BuiltAtBootServiceNames() {
        if _, buildErr := runtime.FromRuntime[any](bootRuntime, serviceName); nil != buildErr {
            exception.Panic(
                exception.NewError("a service built at boot cannot be built", exceptioncontract.Context{"serviceName": serviceName}, buildErr),
            )
        }
    }
}

/* the names a phase installs into its scopes: a scope of another kind, the boot's among them, refuses them by name */
func (instance *Application) reservePhaseServices() {
    serviceContainer := instance.kernel.ServiceContainer()

    for serviceName, kind := range map[string]containercontract.Kind{
        http.ServiceRequestContext:              containercontract.KindRequest,
        securitycontract.ServiceSecurityContext: containercontract.KindRequest,
        ServiceProcessContext:                   containercontract.KindRun,
    } {
        if reserveErr := serviceContainer.ReservePhaseService(serviceName, kind); nil != reserveErr {
            exception.Panic(exception.NewError("could not reserve a phase service", exceptioncontract.Context{"serviceName": serviceName}, reserveErr))
        }
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
                resolutionContext := resolver.Runtime().Context()
                resolvedConfiguration := config.ConfigMustFromResolver(resolutionContext, resolver)

                return newContainerLogger(
                    resolveRuntimePath(resolvedConfiguration.Kernel().ProjectDirectory(), resolvedConfiguration.Kernel().LogPath()),
                    resolvedConfiguration.Kernel().LogLevel(),
                    instance.moduleConfigurations,
                    clock.ClockMustFromResolver(resolutionContext, resolver),
                    true,
                    consoleLogWriter(instance.runtimeFlags.Mode()),
                ), nil
            },
            container.RegisterOptions{},
        )
    }

    instance.RegisterService(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return configuration, nil
        },
        container.RegisterOptions{},
    )

    instance.RegisterService(
        ServiceProcessRole,
        func(resolver containercontract.Resolver) (string, error) {
            return instance.runtimeFlags.Role(), nil
        },
        container.RegisterOptions{},
    )

    instance.RegisterService(
        http.ServiceRouteRegistry,
        func(resolver containercontract.Resolver) (httpcontract.RouteRegistry, error) {
            return instance.routeRegistry, nil
        },
        container.RegisterOptions{},
    )

    if false == serviceContainer.Has(http.ServiceUrlGenerator) {
        instance.RegisterService(
            http.ServiceUrlGenerator,
            func(resolver containercontract.Resolver) (httpcontract.UrlGenerator, error) {
                return http.NewUrlGenerator(
                    container.MustFromResolver[httpcontract.RouteRegistry](resolver.Runtime().Context(), resolver, http.ServiceRouteRegistry),
                    http.UrlGeneratorConfig{PublicUrl: instance.httpPublicUrl},
                ), nil
            },
            container.RegisterOptions{},
        )
    }

    /* not gated: the kernel reads these directly, so a substitute would be ignored */

    instance.RegisterService(
        http.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return kernelInstance.HttpRouter(), nil
        },
        container.RegisterOptions{},
    )

    instance.RegisterService(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return kernelInstance.EventDispatcher(), nil
        },
        container.RegisterOptions{},
    )

    if false == serviceContainer.Has(serializer.ServiceSerializerManager) {
        instance.RegisterService(
            serializer.ServiceSerializerManager,
            func(resolver containercontract.Resolver) (*serializer.SerializerManager, error) {
                return serializer.NewSerializerManager(
                    map[string]serializercontract.Serializer{
                        "application/json": serializer.NewJsonSerializer(),
                        "text/plain":       serializer.NewPlainTextSerializer(serializer.PlainTextSerializerConfig{}),
                    },
                )
            },
            container.RegisterOptions{},
        )
    }

    if false == serviceContainer.Has(serializer.ServiceSerializer) {
        instance.RegisterService(
            serializer.ServiceSerializer,
            func(resolver containercontract.Resolver) (serializercontract.Serializer, error) {
                return serializer.NewJsonSerializer(), nil
            },
            container.RegisterOptions{},
        )
    }

    if false == serviceContainer.Has(validation.ServiceValidator) {
        instance.RegisterService(
            validation.ServiceValidator,
            func(resolver containercontract.Resolver) (*validation.Validator, error) {
                return validation.NewValidator(validation.ValidatorConfig{}), nil
            },
            container.RegisterOptions{},
        )
    }

    instance.RegisterService(
        clock.ServiceClock,
        func(resolver containercontract.Resolver) (clockcontract.Clock, error) {
            return kernelInstance.Clock(), nil
        },
        container.RegisterOptions{},
    )

    instance.registerCache()

    instance.registerHttpSession()

    instance.registerSecurity()

    /* resolved eagerly so a failing provider fails the boot step that owns it */
    bootRuntime := instance.kernel.BootRuntime()
    _, loggerProbeErr := runtime.FromRuntime[loggingcontract.Logger](bootRuntime, logging.ServiceLogger)
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

/* a command logs on standard error, since its standard output carries what it prints */
func consoleLogWriter(mode string) io.Writer {
    if config.ModeCli == mode {
        return os.Stderr
    }

    return os.Stdout
}

/* the configuration is read before the file is opened, since it panics and an opened file would have no owner */
func newContainerLogger(
    logPath string,
    logLevel loggingcontract.Level,
    moduleConfigurations map[string]any,
    clockInstance clockcontract.Clock,
    armRotationReopen bool,
    consoleWriter io.Writer,
) loggingcontract.Logger {
    loggingConfigurationInstance := logging.LoggingConfigurationFromModules(moduleConfigurations)

    writer := consoleWriter

    if "" != logPath {
        mkdirErr := os.MkdirAll(filepath.Dir(logPath), 0o755)
        if nil != mkdirErr {
            exception.Panic(
                exception.NewError(
                    "failed to create the log directory",
                    exceptioncontract.Context{
                        "path":      logPath,
                        "directory": filepath.Dir(logPath),
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
            armErr := fileWriter.ArmReopenOnSignal([]os.Signal{syscall.SIGHUP})
            if nil != armErr {
                /* closed before the panic: the container does not memoize a creation failure, so each resolution would open another file */
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

    return logging.NewJsonLogger(
        writer,
        logging.JsonLoggerConfig{
            MinLevel: logLevel,
            Labels:   loggingConfigurationInstance.LevelLabels(),
            Clock:    clockInstance,
        },
    )
}

func (instance *Application) registerCache() {
    serviceContainer := instance.kernel.ServiceContainer()

    if false == serviceContainer.Has(cache.ServiceCacheSerializer) {
        instance.RegisterService(
            cache.ServiceCacheSerializer,
            func(resolver containercontract.Resolver) (cachecontract.Serializer, error) {
                return cache.NewJsonSerializer(), nil
            },
            container.RegisterOptions{},
        )
    }

    if false == serviceContainer.Has(cache.ServiceCacheBackend) {
        instance.RegisterService(
            cache.ServiceCacheBackend,
            func(resolver containercontract.Resolver) (cachecontract.Backend, error) {
                clockInstance := clock.ClockMustFromResolver(resolver.Runtime().Context(), resolver)

                return cache.NewInMemoryBackend(
                    cache.InMemoryBackendConfig{
                        Clock: clockInstance,
                    },
                ), nil
            },
            container.RegisterOptions{},
        )
    }

    if false == serviceContainer.Has(cache.ServiceCache) {
        instance.RegisterService(
            cache.ServiceCache,
            func(resolver containercontract.Resolver) (cachecontract.Cache, error) {
                backend := cache.CacheBackendMustFromResolver(resolver.Runtime().Context(), resolver)
                serializerInstance := cache.CacheSerializerMustFromResolver(resolver.Runtime().Context(), resolver)

                return cache.NewManager(
                    backend,
                    serializerInstance,
                    cache.ManagerConfig{},
                ), nil
            },
            container.RegisterOptions{},
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
                return session.NewInMemoryStorage(
                    session.InMemoryStorageConfig{
                        MaxSessions: instance.configuration.Http().SessionMaxSessions().Limit(),
                        Clock:       clock.ClockMustFromResolver(resolver.Runtime().Context(), resolver),
                    },
                ), nil
            },
            container.RegisterOptions{},
        )
    }

    if false == serviceContainer.Has(session.ServiceSessionManager) {
        instance.RegisterService(
            session.ServiceSessionManager,
            func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
                resolutionContext := resolver.Runtime().Context()
                storage := session.SessionStorageMustFromResolver(resolutionContext, resolver)
                resolvedConfiguration := config.ConfigMustFromResolver(resolutionContext, resolver)

                /* the clock the storage resolves: the tombstone record and the entry expiry are two halves of one lifetime */
                return session.NewManager(
                    storage,
                    session.ManagerConfig{
                        Ttl:                resolvedConfiguration.Http().SessionTtl().Limit(),
                        TombstoneRetention: resolvedConfiguration.Http().SessionTombstoneRetention().Limit(),
                        StorageTimeout:     resolvedConfiguration.Http().SessionStorageTimeout().Limit(),
                        Clock:              clock.ClockMustFromResolver(resolutionContext, resolver),
                    },
                ), nil
            },
            container.RegisterOptions{},
        )
    }
}

/* the firewall manager is registered in every mode; the two kernel listeners are the enforcement and stay http-only */
func (instance *Application) registerSecurity() {
    if nil == instance.securityConfiguration {
        return
    }

    serviceContainer := instance.kernel.ServiceContainer()

    if false == serviceContainer.Has(security.ServiceFirewallManager) {
        instance.RegisterService(
            security.ServiceFirewallManager,
            func(resolver containercontract.Resolver) (securitycontract.FirewallManager, error) {
                return security.NewFirewallManager(instance.securityConfiguration), nil
            },
            container.RegisterOptions{},
        )
    }

    if config.ModeHttp != instance.runtimeFlags.Mode() {
        return
    }

    registry := security.NewFirewallRegistry(instance.securityConfiguration)

    kernelInstance := instance.kernel

    security.RegisterKernelSecurityResolutionListener(kernelInstance, registry)
    security.RegisterKernelAccessControlListener(kernelInstance, registry)

}

/* verifyDeclaredTeardownDependencies refuses the boot when a declared teardown dependency names no single container service. */
func (instance *Application) verifyDeclaredTeardownDependencies() {
    verifier, verifies := instance.kernel.ServiceContainer().(interface {
        VerifyDeclaredTeardownDependencies() error
    })
    if false == verifies {
        return
    }

    if verifyErr := verifier.VerifyDeclaredTeardownDependencies(); nil != verifyErr {
        exception.Panic(exception.NewError("a declared teardown dependency does not name one container service", nil, verifyErr))
    }
}

func (instance *Application) verifyLazyHandles() {
    verifier, verifies := instance.kernel.ServiceContainer().(interface {
        VerifyLazyNames() error
    })
    if false == verifies {
        return
    }

    if verifyErr := verifier.VerifyLazyNames(); nil != verifyErr {
        exception.Panic(exception.NewError("a container.Lazy handle names no container service", nil, verifyErr))
    }
}

/* a bus routes through transport values its module captured, which no resolution records, so the teardown edge is declared */
func (instance *Application) buildMessageBusTransportsCloser() {
    serviceContainer := instance.kernel.ServiceContainer()

    if false == serviceContainer.Has(messagebus.ServiceTransportsCloser) {
        return
    }

    if declarer, declares := serviceContainer.(interface {
        DeclareTeardownDependency(serviceName string, dependencyNames []string) error
    }); true == declares {
        for _, busName := range []string{messagebus.ServiceBus, messagebus.ServiceConsumeBus} {
            if false == serviceContainer.Has(busName) {
                continue
            }

            declareErr := declarer.DeclareTeardownDependency(busName, []string{messagebus.ServiceTransportsCloser})
            if nil != declareErr {
                exception.Panic(
                    exception.NewError(
                        "could not order the message bus before its transports",
                        exceptioncontract.Context{"serviceName": busName},
                        declareErr,
                    ),
                )
            }
        }
    }

    bootRuntime := instance.kernel.BootRuntime()
    _, closerErr := runtime.FromRuntime[*messagebus.TransportsCloser](bootRuntime, messagebus.ServiceTransportsCloser,
    )
    if nil != closerErr {
        exception.Panic(
            exception.NewError(
                "could not build the message bus transports closer",
                exceptioncontract.Context{"serviceName": messagebus.ServiceTransportsCloser},
                closerErr,
            ),
        )
    }
}

var _ applicationcontract.ServiceRegistrar = (*Application)(nil)
var _ applicationcontract.ScopedServiceRegistrar = (*Application)(nil)
