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
3. **Post-resolve**: modules may register services via [`ServiceModule`](../../application/contract/service_module.go), then request-lifetime services via [`ScopedServiceModule`](../../application/contract/scoped_service_module.go), then — in this order — security, event subscribers, HTTP middlewares, HTTP routes and CLI commands.

This allows HTTP/CLI module code to read resolved configuration values during registration, e.g.
`kernelInstance.Config().MustGet("my.param").String()`.

## Runtime mode

Runtime mode is determined by [`ParseRuntimeFlags`](../../application/cli.go):

- `--mode=http` or `--mode=cli` (also `-mode=...`)
- When no explicit mode is provided, non-runtime arguments imply CLI mode.

## Middleware ordering

An [`HttpMiddlewareModule`](../../application/contract/http_middleware_module.go) registers middleware through [`HttpMiddlewareRegistrar`](../../application/contract/http_middleware_module.go): `Use` registers at `MiddlewarePriorityDefault` (`0`) and `UseWithPriority` states the value. [`(*HttpMiddleware).UseFactories`](../../application/http_middleware.go) and [`UseFactoriesWithPriority`](../../application/http_middleware.go) do the same for a middleware that has to be built from the kernel.

The order is decided when the pipeline is built, not at registration:

1. A **lower** priority value ends up **further out** in the chain: it wraps everything after it, so it runs earlier on the way in and sees the response last on the way out.
2. Registrations at **equal priority** keep **registration order**, the first registered being the outer one. A factory registration and a direct one share one registration sequence, so they compete on the same footing.
3. `before` / `after` edges declared on a [`pipeline.NewHttpMiddlewareDefinition`](../../http/middleware/pipeline/definition.go) override both. The registrar exposes priority only; edges are for a pipeline assembled directly through [`pipeline.NewBuilder`](../../http/middleware/pipeline/builder.go).

A middleware that answers a request itself, without calling `next`, short-circuits everything ordered inside it — the framework's own static middleware serves a matching file and returns without calling the rest of the chain. It is registered at a priority below the default, which keeps it outermost, so a request for a file that exists is answered before anything registered through the registrar observes it. [`(*HttpMiddleware).LastBuildReport`](../../application/http_middleware.go) reports the chain the serving process actually built, for in-process inspection; `debug:middleware` describes and — under `--build` — builds a report of its own rather than reading that record. See [HTTP](HTTP.md) for the full ordering contract and for what a `before`/`after` edge does to the build when it names a definition that is not there.

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
	nethttp "net/http"

	"github.com/precision-soft/melody/application"
	applicationcontract "github.com/precision-soft/melody/application/contract"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/http"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	"github.com/precision-soft/melody/logging"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
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
		func(resolver containercontract.Resolver) (string, error) {
			_ = resolver
			return "value", nil
		},
	)
}

func (instance *demoModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
	router := kernelInstance.HttpRouter()

	router.HandleNamed(
		"health",
		nethttp.MethodGet,
		"/health",
		func(
			_ runtimecontract.Runtime,
			_ nethttp.ResponseWriter,
			_ httpcontract.Request,
		) (httpcontract.Response, error) {
			return http.TextResponse(
				200,
				"ok",
			), nil
		},
	)
}

var _ applicationcontract.ConfigModule = (*demoModule)(nil)
var _ applicationcontract.ParameterModule = (*demoModule)(nil)
var _ applicationcontract.ServiceModule = (*demoModule)(nil)
var _ applicationcontract.HttpModule = (*demoModule)(nil)

func buildApplication(embeddedEnvFiles fs.FS, embeddedPublicFiles fs.FS) *application.Application {
	app := application.NewApplication(
		embeddedEnvFiles,
		embeddedPublicFiles,
	)

	app.RegisterModule(&demoModule{})

	/*
	 * Backwards compatible: direct registration is still available
	 * (RegisterParameter/RegisterService/RegisterHttpRoute/etc.).
	 */

	app.RegisterHttpRoute(
		nethttp.MethodGet,
		"/ping",
		func(
			_ runtimecontract.Runtime,
			_ nethttp.ResponseWriter,
			_ httpcontract.Request,
		) (httpcontract.Response, error) {
			return http.TextResponse(
				200,
				"pong",
			), nil
		},
	)

	return app
}

