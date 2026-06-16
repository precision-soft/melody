# HTTP

The [`http`](../../http) package provides Melody’s HTTP stack: routing and route registry, request/response primitives, middleware execution, URL generation, static file serving, and HTTP kernel orchestration.

## Scope

This package covers the HTTP runtime behavior inside Melody:

* route registration, grouping, and matching via [`Router`](../../http/contract/router.go), [`RouteGroup`](../../http/contract/router_group.go), and [`RouteRegistry`](../../http/contract/route_registry.go)
* route configuration via [`RouteOptions`](../../http/contract/route_option.go)
* request/response conversion and helpers via [`Request`](../../http/contract/request.go) and [`Response`](../../http/contract/response.go)
* middleware composition via [`http/middleware`](../../http/middleware)
* URL generation by route name via [`UrlGenerator`](../../http/contract/url_generator.go)
* static file serving via [`http/static`](../../http/static)
* kernel orchestration via [`Kernel`](../../http/contract/kernel.go) and kernel lifecycle events in [`kernel_event.go`](../../http/kernel_event.go)

## Subpackages

* [`http/contract`](../../http/contract)
  Public contracts for handler, request, response, router, kernel, URL generator, route groups, and route options.
* [`http/middleware`](../../http/middleware)
  Built-in middlewares (CORS, compression, static, rate limiting) and middleware utilities.
* [`http/middleware/pipeline`](../../http/middleware/pipeline)
  Middleware pipeline builder and build reports.
* [`http/static`](../../http/static)
  Static file server implementation with filesystem/embedded modes and HTTP cache helpers.

## Responsibilities

* Router and route registry:
    * [`Router`](../../http/router.go) / [`NewRouter`](../../http/router.go)
    * [`RouteRegistry`](../../http/route_registry.go) / [`NewRouteRegistry`](../../http/route_registry.go)
    * [`RouteGroup`](../../http/router_group.go) / [`NewRouteGroup`](../../http/router_group.go)
    * [`RouteOptions`](../../http/route_option.go) / [`NewRouteOptions`](../../http/route_option.go)

* Request and response primitives:
    * [`Request`](../../http/request.go) / [`NewRequest`](../../http/request.go)
    * [`Response`](../../http/response.go) / [`NewResponse`](../../http/response.go)
    * Response helpers (`JsonResponse`, `HtmlResponse`, `RedirectFound`, …) in [`response.go`](../../http/response.go)

* URL generation:
    * [`UrlGenerator`](../../http/url_generator.go) / [`NewUrlGenerator`](../../http/url_generator.go)

* Kernel orchestration:
    * [`Kernel`](../../http/kernel.go) / [`NewKernel`](../../http/kernel.go)
    * Kernel options via [`KernelOptions`](../../http/kernel.go) / [`DefaultKernelOptions`](../../http/kernel.go)
    * Kernel lifecycle events in [`kernel_event.go`](../../http/kernel_event.go)

* Container resolver helpers:
    * [`ServiceRouteRegistry`](../../http/service_resolver.go)
    * [`ServiceUrlGenerator`](../../http/service_resolver.go)
    * [`ServiceRouter`](../../http/service_resolver.go)
    * [`RouteRegistryMustFromContainer`](../../http/service_resolver.go)
    * [`UrlGeneratorMustFromContainer`](../../http/service_resolver.go)
    * [`RouterMustFromContainer`](../../http/service_resolver.go)

## Container integration

The package defines service names for common HTTP services (see [`service_resolver.go`](../../http/service_resolver.go)) which are resolved from a [`container/contract.Container`](../../container/contract) at runtime.

* `ServiceRouteRegistry` (`"service.http.route.registry"`)
* `ServiceUrlGenerator` (`"service.http.url.generator"`)
* `ServiceRouter` (`"service.http.router"`)
* `ServiceRequestContext` (`"service.http.request.context"`)

These services are typically registered by the application/kernel wiring. Userland code may resolve them from the runtime container when needed.

### Runtime parameter injection

When a controller function declares a parameter of type
[`runtimecontract.Runtime`](../../runtime/contract),
Melody injects the current `runtimeInstance` directly (it is **not** resolved from the scope/container by type).

This allows controllers to access request-scoped state via `runtimeInstance.Scope()` without registering `runtimecontract.Runtime` as a service.

