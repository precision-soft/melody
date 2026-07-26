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

An allowlist entry is matched by [`OriginAllowed`](../../http/cors/service.go) in this order: `*` allows every origin; an entry that spells a scheme is compared whole, port included (`https://app.example.com`, or the subdomain form `https://*.example.com`, which matches only that scheme); an entry that spells **no** scheme — `example.com`, `*.example.com` — is compared against the request origin's **host alone**. A scheme-less entry therefore allows that host under **any** scheme and on any port, `http://example.com` included. It is the short way to admit a development origin served over plaintext, but it withholds exactly the downgrade protection an allowlist usually exists for, and nothing reports that: write the scheme out (`https://example.com`) unless permitting `http://` is the intent.

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

Two routes may also share one **pattern** under two different names. Only names are checked for collisions — a duplicate name panics at registration with `route name already exists` ([`registerRoute`](../../http/route_registry.go)) — so a duplicated pattern is accepted silently, the earlier registration serves every request, and the later one is unreachable while [`UrlGenerator`](../../http/url_generator.go) still resolves its name and mints its path. Links generated for the second route run the first route's handler.

### Locale-restricted routes

`locales` on [`NewRouteOptions`](../../http/route_option.go) is a whitelist checked against the route parameter named exactly `_locale` — the [`RouteAttributeLocale`](../../http/route.go) constant. [`match`](../../http/router.go) reads `params["_locale"]`, so a pattern that binds no parameter under that name yields an empty value, which fails the whitelist for **every** request: the route matches nothing at all. There is no diagnostic — no panic at registration, no log line at match time, and a `404` indistinguishable from a path that was never registered.

```go
/* works: the pattern binds _locale, so the whitelist has a value to check */
router.HandleWithOptions("/:_locale/list", listHandler, localizedOptions)

/* permanently dead: "locale" is not "_locale", so every request fails the whitelist */
router.HandleWithOptions("/:locale/list", listHandler, localizedOptions)
```

A wildcard named `*_locale` (single-segment or catch-all) binds it too. A route `defaults` entry does not rescue the pattern: defaults are applied **after** the whitelist has already rejected the route. Name the parameter `_locale` whenever the route carries `locales`, and read the matched value back through [`Request.Locale`](../../http/request.go), which the kernel publishes from that same parameter, falling back to the configured default locale.

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

## Middleware ordering

The chain is ordered by [`orderDefinitions`](../../http/middleware/pipeline/builder.go) and applied by [`wrapWithMiddlewares`](../../http/router_utility.go), which wraps from the last element inwards — so the **first** element of the built chain is the **outermost** one. The contract that follows:

* A **lower** priority value sits **further out**: it wraps everything after it, so it runs earlier on the way in and sees the response last on the way out. [`Use`](../../application/http_middleware.go) registers at `MiddlewarePriorityDefault` (`0`); [`UseWithPriority`](../../application/http_middleware.go) states the value.
* Middlewares registered at **equal priority** run in **registration order**, the first registered being the outer one. The tie-break is the registration rank, not the definition's name.
* `before` / `after` edges declared on a [`pipeline.NewHttpMiddlewareDefinition`](../../http/middleware/pipeline/definition.go) **override both**: they are topological constraints, and a definition is only ordered against its equals once everything it must follow has been placed. An edge naming a definition that is not in the pipeline fails the build with `middleware pipeline has missing references`, and a cycle fails it with `middleware pipeline has a cycle`; both panic during boot rather than shipping a silently reordered chain.

Position is not cosmetic, because a middleware that answers a request itself — returning a response without calling `next` — **short-circuits everything inside it**. Nothing ordered further in observes that request at all: no rate-limit accounting, no compression, no header the inner middleware would have added. The framework's own static middleware is one of those: [`StaticMiddleware`](../../http/middleware/static.go) returns the file it resolved and never calls `next`, so any middleware that must also apply to static assets has to sit **outside** it.

