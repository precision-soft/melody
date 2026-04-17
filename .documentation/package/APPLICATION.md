# APPLICATION

The [`application`](../../application) package provides Melody’s high-level entrypoint for building and running a combined HTTP + CLI application.

It coordinates configuration resolution, container bootstrapping, module wiring (parameters/services/HTTP/CLI/events/security), and process lifecycle.

## Scope

- Package: [`application/`](../../application)
- Subpackage: [`application/contract/`](../../application/contract)

## Subpackages

- [`application/contract`](../../application/contract)
  Public module contracts (`Module`, `ModuleProvider`, `ParameterModule`, `ServiceModule`, `HttpModule`, `HttpMiddlewareModule`, `HttpMiddlewareRegistrar`, `CliModule`, `EventModule`, `ConfigModule`).

## Responsibilities

- Provide the [`Application`](../../application/application.go) type that:
    - wires modules in a deterministic lifecycle
    - resolves configuration before HTTP/CLI module wiring so modules can safely read config values during registration
    - boots the container and runs either CLI or HTTP mode based on runtime flags
- Provide a small module system for application-level composition ([`application/contract`](../../application/contract)).
- Provide an HTTP middleware wiring helper ([`HttpMiddleware`](../../application/http_middleware.go)) for user-registered middleware and middleware factories.

## Lifecycle overview

The application boot is split around configuration resolve:

1. **Pre-resolve**: modules may register module-level configurations via [`ConfigModule`](../../application/contract/config_module.go), then register parameters via [`ParameterModule`](../../application/contract/parameter_module.go).
2. **Resolve**: application configuration is resolved.
3. **Post-resolve**: modules may register services via [`ServiceModule`](../../application/contract/service_module.go), then register security/events/CLI/HTTP.

This allows HTTP/CLI module code to read resolved configuration values during registration, e.g.
`kernelInstance.Config().MustGet("my.param").String()`.

## Runtime mode

Runtime mode is determined by [`ParseRuntimeFlags`](../../application/cli.go):

- `--mode=http` or `--mode=cli` (also `-mode=...`)
- When no explicit mode is provided, non-runtime arguments imply CLI mode.

## Usage

The example below demonstrates creating an application and registering a module that:

- registers module-level configurations (pre-resolve),
- registers parameters (pre-resolve),
- registers services (post-resolve),
- registers HTTP routes (post-resolve) while reading from resolved configuration.

```go
package main

import (
	"context"
	"io/fs"

	"github.com/precision-soft/melody/application"
	applicationcontract "github.com/precision-soft/melody/application/contract"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	"github.com/precision-soft/melody/logging"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type demoModule struct{}

func (instance *demoModule) Name() string {
	return "demo"
}

func (instance *demoModule) Description() string {
	return "demo module"
}

func (instance *demoModule) RegisterConfigurations(registrar applicationcontract.ConfigRegistrar) {
	registrar.RegisterConfiguration(
		loggingcontract.LoggingConfigurationName,
		logging.NewLoggingConfiguration(loggingcontract.LevelLabels{
			loggingcontract.LevelDebug:     "100",
			loggingcontract.LevelInfo:      "200",
			loggingcontract.LevelWarning:   "300",
			loggingcontract.LevelError:     "400",
			loggingcontract.LevelEmergency: "500",
		}),
	)
}

func (instance *demoModule) RegisterParameters(registrar applicationcontract.ParameterRegistrar) {
	registrar.RegisterParameter(
		"app.name",
		"demo",
	)
}

func (instance *demoModule) RegisterServices(
	kernelInstance kernelcontract.Kernel,
	registrar applicationcontract.ServiceRegistrar,
) {
	_ = kernelInstance

	registrar.RegisterService(
		"service.demo.value",
		func(serviceLocator any) (any, error) {
			_ = serviceLocator
			return "value", nil
		},
	)
}

func (instance *demoModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
	router := kernelInstance.HttpRouter()

	router.HandleNamed(
		"health",
		httpcontract.MethodGet,
		"/health",
		func(kernelInstance kernelcontract.Kernel) httpcontract.Handler {
			_ = kernelInstance

			return func(writer httpcontract.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
				_ = writer
				_ = request

				return httpcontract.NewStaticResponse(
					"ok",
					200,
				), nil
			}
		}(kernelInstance),
	)
}

var _ applicationcontract.ConfigModule = (*demoModule)(nil)
var _ applicationcontract.ParameterModule = (*demoModule)(nil)
var _ applicationcontract.ServiceModule = (*demoModule)(nil)
var _ applicationcontract.HttpModule = (*demoModule)(nil)

func buildApplication(embeddedPublicFiles fs.FS, embeddedConfigFiles fs.FS) *application.Application {
	app := application.NewApplication(
		embeddedPublicFiles,
		embeddedConfigFiles,
	)

	app.RegisterModule(&demoModule{})

	/**
	 * Backwards compatible: direct registration is still available
	 * (RegisterParameter/RegisterService/RegisterHttpRoute/etc.).
	 */

	app.RegisterHttpRoute(
		httpcontract.MethodGet,
		"/ping",
		func(writer httpcontract.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			_ = writer
			_ = request

			return httpcontract.NewStaticResponse(
				"pong",
				200,
			), nil
		},
	)

	return app
}

func run(ctx context.Context, embeddedPublicFiles fs.FS, embeddedConfigFiles fs.FS) {
	app := buildApplication(embeddedPublicFiles, embeddedConfigFiles)
	app.Run(ctx)
}
```

