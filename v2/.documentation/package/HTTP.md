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
* [`http/cors`](../../http/cors)
  CORS policy service, preflight middleware, and `kernel.response` listener that applies CORS headers to error responses.
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
    * Response helpers (`JsonResponse`, `HtmlResponse`, `RedirectFound`, `FileResponse`, `AttachmentResponse`, …) in [`response.go`](../../http/response.go)
    * RFC 6266 Content-Disposition helper [`BuildContentDisposition`](../../http/response.go)

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
[`runtimecontract.Runtime`](../../runtime/contract), Melody injects the current `runtimeInstance` directly (it is **not** resolved from the scope/container by type).

This allows controllers to access request-scoped state via `runtimeInstance.Scope()` without registering `runtimecontract.Runtime` as a service.

Implementation detail: see [`wrapControllerWithContainer`](../../http/router_utility.go).

## Request body limits

The HTTP kernel wraps `request.Body` with `http.MaxBytesReader` when `Http().MaxRequestBodyBytes()` is set to a positive value in the configuration (see [`kernel.go`](../../http/kernel.go)). A read past that limit fails, and net/http answers `413 Request Entity Too Large` and closes the connection, so an oversized body is never fully delivered to a controller.

## Form parsing

[`NewRequest`](../../http/request.go) only auto-parses the request body as form data when the method is `POST`, `PUT`, or `PATCH` and the `Content-Type` media type is `application/x-www-form-urlencoded` or `multipart/form-data`. JSON, XML, and other content types are left intact so handlers can read the raw body. For explicit reparsing, use [`ParseFormBody`](../../http/request.go).

## CORS

The [`http/cors`](../../http/cors) subpackage provides a dedicated CORS layer:

* [`Service`](../../http/cors/service.go) — policy holder built from a [`Config`](../../http/cors/service.go). An empty `AllowOrigins` is defaulted to the wildcard, and a credentialed configuration holding that wildcard panics — unless `Config.AllowOriginFunc` decides origins, since the list is never consulted once a function is set.
* [`Middleware`](../../http/cors/middleware.go) / [`DefaultMiddleware`](../../http/cors/middleware.go) / [`Restrictive`](../../http/cors/middleware.go) — answers a preflight with `204` and applies CORS headers on the happy path. A preflight is an `OPTIONS` carrying both an allowed `Origin` and an `Access-Control-Request-Method` ([`Service.IsPreflight`](../../http/cors/service.go)); a plain `OPTIONS` reaches the router instead. `Vary: Origin` is added to every response the chain produces, including one produced for a rejected origin or for a request carrying no `Origin` at all, so a shared cache cannot hand an origin-less body to an allowed cross-origin requester.
* [`RegisterResponseListener`](../../http/cors/listener.go) — subscribes to `kernel.response` with [`ResponseListenerPriority`](../../http/cors/listener.go) (`-100`) so CORS headers reach browsers even when error handlers replace the controller response.

## Url generation

[`UrlGenerator.GeneratePath`](../../http/url_generator.go) builds a path from a route name and a parameter map, and a route's `defaults` ([`NewRouteOptions`](../../http/route_option.go)) fill the gaps:

* A parameter the caller **omits** takes the route default, whatever that default is.
* A parameter the caller supplies **empty** takes the route default when that default is non-empty. This is what keeps a non-trailing optional segment — the shape [`rejectNonTrailingOptionalParameter`](../../http/router.go) admits precisely because its default keeps the segment present — generating a path the matcher answers: `/:locale?/list/:page` with `{"locale": "", "page": "2"}` yields `/en/list/2` under a `locale` default of `en`, not the `/list/2` the router replies to with a `404`.
* With no usable value left, an **optional** segment is dropped and a **required** one fails with `route parameter missing` or `route parameter may not be empty` — [`matchPath`](../../http/router_utility.go) refuses an empty segment for a named parameter, so emitting one would mint a url this router does not serve.

A requirement declared on a parameter is validated against the value that is actually emitted, catch-all remainders included, so generation and matching agree.

## HTTP method semantics