Implementation detail: see [`wrapControllerWithContainer`](../../http/router_utility.go).

## HTTP method semantics

* [`HEAD`](../../http) requests are matched against explicit `HEAD` routes and also against `GET` routes. When a `GET` route handles a `HEAD` request, Melody keeps the same status code and headers as the `GET` handler while suppressing the response body during [`WriteToHttpResponseWriter`](../../http/response.go).
* [`OPTIONS`](../../http) responses may be generated automatically by the HTTP kernel when a path matches but the incoming method does not map to a userland handler.
* The `Allow` header is derived from the methods registered for the matched path. When `GET` is registered, `HEAD` is also advertised in `Allow`.

## Controller return contract

Controller functions wired through [`wrapControllerWithContainer`](../../http/router_utility.go) must return a first result that implements [`httpcontract.Response`](../../http/contract/response.go). The first result is not restricted to the concrete [`Response`](../../http/response.go) type; any implementation of the response contract is accepted.

For JSON-body endpoints, [`JsonHandler[Req](handle, ...options)`](../../http/typed_handler.go) wraps a handler so the framework decodes the request body into `Req` and runs the container validator before calling `handle(runtime, request, body)`; a decode/validation failure returns an error, or a caller-supplied response shape via [`WithJsonHandlerErrorResponder`](../../http/typed_handler.go). This removes the per-handler decode-and-validate block.

## Usage

The example below demonstrates:

* implementing an `applicationcontract.HttpModule`,
* registering routes on a group using [`Router.Group`](../../http/contract/router.go),
* returning a JSON response,
* returning a redirect response using the URL generator.

