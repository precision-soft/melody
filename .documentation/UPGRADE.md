# UPGRADE

This document records, per release, every change that can require an action from an application already running Melody: what changed, the symptom an upgrader sees, and the remedy. Releases are listed newest first.

It is a companion to [`CHANGELOG.md`](../CHANGELOG.md), not a replacement: the changelog is the exhaustive record of what moved, this document is the short list of what an upgrader has to do about it.

## Versioning policy for breaking changes

Melody releases a behavioural break as a **MINOR**, with the entry marked `**Behavioural change**` in the changelog and listed here with its symptom and remedy. It does not open a new major for one.

The same decision covers a **method added to an exported contract**, which breaks an out-of-tree implementation of that interface at compile time: it ships as a MINOR with a `**Breaking**` note. A new major would put `/v4` into the import path of every file of every consumer — the cost is paid by everyone, including the majority that implements no framework contract — to spare the one consumer that implements it the addition of a single method. That is the same cost already rejected for behavioural breaks, so it is rejected here too.

An upgrader who needs the old behaviour of any entry below pins the previous patch release; the remedies here are the supported path forward.

## Unreleased

Every entry below is the consequence of fixing a defect, not a preference: each one describes behaviour that was wrong, and the changelog entry for it names the failure it produced. The release train's two data-loss fixes are in the v3-only `awss3` object storage integration and are recorded in [`v3/.documentation/UPGRADE.md`](../v3/.documentation/UPGRADE.md).

This section covers the changes currently sitting in the `[Unreleased]` block of [`CHANGELOG.md`](../CHANGELOG.md); they ship as a MINOR release.

### Application: an http boot with no environment keys refuses to serve

**What changed.** [`Boot`](../application/application.go) fails an http-mode process whose `.env` artifacts contributed no keys at all. Every built-in parameter has a development default, so such a process served as `dev` — debug log level, the http profiler, the debug commands — with one warning as the only signal. A cli process stays permissive.

**Symptom.** A deployment whose binary runs from a directory without its `.env` files — the ordinary cause of zero loaded keys — fails at boot with the message naming the searched directory and the remedy, instead of serving on development defaults.

**Remedy.** Put the `.env` artifacts beside the binary (or in the working directory under `go run`), or build with `-tags melody_env_embedded` to embed them. A deployment that genuinely wants defaults-only http serving writes one key — for instance an explicit `MELODY_ENV=dev` — and by doing so says so.

### Application: a middleware factory that yields nil fails the pipeline build

**What changed.** The factory wrapper in [`UseFactoriesWithPriority`](../application/http_middleware.go) refuses a nil or typed-nil middleware when the pipeline is built, naming the definition. The pipeline used to skip a nil middleware silently, without even an inactive-report entry.

**Symptom.** An http boot whose registered factory returns nil — a guard clause written as `return nil`, a lookup that failed — now panics at the pipeline build instead of serving every request without that middleware.

**Remedy.** A factory that conditionally disables its middleware returns a pass-through middleware instead of nil, or the registration itself is made conditional.

### Http: an exact dispatch-duplicate route is refused

**What changed.** [`registerRoute`](../http/route_registry.go) refuses a route identical to a registered one in everything the matcher discriminates on: pattern, methods, host, schemes, locales, requirements and priority. The name and the defaults stay out — neither participates in matching. Both unnamed routes used to be stored, with the first registered silently shadowing the later one at dispatch.

**Symptom.** A boot registering `GET /health` twice — a module and the application each contributing one — panics at the second registration line instead of silently serving whichever came first.

**Remedy.** Remove one of the two, or make them distinguishable: different methods, hosts, requirements, or an explicit priority if the shadowing was intended — a priority split is accepted and the higher one wins at dispatch.

### Application: a teardown failure on Run's normal return exits non-zero

**What changed.** [`Run`](../application/application.go) exits 1 when the teardown it performed itself reports a failure — a service whose `Close` errored during an http shutdown. The cli path already fails the process for the same condition, through the command result.

**Symptom.** A SIGTERM shutdown whose container close fails — a failed flush, a close that errored — is recorded by the supervisor as exit 1 with the Emergency log line, instead of exit 0 with the same line.

**Remedy.** None for a healthy application. A deployment that treats shutdown-close failures as ignorable handles the non-zero exit in its supervisor policy — restart policies keyed on "always" are unaffected.

### Application: the http timeout override interfaces are removed

**What changed.** `HttpTimeoutConfiguration` and `HttpShutdownConfiguration` are deleted from the application package. Nothing implemented them and nothing could: the configuration the application consults is always the one it builds itself, so the overrides were unreachable through any api. The server limits are the fixed defaults — read 15s, read-header 5s, write 30s, idle 60s, max header 1 MiB, shutdown 5s.

