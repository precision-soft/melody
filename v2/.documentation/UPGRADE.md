# UPGRADE

This document records, per release, every change that can require an action from an application already running Melody: what changed, the symptom an upgrader sees, and the remedy. Releases are listed newest first.

It is a companion to [`CHANGELOG.md`](../CHANGELOG.md), not a replacement: the changelog is the exhaustive record of what moved, this document is the short list of what an upgrader has to do about it.

## Versioning policy for breaking changes

Melody releases a behavioural break as a **MINOR**, with the entry marked `**Behavioural change**` in the changelog and listed here with its symptom and remedy. It does not open a new major for one.

The same decision covers a **method added to an exported contract**, which breaks an out-of-tree implementation of that interface at compile time: it ships as a MINOR with a `**Breaking**` note. A new major would put `/v4` into the import path of every file of every consumer — the cost is paid by everyone, including the majority that implements no framework contract — to spare the one consumer that implements it the addition of a single method. That is the same cost already rejected for behavioural breaks, so it is rejected here too.

An upgrader who needs the old behaviour of any entry below pins the previous patch release; the remedies here are the supported path forward.

## Unreleased

Every entry below is the consequence of fixing a defect, not a preference: each one describes behaviour that was wrong, and the changelog entry for it names the failure it produced. The release train's two data-loss fixes are in the v3-only `awss3` object storage integration and are recorded in [`v3/.documentation/UPGRADE.md`](../../v3/.documentation/UPGRADE.md).

This section covers the changes currently sitting in the `[Unreleased]` block of [`CHANGELOG.md`](../CHANGELOG.md); they ship as a MINOR release.

### Bunorm: the `bun` requirement moves to v1.2.17, dialects and drivers in lockstep

**What changed.** Every module of the `bunorm` family — the manager, `mysql`, `pgsql` and the three `migrate` modules — requires `github.com/uptrace/bun v1.2.17` and, where they carry one, `dialect/mysqldialect`, `dialect/pgdialect` or `driver/pgdriver` at the same version. v1.2.16 swallowed the failure of a migration read from a `.sql` file: the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec error with its own nil return, so `db:migrate` printed `[success]`, exited 0 and marked a migration applied that never ran.

**Symptom.** If your application pins a bun dialect or driver of its own, the build now selects `bun v1.2.17` through this dependency while your dialect stays where it was, and the process **panics at init**: `mysqldialect and Bun must have the same version: v1.2.16 != v1.2.17`. The dialect packages check this themselves; it is not a melody rule.

**Remedy.** Move your own `github.com/uptrace/bun/...` requirements to `v1.2.17` in the same change — `go get github.com/uptrace/bun@v1.2.17 github.com/uptrace/bun/dialect/mysqldialect@v1.2.17` and the equivalent for `pgdialect` / `pgdriver`. Applications that declare no bun dependency of their own need no action.

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

### Compile-level: `session/contract.Session` gained `SetShared`

**What changed.** [`session/contract.Session`](../session/contract/session.go) declares `SetShared(key string, value any)`. `Set` stores a value the storage layer is free to copy — every entry is deep-copied on the way into the store and on the way out of it, which is what keeps a session read on one request from writing into another request's data — while `SetShared` stores the handle itself, so `Get` hands back that very value and every reader of the session reaches the one object. The write decides the semantics and `Get` honours whichever was chosen, so there is no `GetShared` and no `IsShared` to implement alongside it.

**Symptom.** An out-of-tree implementation of `session/contract.Session` no longer satisfies the interface, so the assignment that hands it to the framework fails to compile with `missing method SetShared`.

**Remedy.** Implement it. An implementation backed by an in-process map already stores what it is given, so delegating is both the shortest implementation and the honest one:

```go
func (instance *CustomSession) SetShared(key string, value any) {
	instance.Set(key, value)
}
```

An implementation whose storage serialises cannot carry a handle across a round trip, and should refuse the save rather than write something that loads back as a plain copy — [`FileStorage.Save`](../session/file_storage.go) refuses the whole session before touching anything and names the offending key. See [`package/SESSION.md`](./package/SESSION.md) for the distinction and what each write is for.

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
