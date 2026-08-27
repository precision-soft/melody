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
    * The method policy — whether `HEAD` falls back to the `GET` route, whether an unrouted `OPTIONS` is answered with the computed `Allow` header — is installed with [`Kernel.SetMethodPolicy`](../../http/kernel.go), which takes the [`MethodPolicy`](../../http/contract/kernel.go) of the contract; both halves default to on, which is what the framework has always done. An api that must answer `405` to `OPTIONS` starts from `DefaultKernelOptions().MethodPolicy` and turns the half it means off.
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

The HTTP kernel wraps `request.Body` with `http.MaxBytesReader` when `Http().MaxRequestBodyBytes()` is set to a positive value in the configuration (see [`kernel.go`](../../http/kernel.go)). A read past that limit fails with `*http.MaxBytesError` and net/http closes the connection, so an oversized body is never fully delivered to a controller. The `413 Request Entity Too Large` is Melody's answer on all three paths an overrun surfaces through. Two of them are the ones where Melody itself reads the body before the handler runs: the kernel's automatic form parsing and [`BindJson`](../../http/request_body.go) both recognize the overrun and answer `413` with `payload too large`. The third is the handler's own return: a `*http.MaxBytesError` a controller surfaces by returning it unhandled — `ParseMultipartForm` on an oversized upload is the case, multipart being the one body path the kernel does not pre-read — is mapped onto the same `413` at the chain's exit and filed at warning, rather
than rendering as a generic `500` at error. A controller that reads the raw body itself and answers for the error on its own keeps whatever answer it chose.

## Form parsing

[`NewRequest`](../../http/request.go) only auto-parses the request body as form data when the method is `POST`, `PUT`, or `PATCH` and the `Content-Type` media type is `application/x-www-form-urlencoded` or `multipart/form-data`. JSON, XML, and other content types are left intact so handlers can read the raw body. For explicit reparsing, use [`ParseFormBody`](../../http/request.go).

A urlencoded body is buffered before `ParseForm` drains it, so a later reader — the HMAC body-hash check above all — still sees the raw bytes. When that read **fails**, or when the body **does not parse** (an invalid percent escape), the kernel refuses the request instead of dispatching it: `413` when the request-body ceiling stopped the read, `400` otherwise, negotiated between html and json like every other kernel refusal. The handler is never reached, because either failure leaves a request whose form is indistinguishable from an empty submission — a real submission would otherwise be processed as a blank one. A multipart body is left untouched (`ParseForm` does not read it), so the disk spooling for large uploads is preserved.

`Request.Input` reads post, then query, then the route parameters, and delivers the single value a key carries; on a genuinely repeated key it answers the **first** value, because the caller asked by name and cannot act on a panic. The request bags keep the single and the repeated key apart by type: a key that appeared once is the string it is, while a repeated key (`?a=1&a=2`) is a slice, and reading the bag itself with `bag.String` panics naming the key — read it through `bag.StringSlice` over `Request.Query()`/`Request.Post()`.

## CORS

The [`http/cors`](../../http/cors) subpackage provides a dedicated CORS layer:

* [`Service`](../../http/cors/service.go) — policy holder built from a [`Config`](../../http/cors/service.go). A **nil** `AllowOrigins` expresses no preference and is defaulted to the wildcard; an **empty** one is an expressed preference — no origin is allowed — and denies every origin, so a list built from an environment variable that failed to arrive cannot widen into the wildcard. A credentialed configuration holding the defaulted wildcard panics, and so does one holding an empty list, since credentials with nothing to grant them to can only come from a list that failed to load — unless `Config.AllowOriginFunc` decides origins, since the list is never consulted once a function is set.
* [`Middleware`](../../http/cors/middleware.go) / [`DefaultMiddleware`](../../http/cors/middleware.go) / [`Restrictive`](../../http/cors/middleware.go) — answers a preflight with `204` and applies CORS headers on the happy path. A preflight is an `OPTIONS` carrying both an allowed `Origin` and an `Access-Control-Request-Method` ([`Service.IsPreflight`](../../http/cors/service.go)); a plain `OPTIONS` reaches the router instead. `Vary: Origin` is added to every response the chain produces, including one produced for a rejected origin or for a request carrying no `Origin` at all, so a shared cache cannot hand an origin-less body to an allowed cross-origin requester.
* [`RegisterRequestListener`](../../http/cors/listener.go) — subscribes to `kernel.request` with [`RequestListenerPriority`](../../http/cors/listener.go) (`100`), ahead of token resolution (`50`) and access control (`20`), and answers a well-formed preflight from an allowed origin there — which is what makes a preflight to a protected path answerable, since a preflight carries no credential. A disallowed origin and a bare `OPTIONS` fall through to the security chain untouched. An already-answered preflight from an allowed origin — a `429` from the rate limiter at priority `200` is the case — keeps the status that earlier listener chose, but is decorated with the cross-origin headers: a browser reads a preflight refusal only if it carries them, and an opaque one reaches the page as a bare network failure it cannot tell apart from a rate limit.
* [`RegisterResponseListener`](../../http/cors/listener.go) — subscribes to `kernel.response` with [`ResponseListenerPriority`](../../http/cors/listener.go) (`-100`) so CORS headers reach browsers even when error handlers replace the controller response.
* [`RegisterListeners`](../../http/cors/listener.go) — wires both doors, the preflight answer and the response headers, in one call. The middleware decorates the handler path only.

An allowlist entry is matched by [`OriginAllowed`](../../http/cors/service.go) in this order: `*` allows every origin; an entry that spells a scheme is compared whole, port included (`https://app.example.com`, or the subdomain form `https://*.example.com`, which matches only that scheme); an entry that spells **no** scheme — `example.com`, `*.example.com` — is compared against the request origin's **host with its port**. A scheme-less entry therefore allows that host under **any** scheme, `http://example.com` included, but only on the port it names: `example.com` matches the portless origin alone, and an entry meaning to admit a port writes it (`example.com:8443`). The port is significant because two ports of one host are two origins to a browser, and a grant that spanned them would extend to anything an attacker can get served from another port of an allowed host — with the credentials [`RestrictiveService`](../../http/cors/service.go) pairs with its list. The scheme-agnosticism
remains: it is the short way to admit a development origin served over plaintext, but it withholds exactly the downgrade protection an allowlist usually exists for, and nothing reports that: write the scheme out (`https://example.com`) unless permitting `http://` is the intent.

## Route matching

### Precedence

Two routes can both match one path, and which one runs is decided by [`match`](../../http/router.go) on two things only: the candidate with the **higher** `priority` ([`NewRouteOptions`](../../http/route_option.go)) wins, and among equal priorities the one **registered first** wins. Specificity plays no part — a static segment does not outrank a parameter, a short pattern does not outrank a catch-all, and the shape of the pattern is never compared. Only routes that match the request in full compete: host, scheme, `locales` and method are filtered before the tie-break, so a sibling registered for a different method is still reached.

Registration order is therefore load-bearing, and getting it wrong produces a route that is unreachable rather than an error:

* `/products/:id?` registered **before** `/products/create` answers `/products/create` itself, with `id` bound to `"create"`. The static sibling is dead code — nothing ever reaches it, and the parameterised handler is handed a path segment it will read as an identifier.
* `/files/*rest...` registered **before** `/files/readme.txt` swallows `/files/readme.txt` the same way: a catch-all is a candidate for every path under its prefix, and being registered earlier is all it takes to win.
* `priority` overrides both directions: a higher value wins wherever it was registered, and a lower one loses even from the front of the list.