**Symptom.** Code referencing either interface — an implementation written in the hope it would be picked up, a type assertion — no longer compiles.

**Remedy.** Delete the dead implementation; it never took effect, so behavior is unchanged. A deployment that needs different limits runs its own `net/http.Server` around `Boot()`'s kernel.

### Application: `RegisterConfiguration` accepts only the logging configuration name

**What changed.** [`RegisterConfiguration`](../application/application.go) refuses any name other than `logging`. The registry is consumed in exactly one place in this major and no accessor exists for any other name, so every other registration was inert by construction — most dangerously the near-miss spelling that left the logger silently on defaults.

**Symptom.** A boot registering a configuration under any other name panics at the registration line, naming the one supported name.

**Remedy.** Spell the logging configuration name through `loggingcontract.LoggingConfigurationName`; delete registrations under other names — they never did anything.

### Container: the protected `service.` namespace refuses scoped registration

**What changed.** [`RegisterScoped`](../container/container_scoped_registrar.go) — on the container and on a live scope — refuses a `service.`-prefixed name, with or without `Replacing()`. The override path has always refused to substitute a protected name; the scoped registration performed the same substitution inside every scope and was accepted.

**Symptom.** A boot that registered a scoped service under a `service.` name now fails at that registration line; `MustRegisterScoped` panics there.

**Remedy.** Register the substitute under an application-owned name, or install it per scope through `OverrideProtectedInstance`, which is the API that owns deliberate substitution of protected services.

### Container: a closed container refuses registrations and overrides

**What changed.** [`Register`](../container/container.go) and `OverrideInstance`/`OverrideProtectedInstance` on a closed container return the container-is-closed error, the way `RegisterScoped` always has. The read paths are untouched: already-built instances keep being served during shutdown.

**Symptom.** Shutdown-adjacent code that registered or overrode after `Close()` — and silently produced a service nothing would build, or a value nothing would close — now receives an error; the `Must*` forms panic.

**Remedy.** Order the shutdown so writes precede `Close()`; a write that can legitimately race the shutdown checks the returned error.

### Container: an override fits the registered type, wins its race, and frees what it replaces

**What changed.** Three related override rules. A value not assignable to every type its name is registered under is refused before anything is written, so `GetByType` keeps its contract. An override installed while the service's provider was running wins the slot: the racing resolution yields the override and the value it built is closed, where the creation used to clobber the name entry while the type entry kept the override — the two maps then disagreeing forever. And an override replacing an instance the container itself built no longer leaks it: the replaced instance is closed by the container's teardown, once; an override evicted by a later override still belongs to its installer. The scope-side `ClosedWithScope` eviction closes the evicted created instance with the scope in the same way.

**Symptom.** `OverrideInstance("db", wrongTypedValue)` now errors instead of poisoning type-keyed resolution; a mock installed mid-creation is what every caller sees, including the racing one; a Closeable the container built and an override displaced is closed at shutdown instead of never.

**Remedy.** Override with a value of a compatible type — a substitute implementing the registered interface is unchanged. Code that relied on the replaced instance staying open past its eviction holds its own reference and manages its own close.

### Container: `Has` answers under the suspension `Get` enforces

**What changed.** [`resolverContext.Has`/`HasType`](../container/resolver_context.go) consult the scope only when the resolution may read it. Inside a container-owned provider — where `Get` of a scope-only entry is refused — `Has` of the same entry answered true and now answers false.

**Symptom.** A container provider gating an optional dependency on `Has("scope-only-name")` takes the absent branch, where it previously took the present branch and then panicked in `MustGet` — or wired a process-lifetime service against one request's contents.

**Remedy.** None for the Has-then-Get idiom, which now agrees with itself. A container service that genuinely needs request data takes it as a method argument.

### Container: two boot-line refusals — the typed-nil provider and the colliding type identity

**What changed.** A typed-nil provider function (`var f containercontract.Provider[T]` handed in uninitialized) is refused at registration instead of panicking on its first resolution. And a type registration whose identity key another, different type already claimed — possible only for pointer-to-unnamed-composite types such as `*[]alpha.Bus` vs `*[]beta.Bus` from two same-short-named packages — is refused at the boot line that declares it, instead of the two types sharing a creation-guard key and reporting a legitimate resolution as a circular dependency.

**Symptom.** A boot that previously reported success and failed at runtime now fails at the registration line, naming the service — and for the collision, both types and the shared key.