That static middleware is contributed as a default definition ([`defaultDefinitions`](../../application/http_middleware.go)) at a priority **below** the one `Use` registers at, which puts it **outermost**: a request for a file that exists is answered there and nothing registered through the application observes it, so a rate limiter does not spend a client's budget on the forty assets of one page and an authentication middleware does not guard the stylesheet of its own login page. The cost is the other side of that: a static response is not compressed and does not reach an access log written as a middleware. A middleware that must also apply to static assets is registered with [`UseWithPriority`](../../application/http_middleware.go) at a priority below the static one; an application that wants a directory served *behind* its own middleware registers a file server of its own, which sits inside the built-in one and receives only what that one declines — and names that directory in [`MELODY_STATIC_EXCLUDED_PATHS`](#excluded-path-prefixes) so the built-in one does decline it. [`(*HttpMiddleware).LastBuildReport`](../../application/http_middleware.go) reports the chain that was actually built and `debug:middleware` renders it — read the order there rather than inferring it from the registration sites.

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

A discarded response carries no `Set-Cookie`, so saving a session the client does not already hold would write a row nothing can ever reference: a first-time visitor on a streamed endpoint would leave one unreachable session behind per reconnect. A session the request already names needs no cookie to be reachable and is saved as before. See [`requestNamesSession`](../../http/router_utility.go).

Clearing is only half unaffected. The delete still happens — a cleared session is destroyed whatever the response does — but the expiring cookie never leaves the process: [`SetCookie`](../../http/cookie.go) adds it to the `Response` object, and [`WriteToHttpResponseWriter`](../../http/response_writer.go) is the only place a response's headers are copied onto the writer, which [`writeResponse`](../../http/router_utility.go) returns before reaching on the discard path. **No `Set-Cookie` of any kind reaches the client on a discarded response**, the `MaxAge: -1` clearing cookie included. The client keeps a cookie naming a row that is gone; its next request presents that id, [`Manager.Session`](../../session/manager.go) finds nothing under it, and the kernel hands out a fresh session instead. The values are unreachable either way, so this is not a session that stays authenticated — but the stale cookie is only replaced when some later ordinary response emits one, so put a logout on an ordinary response rather than on a streamed one.

## Static file serving

### Excluded path prefixes

`MELODY_STATIC_EXCLUDED_PATHS` — parameter `kernel.static.excluded_paths`, read through [`Http().StaticExcludedPaths()`](../../config/http.go) — names the path prefixes the built-in file server declines without looking at the disk. The value is a **comma-separated** list whose entries are trimmed of surrounding whitespace, and it is empty by default: `MELODY_STATIC_EXCLUDED_PATHS=/admin, /api/internal` excludes two prefixes.

