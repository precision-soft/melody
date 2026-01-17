# HTTP

The [`http`](../../http) package provides Melody’s HTTP stack: router and route registry, request/response abstractions, middleware execution, URL generation, static file serving, and the HTTP kernel orchestration.

## Scope

This package covers the HTTP runtime behavior inside Melody:

- route registration and matching (`Router`, `RouteRegistry`)
- request/response conversion and helpers (`Request`, `Response`)
- middleware composition (`http/middleware`)
- URL generation by route name (`UrlGenerator`)
- static file serving (`http/static`)
- kernel orchestration (`Kernel`) and kernel lifecycle events

## Subpackages

- [`http/contract`](../../http/contract)  
  Public contracts for handler, request, response, router, kernel, and URL generator.
- [`http/middleware`](../../http/middleware)  
  Built-in middlewares (CORS, compression, static, rate limiting) and middleware utilities.
    - [`http/middleware/pipeline`](../../http/middleware/pipeline)  
      Middleware pipeline builder and build reports.
- [`http/static`](../../http/static)  
  Static file server implementation with filesystem/embedded modes and HTTP cache helpers.

## Responsibilities

- Router and route registry:
    - [`Router`](../../http/router.go) / [`NewRouter`](../../http/router.go)
    - [`RouteRegistry`](../../http/route_registry.go) / [`NewRouteRegistry`](../../http/route_registry.go)
    - [`RouteGroup`](../../http/router_group.go) / [`NewRouteGroup`](../../http/router_group.go)
- Request and response primitives:
    - [`Request`](../../http/request.go) / [`NewRequest`](../../http/request.go)
    - [`Response`](../../http/response.go) / [`NewResponse`](../../http/response.go)
    - Response helpers (`JsonResponse`, `HtmlResponse`, `RedirectFound`, …) in [`response.go`](../../http/response.go)
- URL generation:
    - [`UrlGenerator`](../../http/url_generator.go) / [`NewUrlGenerator`](../../http/url_generator.go)
- Kernel orchestration:
    - [`Kernel`](../../http/kernel.go) / [`NewKernel`](../../http/kernel.go)
    - Kernel options via [`KernelOptions`](../../http/kernel.go) / [`DefaultKernelOptions`](../../http/kernel.go)
    - Kernel lifecycle events in [`kernel_event.go`](../../http/kernel_event.go)
- Container resolver helpers:
    - [`ServiceRouteRegistry`](../../http/service_resolver.go)
    - [`ServiceUrlGenerator`](../../http/service_resolver.go)
    - [`ServiceRouter`](../../http/service_resolver.go)
    - [`RouteRegistryMustFromContainer`](../../http/service_resolver.go)
    - [`UrlGeneratorMustFromContainer`](../../http/service_resolver.go)
    - [`RouterMustFromContainer`](../../http/service_resolver.go)

## Container integration

The package defines service names for common HTTP services (see [`service_resolver.go`](../../http/service_resolver.go)):

- `ServiceRouteRegistry` (`"service.http.route.registry"`)
- `ServiceUrlGenerator` (`"service.http.url.generator"`)
- `ServiceRouter` (`"service.http.router"`)
- `ServiceRequestContext` (`"service.http.request.context"`)

These services are typically registered by the application/kernel wiring. Userland code may resolve them from the runtime container when needed.

## Usage

The example below demonstrates:

- implementing an `applicationcontract.HttpModule`,
- registering a named route,
- returning a JSON response,
- returning a redirect response using the URL generator.

```go
package example

import (
	nethttp "net/http"

	applicationcontract "github.com/precision-soft/melody/application/contract"
	"github.com/precision-soft/melody/http"
	httpcontract "github.com/precision-soft/melody/http/contract"
	kernelcontract "github.com/precision-soft/melody/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

const pingRouteName = "example.ping"

type ExampleHttpModule struct{}

func (instance *ExampleHttpModule) Name() string {
	return "example.http"
}

func (instance *ExampleHttpModule) Description() string {
	return "example http routes"
}

func (instance *ExampleHttpModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
	router := kernelInstance.HttpRouter()

	router.HandleNamed(
		pingRouteName,
		"GET",
		"/ping",
		handlePing(),
	)

	router.HandleNamed(
		"example.redirect_to_ping",
		"GET",
		"/go-to-ping",
		handleRedirectToPing(),
	)
}

func handlePing() httpcontract.Handler {
	return func(
		_ runtimecontract.Runtime,
		_ nethttp.ResponseWriter,
		_ httpcontract.Request,
	) (httpcontract.Response, error) {
		response, jsonResponseErr := http.JsonResponse(
			200,
			map[string]any{
				"pong": true,
			},
		)
		if nil != jsonResponseErr {
			return nil, jsonResponseErr
		}

		return response, nil
	}
}

func handleRedirectToPing() httpcontract.Handler {
	return func(
		runtimeInstance runtimecontract.Runtime,
		_ nethttp.ResponseWriter,
		_ httpcontract.Request,
	) (httpcontract.Response, error) {
		urlGenerator := http.UrlGeneratorMustFromContainer(
			runtimeInstance.Container(),
		)

		path, generateErr := urlGenerator.GeneratePath(
			pingRouteName,
			map[string]string{},
		)
		if nil != generateErr {
			return nil, generateErr
		}

		return http.RedirectFound(path), nil
	}
}

var _ applicationcontract.HttpModule = (*ExampleHttpModule)(nil)
```