**Remedy.** Initialize the provider variable; for the collision, name one of the composite types (a named slice type carries its package path) or register it by name only with `WithoutTypeRegistration()`.

### Validation: a parameterized rule needs its parameters, whole and non-negative

**What changed.** Three related refusals in how rule parameters are read. A parameterized rule named without parameters (`regex`, `regex()`, `max()`, `lessThan`) fails closed instead of validating with the registered singleton — the bare `regex` validated with the match-everything `.*` default, the bare `lessThan` meant "less than 0". A numeric bound must be an integer in its entirety: the parse previously accepted a valid leading integer and dropped the rest, so `lessThan=-0.5` became a bound of 0 and accepted values the tag as written refuses. And [`Regex.WithParams`](../validation/constraint_regex.go) refuses an empty pattern, while [`MinLength`/`MaxLength`](../validation/constraint_min_length.go) refuse a negative bound.

**Symptom.** A tag of any of these shapes now reports `invalidRuleSyntax` on every value it reaches, where it previously enforced a configuration nobody wrote — or nothing at all. The refusal reason is in the error context under `cause`.

**Remedy.** Write the parameter the rule needs: `regex(pattern=^[a-z]+$)`, `max=100`, `lessThan=0`. A pattern genuinely meant to match everything says so explicitly.

### Validation: the string-form constraints operate on strings

**What changed.** `regex`, `email`, `alpha`, `alphanumeric` and `numeric` refuse a value that is not a string instead of silently passing it, and `min`, `max` and `notBlank` no longer measure the fmt rendering of a non-string — all three refuse the type. A nil pointer and the empty string still pass the five format constraints, so optional-field composition is unchanged.

**Symptom.** A string-form rule on a non-string field — `regex` on `[]byte`, `max=10` on an `int`, `notBlank` on a `bool` — now rejects every value with `value must be a string`. It previously either passed everything (the format five, and `notBlank` on `false`/`0`/empty collections) or measured the Go rendering (an empty slice passed `min=1` as the two runes of `[]`).

**Remedy.** Move the rule to a string field, or use the constraint built for the type: `greaterThan`/`lessThan` for numeric ranges, `notEmpty` for collections. A rule that must inspect a non-string type is a custom [`contract.Constraint`](../validation/contract/constraint.go).

### Behavioural: the 400 body of a failed `BindJsonAndValidate` carries the per-field errors

**What changed.** The kernel exception listener includes the `errors` context key of an [`HttpException`](../exception/http_exception.go) in the JSON body — that key only, in every mode. [`BindJsonAndValidate`](../http/request_body.go) attaches the validation errors under it, so the client receives an `errors` array with `field`, `message`, `code` and `context` per violation, where it previously received only `{"error":"validation failed"}`.

**Symptom.** Clients of endpoints using `BindJsonAndValidate` see a new `errors` field in 400 responses. An application that itself attaches an `errors` context key to an `HttpException` now has that value rendered to the client.

**Remedy.** None for a client that ignores unknown fields. An application that stored private data under an `errors` context key renames the key; every other context key stays private as before.

### Behavioural: a session write lost to a storage outage answers 500

**What changed.** [`writeResponse`](../http/router_utility.go) replaces the handler's response with `500` when [`SaveSession`](../session/manager.go) fails. It previously logged the failure, suppressed the session cookie and served the handler's response unchanged.

**Symptom.** A request that writes to the session and succeeds now answers `500` while the session backend is unreachable, where it used to answer whatever the handler returned. The delete path is unchanged: a failed logout still expires the browser cookie and serves the handler's response, because clearing a cookie can only end a session and never resurrect one.

**Remedy.** None for a healthy deployment — the new status only appears where a write was already being lost. What it removes is the case that could not be seen from the outside: a login answering `302 /dashboard` with the identity never stored, or a session-backed attempt counter that stops growing exactly while the backend is under the pressure an attack produces. If an endpoint must survive a session-store outage, do not write to the session on it.

### Behavioural: `Session.Clear` latches, and a deleted session cannot be saved again

**What changed.** Two related refusals. [`Session.Clear`](../session/session.go) now latches: a later `Set` puts the value back and marks the session modified, but the session stays cleared, so the response path still deletes it. And [`Manager.DeleteSession`](../session/manager.go) remembers the id for [`TombstoneRetention`](../session/manager.go), so [`SaveSession`](../session/manager.go) refuses a write under an id another request deleted, returning an error whose cause is [`ErrSessionDeleted`](../session/manager.go). The unexported `abandon`, which applied the latch for rotation alone, is gone.