```go
package main

import (
	nethttp "net/http"

	applicationcontract "github.com/precision-soft/melody/v3/application/contract"
	"github.com/precision-soft/melody/v3/http"
	httpcontract "github.com/precision-soft/melody/v3/http/contract"
	kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const pingRouteName = "example.api.ping"

type ExampleHttpModule struct{}

func (instance *ExampleHttpModule) Name() string {
	return "example.http"
}

func (instance *ExampleHttpModule) Description() string {
	return "example http routes"
}

func (instance *ExampleHttpModule) RegisterHttpRoutes(kernelInstance kernelcontract.Kernel) {
	router := kernelInstance.HttpRouter()

	api := router.Group("/api")
	api.WithNamePrefix("example.api.")

	api.HandleNamed(
		"ping",
		"GET",
		"/ping",
		handlePing(),
	)

	api.HandleNamed(
		"redirect_to_ping",
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

## Server-Sent Events

Handlers receive the raw [`nethttp.ResponseWriter`](../../http/contract/handler.go), so they can stream a long-lived response instead of returning a buffered one. [`NewServerSentEventWriter`](../../http/server_sent_event.go) type-asserts the writer to `http.Flusher`, sets the `text/event-stream` headers, and flushes after every [`Send`](../../http/server_sent_event.go). A streaming handler returns `(nil, nil)` when the client disconnects (detected via `request.HttpRequest().Context().Done()`); the kernel writes nothing further because it only writes a response when one is returned.

[`ServerSentEventHub`](../../http/server_sent_event_hub.go) is an optional topic-keyed fan-out registry: [`Subscribe`](../../http/server_sent_event_hub.go) returns a buffered subscriber, [`Broadcast`](../../http/server_sent_event_hub.go) delivers an [`ServerSentEvent`](../../http/server_sent_event.go) to every subscriber of a topic (non-blocking — a full subscriber buffer drops the event), and [`Unsubscribe`](../../http/server_sent_event_hub.go) removes and closes it. This pairs naturally with the message bus: a message handler can `hub.Broadcast(...)` so domain events become real-time pushes.

```go
func StreamHandler(hub *http.ServerSentEventHub) httpcontract.Handler {
	return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
		serverSentEventWriter, serverSentEventErr := http.NewServerSentEventWriter(writer)
		if nil != serverSentEventErr {
			return http.JsonErrorResponse(nethttp.StatusInternalServerError, "streaming is not supported"), nil
		}

		subscriber := hub.Subscribe("demo", 16)
		defer hub.Unsubscribe(subscriber)

		requestContext := request.HttpRequest().Context()
		for {
			select {
			case <-requestContext.Done():
				return nil, nil
			case event, open := <-subscriber.Events():
				if false == open {
					return nil, nil
				}
				if sendErr := serverSentEventWriter.Send(event); nil != sendErr {
					return nil, nil
				}
			}
		}
	}
}
```

The example application wires an `ServerSentEventHub`, an `/events/stream` Server-Sent Events endpoint (`handler/events/stream_handler.go`), and an `/events/publish` endpoint (`handler/events/publish_handler.go`) that dispatches a message through the bus to a handler which broadcasts to the hub.

### Behind a load balancer

`ServerSentEventHub` keeps its subscribers in process, so a plain `Broadcast` only reaches clients connected to **this** instance. When the application runs on several instances behind a load balancer, attach an [`ServerSentEventBackplane`](../../http/server_sent_event_hub.go) with [`SetBackplane`](../../http/server_sent_event_hub.go): `Broadcast` then also replicates the event to the other instances, each of which delivers it to its own subscribers via [`DeliverLocal`](../../http/server_sent_event_hub.go). The backplane tags every event with a per-instance origin and ignores the echo of its own broadcasts, so nothing is delivered twice. Concrete backplanes ship in [`integrations/rueidis`](../../../integrations/rueidis) (Redis pub/sub) and [`integrations/amqp`](../../../integrations/amqp) (fanout exchange); the WebSocket integration shares the same hub, so it fans out the same way. Without a backplane, pin clients to an instance with sticky sessions and accept that an event only
reaches that instance. Replication is best-effort like local delivery; [`BackplaneFailures`](../../http/server_sent_event_hub.go) counts broadcasts that could not be replicated. After [`Shutdown`](../../http/server_sent_event_hub.go) the hub stops replicating — a `Broadcast` during or after a graceful stop delivers to nobody locally and is not pushed to the backplane.

## Footguns & caveats

* Server-Sent Events handlers must return `(nil, nil)` after streaming; returning a non-nil response would make the kernel write a second header/body.
* [`ServerSentEventHub.Broadcast`](../../http/server_sent_event_hub.go) is non-blocking and drops events for subscribers whose buffer is full; delivery is **at-most-once**. Size the subscribe buffer for the expected burst, or treat the stream as best-effort. [`ServerSentEventHub.DroppedEventCount`](../../http/server_sent_event_hub.go) returns the cumulative number of dropped events so the loss can be surfaced as a metric.
* Route names must be unique. URL generation relies on a [`RouteRegistry`](../../http/contract/route_registry.go) entry for the route name.
* [`UrlGeneratorMustFromContainer`](../../http/service_resolver.go) is a fail-fast helper and will panic if `ServiceUrlGenerator` is missing or has an invalid type.

## Userland API

### Contracts (`http/contract`)

* [`type Handler`](../../http/contract/handler.go)
* [`type ErrorHandler`](../../http/contract/handler.go)
* [`type Request`](../../http/contract/request.go)
* [`type Response`](../../http/contract/response.go)
* [`type Router`](../../http/contract/router.go)
* [`type RouteHandler`](../../http/contract/router.go)
* [`type RouteGroup`](../../http/contract/router_group.go)
* [`type RouteOptions`](../../http/contract/route_option.go)
* [`type RouteDefinition`](../../http/contract/route_definition.go)
* [`type RouteRegistry`](../../http/contract/route_registry.go)
* [`type UrlGenerator`](../../http/contract/url_generator.go)
* [`type Kernel`](../../http/contract/kernel.go)
* [`type Middleware`](../../http/contract/middleware.go)

### Core types and helpers (`http`)

* Router and registry:
    * [`NewRouter()`](../../http/router.go)
    * [`NewRouterWithRouteRegistry(httpcontract.RouteRegistry)`](../../http/router.go)
    * [`NewRouteRegistry()`](../../http/route_registry.go)
    * [`NewRouteGroup(router httpcontract.Router, pathPrefix string) httpcontract.RouteGroup`](../../http/router_group.go)
    * [`NewRouteOptions(name string, methods []string, host string, schemes []string, requirements map[string]string, defaults map[string]string, locales []string, priority int, attributes map[string]any) httpcontract.RouteOptions`](../../http/route_option.go)

* URL generator:
    * [`NewUrlGenerator(httpcontract.RouteRegistry)`](../../http/url_generator.go)

* Server-Sent Events:
    * [`type ServerSentEvent`](../../http/server_sent_event.go)
    * [`type ServerSentEventWriter`](../../http/server_sent_event.go) with [`NewServerSentEventWriter(nethttp.ResponseWriter) (*ServerSentEventWriter, error)`](../../http/server_sent_event.go), [`(*ServerSentEventWriter).Send(ServerSentEvent) error`](../../http/server_sent_event.go), [`(*ServerSentEventWriter).Comment(string) error`](../../http/server_sent_event.go), [`(*ServerSentEventWriter).Ping() error`](../../http/server_sent_event.go). Both `Send` (`Id`/`Event`/`Data`) and `Comment` strip `CR`/`LF` from caller-supplied text so a dynamic value cannot inject extra Server-Sent Events fields or events; `Send` additionally treats a bare `CR`, `LF`, or `CRLF` inside `Data` as a data-line boundary per the EventSource specification.
    * [`type ServerSentEventHub`](../../http/server_sent_event_hub.go) with [`NewServerSentEventHub()`](../../http/server_sent_event_hub.go), [`Subscribe(topic string, bufferSize int) *ServerSentEventSubscriber`](../../http/server_sent_event_hub.go), [`Unsubscribe(*ServerSentEventSubscriber)`](../../http/server_sent_event_hub.go), [`Broadcast(topic string, event ServerSentEvent) int`](../../http/server_sent_event_hub.go), [`DeliverLocal(topic string, event ServerSentEvent) int`](../../http/server_sent_event_hub.go), [`SubscriberCount(topic string) int`](../../http/server_sent_event_hub.go), [`DroppedEventCount() uint64`](../../http/server_sent_event_hub.go), and the cross-instance backplane/shutdown surface [`SetBackplane(ServerSentEventBackplane)`](../../http/server_sent_event_hub.go), [`BackplaneFailures() uint64`](../../http/server_sent_event_hub.go), [`Shutdown()`](../../http/server_sent_event_hub.go)
    * [`type ServerSentEventSubscriber`](../../http/server_sent_event_hub.go) with [`(*ServerSentEventSubscriber).Events() <-chan ServerSentEvent`](../../http/server_sent_event_hub.go), [`(*ServerSentEventSubscriber).DroppedCount() uint64`](../../http/server_sent_event_hub.go)

* Response helpers:
    * [`JsonResponse`](../../http/response.go)
    * [`HtmlResponse`](../../http/response.go)
    * [`RedirectResponse`](../../http/response.go)
    * [`RedirectFound`](../../http/response.go)
    * [`RedirectMovedPermanently`](../../http/response.go)

* Container helpers:
    * [`const ServiceRouteRegistry`](../../http/service_resolver.go)
    * [`const ServiceUrlGenerator`](../../http/service_resolver.go)
    * [`const ServiceRouter`](../../http/service_resolver.go)
    * [`const ServiceRequestContext`](../../http/service_resolver.go)
    * [`RouteRegistryMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)
    * [`UrlGeneratorMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)
    * [`RouterMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)

### Middleware (`http/middleware`)

* CORS:
    * [`type CorsConfig`](../../http/middleware/cors.go)
    * [`CorsMiddleware`](../../http/middleware/cors.go)
    * [`DefaultCorsMiddleware`](../../http/middleware/cors.go)

* Compression:
    * [`type CompressionConfig`](../../http/middleware/compression.go)
    * [`CompressionMiddleware`](../../http/middleware/compression.go)
    * [`DefaultCompressionMiddleware`](../../http/middleware/compression.go)

* Rate limiting:
    * [`RateLimitMiddleware`](../../http/middleware/rate_limit.go)
    * `TokenBucketLimiter` / `SlidingWindowLimiter` in [`rate_limit.go`](../../http/middleware/rate_limit.go)

* Static:
    * [`StaticMiddleware`](../../http/middleware/static.go)

### Static file server (`http/static`)

* [`type FileServer`](../../http/static/file_server.go)
    * [`NewFileServer`](../../http/static/file_server.go)

* [`type FileServerConfig`](../../http/static/option.go)
    * [`NewFileServerConfig`](../../http/static/option.go)

* [`type Options`](../../http/static/option.go)
    * [`NewOptions`](../../http/static/option.go)

* [`GenerateEtag`](../../http/static/etag.go)
