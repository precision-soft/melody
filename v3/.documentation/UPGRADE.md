# UPGRADE

This document records, per release, every change that can require an action from an application already running Melody: what changed, the symptom an upgrader sees, and the remedy. Releases are listed newest first.

It is a companion to [`CHANGELOG.md`](../CHANGELOG.md), not a replacement: the changelog is the exhaustive record of what moved, this document is the short list of what an upgrader has to do about it.

## Versioning policy for breaking changes

Melody releases a behavioural break as a **MINOR**, with the entry marked `**Behavioural change**` in the changelog and listed here with its symptom and remedy. It does not open a new major for one.

The same decision covers a **method added to an exported contract**, which breaks an out-of-tree implementation of that interface at compile time: it ships as a MINOR with a `**Breaking**` note. A new major would put `/v4` into the import path of every file of every consumer — the cost is paid by everyone, including the majority that implements no framework contract — to spare the one consumer that implements it the addition of a single method. That is the same cost already rejected for behavioural breaks, so it is rejected here too.

An upgrader who needs the old behaviour of any entry below pins the previous patch release; the remedies here are the supported path forward.

## Unreleased

Every entry below is the consequence of fixing a defect, not a preference: each one describes behaviour that was wrong, and the changelog entry for it names the failure it produced. Two of them lost data — both in the `awss3` object storage integration, where a wrongly declared size could replace a stored object with a truncated one and then delete what was left.

This section covers the changes currently sitting in the `[Unreleased]` block of [`CHANGELOG.md`](../CHANGELOG.md); they ship as a MINOR release.

### Bunorm: the registry refuses new callers while a pool is still closing

**What changed.** `ManagerRegistry.Close` marked the registry closed and then held the registry lock for the whole teardown, closing every manager and every migration database inside the critical section. It now publishes the flag under the lock, takes a snapshot of the two maps, releases the lock, and closes the pools outside it.

**Symptom.** A call to `Manager`, `Database` or `MigrationDatabase` arriving while `Close` is running no longer waits for the teardown to finish; it is refused at once with `ErrManagerRegistryClosed`. Previously such a call parked on the registry lock, and against a peer that had stopped answering — a network partition at shutdown, where the migration connection's write deadlines are deliberately lifted — it could park for as long as the driver waited, so a graceful-shutdown drain expired with goroutines wedged in the registry. Code that relied on that blocking to serialise its last queries behind the teardown now sees the refusal instead.

**Remedy.** None for the ordinary case: the refusal is what the flag has always meant, and every caller already had to handle `ErrManagerRegistryClosed`, which is what the same call answered a moment later anyway. A caller that genuinely needs its work to finish before the pools close must order that itself — run it before `Close`, or gate `Close` behind it — rather than relying on the lock to do the ordering.

### Bunorm mysql and pgsql: a transient marker inside a word is no longer transient

**What changed.** The providers decide whether to retry a failed open by scanning the lowercased error message for a list of markers. The scan matched them as bare substrings, so the short spellings fired inside ordinary identifiers. The markers are now matched as words: a match counts only where the characters on either side are not letters, digits or underscores.

**Symptom.** A permanent failure whose message happens to contain a marker inside a word now fails on the first attempt instead of being retried for the whole budget. The two measured cases are a missing table whose name contains `eof` — `Table 'app.geofences' doesn't exist` — and an unknown column named `session_timeout`; both were retried to exhaustion and then reported as "database connection failed after max retry attempts" rather than as a non-transient failure. Such a boot now fails faster and under the correct classification.

**Remedy.** None is required, and the change is in the safe direction: the failure was permanent in both cases and the retries only delayed the report. The `io.EOF` and `net.Error` checks that run ahead of the message scan are untouched, so a genuine end-of-file or timeout is classified by type as before, and every marker that appears as its own word — `i/o timeout`, `connection refused`, `bad connection`, a bare `EOF` — matches exactly as it did. An operator who wants a permanent failure retried anyway raises the retry budget rather than relying on a substring collision.