## Footguns & caveats

- Route names must be unique. URL generation relies on a `RouteRegistry` entry for the route name.
- `UrlGeneratorMustFromContainer` is a fail-fast helper and will panic if `ServiceUrlGenerator` is missing or has an invalid type.
- Middleware execution order is determined by the kernel pipeline (see [`http/middleware/pipeline`](../../http/middleware/pipeline)).

## Userland API

### Contracts (`http/contract`)

- [`type Handler`](../../http/contract/handler.go)
- [`type ErrorHandler`](../../http/contract/handler.go)
- [`type Request`](../../http/contract/request.go)
- [`type Response`](../../http/contract/response.go)
- [`type Router`](../../http/contract/router.go)
- [`type RouteDefinition`](../../http/contract/route_definition.go)
- [`type RouteRegistry`](../../http/contract/route_registry.go)
- [`type UrlGenerator`](../../http/contract/url_generator.go)
- [`type Kernel`](../../http/contract/kernel.go)
- [`type Middleware`](../../http/contract/middleware.go)

### Core types and helpers (`http`)

- Router and registry:
    - [`NewRouter()`](../../http/router.go)
    - [`NewRouterWithRouteRegistry(httpcontract.RouteRegistry)`](../../http/router.go)
    - [`NewRouteRegistry()`](../../http/route_registry.go)
    - [`NewRouteGroup(prefix string, router httpcontract.Router)`](../../http/router_group.go)
- URL generator:
    - [`NewUrlGenerator(httpcontract.RouteRegistry)`](../../http/url_generator.go)
- Response helpers:
    - [`JsonResponse`](../../http/response.go)
    - [`HtmlResponse`](../../http/response.go)
    - [`RedirectResponse`](../../http/response.go)
    - [`RedirectFound`](../../http/response.go)
    - [`RedirectMovedPermanently`](../../http/response.go)
- Container helpers:
    - [`const ServiceRouteRegistry`](../../http/service_resolver.go)
    - [`const ServiceUrlGenerator`](../../http/service_resolver.go)
    - [`const ServiceRouter`](../../http/service_resolver.go)
    - [`const ServiceRequestContext`](../../http/service_resolver.go)
    - [`RouteRegistryMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)
    - [`UrlGeneratorMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)
    - [`RouterMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)

### Middleware (`http/middleware`)

- CORS:
    - [`type CorsConfig`](../../http/middleware/cors.go)
    - [`CorsMiddleware`](../../http/middleware/cors.go)
    - [`DefaultCorsMiddleware`](../../http/middleware/cors.go)
- Compression:
    - [`type CompressionConfig`](../../http/middleware/compression.go)
    - [`CompressionMiddleware`](../../http/middleware/compression.go)
    - [`DefaultCompressionMiddleware`](../../http/middleware/compression.go)
- Rate limiting:
    - [`RateLimitMiddleware`](../../http/middleware/rate_limit.go)
    - `TokenBucketLimiter` / `SlidingWindowLimiter` in [`rate_limit.go`](../../http/middleware/rate_limit.go)
- Static:
    - [`StaticMiddleware`](../../http/middleware/static.go)

### Static file server (`http/static`)

- [`type FileServer`](../../http/static/file_server.go)
    - [`NewFileServer`](../../http/static/file_server.go)
- [`type FileServerConfig`](../../http/static/option.go)
    - [`NewFileServerConfig`](../../http/static/option.go)
- [`type Options`](../../http/static/option.go)
    - [`NewOptions`](../../http/static/option.go)
- [`GenerateEtag`](../../http/static/etag.go)