**Symptom.** A handler that clears a session and then writes to the same object no longer keeps the session alive: previously the write lifted the cleared flag and the response path saved the session back under the pre-logout id and re-issued its cookie. And a request holding a session that another request deleted mid-flight now gets an error from `SaveSession` instead of silently re-creating the entry; the response path answers that by expiring the browser cookie and serving the handler's response unchanged.

**Remedy.** A handler that wants a usable session after ending one asks the manager for a new session rather than writing to the cleared one:

```go
sessionInstance.Clear()

replacement := manager.NewSession()
replacement.Set(sessionKeyFlash, "you have been signed out")

request.Attributes().Set(melodyhttp.RequestAttributeSession, replacement)
```

Code that calls `SaveSession` directly should treat `ErrSessionDeleted` as the session having ended rather than as a failure:

```go
if err := manager.SaveSession(sessionInstance); nil != err {
    if true == errors.Is(err, session.ErrSessionDeleted) {
        /* another request signed this session out; nothing to persist */
        return nil
    }

    return err
}
```

### Behavioural: `Manager.Close` no longer closes the storage it was handed

**What changed.** [`session.NewManager`](../session/manager.go) builds a manager that does not own its storage, so `Close` leaves the storage open. [`session.NewManagerOwningStorage`](../session/manager.go) is the constructor that keeps the previous cascade. This is the rule [`NewFileStorageFromFile`](../session/file_storage.go) already followed for an injected file handle: what you were handed, you do not close.

**Symptom.** An application that built both by hand and relied on `manager.Close()` to close the storage now leaves it open — for [`InMemoryStorage`](../session/in_memory_storage.go) that means its cleanup goroutine keeps running. Nothing changes for the container path, which is where the defect was: the storage is a registered service the container closes itself, so the manager closed it a second time, and a storage wrapping a connection typically reports that second call as a failure — turning a clean shutdown into `failed to close container services`.

**Remedy.** Switch a hand-wired pair to the owning constructor:

```go
manager := session.NewManagerOwningStorage(storage, ttl)
```

Do not use it for a storage that is also registered as a service; that brings the double close back. Close such a storage through the container, as before.

### Behavioural: a negative session ttl fails at construction

**What changed.** [`session.NewManager`](../session/manager.go) panics on a negative `ttl`, as the configuration path already did.

**Symptom.** Code that computed a ttl dynamically and could produce a negative value — `time.Until(expiry)` on an instant already past — now fails at construction instead of building a manager. It previously produced sessions with **no expiry at all**, because both storages test `0 < ttl` and treat anything else as "never expires": the value that reads as "already lapsed" produced the immortal session.

**Remedy.** Clamp before constructing, and use zero when no expiry is what you mean:

```go
ttl := time.Until(expiry)
if 0 > ttl {
    ttl = config.MinimumSessionTtl
}
```

### Compile-level: `container/contract.ScopeManager` and `container/contract.Scope` gained `RegisterScoped`

**What changed.** A scope is now a registrar of its own. [`container/contract.ScopeManager`](../container/contract/scope.go) declares `RegisterScoped(serviceName string, provider any, options ...RegisterOption) error` and `MustRegisterScoped(...)`, which declare a service whose lifetime is one scope — one http request, one command run — built lazily on the first resolution through a scope and closed when that scope closes. [`container/contract.Scope`](../container/contract/scope.go) declares the same two verbs through [`ScopedRegistrar`](../container/contract/scoped_registrar.go), for adding a service to one live scope.

The declaration sits on `ScopeManager` rather than beside the container's own registrations because a scope does not exist until a request arrives: what a scope will own has to be declared at boot by whatever will be creating the scopes.

**Symptom.** An out-of-tree implementation of `container/contract.Scope`, of `container/contract.ScopeManager`, or of `container/contract.Container` — which embeds `ScopeManager` — no longer satisfies the interface, so the assignment fails to compile with `missing method RegisterScoped` or `missing method MustRegisterScoped`. In practice the implementations that break are test doubles: the framework's own sweep had to repair twelve of them, and none of them was production code.

**Remedy.** A double that only stands in for a scope can answer that it registers nothing, which is truthful for a stub and keeps the compiler satisfied:

```go
func (instance *TestScope) RegisterScoped(
	serviceName string,
	provider any,
	options ...containercontract.RegisterOption,
) error {
	return exception.NewError(
		"this scope holds no registrations of its own",
		map[string]any{"serviceName": serviceName},
		nil,
	)
}

func (instance *TestScope) MustRegisterScoped(
	serviceName string,
	provider any,
	options ...containercontract.RegisterOption,
) {
	exception.Panic(exception.FromError(instance.RegisterScoped(serviceName, provider, options...)))
}
```

