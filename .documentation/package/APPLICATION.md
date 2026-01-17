# APPLICATION

The [`application`](../../application) package provides Melody’s high-level entrypoint for building and running a combined HTTP + CLI application.

It coordinates configuration, container bootstrapping, module wiring (HTTP/CLI/events/security), and process lifecycle.

## Scope

- Package: `application/`
- Subpackage: `application/contract/`

## Subpackages

- [`application/contract`](../../application/contract)  
  Public module contracts (`Module`, `HttpModule`, `CliModule`, `EventModule`, `ModuleProvider`).

## Responsibilities

- Provide the [`Application`](../../application/application.go) type that:
    - boots the kernel, container, CLI, and HTTP route registration
    - runs either CLI or HTTP mode based on runtime flags
    - manages the service container lifecycle
- Provide a small module system for application-level composition (`application/contract`).
- Provide an HTTP middleware wiring helper (`HttpMiddleware`) for user-registered middleware and middleware factories.

## Runtime mode

Runtime mode is determined by [`ParseRuntimeFlags`](../../application/cli.go):

- `--mode=http` or `--mode=cli` (also `-mode=...`)
- When no explicit mode is provided, non-runtime arguments imply CLI mode.

## Usage

The example below demonstrates creating an application, registering a module, a service, and a simple HTTP route.

```go
package example

import (
	"context"
	"io/fs"

	"github.com/precision-soft/melody/application"
	applicationcontract "github.com/precision-soft/melody/application/contract"
	"github.com/precision-soft/melody/http"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type demoModule struct{}

func (instance *demoModule) Name() string {
	return "demo"
}

func (instance *demoModule) Description() string {
	return "demo module"
}

func (instance *demoModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
	kernelInstance.HttpRouter().Handle(
		httpcontract.MethodGet,
		"/health",
		func(
			runtimeInstance runtimecontract.Runtime,
			writer httpcontract.ResponseWriter,
			request httpcontract.Request,
		) (httpcontract.Response, error) {
			_ = writer
			_ = request

			return http.JsonResponse(
				map[string]any{"ok": true},
				200,
			), nil
		},
	)
}

var _ applicationcontract.HttpModule = (*demoModule)(nil)

func buildApplication(embeddedPublicFiles fs.FS, embeddedConfigFiles fs.FS) *application.Application {
	app := application.NewApplication(
		embeddedPublicFiles,
		embeddedConfigFiles,
	)

	app.RegisterModule(&demoModule{})

	app.RegisterParameter(
		"app.name",
		"demo",
	)

	app.RegisterService(
		"service.demo.value",
		func(serviceLocator any) (any, error) {
			_ = serviceLocator
			return "value", nil
		},
	)

	app.RegisterHttpRoute(
		httpcontract.MethodGet,
		"/ping",
		http.StaticResponse("pong", 200),
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

- [`ModuleProvider`](../../application/contract/module.go)
- [`Module`](../../application/contract/module.go)
- [`HttpModule`](../../application/contract/module.go)
- [`CliModule`](../../application/contract/module.go)
- [`EventModule`](../../application/contract/module.go)

### Types

- [`Application`](../../application/application.go)
- [`RuntimeFlags`](../../application/cli.go)
- [`HttpMiddleware`](../../application/http_middleware.go)

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

