# APPLICATION

The [`application`](../../application) package provides Melody’s high-level entrypoint for building and running a combined HTTP + CLI application.

It coordinates configuration resolution, container bootstrapping, module wiring (parameters/services/HTTP/CLI/events/security), and process lifecycle.

## Scope

- Package: [`application/`](../../application)
- Subpackage: [`application/contract/`](../../application/contract)

## Subpackages

- [`application/contract`](../../application/contract)
  Public module contracts (`Module`, `ModuleProvider`, `ParameterModule`, `ServiceModule`, `HttpModule`, `HttpMiddlewareModule`, `HttpMiddlewareRegistrar`, `HttpHandlerDecoratorModule`, `CliModule`, `EventModule`, `ConfigModule`).

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

Runtime mode is determined by [`ParseRuntimeFlags`](../../application/cli.go). The full contract, in precedence order:

1. An explicit `--mode=http` or `--mode=cli` flag (also `-mode=...`, or the space-separated form `--mode cli`) always wins.
2. Without an explicit flag, ANY other non-runtime argument implies CLI mode — `./app melody:messagebus:consume`, `./app db:migrate` and so on each run that one command and exit.
3. With no arguments at all, the `MELODY_DEFAULT_MODE` parameter applies (settable only through the `.env` artifacts — see the CONFIG note below), and it defaults to `http`.

An invalid mode panics at startup. The consequence worth internalizing: **`./app` with no arguments starts HTTP mode**, which boots every module — so anything wired to run in the serving process (outbox relays, consumers, schedulers) starts too. Any command argv flips the process to a one-shot CLI run. This is a supported contract, not an implementation detail; applications may rely on it as their web/background gate, or use the dedicated process role below for a gate that is independent of the transport.

## Process role

A process additionally declares a role — `web`, `worker` or `all` — resolved by [`ParseRuntimeFlagsWithRole`](../../application/cli.go):

1. An explicit `--role=web|worker|all` flag (also `-role=...` or space-separated) always wins.
2. Otherwise the `MELODY_PROCESS_ROLE` parameter applies, defaulting to `all`.

Melody itself gates nothing on the role: it is declared intent for the composition root and long-running runners to query — through [`Application.ProcessRole()`](../../application/application.go), the `KernelConfiguration.ProcessRole()` accessor, the [`ServiceProcessRole`](../../application/service_resolver.go) container service, and the predicates [`config.RoleAllowsBackgroundWork` / `config.RoleAllowsHttp`](../../config/process_role.go). The flag exists because melody never reads the process environment: a docker-compose deployment differentiates containers built from one image with `command: ["/app", "--role=worker"]`, where an `APP_ROLE` environment variable would be silently inert.

Like `--mode`, the `--role` flag never implies CLI mode and is stripped from `os.Args` before the CLI framework parses the arguments.

## Service registration

`Application` is itself a container registrar: [`ServiceRegistrar`](../../application/contract/service_module.go) embeds [`container/contract.Registrar`](../../container/contract/registrar.go), so a `RegisterServices` hook reaches the container's own typed helpers — `container.MustRegisterType` and anything generated on top of it (see [WIRING](WIRING.md)) — through `registrar.Register` / `registrar.MustRegister`, not only the name-based `RegisterService`. A duplicate registration is absorbed into the aggregated boot-collision report whichever entry point produced it, naming the user's registration call site.

## Middleware ordering

An [`HttpMiddlewareModule`](../../application/contract/http_middleware_module.go) registers middleware through [`HttpMiddlewareRegistrar`](../../application/contract/http_middleware_module.go): `Use` registers at `MiddlewarePriorityDefault` (`0`) and `UseWithPriority` states the value. [`(*HttpMiddleware).UseFactories`](../../application/http_middleware.go) and [`UseFactoriesWithPriority`](../../application/http_middleware.go) do the same for a middleware that has to be built from the kernel.

The order is decided when the pipeline is built, not at registration:

1. A **lower** priority value ends up **further out** in the chain: it wraps everything after it, so it runs earlier on the way in and sees the response last on the way out.
2. Registrations at **equal priority** keep **registration order**, the first registered being the outer one. A factory registration and a direct one share one registration sequence, so they compete on the same footing.
3. `before` / `after` edges declared on a [`pipeline.NewHttpMiddlewareDefinition`](../../http/middleware/pipeline/definition.go) override both. The registrar exposes priority only; edges are for a pipeline assembled directly through [`pipeline.NewBuilder`](../../http/middleware/pipeline/builder.go).

A middleware that answers a request itself, without calling `next`, short-circuits everything ordered inside it — the framework's own static middleware serves a matching file and returns without calling the rest of the chain. It is registered at a priority below the default, which keeps it outermost, so a request for a file that exists is answered before anything registered through the registrar observes it. [`(*HttpMiddleware).LastBuildReport`](../../application/http_middleware.go) reports the chain that was actually built and `debug:middleware` renders it. See [HTTP](HTTP.md) for the full ordering contract and for what a `before`/`after` edge does to the build when it names a definition that is not there.

The chain is not the outermost thing there is, though: a request that a `kernel.request` listener answers never reaches it at all. Wrapping *those* too is what [Handler decorators](#handler-decorators) are for.

## Handler decorators

A **handler decorator** and a middleware are two different seams, and the distance between them is the reason both exist. [`HttpHandlerDecorator`](../../application/contract/http_handler_decorator_module.go) is `func(next nethttp.Handler) nethttp.Handler` — plain `net/http` — and it wraps the single handler [`(*Kernel).ServeHttp`](../../http/kernel.go) produces, which [`runHttp`](../../application/application_http.go) then hands to `nethttp.Server` as its `Handler`. A middleware is `func(next httpcontract.Handler) httpcontract.Handler` and is composed *inside* that handler, around the routed one.

Everything the kernel does for a request therefore happens **inside** what a decorator wraps: opening the container scope, building the request logger, stamping `X-Request-Id` on the writer, loading the session, dispatching `kernel.request`, matching the route, recovering a panic, dispatching `kernel.response` and `kernel.terminate`. The middleware chain is the last of those steps and it is not always reached — when a `kernel.request` listener produces a response, the kernel writes it and returns without ever building the chain, and the same holds for `kernel.controller`. An access-control denial is exactly that shape. So a middleware observes only requests that reached the handler stage — which does include the kernel's own `404` and `405` fallbacks, since the chain is wrapped around those too, but not one answered before the chain was built — while a decorator observes **every** request the server accepted: the `401` and `403` the security listener wrote, the response a listener substituted, the `500` the recovery path produced. That is where a metric, a trace span or a per-request timer belongs, because those want the requests the chain never saw as much as the ones it did.

What a decorator gives up in exchange is the request itself. It runs before the container scope exists, so there is no scoped service to resolve, no [`httpcontract.Request`](../../http/contract/request.go), no [`runtimecontract.Runtime`](../../runtime/contract/runtime.go) — only the raw `*nethttp.Request` and `nethttp.ResponseWriter`. Reading the status code means wrapping the writer; reading the request id means reading `X-Request-Id` back off `writer.Header()` after `next` returns, since the kernel sets it there. A decorator also sits outside the kernel's panic recovery for the one case the kernel deliberately re-panics — `net/http`'s `ErrAbortHandler` — so work that must happen on the way out belongs in a `defer`, not on the line after the `next` call.

Decorators come from two places and share one order:

1. [`(*Application).RegisterHttpHandlerDecorator`](../../application/application_http.go) registers one directly. It panics after boot, and it panics on a `nil` decorator rather than storing one.
2. A module implementing [`HttpHandlerDecoratorModule`](../../application/contract/http_handler_decorator_module.go) returns a slice from `RegisterHttpHandlerDecorators(kernelInstance)` during boot. A `nil` entry in that slice is skipped, not stored.

**The first registered decorator is the outermost**: [`runHttp`](../../application/application_http.go) wraps the kernel handler last-to-first, so registration order reads outside-in, the same way the middleware chain does. Direct registrations all happen before `Boot`, and a module's hook runs during it, so a directly registered decorator is always outside every module-contributed one.

```go
package main

import (
	nethttp "net/http"
	"sync/atomic"
	"time"

	applicationcontract "github.com/precision-soft/melody/v3/application/contract"
	kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

var servedRequestCount atomic.Int64
var servedNanoseconds atomic.Int64

type observabilityModule struct{}

func (instance *observabilityModule) Name() string {
	return "observability"
}

func (instance *observabilityModule) Description() string {
	return "times every request, the short-circuited ones included"
}

func (instance *observabilityModule) RegisterHttpHandlerDecorators(
	kernelInstance kernelcontract.Kernel,
) []applicationcontract.HttpHandlerDecorator {
	_ = kernelInstance

	return []applicationcontract.HttpHandlerDecorator{
		func(next nethttp.Handler) nethttp.Handler {
			return nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
				startedAt := time.Now()

				/*
				 * deferred rather than written after the call, so an aborted
				 * connection — which the kernel re-panics with ErrAbortHandler —
				 * is still counted.
				 */
				defer func() {
					servedRequestCount.Add(1)
					servedNanoseconds.Add(int64(time.Since(startedAt)))
				}()

				next.ServeHTTP(writer, request)
			})
		},
	}
}

var _ applicationcontract.HttpHandlerDecoratorModule = (*observabilityModule)(nil)
```

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

	"github.com/precision-soft/melody/v3/application"
	applicationcontract "github.com/precision-soft/melody/v3/application/contract"
	httpcontract "github.com/precision-soft/melody/v3/http/contract"
	kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
	"github.com/precision-soft/melody/v3/logging"
	loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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
	registrar applicationcontract.ServiceRegistrar,
) {
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

	/*
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
- [`HttpHandlerDecoratorModule`](../../application/contract/http_handler_decorator_module.go)
- [`HttpHandlerDecorator`](../../application/contract/http_handler_decorator_module.go)
- [`CliModule`](../../application/contract/cli_module.go)
- [`EventModule`](../../application/contract/event_module.go)

### Types

- [`Application`](../../application/application.go)
- [`RuntimeFlags`](../../application/cli.go)
- [`RouteRegistrar`](../../application/application.go) — `func(kernelInstance kernelcontract.Kernel)` function alias used for deferred HTTP route registration
- [`HttpMiddleware`](../../application/http_middleware.go)
- [`MiddlewareFactory`](../../application/http_middleware.go) — `func(kernelInstance kernelcontract.Kernel) httpcontract.Middleware` function alias used by `UseFactories` / `UseFactoriesWithPriority`
- [`SecurityModule`](../../application/security_module.go) — module contract for registering security configuration via `RegisterSecurity(builder *securityconfig.Builder)`

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
- [`(*Application).RegisterModuleProvider(provider)`](../../application/application_module.go) — registers every module returned by a [`ModuleProvider`](../../application/contract/module.go). `RegisterModule` also expands a module that additionally implements `ModuleProvider`, so a single registration can contribute a whole group of capability-modules. Each Melody integration ships a self-registering module via its `NewModule(ModuleConfig{...})` (e.g. `app.RegisterModule(amqp.NewModule(...))`) that wires that integration's services, parameters and commands in one call instead of hand-calling the individual `Register*` helpers.
- [`(*Application).RegisterCliCommand(command)`](../../application/application_cli.go)
- [`(*Application).RegisterHttpRoute(method, pattern, handler)`](../../application/application_http.go)
- [`(*Application).RegisterHttpMiddlewares(middlewares...)`](../../application/application_http.go)
- [`(*Application).RegisterHttpMiddlewareFactories(factories...)`](../../application/application_http.go)
- [`(*Application).RegisterHttpHandlerDecorator(decorator)`](../../application/application_http.go) — see [Handler decorators](#handler-decorators)
- [`(*Application).OnHttpShutdown(hook)`](../../application/application_http.go) — runs the moment the http server begins shutting down, before it waits for connections to drain, which is how a Server-Sent Events stream or a websocket is released instead of blocking the whole shutdown timeout

### Middleware helpers

- [`(*HttpMiddleware).Use(middlewares...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseWithPriority(priority, middlewares...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseFactories(factories...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).UseFactoriesWithPriority(priority, factories...)`](../../application/http_middleware.go)
- [`(*HttpMiddleware).LastBuildReport()`](../../application/http_middleware.go)