### Bunorm mysql: the provider negotiates verified TLS by default

**What changed.** The mysql provider set no TLS on its connector, so it connected in plaintext and offered no option to enable TLS. It now builds a verifying `tls.Config` by default — the system roots, the configured host as the name to verify against, `MinVersion` TLS 1.2 — the same posture its pgsql sibling already carried, and refuses the driver spellings that would downgrade silently.

**Symptom.** A mysql server that speaks no TLS fails the dial where it previously connected in plaintext. The example's development mysql is such a server.

**Remedy.** A database reached over a trusted network, or one that speaks no TLS, arms `mysql.WithInsecure(true)` on the provider — the deliberate opt-out spelled the same way as pgsql's. A database with a certificate needs no change; one needing a pinned or client certificate passes `mysql.WithTlsConfig`. The example arms the opt-out through a new `MYSQL_INSECURE` switch in its `.env`.

### Bunorm: bun's own diagnostics go to the journal

**What changed.** Opening a connection through the mysql or pgsql provider routes bun's package-level logger into the application's journal, once per process, through the new `bunorm.RouteDiagnostics`. Bun's reports of a declaration mistake — an unknown struct tag option, an unknown `on_update` or `on_delete` rule, a query carrying arguments and no placeholders — arrive as warning records under the message `bun diagnostic` with the line in the context.

**Symptom.** Those lines stop appearing on standard error and start appearing in the journal. An operator or a test grepping standard error for `WARN: bun:` finds nothing.

**Remedy.** Read them from the journal, filtering on the `bun diagnostic` message. One line is deliberately unaffected and stays on standard error: the mysql dialect writes `can't discover MySQL version` through the **standard library's** default logger rather than bun's, so routing it would mean taking `log.SetOutput` for the whole process — every dependency and your own `log` calls with it. That is the application's decision; take it in your composition root if you want it, as the mysql readme shows.

### Bunorm pgsql: every driver deadline is named, configured and lifted for migrations

**What changed.** `pgsql.TimeoutConfig` carries `ReadTimeout` and `WriteTimeout` beside `ConnectTimeout`, the connector receives all three (the dial included), and the provider implements `bunorm.MigrationProvider`. Until now the dial ran under pgdriver's internal 5s default whatever `ConnectTimeout` said, every query ran under invisible 10s read / 5s write deadlines, and `db:migrate` ran on the request pool — an 11-second DDL statement died at 10.004s, measured.

**Symptom.** `pgsql.NewTimeoutConfig(connect)` no longer compiles — the constructor takes the three durations, the mysql signature. Behaviourally, the effective read/write deadlines move from 10s/5s to the documented 30s/30s.

**Remedy.** `NewTimeoutConfig(connect, 0, 0)` keeps the connect timeout and takes the 30s/30s defaults; name tighter deadlines where request traffic needs them. Migrations need nothing: `db:migrate` now prefers the dedicated lifted connection automatically.

### Bunorm: the `bun` requirement moves to v1.2.17, dialects and drivers in lockstep

**What changed.** Every module of the `bunorm` family — the manager, `mysql`, `pgsql` and the three `migrate` modules — requires `github.com/uptrace/bun v1.2.17` and, where they carry one, `dialect/mysqldialect`, `dialect/pgdialect` or `driver/pgdriver` at the same version. v1.2.16 swallowed the failure of a migration read from a `.sql` file: the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec error with its own nil return, so `db:migrate` printed `[success]`, exited 0 and marked a migration applied that never ran.

**Symptom.** If your application pins a bun dialect or driver of its own, the build now selects `bun v1.2.17` through this dependency while your dialect stays where it was, and the process **panics at init**: `mysqldialect and Bun must have the same version: v1.2.16 != v1.2.17`. The dialect packages check this themselves; it is not a melody rule.