A double built by embedding `containercontract.Scope` or `containercontract.Container` in a struct keeps compiling untouched and needs nothing — but it will panic on a nil embed if anything calls the new methods, so give it the two methods above if the code under test can reach them.

An implementation that means to carry real scoped registrations should hold the providers apart from the instances it already keeps, build one instance per scope on first resolution, and close what it built when the scope closes. The framework's own implementation is the reference; see [`package/CONTAINER.md`](./package/CONTAINER.md) for what the two lifetimes may read from each other.

See [Versioning policy for breaking changes](#versioning-policy-for-breaking-changes) for why an added contract method ships as a MINOR.

### Compile-level: `session/contract.Manager` gained `RegenerateSession`

**What changed.** [`session/contract.Manager`](../session/contract/manager.go) declares `RegenerateSession(session Session) (Session, error)`, the session-fixation defence: it mints a fresh id, carries the values over, removes the entry the previous id pointed at, and latches the session passed in out of use. The framework's own [`session.Manager`](../session/manager.go) implements it, and [`http.RegenerateRequestSession`](../http/session.go) rotates and republishes in one call.

**Symptom.** An out-of-tree implementation of `session/contract.Manager` — a Redis-backed or database-backed session manager, say — no longer satisfies the interface, so the assignment that hands it to the container fails to compile with `missing method RegenerateSession`.

**Remedy.** Implement the method. It has to mint an id the storage does not already hold, carry the values over, delete the previous entry and put the session it was given out of use, so that a caller who forgets to republish the rotated session is logged out cleanly instead of being left presenting a deleted id:

```go
type CustomSessionManager struct {
	/* the embed stands for the rest of the implementation this excerpt does not repeat */
	sessioncontract.Manager
}

func (instance *CustomSessionManager) RegenerateSession(
	sessionInstance sessioncontract.Session,
) (sessioncontract.Session, error) {
	rotatedSession := instance.NewSession()

	for key, value := range sessionInstance.All() {
		rotatedSession.Set(key, value)
	}

	deleteErr := instance.DeleteSession(sessionInstance.Id())
	if nil != deleteErr {
		return nil, deleteErr
	}

	sessionInstance.Clear()

	return rotatedSession, nil
}
```

The framework's own `Session` is latched out of use rather than merely cleared, because `Session.Set` lifts the cleared flag and a caller that rotated and then kept writing to the original object would otherwise have the response path re-create the just-deleted id and re-issue it as the cookie. That latch is unexported and no contract method was added for it, so an out-of-tree `Session` implementation is only `Clear()`ed — which a later write still undoes. An application that supplies its own `Session` must therefore not write to the object it rotated away.

See [Versioning policy for breaking changes](#versioning-policy-for-breaking-changes) for why an added contract method ships as a MINOR, and [`package/SESSION.md`](./package/SESSION.md) for what a rotation has to guarantee.

### Compile-level: `config/contract.HttpConfiguration` gained `StaticExcludedPaths`

**What changed.** [`config/contract.HttpConfiguration`](../config/contract/http.go) declares `StaticExcludedPaths() []string`, the path prefixes the built-in file server declines before it looks at the disk. The framework's own implementation reads them from `MELODY_STATIC_EXCLUDED_PATHS` (`kernel.static.excluded_paths`), a comma-separated list that is empty by default. Since the built-in file server sits outermost in the pipeline, excluding a prefix is how an application takes a part of the url back — to put authentication in front of a directory, or to serve it from a root of its own.

**Symptom.** A type of your own implementing `config/contract.HttpConfiguration` — a test double, or a configuration assembled in code rather than from `.env` artifacts — no longer satisfies the interface, and the assignment fails to compile with `missing method StaticExcludedPaths`.

**Remedy.** Implement it. An empty list excludes nothing, so returning an empty slice keeps the behaviour the interface had without the method. Return a copy rather than the field itself: the configuration is read on every request while the caller is free to keep the slice it was handed.

```go
func (instance *CustomHttpConfiguration) StaticExcludedPaths() []string {
	return append([]string{}, instance.staticExcludedPaths...)
}
```

### Compile-level: `cli/output.Option` lost `Fields` and `SortKey`

**What changed.** The `--fields` and `--sort` flags are withdrawn. No printer ever read them and no command ever sorted on a supplied key, so they are gone from the flag set, from [`output.Option`](../cli/output/option.go) and from the `meta.flags` block of the json envelope; `output.SplitFields` is removed with them.

**Symptom.** A custom command that constructed an `output.Option` literal naming `Fields` or `SortKey`, or that called `output.SplitFields`, no longer compiles. At runtime, an invocation passing `--fields` or `--sort` now fails as an unknown flag instead of being silently ignored.

**Remedy.** Drop the fields from the literal and drop the call. A command that genuinely wants a projection or a sort key declares its own flag and applies it to the payload it builds.

### Boot-level: `websocket/v3` refuses a zero `IdleTimeout`

**What changed.** [`websocket.NewStreamHandler`](../integrations/websocket/v3/handler.go) panics on `Options.IdleTimeout` of zero, where zero previously meant "no keepalive" and was documented as the default. The keepalive ping is the only thing that can reap a peer that goes away without a fin: `Accept` hijacks the connection out of `http.Server`'s read and write timeouts, the read loop then blocks with no deadline of its own, and a write into a half-open socket keeps succeeding for as long as the send buffer has room — so a broadcast is no liveness signal either. Left at zero, connections opened and abandoned accumulate for the life of the process, each costing a descriptor, a hub subscription and three goroutines.

**Symptom.** The application fails to boot. `RegisterHttpRoutes` panics with a message naming the field and a value to start from, rather than the handler being built and the leak beginning.

**Remedy.** Set it. `30 * time.Second` suits a browser client, which answers the ping inside the protocol stack where the page's JavaScript never sees it; a mobile client on a metered link wants a longer interval, and an internal service that holds long-lived streams longer still. There is deliberately no default: the right interval is a property of the peer, and a value melody picked would be wrong for half of them while looking deliberate.

### Routing: a non-final optional parameter without a default is refused at registration

**What changed.** An omitted optional parameter is dropped wherever it sits in the pattern, while a match only ever ends early at the tail. A pattern such as `/blog/:locale?/posts` therefore let [`UrlGenerator.GeneratePath`](../http/url_generator.go) mint `/blog/posts`, a path this very router answered with `404`. Such a pattern is now refused at the definition site by [`rejectNonTrailingOptionalParameter`](../http/router.go). A mid-pattern optional that carries a **non-empty default** is still accepted, because the default is always substituted and the segment is therefore never dropped.

**Symptom.** The application no longer boots. Registration panics with `optional route parameter must be the last pattern segment unless it has a default`, and the exception context names the offending `pattern` and `parameterName`.

**Remedy.** One of three, depending on what the route meant:

```go
/* refused at registration: the optional parameter is not the last segment and carries no default */
router.Handle(nethttp.MethodGet, "/blog/:locale?/posts", blogHandler)

/* accepted: the optional parameter is the last segment */
router.Handle(nethttp.MethodGet, "/blog/posts/:locale?", blogHandler)

/* accepted: a mid-pattern optional whose non-empty default is always substituted */
router.HandleWithOptions(
	"/blog/:locale?/posts",
	blogHandler,
	http.NewRouteOptions(
		"blog.posts.localized",
		[]string{nethttp.MethodGet},
		"",
		nil,
		nil,
		map[string]string{"locale": "en"},
		nil,
		0,
		nil,
	),
)

/* accepted: the long and the short pattern registered as two routes */
router.HandleNamed("blog.posts", nethttp.MethodGet, "/blog/posts", blogHandler)
router.HandleNamed("blog.posts.locale", nethttp.MethodGet, "/blog/:locale/posts", blogHandler)
```

An empty default does not lift the refusal: it would emit an empty segment, which no longer satisfies a parameter.

### Routing: a non-empty route default fills in for a parameter supplied empty

**What changed.** [`UrlGenerator.GeneratePath`](../http/url_generator.go) substitutes a non-empty route default for a parameter supplied with an **empty** value, not only for an absent one. A non-trailing optional segment is admitted at registration precisely because its default keeps the segment present, but generating `/:locale?/list/:page` with `{"locale": "", "page": "2"}` dropped it and produced `/list/2` — which this router answers with a `404`, the generator and the matcher disagreeing on the one class of pattern the registration guard newly admits.

**Symptom.** A call that passed an empty string for a parameter that has a non-empty default now gets the default in the path instead of an omitted segment: `/en/list/2` where it used to be `/list/2`. A **required** parameter supplied empty is likewise filled from a non-empty default instead of failing with `route parameter may not be empty`.

**Remedy.** Nothing, in the normal case — the natural caller passes the current locale, which is sometimes `""`, and now gets a path the router actually serves. A caller that relied on an empty value dropping the segment must omit the parameter instead, or give the route no default (or an empty one), which leaves the old behaviour: an optional segment is dropped and a required one is still refused.

### Middleware: equal-priority middlewares run in registration order

**What changed.** [`orderDefinitions`](../http/middleware/pipeline/builder.go) breaks a priority tie on the registration rank instead of on the definition's generated name. The generated name carries the registration counter as decimal text, so a lexicographic tie-break read it as `1, 10, 11, 2` and sorted every factory-provided middleware ahead of every directly registered one. Explicit priorities and `before`/`after` edges decide the order exactly as before.

**Symptom.** The pipeline nests differently. A middleware that used to run outside another may now run inside it, and the reverse. The visible case is a cors middleware registered before an authentication factory at the same priority: it used to end up **inside** the factory, so a preflight was answered `401` with no `Access-Control-Allow-Origin`; it now runs outside it, as the registration order asked.

**Remedy.** If the old nesting was load-bearing, say so explicitly rather than leaning on registration order. A lower priority runs further out:

```go
func (instance *ExampleHttpMiddlewareModule) RegisterHttpMiddlewares(
	kernelInstance kernelcontract.Kernel,
	registrar applicationcontract.HttpMiddlewareRegistrar,
) {
	/* a lower priority runs further out, so cors wraps authentication whatever order the two are registered in */
	registrar.UseWithPriority(-100, cors.DefaultMiddleware())
	registrar.UseWithPriority(0, authenticationMiddleware())
}
```

`before`/`after` edges live on [`pipeline.NewHttpMiddlewareDefinition`](../http/middleware/pipeline/definition.go) for a pipeline assembled directly through [`pipeline.NewBuilder`](../http/middleware/pipeline/builder.go); the module registrar exposes priority. [`(*HttpMiddleware).LastBuildReport`](../application/http_middleware.go) reports the order that was built, and `debug:middleware` renders it.

### Validation: a nil pointer embed is validated as "nothing was supplied"

**What changed.** [`dereferencedValidationStructValue`](../validation/validator.go) yields the zero embed for a nil pointer embed, so the constraints its promoted fields declare run against their zero values exactly as a value embed's already did.

**Symptom.** A request that mentioned no field of a `*T` embed is now rejected with the constraint errors the embed's tags declare. It used to pass: naming any sibling field made `encoding/json` allocate the embed and re-arm the constraints, so a body of `{"status":"open"}` on a request whose `*Audit` embed declares `ActorId` as `notBlank` was accepted and then dereferenced nil in the handler.

**Remedy.** Supply the fields, or stop declaring constraints on an embed the payload is allowed to omit. A promoted field shadowed by an outer field of the same json name stays unvalidated, so the `encoding/json` dominance rules are unchanged.

### Validation: nesting past the depth cap is a validation error when the subtree could carry a tag

**What changed.** Exceeding the nesting-depth cap is reported as [`ErrorNestingDepthExceeded`](../validation/const.go) (`nestingDepthExceeded`) when the truncated subtree could actually carry a `validate` tag, and passes silently when it could not. The walk previously returned an empty error list past the cap, which `Validate` converted to `nil`, so nesting a payload one level deeper than the cap bypassed every constraint in it. The reachability check follows pointers, slices, arrays and map elements and is memoized per type; the cap value is unchanged.

**Symptom.** A deeply nested payload that used to validate now fails with a `nestingDepthExceeded` error naming the field. Tag-free free-form client json — a `map[string]any` metadata field, for example — is still accepted at any depth.

**Remedy.** Flatten the request type, or keep the deep part of the payload tag-free so nothing below the cap declares a constraint.

### Validation: a parameterized constraint is constructed once and shared

**What changed.** The parsed `validate` tag and the constraint a parameterized rule resolves to are memoized instead of being rebuilt for every value the validator reaches — a `regex` tag recompiled its pattern once per element. The parse cache is keyed on the tag string, the constraint cache on the rule name and its parameters, and the constraint cache is per-validator, so custom constraints registered under the same name in different validators stay separate.

**Symptom.** A custom [`contract.ParameterizedConstraint`](../validation/contract/constraint.go) whose `WithParams` result carried per-value state, or was not safe for concurrent use, now leaks that state between unrelated values and unrelated requests.

**Remedy.** Make the constraint `WithParams` returns immutable and safe for concurrent use; do not retain the params map it was handed, and do not accumulate state in `Validate`. One instance is shared for the process lifetime across every request and goroutine that reaches the rule.

### HTTP kernel: `SetSessionCookiePolicy` keeps the `SameSite=Lax` default

**What changed.** [`resolveSessionCookieSameSite`](../http/router_utility.go) treats the zero `SameSite` as unset and falls back to `Lax`, the same way an empty `Path` falls back to `/`.

**Symptom.** A policy that named only `Path` or `Domain` used to emit no `SameSite` attribute at all; it now emits `SameSite=Lax`.

**Remedy.** None, unless the omission was deliberate — `nethttp.SameSiteDefaultMode` remains the way to ask for no attribute on purpose.

### HTTP kernel: the session saved is the one published on the request

**What changed.** [`republishedSession`](../http/router_utility.go) reads `RequestAttributeSession` at the moment the response is written, preferring the session a handler published over the one the kernel captured before routing.

**Symptom.** Replacing that attribute in a handler now takes effect: the published session is what gets stored and what the `Set-Cookie` advertises. It used to be discarded.

**Remedy.** None for a handler that wanted that. A handler that put something else under `RequestAttributeSession` as scratch space must stop — the constant is framework-owned and the response path acts on it.

### HTTP client: `MaxIdleConnsPerHost` is set on the transport

**What changed.** [`TransportConfig.MaxIdleConnsPerHost`](../httpclient/transport_config.go) is exposed and defaults to `MaxIdleConns` (100), following an override of it unless pinned explicitly. It was never set, so `net/http` fell back to `DefaultMaxIdleConnsPerHost` (2) and the configured `MaxIdleConns: 100` was inert.

**Symptom.** Connection reuse against a single host now scales with `MaxIdleConns` instead of stopping at two. Idle sockets to one upstream are held rather than closed, so the process keeps more open file descriptors and the upstream sees more long-lived connections. The old behaviour exhausted the ephemeral port range under a burst — every connection past the second closed straight into `TIME_WAIT` — and reported `connect: cannot assign requested address` as `"request failed"`.

**Remedy.** Nothing, in the normal case. A caller who relied on the two-connection ceiling, or whose upstream caps connections per client, sets `MaxIdleConnsPerHost` explicitly.

### CLI: json mode writes the document and nothing else

**What changed.** In json mode the ansi start/finish banner that [`cli.Register`](../cli/command.go) wraps around every registered command is suppressed, and `--format=json` implies `--no-color` through [`NormalizeOption`](../cli/output/option_parser.go).

**Symptom.** `jq` and `json.Unmarshal` now consume `debug:*` output directly instead of failing on the first byte. A consumer that scraped the banner off stdout finds it gone.

**Remedy.** Read the envelope. `meta` already reports the command, its arguments, the start time and the duration, and `error` reports the final status.

### CLI: a command whose envelope reports an error exits non-zero

**What changed.** [`output.Render`](../cli/output/renderer.go) returns an exit-coded error after writing the envelope. A registered service that errors or panics while being constructed is reported as `debug.buildFailed` rather than `debug.notFound`.

**Symptom.** A command that reported an error in its payload while exiting `0` now exits `1`. `debug:container <name>` fails when the service cannot be resolved instead of printing `[success]`.

**Remedy.** Nothing to change in the framework. A wrapper script that treated a zero exit as success was reading a status that was never true; a deployment gate such as `app debug:container app.repository.order || exit 1` now works as written. A command of your own that renders a non-nil `Envelope.Error` deliberately and still wants a zero exit must not put the failure in the envelope.

### CLI: `--format` and `--order` reject an unrecognised value

**What changed.** Both flags carry a validator ([`StandardFlags`](../cli/output/standard_flag.go)), so `--format=JSON`, `--format=yaml` and `--order=ascending` fail during flag parsing with a message naming the accepted values, matching how `--limit=abc` already behaved.

**Symptom.** A script passing an unsupported value now fails with a non-zero exit instead of quietly receiving the human table.

**Remedy.** Pass `table` or `json`, and `asc` or `desc`. Omitting either flag still defaults to `table` and `asc`.

### CLI: `--limit`, `--offset` and `--order` are applied to the rendered items

**What changed.** `debug:router`, `debug:events`, `debug:parameters`, `debug:middleware` and `debug:container` apply the window through [`output.WindowItems`](../cli/output/list_payload.go) and the order through [`output.ApplySortOrder`](../cli/output/list_payload.go), reversal running before the window so a descending window returns the end of the list. `total` continues to report the unwindowed count.

**Symptom.** An invocation already passing `--limit` or `--offset` received the full list and now receives a window; with `--verbose`, `debug:events` also narrows its listeners block to the windowed events. `--order=desc` was accepted and ignored before, so an invocation that passed it now gets different output.

**Remedy.** Nothing for a client that paged with `offset += limit` — it now walks each item exactly once instead of re-reading the whole list on every page. A consumer that passed `--limit` while expecting everything must drop the flag.