* [`HEAD`](../../http) requests are matched against explicit `HEAD` routes and also against `GET` routes. When a `GET` route handles a `HEAD` request, Melody keeps the same status code and headers as the `GET` handler while suppressing the response body during [`WriteToHttpResponseWriter`](../../http/response.go).
* [`OPTIONS`](../../http) responses may be generated automatically by the HTTP kernel when a path matches but the incoming method does not map to a userland handler.
* The `Allow` header is derived from the methods registered for the matched path. When `GET` is registered, `HEAD` is also advertised in `Allow`.

## Controller return contract

Controller functions wired through [`wrapControllerWithContainer`](../../http/router_utility.go) must return a first result that implements [`httpcontract.Response`](../../http/contract/response.go). The first result is not restricted to the concrete [`Response`](../../http/response.go) type; any implementation of the response contract is accepted.

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

	applicationcontract "github.com/precision-soft/melody/v2/application/contract"
	"github.com/precision-soft/melody/v2/http"
	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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

## Session cookie

The kernel loads a session from the incoming cookie, publishes it on the request as [`RequestAttributeSession`](../../http/request.go), and on the way out saves a modified session and emits its `Set-Cookie`. [`Kernel.SetSessionCookiePolicy`](../../http/contract/kernel.go) shapes that cookie through a [`SessionCookiePolicy`](../../http/contract/kernel.go):

| Field      | Type                                                                       | Default when unset                     |
|------------|----------------------------------------------------------------------------|----------------------------------------|
| `Path`     | `string`                                                                   | `/`                                    |
| `Domain`   | `string`                                                                   | omitted (host-only cookie)             |
| `SameSite` | `nethttp.SameSite`                                                         | `SameSiteLaxMode`                      |
| `Secure`   | [`SessionCookieSecurePolicy`](../../http/contract/kernel.go)                | derived from the resolved scheme        |

`HttpOnly` is always set and is not configurable.

### `Secure`

[`SessionCookieSecurePolicy`](../../http/contract/kernel.go) has three values ([`resolveSessionCookieSecure`](../../http/router_utility.go)):

* [`SessionCookieSecureFromScheme`](../../http/contract/kernel.go) — the **zero value and the default**. `Secure` is set when the scheme the forwarded-headers policy resolved is `https`, so a policy that does not mention `Secure` behaves as the framework always has.
* [`SessionCookieSecureAlways`](../../http/contract/kernel.go) — forces `Secure` on regardless of the resolved scheme. This is the setting for a deployment whose proxy terminates TLS and forwards plaintext, but which is **not** trusted for `X-Forwarded-Proto`: scheme detection reports `http` there, so the derived policy would ship the session cookie without `Secure` even though every browser hop is HTTPS.
* [`SessionCookieSecureNever`](../../http/contract/kernel.go) — forces `Secure` off. **Discouraged**: it is the only one of the three that can weaken the cookie relative to the deployment, and it does so unconditionally — a session cookie without `Secure` is sent over plaintext HTTP and can be captured. Reach for it only for a local development origin that genuinely has no TLS.

### `SameSite`

A policy that leaves `SameSite` at its zero value gets `SameSiteLaxMode`. `net/http` has no name for the zero `SameSite` and emits **no attribute** for it, which is what an operator who set only `Path` or `Domain` would otherwise silently get; treating it as unset and applying the framework default keeps the partial policy safe. Use `nethttp.SameSiteDefaultMode` to omit the attribute deliberately. See [`resolveSessionCookieSameSite`](../../http/router_utility.go).

### Rotating the session id

The response path saves and advertises the session published on [`RequestAttributeSession`](../../http/request.go) at the moment the response is written, not the one the kernel loaded before routing, so a handler can replace it. [`RegenerateRequestSession`](../../http/session.go) is that operation done whole: it rotates the id of the session the request carries — the defence against **session fixation** — and republishes the rotated session on the request, so one call is all a login handler needs.

```go
rotated, rotateErr := melodyhttp.RegenerateRequestSession(request)
if nil != rotateErr {
    return nil, rotateErr
}

rotated.Set(sessionKeyUserId, user.Id())
```