**Remedy.** Move your own `github.com/uptrace/bun/...` requirements to `v1.2.17` in the same change — `go get github.com/uptrace/bun@v1.2.17 github.com/uptrace/bun/dialect/mysqldialect@v1.2.17` and the equivalent for `pgdialect` / `pgdriver`. Applications that declare no bun dependency of their own need no action.

### CLI: `--format=json` writes one document per line

**What changed.** The json printer no longer indents. Every melody command's `--format=json` envelope — the framework's `debug:*` family and the core commands it contributes — is now one compact line terminated by a newline, where it used to be a block of indented lines. `--format=json-pretty` is the same document with the indentation back.

**Symptom.** Output that was read by eye, or a test asserting the rendered text with the spacing `encoding/json` puts after a colon (`"error": null`), sees the compact spelling instead (`"error":null`). Nothing that decodes the document is affected: it is the same document.

**Remedy.** For reading by hand, use `--format=json-pretty`, or pipe through `| jq`, which the documentation already recommended. For an assertion on rendered output, decode the document and assert the value rather than the text — the format the printer chooses is not part of what the command reports. Consumers that read the stream a document at a time, and every `jq` pipeline, need no change at all; the reason for the change is the consumers that could not work before, since a long-running command that renders a document per unit of work promised a stream of closed documents and emitted fragments.

### Cli: a duplicated flag name and a mismatched table row fail fast

**What changed.** `output.MergeFlags` panics on a flag name declared twice, and on a nil flag — the parser resolves a name to the first declaration, so a command-specific flag reusing a standard name was silently inert. `TableBlockBuilder.AddRow` panics on a row whose cell count disagrees with the block's declared columns — a surplus cell silently never rendered; the single-token separator row stays admitted.

**Symptom.** A command whose flags redeclare a standard name, or whose table rows disagree with their block's columns, panics at registration or at the row instead of silently misbehaving.

**Remedy.** Rename the colliding flag (the standard names are the `FlagName*` constants), or make the row's cell count match the columns.

### Cli: negative values for the standard integer flags are refused

**What changed.** `--verbosity`, `--limit`, `--offset` and `--table-width` carry validators refusing a negative value, the way `--format` and `--order` refuse an unsupported one. A negative was clamped to zero, and zero means unlimited for the limit — an argument asking for less than nothing silently delivered everything.

**Symptom.** `--limit=-5` fails at argument parsing, naming the flag, instead of listing everything with exit 0.

**Remedy.** Pass a non-negative value; `0` keeps meaning unlimited/default.

### Cli: the table format stops hiding warnings and errors, and printing failures fail the command

**What changed.** Three output changes in the table format. The `WARNINGS` block renders under `--quiet` too — with `StandardFlags` defaulting quiet to true, an application command's warning was invisible at every verbosity; the warning details stay behind `--verbose`. The envelope error now renders whole (message, code, details, cause) — it previously rendered nowhere in the table format. And the first write failure is returned instead of discarded, so a report truncated by a full disk no longer ends in a success banner and exit zero.

**Symptom.** Quiet table runs may print new `WARNINGS:`/`ERROR:` lines; a run whose output stream fails now exits non-zero.

**Remedy.** None for correct runs; output parsers that assumed quiet suppressed warnings read the json format instead, which has always carried both.

### Cli: a failed run reaches the application log

**What changed.** The exit-coded errors built from a rendered envelope and from the command-suggestion refusal travel unmarked, so the exit path logs them through the application logger before the teardown. They were pre-marked as logged while the rendered report lived only on stdout/stderr — a failed run was invisible to anything reading the log file.

**Symptom.** The log file gains one record per failed command run and per mistyped command name. Exit codes are unchanged.

**Remedy.** None; log-volume alerts keyed on error records may need the new entries accounted for.

### Cli: the command action leaves the container to the process-exit owner