## Userland API

### Contracts (`application/contract`)

#### Types

- [`Module`](../../application/contract/module.go)
- [`ModuleProvider`](../../application/contract/module.go)
- [`ConfigModule`](../../application/contract/config_module.go)
- [`ConfigRegistrar`](../../application/contract/config_module.go)
- [`ParameterModule`](../../application/contract/parameter_module.go)
- [`ParameterRegistrar`](../../application/contract/parameter_module.go)
- [`ServiceModule`](../../application/contract/service_module.go)
- [`ServiceRegistrar`](../../application/contract/service_module.go)
- [`HttpModule`](../../application/contract/http_module.go)
- [`HttpMiddlewareModule`](../../application/contract/http_middleware_module.go)
- [`HttpMiddlewareRegistrar`](../../application/contract/http_middleware_module.go)
- [`CliModule`](../../application/contract/cli_module.go)
- [`EventModule`](../../application/contract/event_module.go)

### Types

- [`Application`](../../application/application.go)
- [`RuntimeFlags`](../../application/cli.go)
- [`RouteRegistrar`](../../application/application.go) — `func(kernelInstance kernelcontract.Kernel)` function alias used for deferred HTTP route registration
- [`HttpMiddleware`](../../application/http_middleware.go)
- [`MiddlewareFactory`](../../application/http_middleware.go) — `func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware` function alias used by `UseFactories` / `UseFactoriesWithPriority`
- [`SecurityModule`](../../application/security_module.go) — module contract for registering security configuration via `RegisterSecurity(builder *securityconfig.Builder)`

### Constants

- [`MiddlewareGroupHttp`](../../application/http_middleware.go)
- [`MiddlewareNameStatic`](../../application/http_middleware.go)
- [`MiddlewarePriorityStatic`](../../application/http_middleware.go)
- [`MiddlewarePriorityDefault`](../../application/http_middleware.go)

### Constructors

- [`NewApplication(embeddedPublicFiles, embeddedConfigFiles)`](../../application/application_new.go)
- [`NewRuntimeFlags(mode)`](../../application/cli.go)
- [`ParseRuntimeFlags(defaultMode)`](../../application/cli.go)
- [`NewHttpMiddleware(staticOptions, configuration)`](../../application/http_middleware.go)

### Application lifecycle

- [`(*Application).Boot()`](../../application/application.go)
- [`(*Application).Run(ctx)`](../../application/application.go)
- [`(*Application).Close()`](../../application/application_close.go)

### Registration APIs

- [`(*Application).RegisterConfiguration(name, configuration)`](../../application/application.go)
- [`(*Application).RegisterParameter(name, value)`](../../application/application.go)
- [`(*Application).RegisterService(name, factory)`](../../application/application_container.go)
- [`(*Application).RegisterModule(module)`](../../application/application_module.go)
- [`(*Application).RegisterCliCommand(command)`](../../application/application_cli.go)
- [`(*Application).RegisterHttpRoute(method, pattern, handler)`](../../application/application_http.go)
- [`(*Application).RegisterHttpMiddlewares(middlewares...)`](../../application/application_http.go)
- [`(*Application).RegisterHttpMiddlewareFactories(factories...)`](../../application/application_http.go)

### Middleware helpers

- [`(*HttpMiddleware).Use(middlewares...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseWithPriority(priority, middlewares...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseFactories(factories...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseFactoriesWithPriority(priority, factories...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).LastBuildReport()`](../../application/http_middleware.go)