A route that must not be shadowed therefore either goes in first, or says so explicitly:

```go
router.HandleWithOptions(
	"/products/create",
	createProductHandler,
	http.NewRouteOptions(
		"product.create",
		[]string{nethttp.MethodGet},
		"", nil, nil, nil, nil,
		100,
		nil,
	),
)

router.HandleNamed("product.show", nethttp.MethodGet, "/products/:id?", showProductHandler)
```

Two routes may share one **pattern** as long as the matcher can tell them apart. Collisions are checked twice at registration ([`registerRoute`](../../http/route_registry.go)): a duplicate name, and a route identical to a registered one in everything the matcher discriminates on — pattern, methods, host, schemes, locales, requirements and priority — because the later one could never be dispatched and would be shadowed silently. Either collision panics at the registration site, with `route name already exists` or with the refusal naming the identical discriminators; an application that prefers one aggregated report over dying at the first duplicate arms [`RouteRegistry.SetBootCollisionRecorder`](../../http/route_registry.go) for its boot window, under which the first registration wins — and on a name collision the route itself stays registered and dispatchable, only the name keeps pointing at the first claimant. The name and the defaults are not part of the dispatch identity: neither participates in matching, so two differently named but otherwise identical routes are still refused. Routes sharing a pattern under different methods, hosts, requirements or priorities are legitimately distinct and stay accepted; at equal specificity the higher priority wins and, at equal priority, the first registered.

### Locale-restricted routes

`locales` on [`NewRouteOptions`](../../http/route_option.go) is a whitelist checked against the route parameter named exactly `_locale` — the [`RouteAttributeLocale`](../../http/route.go) constant. A route whose `locales` list and whose pattern contradict each other is **refused at registration**, in both directions, because each shape used to fail in silence:

* declaring `locales` while neither the pattern nor the `defaults` supply `_locale` made the route match **nothing at all** — the whitelist read an empty value and rejected every request, with no panic at registration, no log line at match time, and a `404` indistinguishable from a path that was never registered;
* carrying `:_locale` in the pattern while declaring **no** `locales` list was the inverse: the whitelist returned early, the segment bound whatever the client sent, and the kernel published that unvalidated, client-chosen value as the request's locale to the translator and to every other consumer.

```go
/* works: the pattern binds _locale, so the whitelist has a value to check */
router.HandleWithOptions("/:_locale/list", listHandler, localizedOptions)

/* refused at registration: "locale" is not "_locale", so nothing supplies the whitelist */
router.HandleWithOptions("/:locale/list", listHandler, localizedOptions)
```

A wildcard named `*_locale` (single-segment or catch-all) binds it too. A route `defaults` entry now **does** supply it: defaults are merged before the whitelist reads them, in [`match`](../../http/router.go) and in [`AllowedMethods`](../../http/router_matching.go) alike, so a route that declares its locale through `Defaults{"_locale": "en"}` is reachable. It was not before — the gate rejected it sixteen lines before the value meant to satisfy it was filled in, while the kernel read the same map *after* the merge, so the router and the kernel disagreed about whether a default counted. Read the matched value back through [`Request.Locale`](../../http/request.go), which the kernel publishes from that same parameter, falling back to the configured default locale.

### The spelling a route is matched on

The router matches on the escaped path — `request.URL.EscapedPath()` in [`ServeHttp`](../../http/kernel.go) — and unescapes each segment **after** splitting on the separators the client actually sent. An encoded slash therefore stays inside the value where the client put it: `/admin%2Fusers` is one segment naming a resource called `admin/users`, not two segments reaching the `/admin/users` handler. Matching the decoded path made `%2F` a segment separator, so one url reached a handler that a proxy or WAF rule written against the raw request line had never seen — which is also why [`GeneratePath`](../../http/url_generator.go) refuses a `/` inside a parameter value.

A host is compared the way the header is defined: case-insensitively, and with the port significant **only when the route declared one**. A route bound to `example.com` matches `Example.com` and `example.com:8443`; a route bound to `example.com:8443` matches only that port.

A request target that is not a path is not path-routed at all. net/http hands the asterisk-form of `OPTIONS` through as the path `*`, which used to be read as a single segment and offered to every `/:param` route in the application. An empty path is read as the root, so a route registered as `/` answers it.

### Registration is boot-only

Every registration door — `Handle`, `HandleNamed`, `HandleController`, `HandleNamedController`, `HandleWithOptions`, and the same set on a [`RouteGroup`](../../http/router_group.go) — is **boot-only**, and refuses by name once [`Kernel.ServeHttp`](../../http/kernel.go) has built the handler. The same holds for the kernel's own configuration doors: `Use`, `SetNotFoundHandler`, `SetErrorHandler`, `SetForwardedHeadersPolicy`, `SetSessionCookiePolicy` and `SetMethodPolicy`.

The route table is a tree of plain maps that every request goroutine reads, so writing to it while requests are in flight is an unrecoverable fatal error rather than a torn read; the kernel's fields are an unsynchronized read on every request. Configuration is compiled once and then served. The **reading** doors are untouched and stay open for the life of the process — [`RouteDefinitions`](../../http/router_introspection.go), `RouteDefinition`, `RouteRegistry` and the introspection surface are read from inside handlers, which is how the openapi document and the route manifest are served.

## Url generation

[`UrlGenerator.GeneratePath`](../../http/url_generator.go) builds a path from a route name and a parameter map, and a route's `defaults` ([`NewRouteOptions`](../../http/route_option.go)) fill the gaps:

* A parameter the caller **omits** takes the route default, whatever that default is.
* A parameter the caller supplies **empty** takes the route default when that default is non-empty. This is what keeps a non-trailing optional segment — the shape [`rejectNonTrailingOptionalParameter`](../../http/router.go) admits precisely because its default keeps the segment present — generating a path the matcher answers: `/:locale?/list/:page` with `{"locale": "", "page": "2"}` yields `/en/list/2` under a `locale` default of `en`, not the `/list/2` the router replies to with a `404`.
* With no usable value left, an **optional** segment is dropped and a **required** one fails with `route parameter missing` or `route parameter may not be empty` — [`matchPath`](../../http/router_utility.go) refuses an empty segment for a named parameter, so emitting one would mint a url this router does not serve.

A requirement declared on a parameter is validated against the value that is actually emitted, catch-all remainders included, so generation and matching agree.

## HTTP method semantics

* [`HEAD`](../../http) requests are matched against explicit `HEAD` routes and also against `GET` routes. When a `GET` route handles a `HEAD` request, Melody keeps the same status code and headers as the `GET` handler while suppressing the response body during [`WriteToHttpResponseWriter`](../../http/response_writer.go).
* [`OPTIONS`](../../http) responses may be generated automatically by the HTTP kernel when a path matches but the incoming method does not map to a userland handler.
* The `Allow` header is derived from the methods registered for the matched path. When `GET` is registered, `HEAD` is also advertised in `Allow`.

## Middleware ordering

The chain is ordered by [`orderDefinitions`](../../http/middleware/pipeline/builder.go) and applied by [`wrapWithMiddlewaresRecording`](../../http/router_utility.go), which wraps from the last element inwards — so the **first** element of the built chain is the **outermost** one. The contract that follows:

* A **lower** priority value sits **further out**: it wraps everything after it, so it runs earlier on the way in and sees the response last on the way out. [`Use`](../../application/http_middleware.go) registers at `MiddlewarePriorityDefault` (`0`); [`UseWithPriority`](../../application/http_middleware.go) states the value.
* Middlewares registered at **equal priority** run in **registration order**, the first registered being the outer one. The tie-break is the registration rank, not the definition's name.
* `before` / `after` edges declared on a [`pipeline.NewHttpMiddlewareDefinition`](../../http/middleware/pipeline/definition.go) **override both**: they are topological constraints, and a definition is only ordered against its equals once everything it must follow has been placed. An edge naming a definition that is not in the pipeline fails the build with `middleware pipeline has missing references`, and a cycle fails it with `middleware pipeline has a cycle`; both panic during boot rather than shipping a silently reordered chain.

Position is not cosmetic, because a middleware that answers a request itself — returning a response without calling `next` — **short-circuits everything inside it**. Nothing ordered further in observes that request at all: no rate-limit accounting, no compression, no header the inner middleware would have added. The framework's own static middleware is one of those: [`StaticMiddleware`](../../http/middleware/static.go) returns the file it resolved and never calls `next`, so any middleware that must also apply to static assets has to sit **outside** it.