**What changed.** A registered command's action closes the request scope and reports its teardown failure beside the command's own error, and no longer closes the service container. The handler that owns the process exit closes it, after it resolved the logger the final record is written through.

**Symptom.** A container close failure is now reported by the exit handler rather than folded into the command's aggregate, and a failed command's final record is written through the live application logger instead of the stderr fallback. A command that closes the container itself is unaffected.

**Remedy.** None. A command that relied on the container being closed by the time its action returned should close what it owns itself, or use the scope.

### Opaque tokens: a stored token with no issue instant is refused once its user is revoked

**What changed.** A revocation is no longer an enumeration. [`security/contract.RevocationEpochStore`](../security/contract/token_store.go) publishes a boundary per user, and per device of a user, and [`Lookup`](../security/in_memory_token_store.go) refuses a token issued before the boundary that governs it. This closes the window [`DeleteByUser`](../../integrations/rueidis/v3/token_store.go) could never close: it walks an index with `SSCAN`, which does not promise to return a member added while the walk is in progress, so a token issued during a revocation survived it. The comparison needs an issue instant, so [`security/contract.Claims`](../security/contract/token_validator.go) carries `IssuedAt`, stamped by the store on every write.

Nothing breaks at compile time. The new methods live on their own interface, composed into `EpochRevocableTokenStore` rather than added to `RevocableTokenStore`, so an out-of-tree token store still satisfies the interface it was written against — it simply cannot publish boundaries, and a caller that needs one is told so by `EpochRevocableTokenStoreMustFromResolver` at the moment the service is asked for.

**Symptom.** A token stored by an earlier release carries no issue instant. The zero instant precedes every boundary, so the first time `RevokeBefore` is called for a user, that user's pre-upgrade tokens stop resolving — including ones an operator did not mean to end. Users nobody revokes are unaffected: with no boundary there is nothing to compare against and the token resolves exactly as before.

**Remedy.** None is needed in the ordinary case, and the behaviour is the safe direction: the tokens that stop resolving belong to an account somebody deliberately revoked. If an upgrade must not end any pre-upgrade session, do not call `RevokeBefore` until the longest token lifetime has passed since the deploy; every token written after it carries an instant and is compared normally.

Two consequences worth knowing before wiring it up. A token whose issue was in flight across a revocation is refused — the instant is stamped before the write reaches the store, so a token stamped just before the boundary and written just after it is treated as predating it. That is over-strict rather than under-strict, and deliberate. And the instants come from application clocks, so a node whose clock runs ahead of the node a revocation is issued from stamps tokens that read as later than the boundary and survive it: the window is exactly the skew between the two, and a single node whose clock steps backwards — an NTP correction, a restored snapshot, a resumed virtual machine — produces the same thing without any second node. `WithTokenStoreMaximumClockSkew` on the redis store, and `JwtConfig.RevocationEpochSkew` on the json web token path, bound that window: they widen the boundary by the stated amount and, on the store, additionally refuse a stamp further ahead of the verifying
node than the same amount. Both default to zero, which leaves the behaviour of this release unchanged; set them to the worst skew the fleet can carry. The cost is symmetrical and deliberate: a token issued within that window AFTER a revocation is refused too. `WithRevocationEpochRetention` is unrelated to any of this — it floors how long a boundary is kept when there is no index deadline to adopt, and does not affect the comparison.

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

### Object storage: `awss3` `Put` enforces the declared size

**What changed.** [`Storage.Put`](../../integrations/awss3/v3/storage.go) proves the body against the `size` it was given *before* anything can be committed at the key, and never holds more than 16 MiB in memory doing it. It previously uploaded first and probed the caller's reader afterwards.

**Symptom.** A call that declared a size **shorter** than the body used to report success and leave a truncated object at the key; it now fails with `storage object size does not match the declared size`, naming the key and the declared size, and nothing reaches the key.