func run(ctx context.Context, embeddedEnvFiles fs.FS, embeddedPublicFiles fs.FS) {
	app := buildApplication(embeddedEnvFiles, embeddedPublicFiles)
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
- [`ScopedServiceModule`](../../application/contract/scoped_service_module.go)  
  `RegisterScopedServices(kernelInstance, registrar)` declares services whose lifetime is one scope — one http request, one command run. What it registers is built on the first resolution through a scope and closed when that scope closes. The hook is a separate interface, so an existing module is unaffected by its arrival.
- [`ScopedServiceRegistrar`](../../application/contract/scoped_service_module.go)  
  Shares no method with `ServiceRegistrar`: a container provider handed to the scoped hook, or a scoped provider handed to `RegisterServices`, does not compile. See [CONTAINER](CONTAINER.md) for what the two lifetimes may read.
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

- [`NewApplication(embeddedEnvFiles, embeddedPublicFiles)`](../../application/application_new.go) — both parameters are `fs.FS`, so the compiler never objects to them being swapped: the **env** files come first and the **public** files second, matching the `melody_env_embedded` and `melody_static_embedded` build tags in that order.
- [`NewRuntimeFlags(mode)`](../../application/cli.go)
- [`ParseRuntimeFlags(defaultMode)`](../../application/cli.go)
- [`NewHttpMiddleware(staticOptions, configuration)`](../../application/http_middleware.go)

### Application lifecycle

- [`(*Application).Boot()`](../../application/application.go)
- [`(*Application).Run(ctx)`](../../application/application.go)
- [`(*Application).Close()`](../../application/application_close.go)

### Registration APIs

- [`(*Application).RegisterConfiguration(name, configuration)`](../../application/application.go) — accepts exactly one name in this major, `loggingcontract.LoggingConfigurationName`; any other name panics at registration, because nothing can ever consume it
- [`(*Application).RegisterParameter(name, value)`](../../application/application.go)
- [`(*Application).RegisterService(name, factory)`](../../application/application_container.go)
- [`(*Application).RegisterModule(module)`](../../application/application_module.go)
- [`(*Application).RegisterModuleProvider(provider)`](../../application/application_module.go) — registers every module returned by a [`ModuleProvider`](../../application/contract/module.go). `RegisterModule` also expands a module that additionally implements `ModuleProvider`, so a single registration can contribute a whole group of capability-modules.
- [`(*Application).RegisterCliCommand(command)`](../../application/application_cli.go)
- [`(*Application).RegisterHttpRoute(method, pattern, handler)`](../../application/application_http.go)
- [`(*Application).RegisterHttpMiddlewares(middlewares...)`](../../application/application_http.go)
- [`(*Application).RegisterHttpMiddlewareFactories(factories...)`](../../application/application_http.go) — a factory that yields nil (or a typed nil) fails the pipeline build, naming the definition; a factory that wants to disable itself conditionally returns a pass-through middleware instead

### Boot refusals and the process exit

Boot fails fast where serving would be the widening: an **http** process whose `.env` artifacts contributed **no keys at all** refuses to boot rather than serve on development defaults (a cli process stays permissive — a command takes its configuration with it when it exits). The http server limits are fixed defaults in this major — read 15s, read-header 5s, write 30s, idle 60s, max header 1 MiB — with no override surface; the shutdown timeout alone is configurable through `MELODY_HTTP_SHUTDOWN_TIMEOUT` (`kernel.http.shutdown_timeout`), defaulting to 5s.

**The shutdown budget covers the request scopes as well as the connections.** `net/http`'s own `Shutdown` drains the connections the server still owns — and a **hijacked** connection is not one of them: a handler that upgrades to a websocket or takes the socket for any other reason removes it from the server's accounting, so `Shutdown` returns immediately and reports success while that handler is still running. Melody counts the request scopes the http kernel opened and waits for them under the *same* budget after `Shutdown` returns. A drain that finishes leaves the exit exactly as it was; one that does not is recorded as `http shutdown left request scopes open`, carrying the count, and the process exits non-zero — the same signal an overrun `Shutdown` gives, for the same reason. An application that serves upgraded connections should close them from a shutdown hook so the drain has something to succeed at; without one, the budget is what bounds the wait.

On the way out, the record that explains a dying process is written **before** the teardown, through a logger that still writes: the exit handler climbs a ladder — the configured container logger while it still answers, a last-resort logger opened on the configured log destination when the container cannot, and the emergency logger when even that destination is unusable. A teardown failure that `Run`'s own close discovers turns into exit 1, symmetric with the cli path, which folds the run scope's close failure into the command's result while the container teardown belongs to the exit handler.

A panic **during boot** exits fail-fast, with the container teardown run between the record and the exit: `Boot`'s own recover handler logs the record first, through a logger the teardown has not touched, then closes the container — which asks `IsClosed` before acting, so a container that never got built costs nothing — and exits. Connections modules opened before the failing boot line — database pools, redis clients — therefore get their `Close`, bounded by the same per-step budget the exit path applies to everything else.

### Middleware helpers

- [`(*HttpMiddleware).Use(middlewares...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseWithPriority(priority, middlewares...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseFactories(factories...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseFactoriesWithPriority(priority, factories...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).LastBuildReport()`](../../application/http_middleware.go)