A declined request is not an error. [`StaticMiddleware`](../../http/middleware/static.go) calls `next`, so the request continues down the rest of the chain exactly as a request for a file that does not exist would. Because the built-in file server is the outermost middleware (see [Middleware ordering](#middleware-ordering)), what it declines is precisely what the chain an application registers through `Use` gets to see — which makes this list the one lever for taking a part of the url space back from it. Name a prefix here and it reaches your authentication middleware, a file server of your own with a narrower dot-prefix policy, or a handler serving it from a root of its own.

The comparison is a plain prefix test against the request path **as it arrived** — before the strip prefix is removed and before the path is folded — and it is the same test [`NewPathPrefixMatcher`](../../security/matcher.go) makes, so one spelling written in a firewall rule and here selects the same requests. Two consequences follow from that. An entry must begin with `/`, because a request path always does, and an empty entry would prefix-match every path and switch the file server off wholesale; validation rejects both at boot (`static excluded path must begin with a slash`, `static excluded path may not be empty`), so a stray comma fails the boot instead of silently taking static serving out of service. And since an entry is a prefix rather than a path element, `/admin` excludes `/administration` too.

### Canonical request paths

Outside the mount root, the file server answers only a path that is **already canonical**. It folds the received path with `path.Clean`, rebuilds the whole url around the configured strip prefix, and compares the result against `URL.Path` as it arrived; anything that differs is declined and the request falls through to the rest of the chain. `path.Clean` folds away `..`, a doubled slash, a `/./` element and a trailing slash, so `//css/app.css`, `/css//app.css`, `/css/app.css/` and `/./css/app.css` are all declined, `/open/../internal/secret.json` with them. See [`FileServer.Serve`](../../http/static/file_server.go).

The reason is that two parties read one request differently. A rule written on `/internal/` — a firewall entry, a reverse-proxy location, an [access-control matcher](SECURITY.md) — compares the raw path, and `//internal/secret.json` does not carry that prefix; the file server, folding the path before resolving it, would have found the file anyway. Refusing is what keeps the two views of the request in agreement. A redirect would not, because it teaches the client a spelling that reaches the file while sidestepping the rule. The comparison rebuilds the full path around the strip prefix rather than comparing only the remainder, so a doubled slash sitting exactly on the prefix boundary cannot be absorbed by the strip and pass unnoticed.

The spellings that fold onto the **root** are the exception and still answer with the configured index file — `/`, `//`, `/.` and `/./` all serve it — because that page is what a browser asks for by visiting the site, and the index file is named by configuration and never by the request, so no spelling can aim that resolution at another file.

### Dot-prefixed paths

The file server refuses every request path carrying an element that begins with a dot. `.env`, `.git` and `.htpasswd` are what a deployment keeps beside its files and never means to publish, and the embedded mode packs them into the binary on purpose, because the embed directive spells `all:public` to keep the packed tree faithful to the directory — so both modes need the refusal. One allowance stands: the **first** element may name a dot-prefixed directory that appears in the file server's allowed-prefix list, which carries [`DefaultAllowedDotPrefix`](../../http/static/option.go) (`.well-known`) alone out of the box. RFC 8615 publishes the ACME http-01 challenge, `security.txt` and `assetlinks.json` there, and a deployment that renews its certificate through the application would otherwise lose the renewal to the refusal. The allowance never reaches past that first element, so `.well-known/.env` is refused exactly like `/.env`. See [`hasDotPrefixedPathElement`](../../http/static/utility.go).

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

`Use` registers at `MiddlewarePriorityDefault` (`0`) and the built-in static middleware sits further out, so of the two the registered one is the **inner** (see [Middleware ordering](#middleware-ordering)). Each answers what it resolves and never calls `next`, so a registered file server receives only what the built-in one declines. Widening the list therefore lands as written — a dot-prefixed path the built-in refuses falls through and is answered by the file server that allows it — while narrowing it below the default does not land at all: a `.well-known` file that exists under the served directory is answered by the built-in server first, which is applying the default allowance, and the request never reaches the narrower one. Serving a directory *behind* the application's own middleware is the case this composition does fit: put the file server after an authentication middleware and keep its root out of the built-in server's reach — naming that root in [`MELODY_STATIC_EXCLUDED_PATHS`](#excluded-path-prefixes) is how the second half is done, since otherwise the built-in server answers the file first and the middleware guarding it never runs.

## Footguns & caveats

* Route names must be unique. URL generation relies on a [`RouteRegistry`](../../http/contract/route_registry.go) entry for the route name.
* An optional route parameter (`:param?`) is only legal as the **last** segment of a pattern. An omitted optional is dropped wherever it sits, while a match only ever ends early at the tail, so `/blog/:locale?/posts` would let [`UrlGenerator`](../../http/url_generator.go) mint `/blog/posts` — a path this router answers with a `404`. Registering such a pattern panics at the definition site; move the optional to the end, or register the two patterns separately.
* [`UrlGeneratorMustFromContainer`](../../http/service_resolver.go) is a fail-fast helper and will panic if `ServiceUrlGenerator` is missing or has an invalid type.
* [`RateLimitMiddleware`](../../http/middleware/rate_limit.go) keys on the client IP alone when no [`SetKeyExtractor`](../../http/middleware/rate_limit.go) is given, so `SimpleRateLimit(n)` is a budget of `n` requests per minute per IP **across the whole service**, not per route. Set an explicit key extractor for a per-route or per-user budget. The IP comes from the direct peer, so behind a reverse proxy every client collapses onto the proxy's address: pass [`NewForwardedClientIpResolver`](../../http/middleware/client_ip.go) to [`SetClientIpResolver`](../../http/middleware/rate_limit.go) — it walks `X-Forwarded-For` against the trusted-proxy policy and falls back to the direct peer whenever the chain cannot be trusted. `SimpleRateLimit`, `IpRateLimit` and `UserRateLimit` build their config internally and return only the middleware, so there is nothing left to call `SetClientIpResolver` on: behind a proxy reach instead for [`SimpleRateLimitWithResolver`](../../http/middleware/rate_limit.go), [`IpRateLimitWithResolver`](../../http/middleware/rate_limit.go) or [`UserRateLimitWithResolver`](../../http/middleware/rate_limit.go), which take the resolver as their last argument — in the `User` variant it decides the anonymous fallback key alone, since a request carrying a user id is keyed on that id. The three plain helpers stay correct wherever the direct peer *is* the client.
* Both in-memory limiters bound how many distinct keys they track ([`SetMaxKeys`](../../http/middleware/rate_limit.go), default 1,000,000), because the key is attacker-influenced and the map would otherwise grow without bound. Once the map is full and reclaiming idle entries frees nothing, a request under an unseen key is **denied** rather than tracked — a deliberate fail-closed choice, so size the ceiling above the distinct-client count you expect. Reclamation walks the map at most once per window, so the bound cannot be turned into a per-request cost.

* An `OPTIONS` request carrying an `Origin` but no `Access-Control-Request-Method` is **not** a preflight: it reaches the router, so a route registered for `OPTIONS` is served and every middleware inner to cors sees it. A browser sends that header on every real preflight, so no genuine one is lost. See [`Service.IsPreflight`](../../http/cors/service.go).
* `Vary: Origin` is emitted on **every** response the cors middleware and listener touch, including one produced for a rejected origin or for a request carrying no `Origin` at all, so a shared cache keyed on the url alone cannot hand an origin-less body to an allowed cross-origin requester. The header is added at most once. See [`addVaryOrigin`](../../http/cors/service.go).
* A credentialed configuration that decides origins through `AllowOriginFunc` is accepted; one with neither an origin list nor a function still panics on the defaulted wildcard, which is the combination the guard exists to prevent. See [`NewService`](../../http/cors/service.go).
* **The static file server declines a request path that is not already canonical.** `//css/app.css`, `/css//app.css`, `/css/app.css/` and `/./css/app.css` are not served; the request falls through the chain and typically ends as a `404` (the spellings that fold onto the root still serve the index file). The rule closes an access-control bypass — see [Canonical request paths](#canonical-request-paths) — and it has teeth, because **a browser does not normalise a doubled slash: it sends the path verbatim.** A template that concatenates a base path ending in `/` with an asset path beginning with `/` emits `/static//app.css`, that is what goes out on the wire, and the page renders with no stylesheet on a deployment where the same markup worked before. Fix the concatenation; that is the whole remedy in nearly every case. Where the url genuinely cannot be changed, name its prefix in [`MELODY_STATIC_EXCLUDED_PATHS`](#excluded-path-prefixes) so those requests reach the application and answer them with a handler of its own.
* A conditional request for a static file is answered `304 Not Modified` when `If-None-Match` carries the entity tag anywhere in its comma-separated list, or carries it in the weak `W/` form a proxy may have rewritten it into. The wildcard `*` is deliberately **not** honoured — it would turn an attacker-supplied header into an unconditional 304. See [`EtagMatchesIfNoneMatch`](../../http/static/etag.go).
* The entity tag a static response carries is derived from the file's **size and modification time in whole seconds** ([`GenerateEtag`](../../http/static/etag.go)), never from its contents. A rewrite that preserves both — a build step that restores timestamps, or a second write inside the same second that lands on the same length — carries the previous tag, so a revalidating cache is answered `304 Not Modified` and goes on serving the stale bytes. Where a deployment can rewrite a file in place, put a content hash in the url instead of trusting the tag. The tag, `Last-Modified` and `Cache-Control` are only emitted at all when static caching is enabled.
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

* [`type FileServerConfig`](../../http/static/option.go)
    * [`NewFileServerConfig`](../../http/static/option.go)
    * [`(*FileServerConfig).SetAllowedDotPrefixList(allowedDotPrefixList []string)`](../../http/static/option.go) — names the dot-prefixed **first** path elements the server may retrieve; `nil` or an empty list refuses every dot-prefixed path
    * [`const DefaultAllowedDotPrefix`](../../http/static/option.go) (`.well-known`) — the only entry a newly built config carries
    * [`(*FileServerConfig).SetExcludedPathList(excludedPathList []string)`](../../http/static/option.go) — names the path prefixes the server declines without touching the disk, compared against the request path as it arrived; the built-in server is given [`Http().StaticExcludedPaths()`](../../config/http.go)

* [`type Mode`](../../http/static/option.go) — `ModeFilesystem` / `ModeEmbedded`

* [`type Options`](../../http/static/option.go)
    * [`NewOptions`](../../http/static/option.go)


* [`GenerateEtag`](../../http/static/etag.go)