Which byte the failure happens at depends on the body:

* A seekable body (`*bytes.Reader`, `*strings.Reader`, `multipart.File`, `*os.File`) is measured in place, so the call fails before a single request is issued.
* A body that cannot seek and is declared at or below 16 MiB is drained and validated in full first, so it also fails before the bucket is touched.
* A body that cannot seek and is declared **above** 16 MiB is checked as MinIO consumes it, so the failure lands partway through the upload: the reader stops one byte short of the declared length, MinIO's multipart upload is aborted, and nothing is visible at the key.

**Remedy.** Declare the real size, or pass `-1` for a stream of unknown length, which is uploaded unchecked:

```go
/* the declared size is enforced, so declare what the body really holds */
putErr := objectStorage.Put(
	runtimeInstance,
	"invoice/2026-07.pdf",
	bytes.NewReader(document),
	int64(len(document)),
	storagecontract.PutOptions{ContentType: "application/pdf"},
)
if nil != putErr {
	return putErr
}

/* a stream of unknown length declares -1 and is uploaded unchecked */
return objectStorage.Put(
	runtimeInstance,
	"upload/report.csv",
	body.Body,
	-1,
	storagecontract.PutOptions{ContentType: "text/csv"},
)
```

A correct size, a zero declared size, and a body **shorter** than its declared size all behave exactly as before. The same pass also stopped reading a legal `(0, nil)` read as the end of the body — which let an over-read go undetected and stored a silently truncated object — and bounds consecutive empty reads while honouring the runtime context, so a stalled body or a client that walked away fails the put instead of pinning a core and an upload.

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

### HTTP: `JsonHandler` rejects a literal `null` body

**What changed.** [`JsonHandler`](../http/typed_handler.go) answers `400` for a literal `null` request body when its request type is instantiated as a pointer. The four-byte body decoded without error and left the value nil, the validator took its nil-pointer early return and reported every constraint satisfied, and the handler then dereferenced nil.

**Symptom.** That request is now a client error instead of a `500`.

**Remedy.** None. A value instantiation and a `{}` body were never affected, and a caller-supplied [`WithJsonHandlerErrorResponder`](../http/typed_handler.go) still shapes the response.

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

### Distributed lock: `LeaderGate.OnElected` receives a term-scoped runtime

**What changed.** [`LeaderGate`](../lock/leader_gate.go) starts renewing its lease **before** `OnElected` runs, and hands the hook a runtime whose context is cancelled when the lease is lost. Nothing renewed the lease while the hook ran, so a hook slower than the ttl let the lease lapse: another replica acquired it, both reported leadership, and the incumbent never demoted, because demotion only follows a failed renewal — which could not happen while the hook held the campaign loop.

**Symptom.** Leader-only work started inside `OnElected` that respects its context now stops when the lease is lost, instead of running alongside the new holder. The runtime the hook receives is no longer the run runtime, so its context ends at the end of the term rather than at the end of the process.

**Remedy.** If the hook needs a context that outlives the term — a cleanup that must finish whatever happens to the leadership — derive it from the run runtime captured outside the hook rather than from the one the hook is handed. Work that is only correct while this replica is leader should keep using the hook's runtime, which is the point of the change.

### Message bus: `melody:messagebus:consume` separates the signal from the handler lifetime

**What changed.** [`ConsumeCommand`](../messagebus/consume_command.go) runs the delivery pull and the handlers on two contexts. The shutdown signal stops the pull; in-flight handlers and their `Ack`/`Nack` keep a live context for the whole [`WithShutdownGrace`](../messagebus/consume_command.go) window (30 seconds by default), and the grace expiring is what cancels them.

**Symptom.** A handler that respects its context is no longer cancelled the instant the signal arrives; it is cancelled when the grace expires. One shared context meant the grace protected nothing, and the acknowledgement ran on the cancelled context too — so any transport honouring the runtime context on publish failed the `Ack` of a message whose side effects had already committed, the broker redelivered it on every deploy, and a failed `Nack` dropped the `RedeliveryStamp` increment so `MaxRetries` never converged.

