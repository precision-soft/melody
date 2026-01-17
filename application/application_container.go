package application

import (
	"os"

	"github.com/precision-soft/melody/cache"
	cachecontract "github.com/precision-soft/melody/cache/contract"
	"github.com/precision-soft/melody/clock"
	clockcontract "github.com/precision-soft/melody/clock/contract"
	"github.com/precision-soft/melody/config"
	configcontract "github.com/precision-soft/melody/config/contract"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/event"
	eventcontract "github.com/precision-soft/melody/event/contract"
	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	"github.com/precision-soft/melody/http"
	httpcontract "github.com/precision-soft/melody/http/contract"
	"github.com/precision-soft/melody/logging"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
	"github.com/precision-soft/melody/security"
	securitycontract "github.com/precision-soft/melody/security/contract"
	"github.com/precision-soft/melody/serializer"
	serializercontract "github.com/precision-soft/melody/serializer/contract"
	"github.com/precision-soft/melody/session"
	sessioncontract "github.com/precision-soft/melody/session/contract"
	"github.com/precision-soft/melody/validation"
)

func (instance *Application) RegisterService(
	serviceName string,
	provider any,
	options ...containercontract.RegisterOption,
) {
	if true == instance.booted {
		exception.Panic(exception.NewError("may not register services after boot", nil, nil))
	}

	instance.kernel.ServiceContainer().MustRegister(serviceName, provider, options...)
}

func (instance *Application) bootContainer() {
	kernelInstance := instance.kernel
	configuration := instance.configuration

	instance.RegisterService(
		logging.ServiceLogger,
		func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
			writer := os.Stdout

			logPath := configuration.Kernel().LogPath()

			if "" != logPath {
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

			return logging.NewJsonLogger(writer, configuration.Kernel().LogLevel()), nil
		},
	)

	instance.RegisterService(
		config.ServiceConfig,
		func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
			return configuration, nil
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

				return session.NewManager(storage, 0), nil
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