That static middleware is contributed as a default definition ([`defaultDefinitions`](../../application/http_middleware.go)) at a priority **below** the one `Use` registers at, which puts it **outermost**: a request for a file that exists is answered there and nothing registered through the application observes it, so a rate limiter does not spend a client's budget on the forty assets of one page and an authentication middleware does not guard the stylesheet of its own login page. The cost is the other side of that: a static response is not compressed and does not reach an access log written as a middleware. A middleware that must also apply to static assets is registered with [`UseWithPriority`](../../application/http_middleware.go) at a priority below the static one; an application that wants a directory served *behind* its own middleware registers a file server of its own, which sits inside the built-in one and receives only what that one declines — and names that directory in [
`MELODY_STATIC_EXCLUDED_PATHS`](#excluded-path-prefixes) so the built-in one does decline it. [`(*HttpMiddleware).LastBuildReport`](../../application/http_middleware.go) reports the chain a serving process actually built. `debug:middleware` does not render it: the command is wired to `describe`, which answers what the chain would be without building it and deliberately leaves the last build report alone — a console process never builds the serving chain at all, and `--build` runs an inspection build of its own that leaves it alone too. Read the order from the command's own listing rather than inferring it from the registration sites.

## Controller return contract

Controller functions wired through [`wrapControllerWithContainer`](../../http/router_utility.go) must return a first result that implements [`httpcontract.Response`](../../http/contract/response.go). The first result is not restricted to the concrete [`Response`](../../http/response.go) type; any implementation of the response contract is accepted.

For JSON-body endpoints, [`JsonHandler[Req](handle, ...options)`](../../http/typed_handler.go) wraps a handler so the framework decodes the request body into `Req` and runs the container validator before calling `handle(runtime, request, body)`; a decode/validation failure returns an error, or a caller-supplied response shape via [`WithJsonHandlerErrorResponder`](../../http/typed_handler.go). This removes the per-handler decode-and-validate block. The body is read through the same door `Request.BindJson` is, so the configured body limit answers 413, the decoder's own diagnosis travels as the refusal's cause, and a nil or empty body is refused by name. Validation travels the standardized envelope: the per-field detail reaches the client under `validationErrors` and a rule-wiring fault is recorded at error rather than at warning. The responder is handed the failure itself beside the status and the message, runs under the kernel's containment discipline, and cannot turn a refusal into a success — a responder that answers no response leaves the original refusal standing, where returning `(nil, nil)` used to be read by the kernel as a handler that answered nothing and served an empty 204. A nil handle or a nil responder is refused at construction, and a `null` body is read as empty for every nilable kind rather than for pointers alone.

## The error envelope

Every error the framework renders answers with one standardized body, whichever path rendered it — the kernel exception listener, each of the kernel's own fallback paths, and [`JsonErrorResponse`](../../http/response.go), which the security entry points answer through:

```json
{
    "status": 422,
    "time": "2026-08-21T12:00:00Z",
    "requestId": "b2f6…",
    "error": { "message": "validation failed" },
    "validationErrors": [ { "field": "email", "message": "…", "code": "…" } ]
}
```

`status` names the answer inside the body the way the status line names it outside; `requestId` appears where the request is known, and the same value travels on the `X-Request-Id` header. In debug mode the error object also carries `context` and `cause`. The per-field validation detail appears under `validationErrors` — the public half of an http exception's context, the one key the listener copies into the body — projected so an entry blaming the rule DECLARATION keeps its field, message and code and loses the internal context naming the developer's own typo (see the validation document's rule-declaration faults). v1/v2 spell that key `errors`; the v3 name is deliberate.

The body is negotiated the way the success path negotiates: a client that prefers html receives the html error page carrying the status, the escaped message and the request id; anything else goes through the serializer manager resolved against the joined `Accept` lines, serialized under a recover so an application serializer dying on the error payload cannot raise a second panic out of the kernel's recovery defer. The one deliberate asymmetry is fail-closed: an `Accept` header that refuses every available media type keeps the error's **own** status with the default json body, where the success path answers `406` — an error status is the signal itself, and masking a security refusal behind not-acceptable would hide it from the client that has to react to it. A runtime without a serializer manager, a failing serializer and an unmarshalable payload all fall back to the default json body under the same rule: an error response always exists.

The success half of the standardization — a data envelope over handler results, with a machine-readable error `code` beside the message — deliberately ships with the feature train; nothing about successful responses is enveloped today.

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

Handlers receive the raw `nethttp.ResponseWriter`, so they can stream a long-lived response instead of returning a buffered one. [`NewServerSentEventWriter`](../../http/server_sent_event.go) sets the `text/event-stream` headers and flushes after every [`Send`](../../http/server_sent_event.go). It refuses two things before it commits the response, because after the commit nothing can be answered any more: a response whose headers were already written, and a connection that cannot really flush. The capability probe reads THROUGH the kernel's response-writer wrapper rather than at it — that wrapper always carries a `Flush` method and forwards it only when its own delegate can flush, so an assertion made at the wrapper always succeeded. The returned writer is **safe for concurrent use**: the natural shape of a stream is a handler emitting events beside a ticker emitting keepalives, and a `net/http` ResponseWriter is not. It must not outlive the handler — that is what the hub exists to make unnecessary. A frame that failed partway poisons the writer, and every later call is refused rather than appended onto torn bytes.

`Send` refuses the shapes the grammar reads as something other than what the caller wrote: an event carrying a **name and no data** (the grammar discards such a frame without dispatching anything, so the listener the caller named never fires), an id or a name that would collapse to empty once its control bytes are removed, and a negative `Retry`. A zero `Retry` is the field's own zero value and means unset; a caller asking for an immediate reconnect names one millisecond. `Comment` deliberately ends the **frame** rather than the line: the blank line is what makes a comment-only keepalive observable to a client reading frame by frame, which is the point of the preamble a stream flushes at subscription time. The hazard a single newline would avoid — a keepalive landing between the fields of a half-built event — cannot arise, because `Send` composes every frame whole and writes it under the lock. A streaming handler returns `(nil, nil)` when the client disconnects (detected via `request.HttpRequest().Context().Done()`); the kernel writes nothing further because it only writes a response when one is returned.

[`ServerSentEventHub`](../../http/server_sent_event_hub.go) is an optional topic-keyed fan-out registry: [`Subscribe`](../../http/server_sent_event_hub.go) returns a buffered subscriber, [`Broadcast`](../../http/server_sent_event_hub.go) delivers an [`ServerSentEvent`](../../http/server_sent_event.go) to every subscriber of a topic (non-blocking — a full subscriber buffer drops the event), and [`Unsubscribe`](../../http/server_sent_event_hub.go) removes and closes it. This pairs naturally with the message bus: a message handler can `hub.Broadcast(...)` so domain events become real-time pushes.

Two framework-internal doors sit under this package's boundary refusals. [`internal.ValidateTrustedProxyList`](../../internal/trusted_proxy.go) is the one reader of a trusted-proxy list's shape, consulted by `Kernel.SetForwardedHeadersPolicy` and by `NewForwardedClientIpResolver` so the same typo is refused wherever the list is ingested; an empty entry is skipped, because a list split from an environment variable carries a trailing empty field. [`internal.RefuseNonJsonOutputTarget`](../../internal/atomic_file.go) and [`internal.WriteFileAtomically`](../../internal/atomic_file.go) are the one way a generated json artifact lands — the refusal that stops a mistyped `--out` from destroying someone's source, and the temp-file-and-rename that leaves the previous artifact intact when a write dies partway. `melody:routes:manifest` and `melody:openapi:generate` both write through them.

Give the hub a journal with [`SetLogger`](../../http/server_sent_event_hub.go). Without one it observes failures it cannot report: a publish that fails and a subscriber whose buffer overflows are counted into atomics nothing reads, so a backplane outage silences cross-node delivery on every node while each keeps serving its own subscribers. With one, a publish failure is recorded at error and a subscriber's **first** drop at warning — the later ones are not, because a consumer that stopped reading drops every event from then on. [`Close`](../../http/server_sent_event_hub.go) is `Shutdown` under the name the container's ordered teardown recognises, so a hub registered as a service is released with everything else; [`IsClosed`](../../http/server_sent_event_hub.go) answers the difference between a shut-down hub and an ordinary end of stream, which a caller's `range` over `Events()` cannot see. `Shutdown` closes the backplane it owns, waits for the publishes already past its closed check, and refuses to take a new backplane afterwards; `SetBackplane` refuses to install a backplane over a live one — the hub is the only holder of that reference, so an overwrite orphaned the previous one, and closing it from this door cannot be the remedy because the shipped backplanes clear themselves from the hub as the first step of their own `Close` and would re-enter it to clear the one just installed — and it reads a typed nil as the nothing it means. Clearing is always allowed, on a live hub and on a shut-down one, since that re-entry is exactly what a backplane's `Close` performs. A negative subscribe buffer is refused; zero takes the default.

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

## Route manifest

Routes can be exported to a frontend-facing JSON manifest, so a TypeScript/JavaScript client generates URLs by route name instead of hardcoding paths. A route opts in by setting the [`RouteAttributeExpose`](../../http/route.go) attribute to `true` in its route-options attributes — the helper [`ExposedRouteAttributes(zone)`](../../http/route.go) builds that map, optionally tagging the route with a [`RouteAttributeZone`](../../http/route.go) (`RouteZonePublic`, `RouteZoneInternal`, `RouteZoneFrontend`, `RouteZoneClient`). [`BuildRouteManifest`](../../http/route_manifest.go) projects the exposed **named** routes (an unnamed route cannot be referenced by the frontend) into a stable [`RouteManifest`](../../http/route_manifest.go) DTO, sorted by name — and by pattern under an equal name, because `sort.Slice` is not stable and the duplicate-name case is exactly the one in which the order decides which entry a consumer keyed by name keeps. It carries every field a generated url has to satisfy on the way back in: name, pattern, methods, **host**, **schemes**, **locales**, requirements, defaults, **priority**, zone; handlers and internal attributes are never leaked. The three emphasised fields used to be absent, and each absence produced the same outcome — the frontend minted a relative path against the current origin, the current scheme and no locale, and the router refused what it had just advertised. Requirements are published as the caller **declared** them, not as the anchored non-capturing form the registration compiles: the wrapped spelling is not the developer's, it re-wraps on every round trip, and it hands RE2-only syntax to a browser engine that cannot parse it. [`FilterRouteManifestByZone`](../../http/route_manifest.go) applies the zone gate in process, so a page or a bundle can narrow the document the way the command's `--zone` flag does — the gate lived only in the command, and the manifest injected into a page therefore carried every zone to every viewer. A zone that is not one of the declared values is refused, at `ExposedRouteAttributes` and at registration; so are an exposed route with no name, an expose attribute that is not a bool and a zone attribute that is not a string, each of which used to make the route silently absent from the artifact it was deliberately opted into. The [`RouteManifestCommand`](../../http/command_route_manifest.go) CLI command (
`melody:routes:manifest [--out <path>] [--zone <zone>]`) writes the manifest as JSON to a file or stdout, mirroring the OpenAPI generate command.

```go
router.HandleWithOptions(
	"/users/:id",
	showUserHandler,
	http.NewRouteOptions(
		"user_show",
		[]string{nethttp.MethodGet},
		"", nil,
		map[string]string{"id": `\d+`},
		nil, nil, 0,
		http.ExposedRouteAttributes(http.RouteZoneFrontend),
	),
)
```

On the frontend, the reference helper [`melody-routes.ts`](../../.example/assets/melody-routes.ts) (bundled into the example's frontend and available to vendor into your project; a usage example lives in [`.example/assets/routes-usage.ts`](../../.example/assets/routes-usage.ts)) loads the manifest and builds URLs from a route name and parameters with the same placeholder grammar the server-side router and Go `UrlGenerator` use, matched per path segment: required `:param`, optional `:param?` (allowed only as the **final** segment, and dropped when no value is given), single wildcard `*name`, and catch-all `*name...` (or a trailing `*name`, which may span multiple slash-separated segments). Parameters not consumed by a placeholder fall back to the route's defaults and any leftovers are appended as query-string parameters.

```ts
import { RouteGenerator, RouteManifest } from "./melody-routes";
import manifestJson from "./routes.json"; // melody:routes:manifest --out ./web/routes.json

const routes = new RouteGenerator(manifestJson as RouteManifest);

const userPath = routes.path("user_show", { id: 42 });                   // /users/42
const ordersPath = routes.path("user_show", { id: 42, tab: "orders" }); // /users/42?tab=orders
```

## Session cookie

A session write the storage could not take answers **500** rather than the response the handler produced. The handler wrote to the session and returned success on the assumption the write would land — a login answering `302 /dashboard` with the identity never stored — and the client cannot otherwise tell the difference from a line in the server log. The session cookie is suppressed either way, so the browser is never pointed at an id nothing persisted. The **delete** path is deliberately different: a failed logout still expires the browser cookie and serves the handler's response, because clearing a cookie can only end a session and never resurrect one. A save refused because another request deleted the session is a third case — the session ending, not a failure — and it expires the cookie and serves the handler's response unchanged. See [`writeResponse`](../../http/router_utility.go) and [`session.ErrSessionDeleted`](../../session/manager.go). A response that carries the session cookie is also marked `Cache-Control: private` unless it already says `private` or `no-store`, so a shared cache cannot replay one client's session id to the next, and the session-persistence records in the log carry a one-way SHA-256 reference to the session id rather than the credential itself.

The kernel loads a session from the incoming cookie, publishes it on the request as [`RequestAttributeSession`](../../http/request.go), and on the way out saves a modified session and emits its `Set-Cookie`. [`Kernel.SetSessionCookiePolicy`](../../http/contract/kernel.go) shapes that cookie through a [`SessionCookiePolicy`](../../http/contract/kernel.go):

| Field      | Type                                                         | Default when unset               |
|------------|--------------------------------------------------------------|----------------------------------|
| `Path`     | `string`                                                     | `/`                              |
| `Domain`   | `string`                                                     | omitted (host-only cookie)       |
| `SameSite` | `nethttp.SameSite`                                           | `SameSiteLaxMode`                |
| `Secure`   | [`SessionCookieSecurePolicy`](../../http/contract/kernel.go) | derived from the resolved scheme |

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

Call it **before** writing the authenticated identity, and write that identity to the session it returns. It reports an error rather than panicking when the request carries no session, has no runtime, or the session manager is not registered. [`session.Manager.RegenerateSession`](../../session/manager.go) is the storage-level primitive underneath, for code that holds no request; rotating through it means republishing the result yourself, and the session that went in is latched out of use so that forgetting to logs the client out rather than stranding it on a deleted id. `Session.Clear` is what makes a rotated-away session unresurrectable: clearing latches, so a later write puts a value back and marks the session modified without making it look live again. A `Session` implementation from outside this package is cleared through its own `Clear`, which may or may not latch.

### Streamed responses

No session row is written for a response the kernel discards because the handler already committed the headers — a streaming handler such as Server-Sent Events — **unless the request already names that session id** in its cookie.

A discarded response carries no `Set-Cookie`, so saving a session the client does not already hold would write a row nothing can ever reference: a first-time visitor on a streamed endpoint would leave one unreachable session behind per reconnect. A session the request already names needs no cookie to be reachable and is saved as before. See [`requestNamesSession`](../../http/router_utility.go).

Clearing is only half unaffected. The delete still happens — a cleared session is destroyed whatever the response does — but the expiring cookie never leaves the process: [`SetCookie`](../../http/cookie.go) adds it to the `Response` object, and [`WriteToHttpResponseWriter`](../../http/response_writer.go) is the only place a response's headers are copied onto the writer, which [`writeResponse`](../../http/router_utility.go) returns before reaching on the discard path. **No `Set-Cookie` of any kind reaches the client on a discarded response**, the `MaxAge: -1` clearing cookie included. The client keeps a cookie naming a row that is gone; its next request presents that id, [`Manager.Session`](../../session/manager.go) finds nothing under it, and the kernel hands out a fresh session instead. The values are unreachable either way, so this is not a session that stays authenticated — but the stale cookie is only replaced when some later ordinary response emits one, so put a logout on an
ordinary response rather than on a streamed one.

## Static file serving

### Excluded path prefixes

`MELODY_STATIC_EXCLUDED_PATHS` — parameter `kernel.static.excluded_paths`, read through [`Http().StaticExcludedPaths()`](../../config/http.go) — names the path prefixes the built-in file server declines without looking at the disk. The value is a **comma-separated** list whose entries are trimmed of surrounding whitespace, and it is empty by default: `MELODY_STATIC_EXCLUDED_PATHS=/admin, /api/internal` excludes two prefixes.

A declined request is not an error. [`StaticMiddleware`](../../http/middleware/static.go) calls `next`, so the request continues down the rest of the chain exactly as a request for a file that does not exist would. Because the built-in file server is the outermost middleware (see [Middleware ordering](#middleware-ordering)), what it declines is precisely what the chain an application registers through `Use` gets to see — which makes this list the one lever for taking a part of the url space back from it. Name a prefix here and it reaches your authentication middleware, a file server of your own with a narrower dot-prefix policy, or a handler serving it from a root of its own.

The comparison is a plain prefix test against the request path **as it arrived** — before the strip prefix is removed and before the path is folded — and it is the same test [`NewPathPrefixMatcher`](../../security/matcher.go) makes, so one spelling written in a firewall rule and here selects the same requests. Two consequences follow from that. An entry must begin with `/`, because a request path always does, and an empty entry would prefix-match every path and switch the file server off wholesale; validation rejects both at boot (`static excluded path must begin with a slash`, `static excluded path may not be empty`), so a stray comma fails the boot instead of silently taking static serving out of service. And since an entry is a prefix rather than a path element, `/admin` excludes `/administration` too. An entry written with a trailing slash additionally claims the bare spelling of its route — `/admin/` excludes `/admin` as well, and nothing wider — because the router reads the two spellings as the same route, and the firewall matcher reads its prefixes the same way.

### Canonical request paths

Outside the mount root, the file server answers only a path that is **already canonical**. It folds the received path with `path.Clean`, rebuilds the whole url around the configured strip prefix, and compares the result against `URL.Path` as it arrived; anything that differs is declined and the request falls through to the rest of the chain. `path.Clean` folds away `..`, a doubled slash, a `/./` element and a trailing slash, so `//css/app.css`, `/css//app.css`, `/css/app.css/` and `/./css/app.css` are all declined, `/open/../internal/secret.json` with them. See [`FileServer.Serve`](../../http/static/file_server.go).

The reason is that two parties read one request differently. A rule written on `/internal/` — a firewall entry, a reverse-proxy location, an [access-control matcher](SECURITY.md) — compares the raw path, and `//internal/secret.json` does not carry that prefix; the file server, folding the path before resolving it, would have found the file anyway. Refusing is what keeps the two views of the request in agreement. A redirect would not, because it teaches the client a spelling that reaches the file while sidestepping the rule. The comparison rebuilds the full path around the strip prefix rather than comparing only the remainder, so a doubled slash sitting exactly on the prefix boundary cannot be absorbed by the strip and pass unnoticed.

The mount **root** is the one path the rule reads generously, and only in the sense that both of its canonical spellings answer with the configured index file: the strip prefix with a trailing slash and without it. Every folded spelling of the root — `//`, `/.` and `/./` — is declined exactly like a folded spelling of any other path, because the two views of the request have to agree there as well.

The kernel enforces the same rule for the application's own routes: a request path that folds to a different spelling is refused with a `400` before it is routed, authorized or handled, so the router, the firewall matchers and the access control never disagree about which resource a request names. A trailing slash is not a fold and is served; a target that does not begin with `/` — the asterisk-form of `OPTIONS`, an authority-form `CONNECT` — is left to the router.

### Dot-prefixed paths

The file server refuses every request path carrying an element that begins with a dot. `.env`, `.git` and `.htpasswd` are what a deployment keeps beside its files and never means to publish, and the embedded mode packs them into the binary on purpose, because the embed directive spells `all:public` to keep the packed tree faithful to the directory — so both modes need the refusal. One allowance stands: the **first** element may name a dot-prefixed directory that appears in the file server's allowed-prefix list, which carries [`DefaultAllowedDotPrefix`](../../http/static/option.go) (`.well-known`) alone out of the box. RFC 8615 publishes the ACME http-01 challenge, `security.txt` and `assetlinks.json` there, and a deployment that renews its certificate through the application would otherwise lose the renewal to the refusal. The allowance never reaches past that first element, so `.well-known/.env` is refused exactly like `/.env`. See [
`hasDotPrefixedPathElement`](../../http/static/utility.go).

[`FileServerConfig.SetAllowedDotPrefixList`](../../http/static/option.go) replaces the list whole. An entry is compared against the complete first element, and an empty list — or `nil` — refuses every dot-prefixed path, `.well-known` included.

### Registering a file server of your own

The framework's built-in static middleware builds its [`FileServerConfig`](../../http/static/option.go) inside `newStaticFileServerOptions` — [`static_local.go`](../../application/static_local.go), or [`static_embedded.go`](../../application/static_embedded.go) under the `melody_static_embedded` build tag — out of `Configuration.Http()`, and that function is unexported, so that instance always serves the default dot-prefix list. No configuration key names those prefixes — `MELODY_STATIC_EXCLUDED_PATHS` decides which paths the built-in server declines outright, not which dot-prefixed elements it allows through. An application that needs a different list registers a static middleware of its own, the same way it would change anything else about static serving: [`static.NewFileServerConfig`](../../http/static/option.go), [`static.NewOptions`](../../http/static/option.go) and [`middleware.StaticMiddleware`](../../http/middleware/static.go) are all exported.

```go
func (instance *ExampleMiddlewareModule) RegisterHttpMiddlewares(
	kernelInstance kernelcontract.Kernel,
	registrar applicationcontract.HttpMiddlewareRegistrar,
) {
	httpConfiguration := kernelInstance.Config().Http()

	fileServerConfig := static.NewFileServerConfig(
		static.ModeFilesystem,
		httpConfiguration.PublicDir(),
		httpConfiguration.StaticIndexFile(),
		"",
		httpConfiguration.StaticEnableCache(),
		httpConfiguration.StaticCacheMaxAge(),
		false,
	)

	fileServerConfig.SetAllowedDotPrefixList(
		[]string{static.DefaultAllowedDotPrefix, ".artifacts"},
	)

	registrar.Use(
		middleware.StaticMiddleware(
			static.NewOptions(
				fileServerConfig,
				kernelInstance.Config().Kernel().ProjectDir(),
				nil,
			),
		),
	)
}
```

Where the list itself comes from is the application's business — a constant as above, an environment variable it reads itself, its own module configuration — because the application owns this construction. The third argument to [`NewOptions`](../../http/static/option.go) is the filesystem, consulted only in `static.ModeEmbedded`; under `static.ModeFilesystem` the server builds its own from the root and the public directory, which is why `nil` is passed above.

`Use` registers at `MiddlewarePriorityDefault` (`0`) and the built-in static middleware sits further out, so of the two the registered one is the **inner** (see [Middleware ordering](#middleware-ordering)). Each answers what it resolves and never calls `next`, so a registered file server receives only what the built-in one declines. Widening the list therefore lands as written — a dot-prefixed path the built-in refuses falls through and is answered by the file server that allows it — while narrowing it below the default does not land at all: a `.well-known` file that exists under the served directory is answered by the built-in server first, which is applying the default allowance, and the request never reaches the narrower one. Serving a directory *behind* the application's own middleware is the case this composition does fit: put the file server after an authentication middleware and keep its root out of the built-in server's reach — naming that root in [
`MELODY_STATIC_EXCLUDED_PATHS`](#excluded-path-prefixes) is how the second half is done, since otherwise the built-in server answers the file first and the middleware guarding it never runs.

## Footguns & caveats

* Server-Sent Events handlers must return `(nil, nil)` after streaming; returning a non-nil response would make the kernel write a second header/body.
* [`ServerSentEventHub.Broadcast`](../../http/server_sent_event_hub.go) is non-blocking and drops events for subscribers whose buffer is full; delivery is **at-most-once**. Size the subscribe buffer for the expected burst, or treat the stream as best-effort. [`ServerSentEventHub.DroppedEventCount`](../../http/server_sent_event_hub.go) returns the cumulative number of dropped events so the loss can be surfaced as a metric.
* Route names must be unique. URL generation relies on a [`RouteRegistry`](../../http/contract/route_registry.go) entry for the route name.
* An optional route parameter (`:param?`) is only legal as the **last** segment of a pattern, unless it carries a **non-empty default** — the default is always substituted, so the segment never drops. An omitted optional without one is dropped wherever it sits, while a match only ever ends early at the tail, so `/blog/:locale?/posts` would let [`UrlGenerator`](../../http/url_generator.go) mint `/blog/posts` — a path this router answers with a `404`. Registering the defaultless shape panics at the definition site; move the optional to the end, give it a non-empty default, or register the two patterns separately.
* [`UrlGeneratorMustFromContainer`](../../http/service_resolver.go) is a fail-fast helper and will panic if `ServiceUrlGenerator` is missing or has an invalid type.
* [`RateLimitMiddleware`](../../http/middleware/rate_limit.go) keys on the client IP alone when no [`SetKeyExtractor`](../../http/middleware/rate_limit.go) is given, so `SimpleRateLimit(n)` is a budget of `n` requests per minute per IP **across the whole service**, not per route. Set an explicit key extractor for a per-route or per-user budget. The IP comes from the direct peer, so behind a reverse proxy every client collapses onto the proxy's address: pass [`NewForwardedClientIpResolver`](../../http/middleware/client_ip.go) to [`SetClientIpResolver`](../../http/middleware/rate_limit.go) — it walks `X-Forwarded-For` against the trusted-proxy policy and falls back to the direct peer whenever the chain cannot be trusted. `SimpleRateLimit`, `IpRateLimit` and `UserRateLimit` build their config internally and return only the middleware, so there is nothing left to call `SetClientIpResolver` on: behind a proxy reach instead for [
  `SimpleRateLimitWithResolver`](../../http/middleware/rate_limit.go), [`IpRateLimitWithResolver`](../../http/middleware/rate_limit.go) or [`UserRateLimitWithResolver`](../../http/middleware/rate_limit.go), which take the resolver as their last argument — in the `User` variant it decides the anonymous fallback key alone, since a request carrying a user id is keyed on that id. The three plain helpers stay correct wherever the direct peer *is* the client.
* [`RegisterRateLimitRequestListener`](../../http/middleware/rate_limit_listener.go) subscribes to `kernel.request` with [`RateLimitRequestListenerPriority`](../../http/middleware/rate_limit_listener.go) (`200`), metering before the security chain: the middleware meters only what reaches the handler path, so a burst of wrong credentials consumes no budget there, while this door charges the burst and refuses it once the budget is gone. The default key is the client address, which exists before any token is resolved. Both doors share the configuration, so registering both meters a request once per door — use distinct budgets or one door.
* Both in-memory limiters bound how many distinct keys they track ([`SetMaxKeys`](../../http/middleware/rate_limit.go), default 1,000,000), because the key is attacker-influenced and the map would otherwise grow without bound. Once the map is full and reclaiming idle entries frees nothing, a request under an unseen key is **denied** rather than tracked — a deliberate fail-closed choice, so size the ceiling above the distinct-client count you expect. Reclamation walks the map at most once per window, so the bound cannot be turned into a per-request cost.

* An `OPTIONS` request carrying an `Origin` but no `Access-Control-Request-Method` is **not** a preflight: it reaches the router, so a route registered for `OPTIONS` is served and every middleware inner to cors sees it. A browser sends that header on every real preflight, so no genuine one is lost. See [`Service.IsPreflight`](../../http/cors/service.go).
* `Vary: Origin` is emitted on **every** response the cors middleware and listener touch, including one produced for a rejected origin or for a request carrying no `Origin` at all, so a shared cache keyed on the url alone cannot hand an origin-less body to an allowed cross-origin requester. The header is added at most once. See [`addVaryOrigin`](../../http/cors/service.go).
* A credentialed configuration that decides origins through `AllowOriginFunc` is accepted; one with neither an origin list nor a function still panics on the defaulted wildcard, which is the combination the guard exists to prevent. See [`NewService`](../../http/cors/service.go).
* **The static file server declines a request path that is not already canonical.** `//css/app.css`, `/css//app.css`, `/css/app.css/` and `/./css/app.css` are not served; the request falls through the chain and typically ends as a `404` (the mount root answers the index file under its two canonical spellings, with and without the trailing slash, and under no folded one). The rule closes an access-control bypass — see [Canonical request paths](#canonical-request-paths) — and it has teeth, because **a browser does not normalise a doubled slash: it sends the path verbatim.** A template that concatenates a base path ending in `/` with an asset path beginning with `/` emits `/static//app.css`, that is what goes out on the wire, and the page renders with no stylesheet on a deployment where the same markup worked before. Fix the concatenation; that is the whole remedy in nearly every case. Where the url genuinely cannot be changed, name its prefix in [`MELODY_STATIC_EXCLUDED_PATHS`](#excluded-path-prefixes) so those requests reach the application and answer
  them with a handler of its own.
* **Only a regular file is served.** A FIFO in the public directory would park the reading goroutine for as long as nobody writes the other end — and os.Open on a FIFO blocks by itself, which is why the mode is refused **before** the open and re-checked on the opened handle after it; a device node or a socket is refused the same way. Directories keep their own dispatch (the index file at the mount root, a refusal elsewhere). See [`openWithinBase`](../../http/static/utility.go).
* **The symlink containment check leaves one narrow window, deliberately.** The disk path is resolved with `EvalSymlinks`, checked against the base, and the resolved path is what is opened — so a component swapped between the check and the open cannot re-route the open through a symlink that was already resolved away. What remains is a component of the resolved path itself being replaced inside that window; closing it entirely needs `openat2`/`RESOLVE_BENEATH`, which is Linux-only, and the framework serves from a directory the deployment owns, so the residual window is documented rather than paid for with platform-specific code.
* A conditional request for a static file is answered `304 Not Modified` when `If-None-Match` carries the entity tag anywhere in its comma-separated list, or carries it in the weak `W/` form a proxy may have rewritten it into. The wildcard `*` is deliberately **not** honoured — it would turn an attacker-supplied header into an unconditional 304. See [`EtagMatchesIfNoneMatch`](../../http/static/etag.go).
* The entity tag a static response carries is a **truncated sha256 digest** of the file's size and modification time at nanosecond resolution ([`GenerateEtag`](../../http/static/etag.go)), never of its contents — and of the size and the **build version** on a filesystem that reports no modification time, which is every embedded one. The digest keeps every cache property of the derivation while disclosing none of it: spelled out, the tag told every anonymous client the file's modification instant to the nanosecond and, on embedded assets, the binary's build version. A rewrite that preserves both halves — a build step that restores timestamps to the nanosecond while keeping the length — still carries the previous tag, so a revalidating cache is answered `304 Not Modified` and goes on serving the stale bytes; where a deployment can rewrite a file in place, put a content hash in the url instead of trusting the tag. The tag, `Last-Modified` and `Cache-Control` are only emitted at all when static caching is enabled.
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
* [`type MethodPolicy`](../../http/contract/kernel.go) — the policy the section above calls "the `MethodPolicy` of the contract", declared beside the two policies listed here
* [`type Middleware`](../../http/contract/middleware.go)
* [`type RateLimiter`](../../http/contract/middleware.go), [`type RuntimeRateLimiter`](../../http/contract/middleware.go) — what a userland limiter implements, the second when it needs the runtime of the request it is deciding on
* [`type MatchResult`](../../http/contract/router.go) — what the router answers a match with
* [`type RequestContext`](../../http/contract/request_context.go) — the per-request identity resolved under `ServiceRequestContext`
* [`type UrlGenerationRouteDefinition`](../../http/contract/url_generation_route_definition.go) — the narrower view of a route the url generator reads

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
    * [`RouteZones() []string`](../../http/route.go), [`IsRouteZone(zone string) bool`](../../http/route.go) — the declared zones and the membership test the refusals name them by
    * [`FilterRouteManifestByZone(RouteManifest, zone string) RouteManifest`](../../http/route_manifest.go)
    * [`NewRequirements(requirements ...*Requirement) map[string]string`](../../http/constraint.go) — refuses an empty parameter name, an empty pattern and one parameter declared twice, each of which it used to drop in silence and each of which failed OPEN
    * [`RequireAlphaLowercase`](../../http/constraint.go) / [`RequireAlpha`](../../http/constraint.go) / [`RequireNumeric`](../../http/constraint.go) / [`RequireAlphaNumeric`](../../http/constraint.go)
    * Constants: [`ConstraintAlphaLowercase`, `ConstraintAlpha`, `ConstraintNumeric`, `ConstraintAlphaNumeric`](../../http/constraint.go)

* Server-Sent Events:
    * [`type ServerSentEvent`](../../http/server_sent_event.go)
    * [`type ServerSentEventWriter`](../../http/server_sent_event.go) with [`NewServerSentEventWriter(nethttp.ResponseWriter) (*ServerSentEventWriter, error)`](../../http/server_sent_event.go), [`(*ServerSentEventWriter).Send(ServerSentEvent) error`](../../http/server_sent_event.go), [`(*ServerSentEventWriter).Comment(string) error`](../../http/server_sent_event.go), [`(*ServerSentEventWriter).Ping() error`](../../http/server_sent_event.go). Both `Send` (`Id`/`Event`/`Data`) and `Comment` strip `CR`/`LF` from caller-supplied text so a dynamic value cannot inject extra Server-Sent Events fields or events; `Send` additionally treats a bare `CR`, `LF`, or `CRLF` inside `Data` as a data-line boundary per the EventSource specification.
    * [`type ServerSentEventHub`](../../http/server_sent_event_hub.go) with [`NewServerSentEventHub()`](../../http/server_sent_event_hub.go), [`Subscribe(topic string, bufferSize int) *ServerSentEventSubscriber`](../../http/server_sent_event_hub.go), [`Unsubscribe(*ServerSentEventSubscriber)`](../../http/server_sent_event_hub.go), [`Broadcast(topic string, event ServerSentEvent) int`](../../http/server_sent_event_hub.go), [`DeliverLocal(topic string, event ServerSentEvent) int`](../../http/server_sent_event_hub.go), [`SubscriberCount(topic string) int`](../../http/server_sent_event_hub.go), [`DroppedEventCount() uint64`](../../http/server_sent_event_hub.go), and the cross-instance backplane/shutdown surface [`SetBackplane(ServerSentEventBackplane)`](../../http/server_sent_event_hub.go), [`BackplaneFailures() uint64`](../../http/server_sent_event_hub.go), [`Shutdown()`](../../http/server_sent_event_hub.go), [`Close() error`](../../http/server_sent_event_hub.go), [`IsClosed() bool`](../../http/server_sent_event_hub.go), [`SetLogger(loggingcontract.Logger)`](../../http/server_sent_event_hub.go)
    * [`type ServerSentEventSubscriber`](../../http/server_sent_event_hub.go) with [`(*ServerSentEventSubscriber).Events() <-chan ServerSentEvent`](../../http/server_sent_event_hub.go), [`(*ServerSentEventSubscriber).DroppedCount() uint64`](../../http/server_sent_event_hub.go), [`(*ServerSentEventSubscriber).Topic() string`](../../http/server_sent_event_hub.go)

* Response helpers:
    * [`JsonResponse`](../../http/response.go)
    * [`JsonErrorResponse`](../../http/response.go)
    * [`HtmlResponse`](../../http/response.go)
    * [`TextResponse`](../../http/response.go)
    * [`EmptyResponse`](../../http/response.go)
    * [`FileResponse`](../../http/response.go) — opens the path as given, with no containment check; never hand it a path built from client input without confining the name to a known directory first (see the GoDoc)
    * [`AttachmentResponse`](../../http/response.go) — `FileResponse` plus a `Content-Disposition`; the same path-safety caveat applies
    * [`ConfinedFileResponse`](../../http/response.go) / [`ConfinedAttachmentResponse`](../../http/response.go) — the doors built for a client-steered name: the name is refused when absolute or climbing, the joined path is resolved through every symlink and checked against the resolved root, and only a regular file is answered
    * [`BuildContentDisposition`](../../http/response.go)
    * [`RedirectResponse`](../../http/response.go) — refuses by panic a location that leaves the application (a scheme, a scheme-relative `//`, a backslash), because a location built from client input is how an open redirect is minted; `RedirectFound` and `RedirectMovedPermanently` inherit the guard
    * [`RedirectExternalResponse`](../../http/response.go) — the unguarded form, whose name is the caller's assertion that the target is trusted
    * [`RedirectFound`](../../http/response.go)
    * [`RedirectMovedPermanently`](../../http/response.go)

* Cookies:
    * [`SetCookie(response httpcontract.Response, cookie *nethttp.Cookie)`](../../http/cookie.go) — appends one `Set-Cookie`, refusing an empty name with a panic and creating the header map when the response carries none
    * [`DeleteCookie(response httpcontract.Response, name string, path string) `](../../http/cookie.go) — the expiring counterpart; an empty path is read as `/`. A `__Secure-` name is deleted with `Secure`, and a `__Host-` name with `Secure` and path `/` whatever path was passed — the browser rejects a prefixed Set-Cookie that breaks its prefix contract, the deleting one included, so a deletion written without them silently never happened

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
    * [`RequestContextMustFromResolver(containercontract.Resolver)`](../../http/service_resolver.go) / [`RequestContextFromResolver(containercontract.Resolver)`](../../http/service_resolver.go) — the only accessors of `ServiceRequestContext`, which lives on the request scope and so takes a resolver rather than the container; the second answers nil where the first panics

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
    * [`RegisterListeners`](../../http/cors/listener.go)
    * [`RegisterRequestListener`](../../http/cors/listener.go)
    * [`RegisterResponseListener`](../../http/cors/listener.go)

### Middleware (`http/middleware`)

* CORS:
    * [`type CorsConfig`](../../http/middleware/cors.go) — deprecated, use `http/cors.Service`
    * [`CorsMiddleware`](../../http/middleware/cors.go) — deprecated, use `http/cors.Middleware`
    * [`DefaultCorsMiddleware`](../../http/middleware/cors.go) — deprecated, use `http/cors.DefaultMiddleware`
    * [`NewCorsConfig`](../../http/middleware/cors.go) — deprecated, use `http/cors.NewService`
    * [`DefaultCorsConfig`](../../http/middleware/cors.go) — deprecated, use `http/cors.DefaultService`
    * [`RestrictiveCorsConfig`](../../http/middleware/cors.go) — deprecated, use `http/cors.RestrictiveService`
    * [`RestrictiveCors`](../../http/middleware/cors.go) — deprecated, use `http/cors.Restrictive`

* Compression:
    * [`type CompressionConfig`](../../http/middleware/compression.go)
    * [`NewCompressionConfig`](../../http/middleware/compression.go)
    * [`DefaultCompressionConfig`](../../http/middleware/compression.go)
    * [`CompressionMiddleware`](../../http/middleware/compression.go)
    * [`DefaultCompressionMiddleware`](../../http/middleware/compression.go)
    * Honors `Accept-Encoding` q-values (RFC 7231), emits `Vary: Accept-Encoding`, and streams the gzip output through [`io.Pipe`](https://pkg.go.dev/io#Pipe) so response bodies never fully land in memory.

* Rate limiting:
    * [`RateLimitMiddleware`](../../http/middleware/rate_limit.go)
    * [`RegisterRateLimitRequestListener`](../../http/middleware/rate_limit_listener.go) — the `kernel.request` door at [`RateLimitRequestListenerPriority`](../../http/middleware/rate_limit_listener.go) (`200`), metering before the security chain; the default key is the client address
    * [`type RateLimitConfig`](../../http/middleware/rate_limit.go)
    * [`NewRateLimitConfig`](../../http/middleware/rate_limit.go)
    * [`type KeyExtractor`](../../http/middleware/rate_limit.go) (`func(httpcontract.Request) string`), set through [`RateLimitConfig.SetKeyExtractor`](../../http/middleware/rate_limit.go)
    * [`type OnLimitExceeded`](../../http/middleware/rate_limit.go) (`func(httpcontract.Request) (httpcontract.Response, error)`)
    * [`type ClientIpResolver`](../../http/middleware/rate_limit.go) (`func(httpcontract.Request) string`) — optional for trusted-proxy deployments
    * [`DefaultClientIp`](../../http/middleware/rate_limit.go)
    * [`NewForwardedClientIpResolver`](../../http/middleware/client_ip.go) — a `ClientIpResolver` that reads the client from `X-Forwarded-For` against the trusted-proxy policy; pass it to [`RateLimitConfig.SetClientIpResolver`](../../http/middleware/rate_limit.go), or to one of the `WithResolver` helpers below, so per-IP limits behind a reverse proxy key on the client instead of the proxy
    * [`SimpleRateLimit`](../../http/middleware/rate_limit.go) / [`IpRateLimit`](../../http/middleware/rate_limit.go) / [`UserRateLimit`](../../http/middleware/rate_limit.go) — configured internally, so the address they key on is the direct peer
    * [`SimpleRateLimitWithResolver(requestsPerMinute int, clientIpResolver ClientIpResolver)`](../../http/middleware/rate_limit.go) / [`IpRateLimitWithResolver(requestsPerMinute int, clientIpResolver ClientIpResolver)`](../../http/middleware/rate_limit.go) / [`UserRateLimitWithResolver(requestsPerMinute int, getUserId KeyExtractor, clientIpResolver ClientIpResolver)`](../../http/middleware/rate_limit.go) — the same three with the client address read through the given resolver; a `nil` resolver keeps the direct peer
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
    * [`ServeReader`](../../http/static/file_server.go) — the path every request actually takes: it answers the status, the headers and the body as a reader the caller closes
    * [`Serve`](../../http/static/file_server.go) — the whole-response form

* [`type FileServerConfig`](../../http/static/option.go)
    * [`NewFileServerConfig`](../../http/static/option.go)
    * [`(*FileServerConfig).SetAllowedDotPrefixList(allowedDotPrefixList []string)`](../../http/static/option.go) — names the dot-prefixed **first** path elements the server may retrieve; `nil` or an empty list refuses every dot-prefixed path
    * [`const DefaultAllowedDotPrefix`](../../http/static/option.go) (`.well-known`) — the only entry a newly built config carries
    * [`(*FileServerConfig).SetExcludedPathList(excludedPathList []string)`](../../http/static/option.go) — names the path prefixes the server declines without touching the disk, compared against the request path as it arrived; the built-in server is given [`Http().StaticExcludedPaths()`](../../config/http.go)

* [`type Mode`](../../http/static/option.go) — `ModeFilesystem` / `ModeEmbedded`

* [`type Options`](../../http/static/option.go)
    * [`NewOptions`](../../http/static/option.go)

* [`GenerateEtag`](../../http/static/etag.go)