**Remedy.** Size the grace to the slowest handler with `WithShutdownGrace`, and make sure a handler that must not be interrupted mid-write finishes inside it. A handler that relied on being killed at the signal now runs to completion or to the grace deadline.

### Websocket: a zero `IdleTimeout` is refused at construction

**What changed.** [`websocket.NewStreamHandler`](../../integrations/websocket/v3/handler.go) panics when `Options.IdleTimeout` is not positive, rather than treating the zero value as "no keepalive". Nothing else in the stack can reap a peer that goes away without a fin: `coderwebsocket.Accept` hijacks the connection, so `http.Server`'s read and write timeouts stop applying to it; the read loop then blocks in `Read` with no deadline of its own; and a write into a half-open socket keeps succeeding for as long as the send buffer has room, so a broadcast is no liveness signal either. The keepalive ping is the only remaining evidence, which makes its interval a required decision rather than a tunable with an off position. Left at zero, connections opened and abandoned each cost a descriptor, a hub subscription and three goroutines for the life of the process.

**Symptom.** An application whose websocket options never named an `IdleTimeout` no longer starts. The construction panics with `websocket options require a positive IdleTimeout: ...`, and the exception context carries the `idleTimeout` it was given. Through [`websocket.NewModule`](../../integrations/websocket/v3/module.go) the handler is built while routes are registered, so the failure surfaces at boot rather than on the first upgrade request — deliberately, the way the framework reports every other unusable configuration.

**Remedy.** Name the interval at which a silent peer should be pinged. `30s` suits a browser client, which answers the ping inside its protocol stack where the page's JavaScript never sees it, so a receive-only client stays connected:

```go
websocket.NewModule(websocket.ModuleConfig{
	Hub:  hub,
	Path: "/ws",
	Options: websocket.Options{
		TopicResolver: topicResolver,
		WriteTimeout:  10 * time.Second,
		IdleTimeout:   30 * time.Second,
	},
})
```

The module supplies no default of its own on purpose: the only thing that reaps a vanished peer should be chosen by the application rather than inherited silently.

### Other integration modules

* **`bunorm` deterministic encryption.** `melody:encrypt:database --mode=encrypt --deterministic` ([`encrypt_database_command.go`](../../integrations/bunorm/v3/encrypt/encrypt_database_command.go)) now rewrites a column that was already bulk-encrypted with random nonces into its deterministic form, keeping the key each value already carries ([`migrate.go`](../../integrations/bunorm/v3/encrypt/migrate.go)). Every such value previously authenticated under a live key and was passed through untouched, so the command reported success while converting nothing and every [`CiphertextCandidates`](../../integrations/bunorm/v3/encrypt/cipher.go) equality lookup on that column returned zero rows. *Symptom:* a deterministic run over an already-encrypted column now writes rows where it used to write none. *Remedy:* none — it remains idempotent and never rotates keys, so `--mode=reencrypt --target-key=...` is still the only way to change a key.
* **`bunorm` audit change-sets.** [`audit.ChangeSet`](../../integrations/bunorm/v3/audit/change.go) always serialises an empty change-set as `[]` rather than the json literal `null`. *Symptom:* a trail consumer that special-cased `null` in the `changes` column sees `[]` instead. *Remedy:* drop the special case; `jsonb_array_length(changes::jsonb)` now reads `0` where it errored or read `1`.
* **`websocket` keepalive.** A received pong refreshes the connection's liveness mark, and a keepalive ping that could not be written because a data frame was in flight is no longer read as a dead peer ([`handler.go`](../../integrations/websocket/v3/handler.go)). *Symptom:* a configuration with `IdleTimeout` below `WriteTimeout` no longer turns transient write contention into a disconnect — a frame in flight excuses a timed-out ping until one interval past the configured write timeout. *Remedy:* none; a receive-only client bridged onto a broadcast hub stops being disconnected for never sending a data frame.
* **`outbox` relay lease.** The distributed lease is released on a context detached from the run and bounded by five seconds, and a release failure is logged rather than discarded ([`relay.go`](../../integrations/outbox/v3/relay.go)). *Symptom:* a graceful restart no longer stalls outbox delivery for a whole `LockTtl`. *Remedy:* none.