Call it **before** writing the authenticated identity, and write that identity to the session it returns. It reports an error rather than panicking when the request carries no session, has no runtime, or the session manager is not registered. [`session.Manager.RegenerateSession`](../../session/manager.go) is the storage-level primitive underneath, for code that holds no request; rotating through it means republishing the result yourself, and the session that went in is latched out of use so that forgetting to logs the client out rather than stranding it on a deleted id. The latch is what makes a rotated-away session unresurrectable: `Session.Set` lifts the cleared flag, nothing lifts the latch. A `Session` implementation from outside this package is only `Clear()`ed, which a later write still undoes.

### Streamed responses

No session row is written for a response the kernel discards because the handler already committed the headers — a streaming handler such as Server-Sent Events — **unless the request already names that session id** in its cookie.

A discarded response carries no `Set-Cookie`, so saving a session the client does not already hold would write a row nothing can ever reference: a first-time visitor on a streamed endpoint would leave one unreachable session behind per reconnect. A session the request already names needs no cookie to be reachable and is saved as before. Clearing is unaffected: a cleared session is still deleted and its expiring cookie still emitted, whatever the response does. See [`requestNamesSession`](../../http/router_utility.go).

## Footguns & caveats

* Route names must be unique. URL generation relies on a [`RouteRegistry`](../../http/contract/route_registry.go) entry for the route name.
* An optional route parameter (`:param?`) is only legal as the **last** segment of a pattern. An omitted optional is dropped wherever it sits, while a match only ever ends early at the tail, so `/blog/:locale?/posts` would let [`UrlGenerator`](../../http/url_generator.go) mint `/blog/posts` — a path this router answers with a `404`. Registering such a pattern panics at the definition site; move the optional to the end, or register the two patterns separately.
* [`UrlGeneratorMustFromContainer`](../../http/service_resolver.go) is a fail-fast helper and will panic if `ServiceUrlGenerator` is missing or has an invalid type.
* [`RateLimitMiddleware`](../../http/middleware/rate_limit.go) keys on the client IP alone when no [`SetKeyExtractor`](../../http/middleware/rate_limit.go) is given, so `SimpleRateLimit(n)` is a budget of `n` requests per minute per IP **across the whole service**, not per route. Set an explicit key extractor for a per-route or per-user budget. The IP comes from the direct peer, so behind a reverse proxy every client collapses onto the proxy's address: pass [`NewForwardedClientIpResolver`](../../http/middleware/client_ip.go) to [`SetClientIpResolver`](../../http/middleware/rate_limit.go) — it walks `X-Forwarded-For` against the trusted-proxy policy and falls back to the direct peer whenever the chain cannot be trusted.
* Both in-memory limiters bound how many distinct keys they track ([`SetMaxKeys`](../../http/middleware/rate_limit.go), default 1,000,000), because the key is attacker-influenced and the map would otherwise grow without bound. Once the map is full and reclaiming idle entries frees nothing, a request under an unseen key is **denied** rather than tracked — a deliberate fail-closed choice, so size the ceiling above the distinct-client count you expect. Reclamation walks the map at most once per window, so the bound cannot be turned into a per-request cost.

* An `OPTIONS` request carrying an `Origin` but no `Access-Control-Request-Method` is **not** a preflight: it reaches the router, so a route registered for `OPTIONS` is served and every middleware inner to cors sees it. A browser sends that header on every real preflight, so no genuine one is lost. See [`Service.IsPreflight`](../../http/cors/service.go).
* `Vary: Origin` is emitted on **every** response the cors middleware and listener touch, including one produced for a rejected origin or for a request carrying no `Origin` at all, so a shared cache keyed on the url alone cannot hand an origin-less body to an allowed cross-origin requester. The header is added at most once. See [`addVaryOrigin`](../../http/cors/service.go).
* A credentialed configuration that decides origins through `AllowOriginFunc` is accepted; one with neither an origin list nor a function still panics on the defaulted wildcard, which is the combination the guard exists to prevent. See [`NewService`](../../http/cors/service.go).
* A conditional request for a static file is answered `304 Not Modified` when `If-None-Match` carries the entity tag anywhere in its comma-separated list, or carries it in the weak `W/` form a proxy may have rewritten it into. The wildcard `*` is deliberately **not** honoured — it would turn an attacker-supplied header into an unconditional 304. See [`EtagMatchesIfNoneMatch`](../../http/static/etag.go).
* The kernel publishes the scheme it resolved through the configured forwarded-headers policy on the request as [`RequestAttributeScheme`](../../http/request.go). A listener has no access to that policy, so re-detecting the scheme without it reports `http` for every request a trusted proxy terminated as `https`.
* The request attributes the kernel owns are reserved under the framework's underscore prefix (`_session`, `_scheme`) and are set **after** the route attributes, so a route attribute cannot replace the session object or the resolved scheme. Read them through [`RequestAttributeSession`](../../http/request.go) and [`RequestAttributeScheme`](../../http/request.go), never through the literal key. The session attribute is the one place the kernel reads back what a handler wrote: the response path saves and advertises whatever session it holds when the response is written, which is what makes [`RegenerateRequestSession`](../../http/session.go) work.
* A handler that panics with `net/http`'s `ErrAbortHandler` aborts the connection silently, as that sentinel documents, instead of being turned into a `500` plus an error log line. The check is on identity, so an application error merely wrapping the sentinel is unaffected.

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
* [`type SessionCookiePolicy`](../../http/contract/kernel.go)
* [`type SessionCookieSecurePolicy`](../../http/contract/kernel.go) — `SessionCookieSecureFromScheme` (zero value), `SessionCookieSecureAlways`, `SessionCookieSecureNever`
* [`type ForwardedHeadersPolicy`](../../http/contract/kernel.go)
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

* Route parameter requirements:
    * [`type Requirement`](../../http/constraint.go)
    * [`NewRequirement(parameterName string, pattern string) *Requirement`](../../http/constraint.go)
    * [`NewRequirements(requirements ...Requirement) map[string]string`](../../http/constraint.go)
    * [`RequireAlphaLowercase`](../../http/constraint.go) / [`RequireAlpha`](../../http/constraint.go) / [`RequireNumeric`](../../http/constraint.go) / [`RequireAlphaNumeric`](../../http/constraint.go)
    * Constants: [`ConstraintAlphaLowercase`, `ConstraintAlpha`, `ConstraintNumeric`, `ConstraintAlphaNumeric`](../../http/constraint.go)

* Response helpers:
    * [`JsonResponse`](../../http/response.go)
    * [`JsonErrorResponse`](../../http/response.go)
    * [`HtmlResponse`](../../http/response.go)
    * [`TextResponse`](../../http/response.go)
    * [`EmptyResponse`](../../http/response.go)
    * [`FileResponse`](../../http/response.go)
    * [`AttachmentResponse`](../../http/response.go)
    * [`BuildContentDisposition`](../../http/response.go)
    * [`RedirectResponse`](../../http/response.go)
    * [`RedirectFound`](../../http/response.go)
    * [`RedirectMovedPermanently`](../../http/response.go)

* Session:
    * [`RegenerateRequestSession(request httpcontract.Request) (sessioncontract.Session, error)`](../../http/session.go)

* Container helpers:
    * [`const ServiceRouteRegistry`](../../http/service_resolver.go)
    * [`const ServiceUrlGenerator`](../../http/service_resolver.go)
    * [`const ServiceRouter`](../../http/service_resolver.go)
    * [`const ServiceRequestContext`](../../http/service_resolver.go)
    * [`RouteRegistryMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)
    * [`UrlGeneratorMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)
    * [`RouterMustFromContainer(containercontract.Container)`](../../http/service_resolver.go)

### CORS (`http/cors`)

* Policy:
    * [`type Service`](../../http/cors/service.go)
    * [`type Config`](../../http/cors/service.go)
    * [`NewService`](../../http/cors/service.go)
    * [`DefaultService`](../../http/cors/service.go)
    * [`RestrictiveService`](../../http/cors/service.go)