## v3.0.0

v3 is a separate import path, so an application moves onto it by rewriting its imports rather than by resolving a new version. The entry below is the one rewrite that does not compile afterwards: v1 and v2 keep the identifiers, v3 has never carried them.

### Compile-level: `validation` does not carry the twelve deprecated constants

**What changed.** [`validation/const.go`](../validation/const.go) declares `ServiceValidator`, `ErrorInvalidRuleSyntax`, `ErrorUnknownRule` and `ErrorNestingDepthExceeded` and nothing else. The twelve deprecated aliases that v1 and v2 still declare are absent. Each one was defined as the constant that replaces it, so every replacement carries the identical string and the rewrite is a rename:

| Absent in v3           | Replacement                                                                              | Value                |
|------------------------|------------------------------------------------------------------------------------------|----------------------|
| `ErrorNotAlpha`        | [`ConstraintAlphaErrorNotAlpha`](../validation/constraint_alpha.go)                      | `notAlpha`           |
| `ErrorNotAlphanumeric` | [`ConstraintAlphanumericErrorNotAlphanumeric`](../validation/constraint_alphanumeric.go) | `notAlphanumeric`    |
| `ErrorInvalidEmail`    | [`ConstraintEmailErrorInvalidEmail`](../validation/constraint_email.go)                  | `invalidEmail`       |
| `ConstraintMax`        | [`ConstraintMaxLength`](../validation/constraint_max_length.go)                          | `max`                |
| `ErrorMaxLength`       | [`ConstraintMaxLengthErrorTooLong`](../validation/constraint_max_length.go)              | `tooLong`            |
| `ConstraintMin`        | [`ConstraintMinLength`](../validation/constraint_min_length.go)                          | `min`                |
| `ErrorMinLength`       | [`ConstraintMinLengthErrorInsufficientLength`](../validation/constraint_min_length.go)   | `insufficientLength` |
| `ErrorNotBlank`        | [`ConstraintNotBlankErrorIsBlank`](../validation/constraint_not_blank.go)                | `isBlank`            |
| `ErrorEmpty`           | [`ConstraintNotEmptyErrorEmpty`](../validation/constraint_not_empty.go)                  | `empty`              |
| `ErrorNotNumeric`      | [`ConstraintNumericErrorNotNumeric`](../validation/constraint_numeric.go)                | `notNumeric`         |
| `ErrorRegexMismatch`   | [`ConstraintRegexErrorMismatch`](../validation/constraint_regex.go)                      | `regexMismatch`      |
| `ErrorInvalidPattern`  | [`ConstraintRegexErrorInvalidPattern`](../validation/constraint_regex.go)                | `invalidPattern`     |

`ConstraintMax` and `ConstraintMin` are rule names — the token a `validate` tag spells — and the other ten are error codes a client reads off a validation failure.

**Symptom.** Code that named any of them stops compiling with `undefined: validation.ErrorNotAlpha` and the like. The failure is per identifier, so a package that used several reports several.

**Remedy.** Rename each reference to the replacement column above:

```go
/* v1 / v2 */
if validation.ErrorNotBlank == validationError.Code() {

/* v3 */
if validation.ConstraintNotBlankErrorIsBlank == validationError.Code() {
```

Nothing outside the Go source changes: the strings are identical, so a `validate` tag, an api client matching on the error code, and a translation catalogue keyed on it all keep working untouched.