* Middleware:
    * [`Middleware`](../../http/cors/middleware.go)
    * [`DefaultMiddleware`](../../http/cors/middleware.go)
    * [`Restrictive`](../../http/cors/middleware.go)

* Listener:
    * [`RegisterResponseListener`](../../http/cors/listener.go)

### Middleware (`http/middleware`)

* CORS:
    * [`type CorsConfig`](../../http/middleware/cors.go)
    * [`CorsMiddleware`](../../http/middleware/cors.go)
    * [`DefaultCorsMiddleware`](../../http/middleware/cors.go)

* Compression:
    * [`type CompressionConfig`](../../http/middleware/compression.go)
    * [`NewCompressionConfig`](../../http/middleware/compression.go)
    * [`DefaultCompressionConfig`](../../http/middleware/compression.go)
    * [`CompressionMiddleware`](../../http/middleware/compression.go)
    * [`DefaultCompressionMiddleware`](../../http/middleware/compression.go)
    * Honors `Accept-Encoding` q-values (RFC 7231), emits `Vary: Accept-Encoding`, and streams the gzip output through [`io.Pipe`](https://pkg.go.dev/io#Pipe) so response bodies never fully land in memory.

* Rate limiting:
    * [`RateLimitMiddleware`](../../http/middleware/rate_limit.go)
    * [`type RateLimitConfig`](../../http/middleware/rate_limit.go)
    * [`NewRateLimitConfig`](../../http/middleware/rate_limit.go)
    * [`type KeyExtractor`](../../http/middleware/rate_limit.go) (`func(httpcontract.Request) string`), set through [`RateLimitConfig.SetKeyExtractor`](../../http/middleware/rate_limit.go)
    * [`type OnLimitExceeded`](../../http/middleware/rate_limit.go) (`func(httpcontract.Request) (httpcontract.Response, error)`)
    * [`type ClientIpResolver`](../../http/middleware/rate_limit.go) (`func(httpcontract.Request) string`) — optional for trusted-proxy deployments
    * [`DefaultClientIp`](../../http/middleware/rate_limit.go)
    * [`NewForwardedClientIpResolver`](../../http/middleware/client_ip.go) — a `ClientIpResolver` that reads the client from `X-Forwarded-For` against the trusted-proxy policy; pass it to [`RateLimitConfig.SetClientIpResolver`](../../http/middleware/rate_limit.go) so per-IP limits behind a reverse proxy key on the client instead of the proxy
    * [`SimpleRateLimit`](../../http/middleware/rate_limit.go) / [`IpRateLimit`](../../http/middleware/rate_limit.go) / [`UserRateLimit`](../../http/middleware/rate_limit.go)
    * Limiters:
        * [`type FixedWindowLimiter`](../../http/middleware/rate_limit.go) — restores the whole allowance at the window edge rather than proportionally to elapsed time, so an instant straddling that edge can admit up to twice the rate; `SlidingWindowLimiter` holds the rate over every trailing window
        * [`type TokenBucketLimiter`](../../http/middleware/rate_limit.go) — deprecated alias of `FixedWindowLimiter`
        * [`NewFixedWindowLimiter`](../../http/middleware/rate_limit.go) / [`NewFixedWindowLimiterWithClock`](../../http/middleware/rate_limit.go)
        * [`NewTokenBucketLimiter`](../../http/middleware/rate_limit.go) / [`NewTokenBucketLimiterWithClock`](../../http/middleware/rate_limit.go) — deprecated
        * [`FixedWindowLimiter.SetMaxKeys(int)`](../../http/middleware/rate_limit.go)
        * [`type SlidingWindowLimiter`](../../http/middleware/rate_limit.go)
        * [`NewSlidingWindowLimiter`](../../http/middleware/rate_limit.go) / [`NewSlidingWindowLimiterWithClock`](../../http/middleware/rate_limit.go)
        * [`SlidingWindowLimiter.SetMaxKeys(int)`](../../http/middleware/rate_limit.go)

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
