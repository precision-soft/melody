# Changelog

All notable changes to `precision-soft/melody` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

**v1 is feature-frozen.** The major is stabilized: no new feature lands here, while security fixes and critical correctness fixes still do. New development continues on [v3](v3/CHANGELOG.md); the move to v3 is described in [`.documentation/UPGRADE.md`](.documentation/UPGRADE.md).

## [Unreleased]

### Fixed

- documentation: the `MinimumSessionTtl` comment and the `MELODY_HTTP_SESSION_TTL` section of `CONFIG.md` name the mechanism that makes a sub-second lifetime broken rather than merely short: the storage purges every lapsed entry on the very write that stores the new one, so a ttl smaller than the time that write takes has `SaveSession` report success and persist nothing. Both documents named only the second reason — that no sub-second lifetime survives the response reaching the client and the client coming back — which is true and is not the one an operator hits first. Documentation only; the guard and its refusal are unchanged

- documentation: `DEBUG.md` lists `NewEventCommand`, `DeferredListener` and `DeferredListenerProvider`, and `EVENT.md` lists the two constructors `NewRequiredListenerSkippedErrorWithStoppedListenerFailure` and `NewRequiredListenerSkippedErrorWithCause`. Five exported symbols this major ships and no document on any major named — the type list stopped at `EventCommand` and at the base constructor, so the declaration channel of `debug:events` and the two refusals that carry a cause were reachable only by reading the source

- documentation: `DEBUG.md` names the cycle-guarded walk that runs before `json.Marshal` on a route attribute. The bullet described the degradation an unserializable value gets and stopped there, so the half that matters most was unwritten: `fmt` has no cycle detection, and a route attribute pointing at itself recursed until the goroutine stack was gone — a fatal error no recover turns into a reported failure, which is the process dying rather than the document degrading

- http: `RateLimitMiddleware` and `RegisterRateLimitRequestListener` read the limiter through the interface, so a typed-nil limiter is refused at construction under the guard's own name — it passed the plain comparison, looked live, and dereferenced its nil receiver on the first request the middleware metered
- http: a serializer resolution failure that is not the not-acceptable refusal is recorded at warning before the result handler's fallback serves the default representation — it was dropped whole, so a client that named an available type and received another had no diagnostic anywhere
- http: the abort sentinel no longer leaks the response it aborts. `http.ErrAbortHandler` was re-raised ten lines before the kernel captured the response in flight and seventy before either close ran, so a deliberate abort raised by a middleware after its `next()` returned dropped the only reference to a file-backed response — `FileResponse`, `ServeReader` — and leaked one descriptor per aborted request. The comment on `invokeErrorHandlerSafely` already refused to honour the sentinel for exactly this reason
- exception: a foreign error whose `Context()` panics no longer takes down the recovery that is reporting it. `renderErrorText` already contained a panicking `Error()`, but the provider's context ran bare in five places — at `LogContext`'s top provider, in its cause-context walk, and in the three `FromError*` constructors that run on the same recovery paths — so the panic unwound through the recovery defer as a second panic while the first was being rendered. The context is read under a recover, and a panicking one costs the context alone, with the panic value kept in its place

### Security

- http: the access log and the kernel's 405 and no-route records keep the query parameter NAMES and redact every value. A query string is the one part of a request line that routinely carries a credential — an api key, a one-time token, a signed link — and one access-log line is written per request, so a token in a url was copied into the journal on every call, where it is read by more people than the request was

## [v1.19.0] - 2026-08-18 - Stabilization Sweep, Hardened Failure Paths and Feature Freeze

### Added

- tooling: `.dev/validate/changelog.sh` and `.dev/validate/changelog.baseline` read the SHAPE of every changelog block, which nothing here had ever read.
- tooling: `.dev/validate/documentation.sh` reads the integration readmes, which nothing had ever compared.
- tooling: `.dev/validate/documentation.sh` reads the entries its own check is built on.
- validation: `.dev/validate/parity.sh` and `.dev/validate/parity.baseline` compare one major against the next after rewriting the import path, over the files, their text, the exported symbols, the test names and the file each test lives in, the section titles of the per-major documents and the dependency set of each `go.mod`.
- validation: `.dev/validate/citation.sh` and `.dev/validate/citation.baseline` check what the documents of a major CITE against what that major declares — the direction the neighbouring documentation check structurally cannot ask.
- event: `RequiredListenerSkippedError` and its three constructors — `NewRequiredListenerSkippedError`, `NewRequiredListenerSkippedErrorWithCause` and `NewRequiredListenerSkippedErrorWithStoppedListenerFailure` — name the class a caller has to separate from an ordinary listener failure: a dispatch ended, by a stop or by a failure, while a listener marked required was still behind it.
- exception: `IsAlreadyLogged` reads the already-logged mark at the depth `MarkLogged` writes it.
- logging: `LogOnRecoverAndExitAfter` logs a recovered value like `LogOnRecoverAndExit` and runs a step of the caller's between the record and the process exit.
- serializer: `ErrNotAcceptable` is the cause the manager's refusal carries when the accept header refused every media type it can produce.
- session: `ErrSessionDeleted` is the cause the save refusal carries for a session deleted while the request holding it was still running.
- session: `(*Session).Snapshot` reads the values, the modified flag and the cleared flag under one lock acquisition, for the response path that has to pair its branch decision with the values it acts on.
- config: `(*Configuration).MarkServing` records that the wiring phase is over, after which `Resolve` is refused: every service built during boot copied the values it read, so re-resolving reconfigures nothing and only rewrites the parameter store under readers entitled to treat it as settled.
- httpclient: `(*HttpClient).RequestStreamWithContext` is `RequestStream` bound to a context: cancelling it ends the request and the body read, which is the only remedy for a stream a server never ends.
- debug: `MiddlewareDescriptionProvider` and `MiddlewareBuildProvider` are the two seams `debug.NewMiddlewareCommand` takes.
- logging: `loggingcontract.LevelReporter`, the optional capability a `Logger` implements to answer whether a record at a given level would survive its threshold, and `logging.LevelEnabled`, the single door onto it.
- http: `(*Kernel).OpenRequestScopes` reports how many request scopes are open right now — one per request being served, hijacked connections included, because the scope belongs to `ServeHttp` rather than to the connection.
- cache: `RememberOption.WithContext` ties a caller's wait to the request that opened it, and `Context` reads it back.
- cli: `--format=json-pretty` renders the same envelope indented for a person reading it by hand.
- security: `NewAccessControlRawPrefixRule` names the cross-segment prefix rule, and `NewAccessControlRule` now builds the segment-bounded one — the plain name is the bounded tool.
- security: `RolesReplacer`, the optional capability a `Token` implements to answer its own twin under another role set, and `(*AuthenticatedToken).WithRoles`, which implements it.
- logging: `RunShieldedStep` runs one step under the exit handler's own budget and answers whether it finished, so the normal return of `Run` tears down under the same shield the panic path has had since the exit-step budget was installed
- example: the example carries the source of its own frontend bundle, in `.example/assets/` — `app.ts`, the `melody-routes.ts` URL generator, and the `package.json` that bundles them into `public/assets/app.js` with esbuild.
- example: a stateless api-key firewall on `/products/api`, which is the door `APP_API_TOKEN` always promised — the key shipped in `.env`, was marked secret, and nothing read it.
- example: the cors LISTENERS, armed by `APP_CORS_ALLOW_ORIGINS` (comma separated; empty keeps cors unwired): a preflight aimed at an access-controlled path is answered 204 before routing and before the security chain can refuse it, and the refusals the security listeners produce carry the cors headers — responses the middleware chain never sees, which is why the listeners are the door the example demonstrates rather than the middleware.
- example: file-backed session storage as a configuration choice — `APP_SESSION_FILE` names the snapshot (a relative path is anchored to the project directory) and the example registers `session.NewFileStorageFromPath` under the framework's storage service id, which wins over the has-guarded in-memory default; empty keeps the default.
- example: a second live database in the same process — the catalog journal moves onto `bunorm/pgsql` while the catalogue stays on mysql, which is what shows the provider is a choice rather than an assumption.
- example: the request-scoped change attribution, `service.ChangeAttribution` — the example's own demonstration of `RegisterScopedServices` and of `container.Lazy`.
- security: `RoleHierarchyAware`, the optional capability an `AccessDecisionManager` implements to receive the declared role hierarchy at compilation, and `(*AccessDecisionManager).WithRoleHierarchy`, which implements it for the built-in manager by wrapping its own role voters.
- http: `Kernel.SetMethodPolicy` installs the method policy the kernel reads on every request — whether `HEAD` falls back to the `GET` route, whether an unrouted `OPTIONS` is answered with the computed `Allow` header.
- exception: `PanicCause` reads a recovered panic value as the cause of the error a recovery boundary fabricates in its place — the error itself when the panic was error-shaped, and nothing when it was a typed nil whose `Error()` would dereference a nil receiver at the first render.
- validation: `IsRuleWiringErrorCode`, `ValidationErrors.HasRuleWiringError` and `ValidationErrors.WithoutRuleWiringContext` tell a mistake in the rule DECLARATION — `unknownRule`, `invalidRuleSyntax`, `invalidPattern` — apart from a refusal of the submitted value.
- exception: `Logged` answers an error that reports itself already logged, and is what a writer returns after filing its record.
- security/config: the seven `FirewallOverrideConfiguration` setters the family always documented — `WithAccessControl`, `WithRoleHierarchy`, `WithAccessDecisionManager`, `WithEntryPoint`, `WithAccessDeniedHandler`, `WithMergeStrategy` and `WithInheritGlobalAccessControl` — beside the `WithStateless` that was the only one.
- security: the `RefusalReason*` constants naming which branch of the access decision produced a 403.
- logging: `NewStandardErrorLogger` adapts a melody logger to the `*log.Logger` net/http's `Server.ErrorLog` wants, so the connection-level failures the http kernel never sees — a TLS handshake that fails before any request exists, a malformed request line, a superfluous `WriteHeader`, a listener degrading — reach the application's journal instead of being printed to stderr as unstructured text
- bunorm: `SecretParameterProvider`, the optional capability of naming the configuration parameters that hold a provider's credentials (see the integration's own changelog)
- container: `contract.ContainerCarrier` — the door a resolver answers its container through — implemented by the container itself, by the concrete scope and by the resolution context a provider receives.
- config: a `.env` value may reference a parameter the composition root registers between construction and boot.
- config: `MELODY_HTTP_SHUTDOWN_TIMEOUT` (`kernel.http.shutdown_timeout`) sets how long a stopping http server waits for the requests it has already admitted, reaching the server through `Configuration.Http().ShutdownTimeout()`.
- bag: `*ParameterBag.AppendString` appends inside one critical section.
- cache: `NewManagerOwningBackend` builds a manager that closes its backend when it is closed itself, for the caller that builds both by hand and wants one Close to end both.
- cache: `DeserializationError` marks a read that found a payload the serializer cannot decode, with `NewDeserializationError` and `IsDeserializationError` beside it.
- example: the development stack serves all three example applications at once, each under a name that says which it is — `v1-example.`, `v2-example.` and `example.melody.localhost.precision-soft.com`.
- example: the example application is a working nomenclature rather than a set of routes that exist to be driven.
- example: every redis key and every table the example writes carries its major.
- config: `MELODY_STATIC_EXCLUDED_PATHS` (`kernel.static.excluded_paths`) names the path prefixes the built-in file server declines without touching the disk, comma separated.
- http: `SimpleRateLimitWithResolver`, `IpRateLimitWithResolver` and `UserRateLimitWithResolver` take the client-ip resolver the convenience helpers could not reach.
- http: `static.FileServerConfig.SetAllowedDotPrefixList` names the dot-prefixed first path elements the file server may retrieve.
- http: `RegenerateRequestSession` rotates the session id of a request and republishes the rotated session in one call, which is the whole session-fixation defence a login handler needs; write the authenticated identity to the session it returns.
- config: `MELODY_HTTP_SESSION_TTL` (`kernel.http.session_ttl`) sets how long a stored session stays valid, reaching the session manager through `Configuration.Http().SessionTtl()`.
- http: `SessionCookiePolicy.Secure` takes a `SessionCookieSecurePolicy`.
- cache: `Manager.GetCounter` reads a key written by `Increment` or `Decrement`.
- application: two boot warnings name a resource melody supplied that nothing will ever reclaim, so the deployment learns it from a log line rather than from a memory graph.
- container: a scope is a registrar in its own right, layered over the container it came from.
- container: `Replacing()` admits a registration whose name — or whose registered type — the other lifetime already claims.
- container: `ClosedWithScope()` hands an installed override to the scope's teardown, through the new `OverrideInstanceWithOptions` and `OverrideProtectedInstanceWithOptions`.
- application: `ScopedServiceModule` is the module hook for request-lifetime services, with `RegisterScopedServices(kernelInstance, registrar)` running at boot beside `RegisterServices`.
- http: `RequestContextMustFromResolver` and `RequestContextFromResolver` resolve the current request's context — its id and start moment — out of the request scope.
- application: `ProcessContext` — `ServiceProcessContext`, `NewProcessContext`, `ProcessContextMustFromResolver`, `ProcessContextFromResolver` — is the console counterpart of the http request context: the generated process id every log record of the run is correlated under, and the moment the run started.
- logging: `NewProcessLogger` is the console sibling of `NewRequestLogger`, and the cli entry point now decorates with it.
- http: `cors.RegisterListeners` wires the two cors listener doors at once, and `cors.RegisterRequestListener` is the new half of the pair: a kernel.request listener at `cors.RequestListenerPriority` (100) that answers a well-formed preflight from an allowed origin before the security chain runs.
- http: `middleware.RegisterRateLimitRequestListener` meters requests on kernel.request at `RateLimitRequestListenerPriority` (200), ahead of token resolution (50) and access control (20), sharing `RateLimitConfig` with the middleware.
- config: `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` (`kernel.http.session_tombstone_retention`) sets how long a deleted session id keeps refusing a write-back, reaching the session manager through `Configuration.Http().SessionTombstoneRetention()`.
- session: `NewManagerWithTombstoneRetention` builds a manager whose write-back refusal window is sized by the caller, for wiring the manager by hand; it refuses a non-positive window with a panic naming the rule, the same answer the constructor gives a negative ttl.
- http: `Kernel.HasErrorHandler` reports whether the application installed an error handler.
- container: `ServiceDescriptions` on the built container answers what it can say WITHOUT running a provider — every name either lifetime knows, with the type read from the built instance when one exists and from the provider's declared return type otherwise, which the container now records at registration.
- http: `pipeline.Builder.Describe` answers what `Build` would run without running it — the same selection, gating and ordering, refused for the same cycles and missing references, but no factory invoked — and `HttpMiddlewareDefinition.SetFunctionName` records at registration the function a description names, so listing a pipeline never has to build one.
- debug: `debug:events` renders a `SERVING-PROCESS LISTENERS` block for the listeners only the serving process wires, fed by the composition root through `debug.NewEventCommand` and `debug.DeferredListenerProvider`: with security configured, a console process declares the security resolution and access control listeners with their real event and priorities instead of rendering an absence that reads as "not wired".

### Changed

- example: each major's example application holds its schema in a database of its own rather than the one all three shared.
- cli, logging: text of unknown origin reaching a terminal or a plain-text log line is escaped rather than obeyed.
- example: the catalog migration set holds four migrations — the journal's `20260814000005` moves, with its name, into the postgres-dialect `JournalMigrations` set (see the Added entry), so `db:migrate` over an emptied catalog database reports `applied 4 migrations`.
- example: the static cache is armed in the shipped `.env` — `MELODY_STATIC_ENABLE_CACHE=true`, `MELODY_STATIC_CACHE_MAX_AGE=3600` — where every example previously opted out of the framework's own default.
- example: the seeded passwords are bcrypt.
- example: a validation refusal answers one `errors` entry per violated field — `presenter.ApiValidationError`, which both product write handlers now answer through — instead of the single semicolon-joined, alphabetically sorted string the presenter used to receive from `ValidationErrors.Error()`.
- example: the four example commands — `app:info`, `product:list`, `catalog:journal`, `catalog:report:refresh` — render through the framework's `cli/output` envelope instead of `fmt`, so each accepts the standard flag set and answers one machine-readable json document under `--format=json`.
- example: the cron wiring moves onto the module facade — `configure.go` registers `cron.NewModule` with the configuration factory and the three scheduled commands, instantiated, as its runner commands, and the hand-wired registration of the generate command is gone.
- example: the database schema is owned by a migration set, `migration/` — five DDL migrations whose column definitions were captured from the tables the repositories used to create — and the repository-owned `EnsureSchema` is gone from the five bun repositories and from the journal repository's public interface.
- cli: `--format=json` writes one document on one line, terminated by a newline, instead of an indented block.
- cache: `InMemoryBackend.Get` and `Many` take the exclusive lock only when an entry's place in the recency list has actually gone stale, where they used to take it on every hit just to move the entry to the front.
- container: the teardown breaks a tie the dependency graph leaves open on **creation order, latest first**, not on the node key descending.
- container: closing has two states, and a service's own `Close` may still resolve.
- logging: the json logger takes its timestamp inside the write mutex, together with the encoding, so the order of the stamps is the order of the writes — which is what `LOGGING.md` has always promised and what the comment on that line claimed the precision was paying for.
- security/config: a firewall's access decision manager, entry point and access denied handler are refused at compilation when they hold a **typed nil**, by firewall name and naming which of the two configurations declared it; `Builder.SetGlobal` refuses the same three at the door.
- http: `Router.AllowedMethods` applies the locale filter the matcher applies, so it no longer announces the methods of a route the path's locale excludes.
- config: `Parameter.Int` reads the same grammar its sibling accessors read, delegating to the shared parser instead of a hand-written `strconv.Atoi`.
- http: the in-process rate limiters say on their constructors that their counters live in the process and nowhere else — a restart hands every caller a full budget back, and each replica enforces the limit on its own — with the pointer to the distributed drop-in in `integrations/rueidis`.
- cache: `NewJsonSerializer` writes down the hazard of a bare json document with no schema discriminant — a value written by one release decodes cleanly in the next, so a field added since reads as the zero value with no decoding error, and an entry cached with a ttl of zero never lapses to heal it.
- security/config: `Compile` says what its argument implies — `Configuration` has only unexported fields and no constructor, and `Builder` never hands one out, so a caller outside the package can only pass the empty value, whose answer is a nil compiled configuration and a nil error.
- http/static: in the embedded mode the cache validators stop being derived from a modification time that does not exist.
- cron: `Configuration.Entries` hands out copies all the way down — the list, each `ScheduledCommand` and each `EntryConfig` behind it, schedule included.
- http: a 400 whose validation errors blame the rule declaration is recorded at **error**, not at the warning a deliberate 4xx earns.
- http: the per-field validation errors in a 400 body no longer carry the internal context of a rule-declaration fault.
- http: a session another request ended while this one was running is recorded at **warning**, under its own name.
- http: the session-persistence records carry a one-way reference to the session id — a SHA-256 truncated to 16 hex characters — beside the request method and the path.
- http: the rate-limit request listener records the caller's own cancellation at warning under its own name instead of as a store failure at error.
- cache: the two recovery boundaries of `Remember` hand the panic value on as the cause and capture the stack of the goroutine that raised it.
- httpclient: `SetHeader` stores the header under its canonical spelling, the one the constructor stores under.
- config: a parameter registered after boot is published before its own template is resolved, so the resolution's secret propagation — which marks the reader it finds by name in the parameter map — can see it.
- application: the http server's `ErrorLog` routes net/http's own reports into the application logger, at warning with the line in the record's context.
- security: the access-control merge strategies state what they decide — the strategy orders the merged rule LIST, while the matcher resolves by category first (exact beats prefix, longer prefix beats shorter, prefix beats regex, fallback last), so position decides only what the categories leave tied; the names carried a first-match connotation the matcher's documented longest-prefix design never honoured, in an authorization decision.
- session: `FileStorage` states the shape degradation its own flush makes invisible until the first redeploy — values reload from JSON at construction, so a session survives a restart with an `int` reading back `float64` and a struct reading back `map[string]any`, while the same session read in-process keeps the types the handler stored
- event: `RegisteredEvents` states on both dispatchers that it reports a point-in-time view a concurrent (un)installation is observed mid-step through, and `MarkListenerRequired` states the window its two-step shape forces — a dispatch between registration and mark sees the listener unmarked, so a runtime registrar must not dispatch the event until the mark is applied; boot-time registration closes the window by construction
- container: `Close` states its re-entrancy contract — the blocking that protects concurrent callers makes a Close called from inside a service's own Close a deadlock by construction, and the documented answer is the protocol `IsClosed` already describes: the flag is set before the first service Close runs, so a defensive closer that asks first skips during the teardown.
- container: the coalesced-waiter receive states its one assumption — the wait channel closes on every exit of the creating call, return and panic alike, so what parks the waiters forever is only a provider that neither returns nor panics, and Close deliberately does not release them because failing them open would hand out services from a teardown in progress
- httpclient: `Response.Headers` and `Response.Body` state their single-owner contract — they hand out the live map and slice on purpose, the response being the caller's own result object, and a caller fanning it across goroutines copies first
- serializer: the qvalue grammar moved to `internal.ParseQualityValue` so the three negotiating readers — the Accept parser here, `PrefersHtml` and the compression middleware — share one rule; behavior of this package is unchanged
- container: the package documentation states the close-time ownership rule the teardown has always applied — when the container itself closes, everything still standing in its maps joins the teardown, installed overrides included, because the container's lifetime is the process's and there is no opt-in; a scope is deliberately the opposite, closing only what it built, with `ClosedWithScope()` as the one door an outside override joins its teardown through
- cli: `output.StandardFlags` and `output.DebugFlags` document the quiet defaults the two have always carried: quiet defaults to true on the standard set — a command's essential output is never gated on quiet, so the flag governs headers and decoration alone — and the debug set flips it to false, because an introspection command's headers are part of its answer
- application: the http shutdown wait's documentation names the deliberate outcome of an exceeded budget — the deadline error surfaces and the process exits non-zero, because requests were lost; a graceful drain that ran out of time is the operator's signal, not a success to smooth over
- documentation: the package documents catch up with the code they describe.
- cache: the key grammar is part of the `cachecontract.Backend` promise — non-empty, no spaces or newlines, at most 1024 bytes — and the in-memory backend enforces it with the exact refusals the redis backend answers.
- application: the application's own http routes register before any module's.
- application: a module's identity is its instance — the same instance reached through two providers, or registered twice, boots once.
- application: `RegisterModuleProvider` handed a provider that itself implements `Module` registers it as that module, own hooks included, followed by its children — the door used to keep only the children and silently drop the provider's own registrations, so the two registration doors registered different applications from the same value.
- application: both module doors refuse a registration arriving from inside a module boot hook, and `RegisterHttpRoute` refuses during the same window.
- application: the second module boot phase runs one loop per hook for the event, middleware, http and cli hooks, the granularity every other phase already had — each hook runs across every module before the next hook begins, in registration order inside each group — and the contracts now document that rule.
- application: the firewall manager is registered whenever a security configuration compiled, whatever the process mode — configured means resolvable.
- cli, application: the container is closed by the recover handler that owns the process exit, on the linear path exactly as on the panic path — the cli action no longer closes it after the command returns.
- http: the kernel exception listener records a 4xx at warning level.
- application: the logger registration is gated behind `Has`, like the cache, the session storage and the firewall manager beside it.
- logging: `LogOnRecoverAndExit` and `LogOnRecoverAndExitAfter` refuse an exit code outside 1..255 with a panic naming the rule — the rule `exception.NewExitError` has always enforced, applied to the code the handler itself would exit with.
- logging: a recovered `*exception.ExitError` carrying an out-of-range code is no longer honored as an exit.
- logging: each step of the exit handler — the record, the before-exit teardown — is abandoned after ten seconds instead of being waited on forever.
- security: `AccessDecisionManager.DecideAll` refuses an empty attribute list instead of granting it.
- security: a typed nil is refused where a nil is refused, on every interface-typed piece of a firewall — the matcher, the token source, the login and logout handlers, a rule, an authenticator, a decision voter and the token a security token wraps.
- security: the constructors that keep a caller's slice copy it, matching the copy they already make when handing it back.
- internal: `Duration` refuses a bare `int`/`int64`.
- internal: `Float64` refuses NaN and the infinities on every branch.
- internal: `MapStringString` reports a typed-nil map as absent.
- clock: `FrozenClock.Advance` is forward-only and panics on a negative duration.
- clock: the frozen ticker's `Stop` returns only after its relay goroutine has exited, so no tick can be minted after `Stop` returns.
- clock: the `Ticker` and `Clock` contracts state their obligations in the GoDoc, and the documentation carries them.
- internal: `testhelper.AssertPanics` is removed.
- internal: `testhelper.HttpTestRequest.Header` mirrors the production request and dereferences its underlying request unguarded.
- httpclient: a client configured with a base url refuses an absolute url that leaves that origin.
- httpclient: a basic credential travels whenever the caller asked for one, empty halves included.
- httpclient: an empty target names the base resource itself.
- httpclient: a nil `RequestOption` is refused instead of called.
- httpclient: `NewHttpClient` refuses a nil configuration where the wiring mistake is made.
- httpclient: a negative per-request timeout bounds a stream instead of unbounding it.
- event: a dispatch that skipped a listener marked required refuses the response the stopping listener produced.
- event: a listener that stops propagation and fails in the same call is reported for the listeners it skipped.
- event: an event that arrives already stopped runs no listener.
- event: `AddSubscriber` refuses a second registration of a subscriber already registered, naming the type.
- event: `AddSubscriber` validates every subscribed event before registering any listener.
- event: `AddSubscriber` refuses a subscriber that declares no subscribed events, and an event name mapped to an empty list.
- event: `MarkListenerRequired` and `MarkListenerMaySkipRequiredListeners` refuse a registration the dispatcher does not hold, naming the event and the listener id.
- event: `EventDispatcherAdapter` refuses to mark a required listener over a wrapped dispatcher that cannot mark one, naming the wrapped type.
- event: `contract.RegisteredListener` reports the required-listener marks a listener carries, and `debug:events --verbose` renders them in a `required` column.
- event: `NewEventDispatcherAdapter` takes the dispatcher alone.
- event: an `*exception.ExitError` raised by a listener travels to whoever owns the process boundary instead of being folded into a listener error.
- cli: `output.MergeFlags` refuses a duplicated flag name at the line that declares it.
- cli: `TableBlockBuilder.AddRow` refuses a row whose cell count disagrees with the block's declared columns, naming the block and both counts.
- cli: the standard integer flags — `--verbosity`, `--limit`, `--offset`, `--table-width` — refuse a negative value at parsing, naming the flag, the way `--format` and `--order` have always refused an unsupported one.
- cli: the table format prints the warnings under `--quiet` too, and renders the envelope error whole — message, code, details, cause.
- cli: a failure reported through the rendered envelope reaches the application log.
- cli: a table printing failure fails the command.
- http/bag: the request bags keep the single and the repeated key apart by type, and `Request.Input` delivers the query and form values it silently lost.
- http: a form that does not parse is refused the way a body that does not read is.
- config: the template scanner finds the real `")%"` closer and refuses what silently survived as text.
- config: a braced `${...}` reference in a `.env` value whose name breaks the key grammar is refused instead of surviving literally — `${DB-PASS}` rode into the dsn as text with no signal, while nobody types `${...}` into a password by accident; the bare-dollar grammar that keeps `pa$sword` and `$1.50` data is untouched, and the refusal names the enclosing key, never the content.
- config: the `.env` trailing comment is cut once, by godotenv's own countback.
- config: the `.env` preprocessor walks bytes, not runes.
- config: `MarkSecret` after the boot resolution travels to every parameter that reads the marked key, and follows the marking to a fixpoint — a reader of a freshly marked name is scanned in turn, so a whole derivation chain is covered however late the mark arrives, the way the early marking always did — the late call redacted the key while the dsn assembled from it printed in full in `debug:parameters`, and the end-of-boot retry reported that unapplied markings had landed when their propagation had nothing left to travel through.
- config: `IntWithDefault` answers the default only for an absent (or typed-nil) parameter; one that exists but does not parse panics — `1O0` silently became the default and the operator believed the configured value was live.
- config: a runtime parameter name is judged trimmed: the whitespace-only name passed the empty guard and registered a phantom, the padded name registered a parameter no exact-match lookup could ever reach; both are refused at the registration line, the whitespace-only one as the empty name it is.
- bag: `ParameterBag.All` copies as deep as the bag's own writers go — a `[]string` or `map[string]string` handed back live aliased the stored value, and a caller mutating the copy wrote into the bag behind its lock.
- cache: `NewManager` builds a manager that does not own its backend — Close leaves it open.
- cache: a closed `InMemoryBackend` refuses every operation with `cache backend is closed` instead of silently serving a map whose cleanup goroutine is gone — an entry written after Close was never reclaimed by anything and grew the map for the rest of the process, while Close had already reported the backend gone; Close itself stays idempotent.
- cache: the in-memory backend refuses the degenerate inputs it silently absorbed.
- cache: a payload the serializer cannot decode is a miss for `Remember`, not a terminal failure.
- cache: `Manager.Many` no longer discards the whole answer over one corrupt entry, and the error finally names its culprits.
- cache: the zero-value `RememberOption` reads as the constructor defaults.
- cache: `Remember` coalesces concurrent callers only for pointer-kind `Cache` implementations, whose address tells instances apart.
- validation: a parameterized rule named without parameters fails closed instead of silently validating with the registered singleton.
- validation: the regex constraint refuses an empty pattern.
- validation: a numeric rule parameter is an integer in its entirety or it is refused.
- validation: the string-form constraints — `regex`, `email`, `alpha`, `alphanumeric`, `numeric` — refuse a value that is not a string instead of silently passing it.
- validation: `min`, `max` and `notBlank` measure strings, not Go renderings.
- validation: a `validate` tag that parses to no rule at all is refused.
- http: the per-field detail of a failed validation reaches the client.
- security: a global access control declared without any firewall is enforced rather than silently discarded.
- security: the zero value of `config.FirewallOverrideConfiguration` inherits the global access control the way its constructor does.
- security: `RoleVoter` and `RoleHierarchyVoter` deny a token that is not authenticated even when it carries roles.
- security: `SecurityContextFromRuntime` returns `(nil, false)` when the security context cannot be resolved instead of panicking.
- security: the authorization denial paths never write a nil response.
- security: a failed event dispatch on a refusal path keeps the real error as the cause rather than replacing it.
- security: an access denied handler that fails keeps the authorization decision as the cause.
- security: `CompiledFirewall.Logout` fails closed on a nil result the way `Login` does.
- security: the token-source recovery survives a nil token source.
- security: `NewApiKeyHeaderRule` rejects a nil matcher at construction, the way the other firewall dependencies are validated, so a configuration mistake fails at boot rather than panicking in `Applies` on the request path, outside any recovery
- security: a role hierarchy or decision manager that neither the firewall nor the global configuration declares is reported as `SourceNone` rather than `SourceFirewall`, so a debug panel no longer claims a manager that does not exist at the point the runtime answers that it is missing
- security: `NewAccessControlRawPrefixRule` refuses `PUBLIC_ACCESS`.
- example: the catalogue reading is served at `/catalog/report/` under the name `example.catalog.report`.
- example: the welcome text on the static index says what the application is — a product nomenclature of products, categories, currencies and users — instead of calling itself a small demo, and the api token shipped in `.env` is named for the example rather than for a demonstration.
- logging: `LogOnRecover` only logs.
- config: `Resolve()` reports an error once the application is serving.
- config: a positive session ttl below one second fails the boot.
- httpclient: the client sets `MaxIdleConnsPerHost` on its transport, exposed as `TransportConfig.MaxIdleConnsPerHost` and defaulting to `MaxIdleConns` (100), following an override of it unless pinned explicitly.
- validation: a nil pointer embed is validated as "nothing was supplied" rather than "nothing to validate", so the constraints on its promoted fields run against their zero values exactly as a value embed's already did.
- validation: exceeding the nesting-depth cap is reported as a validation error (`nestingDepthExceeded`) only when the truncated subtree could actually carry a `validate` tag, so nesting past the cap can no longer bypass validation while free-form tag-free client json is accepted up to the cap as before.
- http: a route pattern whose optional parameter is not its final segment is refused at registration instead of being accepted and half-served.
- cli: in json mode a command's standard output is the rendered document and nothing else — the ansi start/finish banner that `cli.Register` wraps around every registered command is suppressed, and `--format=json` implies `--no-color`.
- cli: a command whose envelope reports an error exits non-zero.
- cli: `--format` and `--order` reject an unrecognised value instead of silently substituting the default.
- http: the response path saves and advertises the session published on the request under `RequestAttributeSession` at the moment the response is written, rather than the one the kernel captured before routing.
- cli: `--order=desc` reverses the listing in `debug:router`, `debug:events`, `debug:parameters`, `debug:middleware` and `debug:container`, applied before `--limit`/`--offset` so a descending window returns the end of the list rather than the beginning; `--order=asc` remains the default.
- cli: the `--fields` and `--sort` flags are withdrawn.
- rate limit: the default key extractor now keys on the client IP alone instead of `ip:path`.
- security: an access control rule whose attribute list normalizes to empty is refused at construction instead of being accepted.
- http: `TokenBucketLimiter` is now `FixedWindowLimiter`, with the old type name and both constructors kept as deprecated aliases so existing code is unaffected.
- http: the request attributes the kernel owns are reserved under the framework's own underscore prefix (`RequestAttributeSession` is `_session`, `RequestAttributeScheme` is `_scheme`) and are published after the route attributes, so a route attribute can no longer replace the session object or the resolved scheme.
- container: the protected `service.` namespace holds at both lifetimes.
- container: a closed container refuses new registrations and new overrides the way its scoped registrar always has.
- container: an override must fit every type its name is registered under.
- container: `Has` and `HasType` answer under the same suspension `Get` enforces.
- container: a typed-nil provider function is refused where it is registered.
- container: two DIFFERENT types that share one identity key refuse to coexist.
- container: `Replacing()`'s documentation says what the code does.
- application: an http process whose `.env` artifacts contributed no keys at all refuses to boot.
- application: a middleware factory that yields nil is refused when the pipeline is built.
- http: a route indistinguishable from one already registered is refused where it is declared.
- application: a teardown failure on Run's normal return exits non-zero.
- application: `HttpTimeoutConfiguration` and `HttpShutdownConfiguration` are removed.
- application: `RegisterConfiguration` refuses a name nothing consumes.
- logging: `LogError` anchors the record on the error the caller handed over and reads the already-logged mark at the depth `exception.MarkLogged` writes it.
- logging: `jsonLogger.Close` recognizes the process console by identity — the `os.Stdout` and `os.Stderr` values themselves — instead of by file name.
- logging: `NewJsonLoggerWithLabels`, `NewLoggingConfiguration` and `LoggingConfiguration.LevelLabels` copy the label map instead of sharing it.
- logging: the json logger weighs a record whose level is not one of the five known ones as an error instead of as debug.
- logging: the json logger's timestamp carries nanosecond precision (`RFC3339Nano`).
- logging: an error in a log context that also implements `json.Marshaler` is handed to the encoder instead of being flattened to its message, and `validation.ValidationErrors` now marshals as the array it is — each element through its own marshaler, with field, message, code and context.
- logging: the request logger writes the real request id under its context key unconditionally.
- application: a panic during `Boot` tears the container down before the exit.
- serializer: `NewSerializerManager` refuses two mime keys that collapse into one normalized key, naming the normalized key and both spellings.
- serializer: a member of the accept header whose `q` parameter falls outside the RFC 7231 qvalue grammar — a zero with up to three decimal digits, or a one with up to three zero decimals — is dropped whole instead of being scored by guesswork.
- serializer: a manager deliberately configured without `application/json` answers an empty accept header — and one that matches nothing it has — with its first configured serializer in lexical mime order, instead of refusing every such request while a serializer sits configured beside the refusal.
- example: the api presenter answers `406 Not Acceptable` when the accept header refuses every available media type, exactly as the framework result handler answers the same header on the success path.
- event: a dispatch aborted by a failing listener scans the listeners behind it for one marked required, exactly as a stop of propagation does, and refuses the dispatch when it finds one.
- http: the kernel publishes the response `writeResponse` actually wrote — the function returns it, and every call site assigns it back before the terminate event fires.
- application, http: the framework exception listener is registered only when the application installed no error handler by boot, which makes `SetErrorHandler` reachable for the first time in a framework-booted application.
- http: every default error rendering goes through one door — the exception listener and each of the kernel's seven fallback paths call the same renderer — and the body honours the negotiation the success path honours: a client that negotiated a registered representation gets its error in it, where the error path used to answer hardcoded json below the two-way html test whatever the accept header said, measured against a success body negotiated to xml on the same header.
- http: `WriteToHttpResponseWriter` gives a header key named by the response to the response: the writer's values for that key are replaced rather than appended to, and keys the response does not name keep what the writer carries.
- session: `Session.Get` hands out a copy at the depth `All` copies at, for the reason `All`'s own documentation names: the live nested value, mutated in place, changed the session without passing through `Set` — `modified` stayed false, `SaveSession` skipped the write and reported success, and the mutation silently never persisted, measured with the stored role unchanged behind a save that returned nil.
- http: `static.NewFileServer` copies the configuration at construction, struct and both lists, so the server is immutable once built.
- application: the kernel's default listeners — the profiler under debug mode, the response normalizer, the terminate access log, and the exception listener when no error handler was installed — register at the end of `Boot`, in every process shape, instead of inside the http run.
- debug: `debug:container` describes by default and builds only when asked.
- debug: `debug:middleware` describes the pipeline by default and builds only under `--build`.
- debug: the verbose listener block of `debug:events` prints the DISPATCH order — the dispatcher's own slice, held sorted at insertion — with an `order` column carrying the rank, in the table and in the verbose json alike.
- debug: the `application` row of `debug:version` reads the process-wide declaration made through `cli/output.SetApplicationVersion` — the door that existed with zero callers — and the framework wiring no longer fills it with melody's own version: both rows printed `v1.19.0`, measured, so the one command that answers "what is deployed here" answered the framework version twice.
- debug: `debug:router` renders the two discriminators the dispatch actually uses — `priority` and `order`, the registration rank that breaks a priority tie — on every row, in the table and the json document; `--verbose` adds the requirements, defaults and attributes as compact cells, and the json items carry the three maps always.
- debug: the trace/stack noise filter of the rendered error context is a display concern and full verbosity turns it off: `debug:container ...
- config: a late `MarkSecret` covers the whole derivation chain.
- container: a scope closes what it built in dependency order, dependents before their dependencies, falling back to creation order, latest first, for anything the graph says nothing about — the same tie-break the container's own teardown applies, since the two share one walk — and reporting a cycle the way that teardown reports one.

### Removed

- cli: the dead unexported error presenter is gone — an error-printing path with sorted context and a bounded cause chain that nothing has called since the envelope renderer became the one presentation, drifting beside the live path it duplicated

### Fixed

- documentation: the integration readmes list the doors their modules ship.
- documentation: four counts in this block are corrected against the code they describe.
- documentation: seventy-seven exported doors this major has always carried are listed at last, and the enumerations that pretend to enumerate stop leaving them out.
- documentation: the package documents, the upgrade guide and the roadmap are corrected where they described something the code does not do.
- documentation: five source comments are corrected against the code they sit on.
- documentation: the entries of this block that described code this major does not carry are corrected against it — the access-control refusal names `NewAccessControlRawPrefixRule`, the constructor that panics on `PUBLIC_ACCESS`, rather than `NewAccessControlRule`, which allows it; the late secret marking is described as the fixpoint walk the code performs rather than as a direct-reader pass with the closure deferred to another major; the scope close falls back to creation order rather than to a descending node key; the cli action is described as not closing the container at all; the event adapter entry describes the ordering index being removed rather than reordered; the runtime registration entry describes the publication preceding the resolution, which is the order the code takes and the order its own comment explains; the session-persistence records are described as carrying a one-way reference rather than the session id; the httpclient entry stops claiming the unsupported-body
  error names the call it came from, which the function raising it cannot know; and the time-codec entry drops the false parenthesis about every sibling memo
- documentation: two claims of this major's documents are brought back to what the code does.
- session: the file storage's atomic save fsyncs the directory after the rename, the way the cron generator's atomic writer always has — without it a power loss after a save could resurface the previous snapshot, silently logging out the user whose session had just been written; and the construction sweeps the `<name>.*.tmp` orphans a hard kill leaves between `CreateTemp` and the rename, each of which is a complete snapshot of every live session and its tokens that nothing ever opened again.
- the example catches up with the identities its own cache promises, on the doors a divergence was measured through: the bun user lookup compares on the binary collation (`LOWER(username) = (? COLLATE utf8mb4_bin)`), because the column's accent-insensitive default (`'café' = 'cafe'` is true under `utf8mb4_0900_ai_ci`) admitted spellings the cache keys and the invalidation listeners — which fold with `NormalizedUsername` alone — could never address, so a deleted user kept authenticating from the ttl-less cache under the collation-only spelling; `UserUpdatedEvent` carries the username the row held before the update and the listener drops both spellings, since a rename left the entry behind under the old one; caller-supplied identifiers are answered as absent by every finder when the cache-key grammar refuses them (a space, a newline, over 255 bytes) instead of surfacing the backend's refusal as a 500 on a read, and the write doors refuse the same spellings as a 400 that names the field —
  a product id with an interior space used to land in the database and then fail every later cache write, with the created row invisible to the ttl-less list forever; the invalidation listeners run every delete and join the failures instead of returning on the first, which used to skip the list entry behind it; the login door no longer concatenates the failure's internals into the client response; the password doors refuse the bcrypt 72-byte ceiling as a 400 instead of a 500, a role carrying a comma is refused before the comma-joined storage would split it into roles nobody granted on the next read, and the embedded-env build embeds the committed `.env` alone — the `.env*` glob also baked the gitignored `.env.local`, the machine-local file that holds real credentials precisely because it never enters git, into the shipped binary, where the loader's precedence let it override the committed configuration.
- http: a multipart upload past `MaxRequestBodyBytes` is answered `413`, the status its urlencoded and json siblings already carry.
- http/cors: cross-origin headers reach a streamed response and a rate-limited preflight.
- example: the login flow rotates the session id before writing the authenticated identity, the framework's own defence against session fixation (`http.RegenerateRequestSession`) that the showcase demonstrated unused — the identity was written onto the pre-login session, so an id an attacker seeded and planted in the victim's browser stayed authenticated as the victim.
- container: a scoped parent writes no edge into the container's dependency graph.
- application: the embedded-static boot guard reads through the interface, so a typed-nil `fs.FS` handed to `NewApplication` under `melody_static_embedded` is refused by the panic that names the argument instead of passing the plain nil comparison and dying later as an anonymous nil dereference inside `fs.Stat` — the exact hazard the environment sibling's guard already documented and closed
- application: two closes racing each other report the one teardown failure once.
- documentation: the logging document states the file journal's rotation constraint — the default destination is a file, its descriptor is opened exactly once for the life of the process, no reopen door or rotation signal exists, so rename-based rotation moves the journal out from under the process silently (records landing in the rotated file or an unlinked inode) and `copytruncate` is the rotation mode that works with the `O_APPEND` descriptor
- documentation: the serializer document states what an untyped deserialization target receives — every JSON number as `float64`, an integer beyond 2^53 silently altered, arrays as `[]any` — where typed struct targets decode exactly; the config and bag documents state that the string grammar `Float`/`Float64` reads is Go's full `ParseFloat` (underscore spellings, hexadecimal floats, exponents), wider than the strict base-10 grammar `Int` reads, replacing the config document's claim that the three accessors read one grammar; the bag document states that `Get` hands back the stored value live — a `[]string` mutated through it writes into the bag behind its lock — with `All`/`StringSlice` as the copying doors; and the validation document states that an integer bound above 2^53 is compared in `float64` against a float-typed field, so a value within one ULP of such a bound can be misjudged against the declared number
- cache: `NewDefaultRememberOption`'s GoDoc states the pinned-leader hazard the cache document already carried — under exactly the defaults (non-cancelable, unbounded wait) a callback that never returns pins its key's single-flight entry and every later caller for the life of the process, with `WithWaitTimeout`, `WithCancelable` and `WithContext` as the opt-out doors
- http/static: the entity tag reads the modification time at nanosecond resolution rather than whole seconds.
- documentation: two security-relevant surfaces gain the caveat their behaviour always had.
- http, security: the kernel refuses a request path that folds to a different spelling, before it is routed or authorized.
- documentation: three claims of the http document catch up with the code.
- documentation: the config document states the bool grammar the typed accessor reads — the string spellings, the whitespace trim, and the refusal of the empty string — which until now was written only in the cron integration's readme; and the container quick-start stops passing `WithTypeRegistration(true)`, a restatement of the default that taught the option must be passed by an example that never resolves by type
- documentation: the application document's logging usage example compiles — `LevelLabels` maps to `LevelLabel` values built through `LevelLabelFromInt`, and the example handed it bare string literals the type cannot hold
- session: the tombstone's boundary is stated where the guarantee is promised — a session another request deleted cannot be saved again *within one process*: the record lives in the manager's own memory, not in the storage it guards, so a shared storage does not carry it between instances and a logout served by one node does not stop a peer instance's in-flight request from writing the entry back.
- example: the login entry point and the access denied handler read the client's preference through `melodyhttp.PrefersHtml`, which is what the rest of the example already used.
- example: `CacheKeyUserByUsername` folds the username itself instead of trusting its callers to have folded it.
- example: an administrator may not modify or delete a peer, and both doors ask the same question.
- example: the seeded password digest is compared in constant time with `crypto/subtle.ConstantTimeCompare` rather than with `!=`.
- example: `/health` stamps its answer with the injected clock rather than with `time.Now`.
- example: the README's structure overview lists `assets/`, the frontend bundle source that produces `public/assets/app.js` and is tracked in git, and no longer describes `repository/` as carrying only in-memory implementations or `service/` as carrying four of its six services.
- application: a shutdown no longer reports a clean stop it did not obtain.
- event: a dispatch builds no debug record the journal would discard.
- config: the refusal of an empty `MELODY_ENV` names the key, the parameter and the files the emptiness already selected.
- config: `Parameter.Bool` reads through the shared parser its sibling accessors use, so a refusal carries the cause that names the parameter, the target type and the value.
- validation: `NewMinLength` and `NewMaxLength` refuse a negative bound, which the tag door beside them has always refused with the reason written in a comment.
- example: `/health` answers a monitoring probe in all three examples, where v1 and v2 left it to the `ROLE_USER` catch-all and v3 alone had made it public.
- example: `/index.html` carries the same public policy as the `/` it serves.
- example: the README of every major describes the cron wiring that exists.
- example: `url/url_generator.go` is deleted from all three examples.
- http: the rate-limit middleware and the rate-limit request listener honour the already-logged mark before writing their record, the way the exception listener and the five sites in the http kernel already do.
- cache: a `Remember` waiter can be given the request's context and leaves when the request does.
- session: `NewFileStorageFromFile` refuses a handle opened with `O_APPEND`, and the in-place write names offset zero rather than seeking to it.
- debug: `debug:router --format=json` survives a cyclic route attribute.
- debug: `debug:container --format=json` keeps `errorContextJson` parseable when a context value refuses `json.Marshal`.
- debug: `debug:events` declares the serving-process listeners on the branch that cannot inspect the dispatcher, in both formats.
- security: an `AccessDeniedHandler` that fails leaves a record of its own failure.
- security: `RoleHierarchyVoter` no longer costs a delegate its token type.
- security: the access control listener judges a typed-nil decision manager as the nil it is instead of comparing it against nil.
- security: a dispatcher that cannot mark the access control listener required is named on the emergency channel instead of disarming the fail-closed guarantee in silence.
- http: an error of rule WIRING returned by a handler is filed at error, not at warning.
- exception: the readers of the cause chain understand a joined error.
- container: a resolution performed after the teardown finished is refused instead of being answered out of the maps.
- container: the panic a service's `Close` raises is recorded with its cause and its stack.
- application: the normal return of `Run` tears the container down under the same ten-second shield the panic path uses, and exits non-zero when it has to abandon it.
- example: the shared icons every page links — `favicon.ico`, `assets/favicon.svg`, `assets/logo.png`, `assets/apple-touch-icon.png` — are produced by the same `npm run build` that produces the frontend bundle, so the one command a fresh clone needs for the browser interface delivers everything a browser asks for.
- example: the comment in `.env` no longer claims that an already-set process or host environment variable overrides the value beside it.
- container: the teardown closes a service before what it resolved through a resolver it kept, not only before what it resolved during construction.
- container: a resolution with nothing to write takes the container's read lock instead of its exclusive one — no scope layered over it, no dependency edge to record, and the instance already built.
- session: burying a tombstone prunes only the burials that have lapsed, walking them in the order they happened, instead of sweeping the whole record on every call.
- session: `SaveSession` and `DeleteSession` hold a lock keyed to the session id across the storage call rather than one lock for the whole manager.
- session: the in-memory storage sweeps expired sessions in chunks, releasing the lock between them, the way the cache backend's sweep already did.
- http/middleware: the sliding-window rate limiter trims the expired marks by index instead of rebuilding the whole window on every call.
- http/middleware: the compression middleware takes its gzip writers from a pool per compression level instead of building one per response.
- cache: a `RememberOption` that asks for both no coalescing and no wait — `NewDefaultRememberOption().WithStampedeProtectionEnabled(false).WithWaitTimeout(0)` — is now honoured instead of collapsing to the constructor defaults.
- security: a failing token source that returns a sub-500 `HttpException` — an expired jwt, a malformed `Authorization`, a bad signature — is recorded at warning rather than error.
- security: a substituted `AccessDecisionManager` no longer loses the role hierarchy in silence.
- security: `NewRoleHierarchyVoter` takes any `Voter` as its delegate, not only the built-in `*RoleVoter`.
- application: `serializer.ServiceSerializerManager`, `validation.ServiceValidator` and `http.ServiceUrlGenerator` are registered behind the `Has` gate the logger, the cache, the session and the firewall manager already had.
- application: `serializer.ServiceSerializer` is registered, so the two published resolvers answer.
- cache: the in-memory backend judges a `Decrement` in the order the shared contract fixes — the closed backend first, then the key, and the magnitude of the delta last.
- logging: the json logger reports its own failed write, once, on stderr.
- config: a failed parameter resolution names the environment key beside the internal alias.
- http: the public write door refuses a status outside net/http's writable range before it touches the writer, headers included.
- http: the exception listener renders the error text and its cause under the containment its siblings received.
- session: the `NewFileStorageFromFile` writer writes before it truncates, and truncates to the length it just wrote.
- cron: `melody:cron:generate --prune` empties the destinations in `dir(--out)` that an earlier version wrote and this run no longer produces.
- debug: `debug:container --build` reports its failures on the envelope, so it exits non-zero the way the single-service door always has.
- debug: two fields of one `debug:container` item stop changing their json TYPE with the value of the row.
- debug: `debug:events --format=json` declares the serving-process listeners at every verbosity, on `data.servingProcessListeners`, beside the listing rather than instead of it.
- debug: `--order=desc` reaches both halves of the `debug:events` document.
- debug: `debug:router --format=json` produces its document when a route carries an attribute the encoder cannot represent.
- debug: every `debug:middleware` item carries `reason`, empty where there is nothing to say.
- serializer: a header refusing one registered type is no longer answered `406 Not Acceptable` while another registered type was never refused.
- application documentation: `NewApplication` is documented on its real signature, `NewApplication(embeddedEnvFiles, embeddedPublicFiles)`.
- http: the wiring panics of the request setup are answered instead of resetting the connection.
- security: a 403 names the branch that produced it.
- http: a handler failure that carries no melody error files one record, not two.
- http: `ReadFrom` on the recording response writer records the commit only after the copy, and only when a byte actually reached the delegate — the convention `WriteHeader` and `Write` already follow in the same type, and the door `io.Copy` takes for every streamed body.
- http: the terminate event and the access log report the status a self-committed stream actually carries: the recording writer now records the committed status code, and `writeResponse` reports it for the response the kernel substituted — the journal recorded 204 for every streamed 200 and a rendered-but-never-written 500 for a panic mid-stream, so status-distribution queries over the access log were wrong for every streaming route.
- http: the kernel's handler-error writers — `controller handler error` and `not found handler error` — read the already-logged mark before filing, record a deliberate 4xx at warning and everything else at error, and mark what they filed, the discipline the panic recovery and the exception listener already share.
- http: a handler that honours its context and returns the request context's own cancellation is answered in the journal as `request cancelled by client` at warning — it was recorded as `controller handler error` at error plus a second error record from the exception listener, indistinguishable from a genuine handler fault, so error-rate alerting fired on the client's own disconnects
- http: a response-write failure the client caused — the broken-pipe family, the cancelled request context — is recorded at warning under `failed to write response; client disconnected`, and both classes of the record now carry the method and path: at error every impatient download paged the operator, in a record that could not even name the route it happened on
- http: `WriteToHttpResponseWriter` refuses a status outside net/http's `[100, 999]` by name through its own error return — it is a public door, and the delegate's panic turned an external caller's arithmetic mistake into a connection reset where the signature promises an error
- http: `Kernel.SetForwardedHeadersPolicy` and `NewForwardedClientIpResolver` copy the trusted proxy list instead of retaining the caller's slice.
- http: `CompressionMiddleware` reads a nil configuration as the default one, the way the cors middleware and the route group read their absent options, and normalizes an out-of-range level or minimum on a private copy instead of writing back into the caller's object — it used to panic on the nil dereference and to silently rewrite the caller's own configuration at construction.
- http: `static.NewFileServer` refuses nil options by name instead of panicking on the dereference — refusal rather than a default, because the default here would be a live file server over the `public` directory, and a nil that is almost always a wiring mistake must not start serving files nobody asked served
- http: the static file server's two ordinary-control-flow exits — the non-retrieval method and the out-of-prefix path — are recorded at debug instead of info: with the middleware registered globally they fired once per api request, exactly the per-request noise the package's own logging comment keeps out of the journal.
- http: `NewHttpMiddlewareDefinition` copies its four constraint lists and `MiddlewareBuildReport.SetInactive` copies like every sibling accessor of the report — a registrant reusing a slice silently rewrote the ordering constraints the pipeline was registered under, and the one non-copying setter let a caller rewrite a report the diagnostics already held.
- http: the rate-limit middleware records a limiter call the caller's own cancellation ended at warning under `rate limiter call cancelled` — at error it read as a store outage against a perfectly healthy store, once per disconnect on a rate-limited route
- httpclient: `RequestOptions.Headers` and `Query` hand out copies.
- exception: `LogContext` and the cause-chain rendering produce an error's text under a recover, the containment the container teardown's close-error rendering already applies.
- http: `RecoverToError` normalizes a typed-nil error to the generic branch the way the exit handler's resolver normalizes it, the kernel's debug-mode message renders through the same containment, a discarded response body whose `Close` panics inside the recovery defer is contained into the error the caller already reports, and an application serializer that panics on the error payload degrades to the json fallback under the renderer's own "an error response always exists" — each of these second-order failures used to escape `ServeHttp` and reset the connection, measured
- logging: the exit handler's resolve step runs under its own shield, honouring the claim of the comment beside the other steps — a recovered value whose `Error()` panicked unwound into `main` and the process died with the Go runtime's exit code 2: no record, no certificate, no stderr echo, no before-exit teardown, measured.
- security: each of the three direct 401 refusals of the access-control listener files one warning naming its reason — `missing_token`, `token_not_authenticated`, or `missing_security_context`, the last a wiring fault rather than a client mistake.
- cache: `SetMultiple` validates its batch over sorted keys on the in-memory backend, so a batch carrying two malformed keys names the same culprit on every call instead of one chosen by map iteration — the rule the redis backend's batch reporting already follows
- event: a subscriber installation on `EventDispatcherAdapter` is one critical section against its twin and its removal, the guard the concrete dispatcher's `subscriberMutex` already carries: without the outer section two concurrent `AddSubscriber` calls for one identity both passed the duplicate refusal and installed every listener twice — measured under four-way contention — and a `RemoveSubscriber` interleaved with an installation removed the half already installed while the rest kept arriving under a record the remover was just told is gone
- event: the adapter's `RegisteredEvents` breaks equal-priority ties by the wrapped dispatcher's listener id — the tiebreak dispatch actually uses — instead of an adapter-side counter issued under a different lock.
- http: a response status code outside net/http's `[100, 999]` answers the rendered 500 instead of an implicit empty 200.
- http: `PrefersHtml` splits Accept members and parameters outside quoted sections through `internal.SplitOutsideQuotes`, the serializer reader's grammar — the third layer of the one-grammar rule after the joined field lines and the shared q-value reader.
- http: the compression middleware joins every line of a repeated `Accept-Encoding` field before parsing, the way both Accept readers join theirs — reading only the first line dropped a coding the client named on the second — splits members outside quoted sections through the same shared grammar, and resolves a repeated coding to its higher q, the tie rule of every Accept reader: last-wins made `gzip;q=0.5, gzip;q=0` and its reversal answer differently for one statement
- http: the compression peek buffer grows with the bytes actually read instead of being allocated at the full `MinSize` upfront: the threshold has no upper bound, so an oversized — or unit-confused — minimum turned every eligible response into an allocation of that size before a single byte arrived, when a response below the threshold needs no more memory than its own length
- serializer: a full negotiation tie — equal quality and equal specificity — resolves through the json-first convention `defaultSerializer` states for the empty header.
- cache: `Remember` answers one shape for one key — the computing call now returns the value passed through the manager's serializer round-trip (one local encode+decode, no backend involved), the exact shape every cached call answers.
- cache: the in-memory counter accepts exactly the redis integer grammar — no whitespace padding, no plus sign, no leading zeros, no minus zero — where a trimmed `ParseInt` adopted them all: the same payload incremented through this backend and errored through redis, measured live, while the refusal's own comment claimed redis parity
- cache: a nil payload is stored as the empty payload and reads back as an empty non-nil slice through both backends, and the rule is written on the `Backend` contract: redis has no nil to store, so the in-memory backend preserving the distinction let a caller tell the implementations apart by reading back what it wrote — measured live
- session: the response path decides a session's fate from one atomic `Snapshot` — values, modified flag and cleared flag read under a single lock acquisition, used by `Manager.SaveSession` and by `writeResponse`'s branch decision alike.
- config: `Resolve` walks the parameters in sorted name order, so a boot with several broken templates fails on the same parameter every time — the dotenv reader's own rule, stated there for exactly this reason; the fixpoint itself was always order-independent, but the failure's identity was chosen by a random map walk
- debug: `debug:router` orders tied rows by registration order and `debug:middleware` orders same-name inactive entries by reason, making both comparators total: under an unstable sort, routes sharing pattern and methods — distinct at dispatch by host, priority or requirements — and same-name inactive entries flipped their rows from run to run, in the commands that exist to show which of them answers
- httpclient: header maps are canonicalized at the door and two spellings that collapse onto one header are refused by name — the serializer's convention for its mime keys.
- cache: a `With` setter called on the exact zero-value `RememberOption` first reads the receiver as the constructor defaults, then applies its own field.
- container: `MustGet` and `MustGetByType` panic with the failure the way `FromResolver` and `FromResolverByType` return it — a melody error travels out whole with the service name or type written into its context in place, and only a foreign error is wrapped naming it.
- container: the wrapper a coalesced waiter receives for a failed creation inherits the already-logged mark of the failure it carries.
- event: the wrapper the dispatcher returns for a failing listener inherits the already-logged mark of the listener's error, the reading the panic half has always applied to a marked panic value; the kernel comment that promises the mark is visible "on anything wrapping a marked error" is now true on the error path too
- event: a listener that fails while also stopping propagation with a required listener behind it has its failure travel as the cause of the stop's refusal, through the new `NewRequiredListenerSkippedErrorWithStoppedListenerFailure`.
- http: `UserRateLimit` and `UserRateLimitWithResolver` refuse a nil user-id callback at construction with a named panic, the way the middleware constructor refuses its missing limiter.
- http: `PrefersHtml` reads the Accept header under the serializer's rules — every line of a repeated field joined before parsing, and a member whose q parameter falls outside the RFC 7231 qvalue grammar dropped whole.
- http: `acceptsGzip` drops an Accept-Encoding entry whose q parameter falls outside the qvalue grammar instead of scoring it with a bare float parse, under which `q=Inf` switched the compression on and `q=NaN` switched it off
- http: the static file server honours an explicit cache max age of zero as `Cache-Control: public, max-age=0` — always revalidate, with the ETag and Last-Modified machinery intact — because the configuration door validates zero as a distinct choice; only a negative value reads as unset and takes the 3600 default.
- http: `cors.NewService` reads a nil method or header list as the one default `DefaultService` grants — Authorization included — and keeps an empty list as the expressed preference, the reading the origins field always had.
- session: `NewManager` and its siblings refuse a positive ttl shorter than one second, the refusal the configuration door has always given for `MELODY_HTTP_SESSION_TTL`: below one second the value is not a short session but a broken one — the storage purges every lapsed entry on the write that stores the new one, so `SaveSession` reported success and persisted nothing.
- bag: the zero-value `ParameterBag` accepts its first write — `Set` and `AppendString` allocate the nil map the way `exception.Error.SetContextValue` always has — instead of half-working: the reads answered the zero value while the first write died on a raw nil-map assignment
- config: the comment at the session-ttl default registration states the default that exists — zero, no expiry, with the boot warning as the compensation — instead of asserting the bounded lifetime the constant it registers contradicts
- event: a subscriber installation and removal are each one critical section against their twins.
- cache: the refusal order joins the shared backend contract — the closed answer wins over the key judgment, and a batch write judges the ttl before its keys, the redis backend's order.
- http: a rate-limit handler that produces neither response nor error still refuses the request through the middleware door, answered 429 exactly as the listener door answers it.
- http: `RateLimitMiddleware(nil)` is refused with the named panic its listener twin gives for the same wiring mistake, instead of an invalid-memory-address panic at boot
- http: `pipeline.Builder.Describe` mirrors `Build`'s nil-factory refusal, so `debug:middleware` no longer reports as healthy a pipeline the serving boot refuses; the description's promise — the same refusals, no factory invoked — now covers all three of them
- httpclient: the streaming path judges an invalid explicit response-body cap before anything is dialled, the rule the buffered path always held.
- container: `FromResolverByType` dresses its failure the way `FromResolver` does — a melody error travels out whole with the service type written into its context, a foreign error is wrapped naming the type — instead of returning the raw error with nothing to say which resolution failed
- http: `RouteGroup.HandleWithOptions` reads nil options as the default options — an unnamed route answering every method, still carrying the group's prefix, requirements and defaults — the answer the router's own door always gave for the same input and the reading the Symfony model it mirrors gives to absence.
- container: a scope override is answered by the type-keyed resolutions of every type its name is registered under, exactly as the container-level override propagates.
- container: the override assignability guard judges the value the way the readers will, on both the container and the scope: raw assignability for an interface registration, whose stored value is asserted against the interface at resolution, and canonical identity for a value-typed registration — a string service is registered under `*string` and its own provider's builds sit raw under that key, so a raw string override occupies exactly the slot a built value occupies.
- container: a lazy handle follows the scope of the resolver it was built over instead of memoizing forever.
- application, http: a duplicate route joins the aggregated boot collision report instead of panicking one duplicate per boot attempt.
- application: a fatal failure of the configured logger fails the boot, at the step that owns it.
- application: a relative `MELODY_LOG_PATH` is anchored to the project directory — the rule `ensureRuntimeDirectories` already applies to the logs and cache directories, now through the one shared helper — and the file's parent directory is created before the open.
- application: the final record of a process that dies without a live container logger is written to the destination the configuration names, not only to stderr.
- logging: the exit handler writes one certificate record at emergency level — "process exiting after unrecovered error", with the exit code and the error in its context — through whatever logger it resolved, always.
- http: the kernel exception listener reads the already-logged mark before it writes, the way the kernel's own two writers beside it already read it.
- security: a token-source resolution failure is recorded once, and the record carries the request coordinates.
- logging: the cause keys a caller set survive the record.
- security: the two doors that take a `Runtime` from application code read it, and its scope, through the interface.
- application, cli: an exit code is taken from a wrapped `*ExitError` only when the chain actually carries one.
- application: the kernel is read through the interface at the two doors that ask whether there is anything to tear down.
- application: the logger provider reads the module configuration before it opens the log file.
- application: the module, module-provider and cli-command registration doors refuse a typed nil.
- http: `Request.Input` answers the first value of a repeated key, as `FormValue` beside it and `url.Values.Get` already did.
- http: the static file server refuses a spelling that `path.Clean` folds into the mount root, which is the refusal the branch resolving every other path already carried.
- security: an access-control rule matches every spelling of the path it names.
- security: a typed-nil token reads as the absence it means at the three doors where a token crosses from application code into the framework — the authenticator, the token resolver and the firewall's token source — and at `SecurityContext.IsGranted`, whose constructor already read the firewall beside it that way.
- http: a typed-nil response reads as no response at all.
- http: the kernel reads a nil match result from `Router.Match` as the "no match" the contract permits.
- http: the exception listener reads an `*HttpException` out of the chain through `exception.AsHttpException`, the door the same function used twice already.
- http: `SetCookie` allocates the response header map when the response has none.
- http: the response the middleware chain produced is published to the recovery defer as soon as the chain returns, rather than after the error branch below it.
- exception, logging, http: the already-logged mark is read at the depth it is written, through the single reader `exception.IsAlreadyLogged` that now sits beside the writer `exception.MarkLogged`.
- container: `Scope.RegisterScoped` refuses a closed container, which is the answer the other two registration doors already gave.
- container: `FromResolver` and `FromResolverByType` refuse a nil resolver with an error naming the service instead of dereferencing it.
- logging: `NewDefaultLoggerWithLabels` copies the labels on the way in, which is the convention the json logger and the logging configuration already applied at their own doors and this one did not.
- logging: a typed-nil logging configuration registered under the module name is refused where it is registered.
- exception: `AsHttpException` refuses a typed nil with the plain one, the guard every other public entry of the package already carried.
- exception: `ExitError.ExitCode` and `ExitError.ErrorValue` answer a nil receiver instead of dereferencing it, as `Error` and `Unwrap` already did.
- event: the error a listener panicked with travels as the cause of the dispatcher's wrapper, and the record is written through the renderer that walks the cause chain.
- container: a typed-nil error from a resolver implemented outside the package reads as the success it means, on both the name and the type path.
- clock: `ClockMustFromContainer` and `ClockMustFromResolver` refuse a nil or typed-nil argument by name.
- config: a runtime parameter is named inside the cause of its conversion errors, not only in the outer context.
- internal: the three `float64` refusals of `Int` carry three distinct causes — not finite, not an integral number, outside the int64 range — and the refused scalar value travels in the error context.
- logging: a typed nil no longer panics the error-reporting path anywhere in the package.
- logging: the exit handler shields the two steps between the recovery and the exit.
- logging: `LogOnRecover` logs the exit wrapper that carries no error value as the anomaly it is.
- logging: an error logged by `LogOnRecover` is marked as logged whether or not it panics again.
- logging: the recover helpers capture the stack of the panic still in flight.
- logging: the json logger's marshal fallback keeps the context as text.
- logging: errors nested inside context values render as their messages.
- logging: `jsonLogger.Closed` answers without taking the write lock.
- logging: `NewRequestLogger`'s decorator forwards `Closed()` to the logger it wraps.
- logging: `LoggerFromRuntime` records the resolution it cannot serve.
- httpclient: a `[]byte` request body is copied instead of aliased.
- httpclient: a typed-nil basic credential reads as the nil it means.
- httpclient: the response body cap is validated before the request is sent.
- httpclient: `WithMaxResponseBodyBytes` is enforced on the streaming path.
- httpclient: an uppercase url scheme names an absolute url.
- httpclient: `StreamResponse.Body` answers with a failing reader rather than a nil one after `Close`.
- httpclient: `HttpClient.Close` releases the idle connections the client is holding, and `RequestStreamWithContext` bounds a stream from the outside.
- httpclient: the errors of a failed exchange say which request failed, on the paths that know it.
- exception: the error utilities tolerate the typed nil they exist to describe.
- exception: the mutable error state is locked, because the single-threaded-per-request premise does not survive the sharing the framework itself introduces.
- exception: `MarkLogged` marks at the depth `logging.LogError` reads.
- exception: `LogContext` anchors the cause walk on the top error's own wrap link instead of the nearest `*Error` a deep search found.
- exception: the zero values the constructors cannot reach are safe to touch.
- exception: `IsHttpException` answers whether `AsHttpException` finds a usable exception, so the pair cannot disagree on a typed nil: `Is` answered true while `As` answered nil, and a caller that trusted `Is` and then dereferenced panicked on the disagreement.
- exception: `NewExitError` refuses an exit code outside `[1, 255]`.
- exception: `NewHttpException` and `NewHttpExceptionWithCause` refuse a status code outside `[100, 599]`.
- exception: `ValidationFailed` puts its detail under the `errors` context key — the one the kernel exception listener copies into the json payload — instead of `validationErrors`, a key nothing on the response path reads, so the detail the caller attached reached no one.
- debug: the error-context walk descends into a defined map or slice type whose underlying shape it already handles, so the framework's own `exceptioncontract.Context` nested inside a context no longer rides past all three guards at once.
- debug: `debug:container` reports why a build failed, not only that it did.
- debug: the error context of a failing service is read through the `ContextProvider` contract rather than the concrete `*exception.Error`, so an `HttpException` — or any userland error carrying a context — in the resolution chain contributes its context to the report instead of nothing
- debug: the table-cell truncation stays out of the json document.
- debug: the `debug:container` list summary puts the shown count before the ok/error split, so the split reads as scoped to the window it was computed over: only the windowed services are resolved, and an unqualified `8 ok | 2 error` beside a larger total implied the rest were neither instead of unprobed
- debug: `debug:events` counts distinct subscribers across the dispatcher in its summary.
- debug: `debug:events --format=json --verbose` carries the listener detail — priority, source, owner and the required and may-skip marks that say whether the fail-closed dispatch guarantee is armed.
- debug: `NewMiddlewareCommand` refuses a nil provider at construction and the zero-value command returns a named refusal through the report — every value the provider returns was guarded while the provider itself was not, so a wiring mistake surfaced as a bare nil-function call far from where it was made
- debug: a secret parameter whose value is nil renders as `(empty)` instead of the mask.
- event: the reason a listener failed reaches the log.
- event: a typed-nil error returned by a listener reads as the success it means.
- event: `Dispatch` refuses a typed-nil event with the error it promises.
- event: `AddSubscriber` refuses a typed-nil subscriber before calling into it.
- event: `EventDispatcherAdapter.RemoveListener` scrubs its own bookkeeping whether or not the wrapped dispatcher still held the listener.
- event: `NewEventFromEvent` carries the stopped propagation.
- event: a listener panic whose value reports itself already logged is not logged a second time.
- event: `EventDispatcherAdapter` reports the execution order the dispatch actually uses.
- cli: the finish banner tells the truth about a command that panicked.
- cli: a typed-nil error reads as the success it means.
- cli: the action no longer re-reports the close failure of a container the command closed itself.
- cli: the banners honour `--no-color`.
- cli: a printing failure no longer replaces the failure the envelope carried.
- cli: `output.NewMeta` honours the caller's Go version the way it honours the application and melody versions — the field was the one of the three silently discarded — and `NormalizeOption` clamps a negative verbosity level like its integer siblings
- config: a template referencing a non-string parameter reports the type, never the value — the raw `environmentValue` sat in the error context one line under the comment forbidding exactly that, and a signing key registered as bytes is precisely what a template would reference; the typed accessors additionally withhold the value-quoting strconv cause for a parameter marked secret, keeping it for ordinary parameters whose mistyped pool size deserves the diagnostic
- config: a runtime registration whose template fails to resolve leaves nothing behind.
- config: a runtime parameter is named in its conversion errors.
- config: the serving refusal in `Resolve` is airtight.
- config: the error for a reference `MELODY_ENV` cannot resolve names its window.
- config: `loadRequiredDotEnvFile` is deleted — nothing called it on this major; every load path is optional and the missing-files condition is handled by the boot warning and the http refusal
- application: the embedded-environment guard reads through the interface — a typed-nil `fs.FS` passed the plain nil comparison and died later as an anonymous nil dereference inside `fs.Stat`, instead of the refusal that names the argument
- application: a configuration that fails to build is reported as what it is.
- cache: `Remember` refuses a typed-nil cache through the interface.
- cache: a typed-nil error from third-party code reads as the success it means.
- cache: the panic a `Remember` leader recovers is attributed to the side that panicked.
- cache: a serialization failure names the key it happened under.
- application: the record that explains a dying process is written through a logger that still writes, before the teardown instead of after it.
- application: a container-close failure in cli mode is reported once.
- application: a serve failure racing the shutdown signal is reported instead of discarded.
- application: an http server failure is logged once.
- application: a cli command's name is judged trimmed at registration, because that is the spelling it is dispatched under.
- application: the go run detection matches a whole path segment.
- application: the process-boundary exit handler survives an application assembled without `NewApplication`.
- logging: `LogOnRecoverAndExit` survives an exit error carrying no error value.
- runtime: the nil guards read through the interface.
- container: a scoped service finishing after its scope closed is closed best-effort instead of leaking.
- container: a scope's teardown collapses the name and type filings of one instance into one node before it orders anything.
- container: a scoped dependency the scope already holds is depended on as hard as one this resolution builds.
- container: a provider panicking with a typed-nil error no longer takes the process down.
- container: a provider declared with a concrete error type can succeed.
- container: the owner of a finished creation drops its waiters' wait-graph edges under the lock that wakes them.
- container: an override installed while the provider ran wins the slot.
- container: an override replacing an instance the container itself built no longer leaks it.
- container: a close that both fails and cycles reports the cycle's nodes.
- container: a melody error passes back through `FromResolver` whole, with the service name added to its context in place.
- validation: a shared, non-cyclic pointer is validated under every path that reaches it.
- validation: a custom constraint that returns a typed nil error is a success, not a panic.
- validation: the time-codec verdict settles on one value per type.
- validation: `ValidationError.ToExceptionError` hands the exception a copy of the context.
- validation: a refused rule parameter names its reason.
- http: `BindJson` keeps the real error as the cause.
- session: a session deleted while a request was still running cannot be written back.
- session: `Session.Clear` ends the session and the ending latches.
- session: `NewManager` does not close the storage it was handed; `NewManagerOwningStorage` is the constructor for a caller that built both by hand and wants one `Close` to end both.
- http: a session write lost to a storage outage answers 500 instead of the response the handler produced.
- session: `Manager.SaveSession` refuses a typed nil session instead of dereferencing it.
- session: `Manager.SaveSession` holds the session id to the standard the load and delete paths hold it to.
- session: `NewManager` refuses a negative ttl.
- session: `Session.All` returns a copy that reaches all the way down, the depth both storages already copy at.
- session: `InMemoryStorage` refuses `Load`, `Save`, `Delete` and `Clear` after `Close`, the way `FileStorage` already did.
- session: the instant a session expires counts as lapsed in `InMemoryStorage`, the boundary `FileStorage` already drew with `now >= ExpiresAt`.
- session: a load of a lapsed entry answers "no such session" even when the housekeeping flush that removes it cannot write.
- http: the kernel tests what a session manager hands back with `IsNilInterface` rather than against `nil`.
- http: `writeResponse` tests both the session and the session manager with `IsNilInterface` before persisting.
- http: a session write dropped because the response was already committed is logged.
- http: the kernel refuses a urlencoded submission whose body could not be read instead of dispatching it as an empty form.
- http: the error handler an application installs runs under the kernel's own panic recovery.
- http: the recovered-panic log record carries the stack of the panic site.
- http: a client that refuses gzip with an uppercase `Q=0` is not sent a gzip-encoded body.
- http: the static file server consults its excluded-path list against the index file the mount root resolves to.
- http: `BindJson` binds a body under a request-body ceiling at the top of the int64 range.
- http: the rate limiters' idle prune holds its threshold for a window past the midpoint of the duration range.
- container: `HasType` canonicalises the type it is asked about, so it can no longer answer "no" for a service `GetByType` resolves happily.
- container: a provider that panics no longer leaves the resolution around it blind to its scope.
- http: a scope-close failure is reported through the emergency logger when the request never got a logger of its own, instead of being dropped.
- container: a request scope closes the services it built.
- internal: the deep copy is linear in distinct nodes instead of exponential in depth on shared substructure.
- debug: an error context that refers to itself is rendered with the loop marked instead of exhausting the stack.
- http: a handler that returns no response reaches the `kernel.response` event like every other outcome.
- container: a service whose creation read an entry out of a request scope is kept in that scope and dies with it, instead of being stored in the root container.
- container: a service that cannot be created reports its own failure.
- config: reading a parameter no longer races the resolution that rewrites it.
- config: a `${...}` reference in an `.env` file resolves against every file the environment loaded, and an undefined, self- or mutually-referencing one fails the boot.
- http: the compression middleware emits `Vary: Accept-Encoding` on every path, including the one taken when the response already carries a `Content-Encoding` and the excluded-path and excluded-content-type paths.
- http: a middleware may only reference, through `before` or `after`, a middleware enabled in at least every environment it is itself enabled in.
- http: `NewForwardedClientIpResolver` reads a bracketed IPv6 literal that carries no port, so a trusted edge emitting `[2001:db8::1]` no longer collapses every IPv6 client onto the proxy's own rate-limit budget while IPv4 clients keep theirs.
- http: the compression middleware bounds the read it uses to decide whether a body reaches the compression threshold.
- http: the static file server no longer trims whitespace off the path it resolves, so `/%20internal/secret.json` no longer retrieves `internal/secret.json`.
- http: the static file server refuses a path that is not already canonical instead of serving the file `path.Clean` folds it onto.
- http: the static file server answers `GET` and `HEAD` only.
- http: the static file server ignores `If-Modified-Since` when the request also carries `If-None-Match`, as RFC 9110 requires.
- http: the static file server accepts all three HTTP date formats for `If-Modified-Since` (RFC 9110 requires accepting the obsolete RFC 850 and asctime forms, not only IMF-fixdate)
- http: the static file server records a refused resolution on the streaming path.
- http: a route whose only segment is a trailing optional parameter is served at the root again.
- http: a `RouteGroup` no longer writes its name prefix and its merged requirements and defaults back into the caller's `RouteOptions`.
- http: a route declared with an empty method no longer contributes an empty token to the `Allow` header of a `405` response; an empty element is not a valid method token (RFC 9110)
- validation: a field promoted through stacked embed levels below a diamond is validated again.
- http: equal-priority middlewares now run in the order the application registered them.
- validation: the parsed validate tag and the constraint a parameterized rule resolves to are memoized instead of being rebuilt for every value the validator reaches.
- debug: `debug:router`, `debug:events`, `debug:parameters` and `debug:middleware` apply `--limit` and `--offset` to the items they render instead of only reporting them in the payload.
- http: the kernel loads the session inside its panic-recovery guard, so a session-storage outage answers with a logged `500` instead of escaping `ServeHttp`.
- http: `Kernel.SetSessionCookiePolicy` no longer drops the `SameSite=Lax` default when the caller names only `Path` or `Domain`.
- http: a session is no longer stored for a response that is discarded because the handler already committed its headers — the `Set-Cookie` could never reach the client, so a first-time visitor on a streamed response left one unreachable session behind per reconnect.
- validation: the parsed-tag and constructed-constraint memos settle on a single instance under a concurrent first touch, so every goroutine validates against the same constraint.
- cli: when a command fails and the container or scope also fails to close, the aggregated error carries the exit code itself, so the shutdown failures it reports survive to the log instead of being skipped in favour of the command's own exit error.
- http: `UrlGenerator.GeneratePath` substitutes a non-empty route default for a parameter supplied with an *empty* value, not only for an absent one.
- validation: a struct whose promoted json codec is `time.Time`'s is no longer walked for constraints.
- config: registering a runtime parameter after boot no longer writes back onto a referenced parameter's resolved value, so a consumer holding the parameter pointer and reading it without a lock no longer races the write — the referenced value is stored only during boot, and the post-boot re-resolution is deterministic anyway
- container: two distinct zero-size services (`struct{}` pointers, which share one address), or a service pointer that aliases another service's first field, are each closed on teardown instead of being collapsed onto one representative by address alone and one of them silently skipped — the teardown identity now pairs the address with the concrete pointer type
- cli: a command that returns an exit-coded error now exits through the application's own shutdown and logging path (structured error log, container close) instead of the cli library calling `os.Exit` from inside its run and bypassing them
- config: a parameter registered between construction and boot keeps its template for the boot pass instead of being resolved on the spot.
- http: a conditional request for a static file is answered `304 Not Modified` when the `If-None-Match` header carries the entity tag anywhere in its comma-separated list, or carries it in the weak form a proxy may have rewritten it into.
- http: the access log records the scheme the kernel actually resolved through the configured forwarded-headers policy instead of re-detecting it without that policy.
- http: a handler that panics with `net/http`'s `ErrAbortHandler` now aborts the connection silently, as that sentinel documents, instead of being converted into a 500 response plus an error log line — a reverse proxy raises it on every client that disconnects mid-stream
- logging: the json logger takes the same lock for closing its writer that it takes for writing to it, and drops later writes instead of handing them to a closed writer.
- serializer: accept-header negotiation gives each available media type the quality of the most specific range covering it, so an exact range wins over a wildcard whatever the header order, and a range carrying `q=0` refuses that type instead of being ignored.
- bag: a key present with a nil value reports as unset from `String`, `StringSlice`, `StringStrict` and `StringSliceStrict`, matching what `Int`, `Bool`, `Float64` and `Duration` already reported for the same state — `Has` and the typed accessors no longer contradict each other.
- cache: the single-flight waiter counter uses the atomic wrapper type rather than a bare 64-bit field, so its alignment no longer depends on where it happens to sit in the struct; on a 32-bit build a bare field that loses its alignment panics on every counted call
- event: two distinct subscribers whose types carry no fields no longer collapse onto one registration, so removing either one leaves the other's listeners registered instead of silently unregistering both.
- http: a request path that differs from a route only by leading or trailing whitespace no longer reaches that route's handler.
- http: an empty path segment no longer satisfies a named route parameter, so `/users//profile` no longer matches `/users/:id/profile` and binds an empty identifier a handler cannot distinguish from a supplied one.
- http: the compression middleware no longer panics on a response whose header map is nil, matching the guard the cors middleware already carries.
- logging: `EnsureLogger` replaces a typed nil logger with the no-op logger instead of returning it unchanged and letting the first call panic — the case the function exists to guard
- container: two distinct services of one type that carries no fields are each closed at teardown instead of one of them being silently skipped.
- cache: the in-memory backend no longer holds its exclusive lock across the whole map.
- example: the route manifest is escaped for the javascript string literal it is spliced into, so a route name or pattern containing a backslash or an apostrophe no longer breaks every page's scripting (the escaping v3 already carried); the firewall session login handler stores the token roles alongside the user identifier, and the logout handler clears them, so a session written by that handler resolves back to an authenticated token rather than an anonymous one; the embedded static build embeds dot-prefixed and underscore-prefixed paths (`all:public`), so it serves the same file set as the filesystem build
- cors: an `OPTIONS` request carrying an `Origin` but no `Access-Control-Request-Method` is no longer treated as a preflight.
- cors: `Vary: Origin` is emitted on every response, not only on one whose origin was allowed.
- cors: `NewService` no longer panics on a credentialed configuration that decides origins through `AllowOriginFunc`.
- example: the in-memory repositories guard their slice with a read-write mutex, and `All` hands back a copy of it.
- example: the api error presenter emits the raw error message, the concrete Go type and the unwrap chain only when the kernel environment is the development one, the same gate the framework exception listener applies, and stays closed when that environment cannot be resolved at all.
- documentation: `CACHE.md` records that a cancelable `Remember` call abandoned by all its waiters is not inherited by a late joiner, which the package has always done and only one major documented.
- tooling: a validation lane whose script is missing or not executable fails the run instead of reporting success.
- tooling: the race lane covers `session`, `http` and `internal` in every major, in `.dev/validate/all.sh` and in the continuous integration job alike.
- tooling: two behaviours nothing asserted are covered.
- serializer: `NewSerializerManager` refuses a typed-nil serializer instance alongside the untyped one.
- serializer: a typed-nil pointer target no longer panics `PlainTextSerializer.Deserialize`.
- serializer: `PlainTextSerializer` owns its bytes on both sides.
- serializer: the soft runtime resolvers no longer panic inside their own reporting branch.
- serializer: a comma inside a quoted parameter value stays inside its member.
- http: the result handler reads every line of a repeated `Accept` header, joined in order, where `Header.Get` answers only the first.
- http: the response the middleware chain had in flight is closed when an outer middleware panics after its `next()` returned.
- http: `NormalizeResultToResponse` asserts against the `httpcontract.Response` contract, the same question the controller registration door asks, with the typed nil read through the interface.
- http: `Router.Match` hands out a deep copy of the winning route's attributes.
- http/static: an embedded file server proves its public directory against the embedded filesystem at construction and refuses by name when it is absent.
- config: a dollar in an `.env` value opens a reference only where godotenv says it does — upper case, digits and underscore.
- http: a middleware reference is satisfiable when the registrations sharing the referenced name cover the referrer between them.
- http: the reference-gating check weighs only what the group being built assembles.
- application: the comment on `MiddlewarePriorityStatic` describes where the static file server actually sits.
- http: `PrefersHtml` is covered again for an html type that arrives on its own in mixed case.
- container: a service the container owns stays one instance for the whole process even when it is resolved through a request scope.
- debug: the error-context walk stops at a depth bound instead of descending until the goroutine stack is gone.

### Security

- http, session: a live session id no longer reaches the log verbatim.
- http: a response carrying the session cookie is kept out of a shared cache.
- http: the Accept-family header parsers cut a value into at most a fixed number of members, so an unauthenticated header cannot convert its byte budget straight into live heap.
- httpclient: a per-request credential header no longer follows a cross-origin redirect on the streaming path.
- httpclient: a url carried into an error no longer carries the secret in it.
- config: a broken `.env` no longer prints its neighbours' credentials.
- http: the cross-origin allow list treats the port as part of the origin.
- http: an explicitly empty cross-origin allow list refuses every origin instead of becoming the wildcard.
- http: the static file server refuses a path element beginning with a dot, so a `.env` or `.git/config` left in the public directory is no longer served — and, with the shipped cache defaults, no longer stored by a shared cache.
- http: a static file server configured with both a strip prefix and an embedded public directory can no longer serve a file outside that directory.

## [v1.18.1] - 2026-07-24 - Contained Container Teardown Panics and Padded Skip Marker

### Fixed

- validation: a padded skip marker (`validate:" - "`) is trimmed and skips validation instead of being read as an unknown rule that rejects every value
- container: a service `Close()` that panics no longer aborts the process when the value is discarded because the container closed mid-resolution, and no longer aborts the teardown loop — the panic is contained, recorded as a close failure, the remaining services still close and a repeated `Close()` reports the same error
- container: a close failure whose user error carries a panicking `Error()` method is contained as well — the failure text is produced under a recover, so the teardown loop finishes and a repeated `Close()` reports the same error instead of a silent success

## [v1.18.0] - 2026-07-23 - Secret Parameters, Positional Config Resolution and Dominance-Aware Validation

### Added

- `config/configuration.go`, `config/configuration_resolve.go` — the `default` processor makes an environment key optional inside a template. `%env(default::AWS_ENDPOINT_URL)%` resolves to the empty string when the key is undefined and `%env(default:aws.default_endpoint:AWS_ENDPOINT_URL)%` falls back to another parameter. A key defined in the .env artifacts always wins over the fallback, so an environment that needs the real endpoint sets it and every other one boots on the default instead of having to declare a key it does not use. The processor is opt-in: a plain `%env(KEY)%` still fails resolution when the key is undefined, so a parameter holding a credential refuses to boot rather than degrading to an empty string. The fallback is resolved through the parameter branch, inheriting its recursion, its circular-reference guard and its undefined-key reporting.
- `config/configuration_resolve.go` — a fragment shaped like an environment placeholder that the strict pattern rejects is now reported instead of surviving as literal text. `%env(default:KEY)%` — the default processor written with one colon instead of two — matched nothing and reached the consuming service as the uninterpreted string `%env(default:KEY)%`; resolution now fails and names the offending placeholder. The error context carries the placeholder text when it is spelled in key-grammar characters — it then names an environment key, never a value — and `%env(<redacted>)%` otherwise, since arbitrary pasted text in that position may be a credential.
- `config/parameter.go`, `config/contract/parameter.go` — `Parameter.Duration()` and `Parameter.Float()` complete the accessor set alongside `Bool()` and `Int()`, converting from the native type or from the string an environment value always arrives as. Both report an unset parameter as a conversion error rather than yielding a zero value, and both identify a failure by environment key alone, keeping an inline credential out of the exception cause-context chain.
- `config/configuration.go`, `config/parameter.go`, `debug/command_parameter.go` — a parameter may be declared as holding a credential, through `RegisterSecretParameter` on the module registrar for one the application declares, or `MarkParameterSecret` for one melody registered automatically from the .env artifacts. `debug:parameters` then renders it as `********` — or `(empty)` when it carries no value, which is what an operator runs the command to find out — and reports the marking in a new `secret` column; the length is withheld along with the value, since on a short credential it narrows the search meaningfully. The marking travels with the value: a parameter whose template reads a secret becomes one itself — through a `%parameter%` reference and through `%env(KEY)%` alike, since the environment key is registered as a parameter under its own name and marking it is how a credential read that way is declared — so a dsn assembled from a declared password no longer prints in full
  beside the password that is redacted. The value reaching the services is untouched — this governs display, not storage. `MarkParameterSecret` leaves an absent name alone rather than failing the boot, since an environment key is legitimately undefined in some environments; the marking is retried before the configuration resolves — so it still propagates into the parameters whose templates read the secret — and once more at the end of the boot, where what still matches nothing is warned about, so a misspelled name is a visible signal instead of an unredacted credential. `Parameter.IsSecret` reads an atomic, since a marking may land while a consumer that already holds the parameter asks for it.

### Changed

- `config/configuration_resolve.go`, `config/configuration_validate.go` — the resolution and validation failures raised by a value that contains a literal percent now say how to write it. A generated password such as `pa%ss%word` reads as a reference to a parameter named `ss` and fails the boot; the percent has to be doubled (`pa%%ss%%word`), which both messages now state outright instead of only naming the parameter they could not resolve.
- `config/contract/parameter.go`, `config/contract/configuration.go`, `application/contract/parameter_module.go` — three contracts gained methods, which is a breaking change for a type implementing one outside the framework: `Parameter` gained `IsSecret`, `Float` and `Duration`; `Configuration` gained `RegisterRuntimeSecret` and `MarkSecret`; and `ParameterRegistrar` gained `RegisterSecretParameter` and `MarkParameterSecret`.

### Fixed

- `container/resolver_context.go`, `container/container_close.go`, `container/utility.go` — the resolution and shutdown keys are unique per type identity, not the type's `String()` which two same-named types from different packages share, so a service resolving another such type by type no longer fails the boot with a spurious cycle, their creation guards no longer alias, and the close-order graph no longer collapses them. The type registration's auto-derived service name is import-path-qualified for the same reason, so two same-named types can both be registered by type.
- `container/container_close.go` — `Close` runs the teardown exactly once and a concurrent or repeated call blocks until it finishes and returns the same error, instead of a second caller reading the not-yet-assigned close error and reporting a premature success while services are still being torn down.
- `config/configuration.go` — a parameter registered through `RegisterRuntime`/`RegisterRuntimeSecret` after the boot resolution is resolved against the configuration on registration, so a `%env(...)%` or `%parameter%` template registered late no longer reaches the consuming service verbatim; a pre-resolve registration is still resolved in the boot batch.
- `config/configuration_resolve.go` — `Resolve` holds the write lock the runtime accessors read under, closing the fatal "concurrent map iteration and map write" a post-boot `RegisterRuntime` racing it would otherwise trip.
- `application/application.go` — the cli exit code survives a wrapped exit error: `Run` walks the cause chain with `errors.As` instead of asserting the top type, so a command that returns an exit code together with a shutdown-close failure exits with that code rather than panicking.
- `validation/validator.go` — the validator validates exactly the fields a payload can populate, resolved with encoding/json's dominance rules. A field carrying `json:"-"` is skipped (a field literally named `-` is spelled `json:"-,"` and stays validated); a promoted embedded field shadowed by a shallower field with the same json name is skipped — its tag ran against a permanent zero value and rejected every request; a name claimed ambiguously at equal depth is dropped entirely, as encoding/json drops it; an explicitly json-named field beats an untagged twin at equal depth; a diamond embed annihilates; and the exported fields promoted through an unexported embed are now validated, since a payload does populate them — a payload that omitted such a field satisfied the endpoint before and is rejected now, the one validator change in the stricter direction. A validate tag declared on an exported promoted embed still runs against the embed value, stacked diamond embeds are counted with the
  same cap-at-two encoding/json uses, and a nil pointer embed keeps its promoted names in the dominance while yielding nothing to validate.
- `application/boot_collision.go` — the boot-collision report names the user's registration call site for every collision kind. The origin was read a fixed number of stack frames above the recording, which was one frame short on the parameter path (`RegisterParameter` delegating to `registerParameter`), so a duplicate parameter was reported as registered at the framework's own wrapper line; the origin is now the first frame outside the framework's registration plumbing.
- `config/configuration_resolve.go`, `config/configuration_validate.go` — template resolution is a positional left-to-right scan, the way Symfony resolves its parameters, replacing the escape-then-regex fixed-point passes. Each percent is decided in place — the `%%` escape for one literal percent, an `%env(...)%` placeholder, a `%parameter%` reference (a single-character name included, matching what the default processor's fallback accepts), or data, which is what a lone percent now is — and a referenced value is resolved recursively and spliced in as pure data, never rescanned. This fixes the values that were previously unwritable: an environment value holding a literal percent (`pa%%ss%%word` read through `%env(APP_PASSWORD)%`) and a dsn reading a password parameter whose value holds one both failed the boot with an `undefined parameter key` for `ss`, since the doubled-percent escape ran only on the entry template and substitution handed the injected text back to the pattern scan.
  Adjacent references (`%a%%b%`) resolve instead of the escape pre-pass swallowing the touching percents, and a self-reference — direct or through any chain of parameters and environment keys — is a circular-reference error at resolve time instead of surviving as literal text. A literal `%env(` stays data unless a well-formed placeholder closes it right there — the scan never reaches past a percent for a distant `)%`, which used to let the closer of a different placeholder turn the literal into a boot failure — while a misspelled placeholder that does close (`%env(FOO-BAR)%`) is still reported. The project directory is read as data wherever a template references it, and the kernel log-path validation no longer pattern-matches the resolved path, which after this scan can only carry a percent as data. Anything placeholder-shaped the scan cannot resolve is an error on the spot, so the after-the-fact unresolved-placeholder validation, which had to guess whether a percent was an escaped
  literal or a failure, is gone with nothing left to check.

## [v1.17.0] - 2026-07-17 - Lazy Service Resolution and Signal-Context Force-Exit

### Added

- `container/lazy.go` — `container.Lazy[T](resolver, serviceName)` and `container.LazyByType[T](resolver)` return a `LazyService[T]` handle that defers resolving a service until its first `Get()` and memoizes success — a failed resolution is returned (or panics, for `Get()`) without being memoized and is retried on the next call, mirroring the container resolver — so a component assembled during the boot phase can hold a service whose provider is registered but not yet safe to resolve at that phase, without hand-rolling a `sync.Once` proxy. `Resolve()` is the non-panicking variant.
- `application/signal_context.go` — `NewSignalContext` (ported from v3) returns a context cancelled by the first SIGINT/SIGTERM, giving the application a graceful shutdown window; a second signal while that shutdown is still running prints one line to stderr and forces the process to exit with the conventional 128+signal code (130 for SIGINT, 143 for SIGTERM), so an operator facing a hung shutdown is never reduced to SIGKILL. The returned stop function unregisters the notifications, cancels the context and releases the watcher goroutine, and is safe to call more than once and from concurrent goroutines.

### Fixed

- `application/signal_context.go` — the stop function closes its stop channel before unregistering the signal notifications, so a signal buffered just before the unregistration can no longer force-exit a process that has already stopped cleanly: the watcher's guards key on the stop channel, and closing it first makes any such delivery provably stale. A second signal landing within half a second of the first is absorbed as a duplicate delivery of the same logical shutdown request — a supervisor and a terminal both forwarding one interrupt land within milliseconds of each other — so near-simultaneous duplicates no longer skip the graceful shutdown entirely, while an operator's deliberate second interrupt past that window still forces the exit.
- `container/lazy.go` — `LazyService.Resolve` runs the resolver outside the handle's lock. Held across the resolution, the lock deadlocked any resolver that reached back into the same handle (a provider chain cycling through a lazy handle); a handle built over a live resolver context now surfaces such a cycle as the container's circular-dependency error instead of hanging, while a handle built over the container itself still blocks on the container's own creation wait, since every `container.Get` mints a fresh resolution context — that is a property of the container's resolution and is unchanged here; concurrent first uses may now each run the resolver, the first to store wins, and the container's own memoization makes the duplicates converge for shared services. A resolution yielding nil without an error is likewise no longer memoized as success: `Get` panicked on the poisoned nil forever, despite its documented retry-on-failure promise — both a failure and a nil yield now retry on
  the next call.
- `application/application_new.go`, `application/environment_local.go` — the missing-.env detection covers every file the source loads without a `.env`: `.env.local` and the development-environment pair (`.env.dev`, `.env.dev.local`). A project configured solely through `.env.dev` boots and loads it, but a plain unresolved key was blamed on missing environment files ("no .env or .env.local file was found") that were in fact found and loaded — and the `go run` project-root pinning walked away from a working directory holding only `.env.dev`.
- `application/application.go`, `application/environment_local.go` — a boot that fails to resolve config parameters because no `.env` was found now says so. A compiled binary run from a directory without a `.env` (the executable-directory branch returns it unchanged; `go run` falls back to the working directory when no `go.mod` is found) resolved against an empty environment and failed with an unsuggestive `undefined environment key`; the resolution-failure panic now appends the directory it looked in and the remedy (create a `.env` or `.env.local` file there, or embed with `-tags melody_env_embedded`). An app whose parameters all have defaults still boots without a `.env` — the hint is added only on an actual resolution failure. The detection behind the hint counts only a regular file — a directory named `.env` is not an environment file — and a stat error that cannot prove absence (a permission failure) suppresses the hint instead of misdirecting the operator; the `go run`
  project-root pinning follows the same detection.
- `application/cli.go` — the runtime `--mode`/`--role` flags are recognized only before the cli subcommand. They were matched and stripped anywhere in argv, so a command declaring its own `--role`/`--mode` flag was silently broken: the runtime captured the value (panicking `invalid role` on anything but `web`/`worker`/`all`) and deleted the flag before the command parsed it. Stripping and parsing now stop at the first positional argument (the command name) — `--mode`/`--role` are documented as always preceding the command — so a command's own flags that follow the command name are left intact. The `invalid mode`/`invalid role` panics now name the likely collision. Repeated runtime flags resolve uniformly to the last occurrence — a later explicitly empty one no longer lets an earlier value survive — and an explicitly empty `--mode=` (an unset environment variable expanding to nothing) fails closed with the invalid-mode panic instead of silently booting the default mode, matching
  `--role=`.

## [v1.16.0] - 2026-07-11 - Platform-Ergonomics Back-ports and Cross-Version Correctness & Security Hardening

### Added

- `http/contract/middleware.go` — optional `RuntimeRateLimiter` widening of `RateLimiter` for shared-store limiters: `AllowWithRuntime(runtime, key) (bool, error)` threads the request context to the store and reports store failures, with the returned allowed value already reflecting the limiter's failure policy. `middleware.RateLimitMiddleware` now prefers this method when the configured limiter implements it (logging the store failure and honoring the returned decision); every existing `RateLimiter` takes the unchanged plain path. Back-port from `v3`.
- `config/environment.go`, `config/process_role.go`, `application/cli.go`, `application/service_resolver.go` — process roles for multi-instance deployments that split web serving from background work. A process now declares a role — `web`, `worker` or `all` (the default, byte-for-byte today's behavior) — via the `MELODY_PROCESS_ROLE` parameter in `.env` or the new `--role` runtime flag, the flag winning; the flag exists because melody deliberately never reads the process environment, so a docker-compose deployment differentiates containers built from one image with `command: ["/app", "--role=worker"]` instead of an inert environment variable. Melody itself gates nothing on the role — it is declared intent that composition-root wiring and long-running runners query through `Application.ProcessRole()`, the `KernelConfiguration.ProcessRole()` accessor, or the `ServiceProcessRole` container service, with `config.RoleAllowsBackgroundWork(role)` / `config.RoleAllowsHttp(role)` as the
  standard predicates (previously every app reinvented this gate on `ModeHttp`, which conflates transport with responsibility). Like `--mode`, the `--role` flag never implies cli mode and is stripped before the cli framework parses the arguments. Note for external implementors of `config/contract.KernelConfiguration`: the interface gains `ProcessRole() string`. Back-port from `v3`.
- `http/middleware/client_ip.go` — `NewForwardedClientIpResolver(policy)`: a trusted-proxy-aware `ClientIpResolver` that walks `X-Forwarded-For` right-to-left, skips hops matching the trusted proxy list (exact addresses and CIDR prefixes) and returns the first untrusted address — the client as attested by the trusted edge. It reuses the same `ForwardedHeadersPolicy` the kernel already takes for scheme detection, so one trusted-proxy list drives both, and falls back to `DefaultClientIp` whenever the chain cannot be trusted (untrusted direct peer, unparseable entry, all-trusted chain), so per-IP rate limits behind a reverse proxy key on the real client instead of collapsing onto the proxy address. Back-port from `v3`.

### Changed

- `config/configuration.go`, `application/application.go` — the misplaced-binary foot-gun is now diagnosable: melody derives the project directory from the executable location (the working directory only under `go run`), so `go run .` finds the `.env` artifacts but the same app built elsewhere and run from the same directory does not — and previously failed much later with an unsuggestive "undefined environment key". Boot now warns when zero environment keys were loaded (naming the searched `projectDirectory`) and the resolve failure carries `projectDirectory` in its error context. Log/diagnostic only; the lookup semantics are unchanged. Back-port from `v3`.
- `application/boot_collision.go`, `application/application.go`, `application/application_container.go`, `application/application_cli.go`, `container/errors.go` — duplicate registrations now surface as ONE aggregated report at boot instead of one panic per run. Previously a consolidation that introduced several collisions (duplicate service ids, duplicate service types under the strict default, duplicate parameters, module configurations or cli command names) sent the developer around the fix-one-reboot-hit-the-next loop; the `Application.Register*` surface now records each duplicate (first registration wins for the remainder of the boot) and `Boot()` panics once, after the cli phase, listing every collision with the file:line of the registration that caused it. The container's raw `Register`/`MustRegister` and `Configuration.RegisterRuntime` keep their fail-fast behavior for direct callers, and any non-duplicate registration failure still panics immediately; the duplicate branches in
  `container.Register` now carry `errors.Is`-able causes (`container.ErrServiceIdAlreadyRegistered`, `container.ErrServiceTypeAlreadyRegistered`) with unchanged messages. Back-port from `v3`.
- `application/environment_warning.go`, `application/application.go` — boot now warns for every process environment variable whose name matches a known configuration parameter: melody deliberately reads configuration only from the `.env` artifacts (the application stays a black box), so such a variable is inert — the report's real-world case being an `APP_ROLE: web` set in docker-compose that consumers assumed was read while every container silently ran the outbox dispatcher. The known set is exactly the resolved parameter names, so `PATH`/`HOME` can never match; a variable whose value equals the resolved parameter value is skipped (platforms often mirror `.env` values); values are never logged. Log-only — behavior does not change. Back-port from `v3`.

### Fixed

- `http/router_utility.go` — `isRequestFromTrustedProxy` unmaps an IPv4-mapped IPv6 CIDR in the trusted-proxy list before matching, mirroring `http/middleware/client_ip.go`. A mapped-form entry such as `::ffff:10.0.0.0/104` matched nothing against an unmapped IPv4 peer, so scheme detection distrusted a proxy the rate-limiter trusted, ignored `X-Forwarded-Proto` and dropped `Secure` from cookies set over a genuinely HTTPS request.
- `http/middleware/pipeline/builder.go` — two definitions sharing a name no longer report a cycle. The Kahn traversal emits every duplicate a node carries, so counting the emitted definitions against the name-keyed node map found a mismatch for the very duplicates `allowDuplicates` exists to permit: `Build` returned "middleware pipeline has a cycle" and the application panicked on it. The sentinel now counts the nodes it drained.
- `http/router_utility.go` — `X-Forwarded-Proto` is read as the list it is. A chain of proxies appends rather than replaces, so the header arrives as `https, http`; returning it whole produced a scheme equal to neither `http` nor `https`, and every session cookie the response set silently lost its `Secure` attribute. The client-facing (leftmost) entry is now used.
- `config/environment_source.go` — a quoted `.env` value that spans lines survives preprocessing. The comment stripper tracked quote state per physical line, so from the second line on it believed it was outside quotes: a `#` in the value opened a comment and a blank line was dropped, silently truncating the value godotenv would then parse. A leading UTF-8 byte order mark is stripped too — `U+FEFF` is not whitespace, so nothing trimmed it and godotenv rejected the first key of every file an editor saved that way.
- `http/accept.go` — `PrefersHtml` honours the `Accept` header's quality values. It compared substring positions, so `text/html;q=0, application/json` — a client explicitly refusing HTML — was served HTML, as was any client that merely mentioned `text/html` first while ranking JSON higher.
- `http/middleware/client_ip.go` — IPv4-mapped IPv6 addresses are unmapped before use. A proxy writing the same client as `::ffff:1.2.3.4` keyed a different rate limit bucket than `1.2.3.4`, and an IPv4 CIDR in the trusted proxy list never matched a 4-in-6 peer.
- `application/cli.go` — stripping the runtime flags no longer eats the command's own next argument. `parseRuntimeFlagFromArguments` refuses to read a token starting with `-` as the value of a bare `--role`/`--mode`, but the stripper consumed it unconditionally, so `command --role --verbose` reached the command without `--verbose`.
- `http/kernel.go` — the panic-recovery path now closes the response it discards. When a panic unwinds while a file-backed response (a `FileResponse` or a static `ServeReader`) is already assigned but not yet written — a session backend blowing up inside `writeResponse`, before `WriteToHttpResponseWriter` registers the body's deferred `Close` — the recover handler replaced it with an error response and dropped the only reference to the open file. One descriptor leaked per such request.
- `http/router_utility.go` — a requirement declared on a catch-all wildcard (`/files/*path`) is now enforced when matching. The catch-all branch assigned the joined remainder to the parameter without ever consulting the compiled regex, while the single-segment wildcard and named-parameter branches enforced theirs — a whitelist that silently failed open.
- `http/url_generator.go` — a requirement declared on a catch-all wildcard (`/files/*path...`) is now enforced when generating, as it already is when matching. The catch-all branch emitted the value unchecked while the single-segment branch validated its own, so `GeneratePath` minted urls this very router answers with a 404 — and a traversal like `../../etc/passwd` passed the one whitelist meant to catch it.
- `http/static/utility.go` — the static file server opens the path it validated. It resolved symlinks with `filepath.EvalSymlinks`, confirmed the result stayed under the base directory, and then opened the *unresolved* path, re-following every symlink component at open time: an attacker who swapped a component between the check and the open was served a file from outside the base directory.
- `httpclient/http_client.go` — the redirect policy no longer reads the client's header map from the request goroutine while `SetHeader` writes it. `net/http` runs `CheckRedirect` on whichever goroutine is performing the request, and the policy iterated the very map `SetHeader` mutates under the client mutex: a concurrent setter during an in-flight redirect crashed the process with a fatal, unrecoverable `concurrent map iteration and map write`. The policy is now a method that takes the read lock.
- `httpclient/http_client.go` — credential headers attached to a single request with `WithHeader`/`WithHeaders` are stripped on a cross-origin redirect too. Only the client-wide headers were in the stripped set, so a per-request `X-Api-Key` was still handed to whatever host the first server pointed at — the same leak the policy exists to prevent. The caller's header names now travel to the policy on the request context.
- `httpclient/http_client.go` — `isSameOrigin` compares the *effective* port and folds host case, so `https://host` and `https://HOST:443` are one origin. A redirect that merely spelled out the default port, or changed the host's letter case, was read as leaving the origin and had its credentials stripped, breaking ordinary same-host flows.
- `httpclient/http_client.go` — the client installs a redirect policy that strips every credential it attaches once a redirect leaves the original origin (a different scheme or host). `net/http` strips only `Authorization`, `WWW-Authenticate` and `Cookie`, and only across domains, so a client configured with an api-key header (`X-Api-Key`, `X-Internal-Token`, …) handed that secret to whatever host the first server pointed it at. The ten-redirect cap is preserved.
- `httpclient/http_client.go` — `WithMaxResponseBodyBytes(math.MaxInt)` no longer returns an empty body. `int64(maxResponseBodyBytes)+1` wrapped negative, so `io.LimitReader` read nothing and reported no error; the bound now saturates.
- `httpclient/http_client.go` — `RequestStream` no longer runs under the client's whole-request `Timeout`, which bounds the body read as well and force-closed any stream that outlived it. The header phase stays bounded by the transport (dial, TLS handshake, response header); an explicit per-request timeout is still honored.
- `session/file_storage.go` — expired sessions are swept before every snapshot write. They were removed only when a `Load` happened to name that exact id, so they accumulated forever in memory and on disk — and since every `Save` rewrites the whole snapshot, the write cost grew with everything that had ever expired.
- `config/configuration.go`, `config/configuration_resolve.go`, `config/configuration_validate.go` — a value that escapes a literal percent with `%%` no longer fails boot. After resolution it legitimately reads `%NAME%`, which the post-resolution scan reported as an unresolved placeholder; parameters that used the escape are now exempt from that check.
- `validation/validation_rule.go` — a `]` outside a character class is treated as the literal RE2 reads it as, instead of failing the whole validation tag. A regex constraint containing one made its field emit an invalid-rule-syntax error on every request, regardless of input.
- `http/middleware/pipeline/builder.go` — `allowDuplicates` survives ordering. `selectDefinitions` kept both same-named definitions, then `orderDefinitions` rebuilt them into a map keyed by name and the second silently overwrote the first, so only one ever reached the built pipeline.
- `debug/command_container.go` — `debug:container` no longer panics on a large `--limit`. `startIndex + option.Limit` overflowed `int` and wrapped negative, producing an out-of-range slice bound.
- `debug/command_container.go` — the stack/trace redaction runs before the context is marshalled, so it also covers the two fallback paths. When `json.Marshal` or `json.Unmarshal` failed, the raw context was printed with `%v` — leaking exactly the trace and stack entries the sanitizer exists to strip.
- `exception/utility.go` — `BuildCauseContextChain` emits one entry per link in the cause chain. It read each level with `errors.As` (a deep search that jumps to the nearest `*Error`) while advancing the cursor a single level with `errors.Unwrap`, so a plain wrapper in front of an `*Error` repeated that error's context once per intervening level.
- `cli/output/table_builder.go` — `AddBlock` returns a builder that holds its owner and index rather than a pointer into the `Blocks` slice. A later `AddBlock` reallocated the backing array, so rows added through an earlier builder were written into memory nobody read and vanished from the table.
- `clock/frozen_clock.go` — `frozenTicker.Stop()` no longer closes the channel it handed out. `time.Ticker` (and so `systemTicker`) leaves its channel open forever, so a consumer selecting on a stopped ticker's channel busy-spun on the zero value from the closed one — the same `Ticker` interface with opposite semantics.
- `internal/parse.go` — `bag.Int` range-checks a `float64` before converting. Values outside the `int64` range converted to the indefinite value (`-9223372036854775808`) with no error at all.
- `internal/parse.go` — `bag.Float64` accepts an `int64`, matching `bag.Int` and `bag.Duration`, which already did.
- `http/router.go` — a route parameter requirement is now wrapped in a non-capturing group before it is anchored (`^(?:...)$`). Alternation binds looser than the anchors, so a requirement like `en|de|fr` compiled to `^en|de|fr$` — which the regexp engine reads as `(^en)|(de)|(fr$)` — and matched `aden`, `frfr` and anything ending in `fr`. A requirement meant as a whitelist therefore failed **open**, both when matching an inbound path segment and when validating a value in `UrlGenerator.GeneratePath`, handing the handler a parameter it had been told was already validated. Note for consumers of the route manifest: the normalized requirement it publishes is now `^(?:\d+)$` where it was `^\d+$` — the same language, in Go and in JavaScript `RegExp` alike.
- `http/middleware/compression.go` — a negative `MinSize` is now normalized along with zero. Only `0` was replaced by the 1024-byte default, so a negative value reached `make([]byte, peekSize)` and panicked on every request routed through the middleware.
- `exception/utility.go` — `BuildCauseChain` / `BuildCauseContextChain` clamp the capacity they preallocate. A caller passing a very large `maxDepth` made `make` request that many entries up front, so the allocation panicked with `makeslice: cap out of range` (or ballooned memory) before a single cause was walked; the walk still honors `maxDepth`, it just no longer trusts it as an allocation size.
- `cli/output/table_printer.go` — the table printer measures, pads and wraps every cell by rune count rather than byte length. A multibyte value (a diacritic, any non-ASCII text) counted each UTF-8 byte as a column, so wrapping sliced through a rune into an invalid sequence and padding over-counted its width, throwing every column after it out of alignment.
- `http/accept.go` — `PrefersHtml` takes an `Accept` entry's quality from the most specific range that matches it (RFC 7231 §5.3.2). A wildcard range with a higher `q` (`text/*` or `*/*`) previously outranked an exact `text/html`, so a client that spelled out `text/html;q=0` to refuse HTML was served HTML anyway.
- `http/router_utility.go` — scheme detection unmaps an IPv4-mapped IPv6 peer (`::ffff:a.b.c.d`) before testing it against the trusted-proxy list, so a reverse proxy that presents itself in 4-in-6 form is trusted and its `X-Forwarded-Proto` honoured. Without the unmapping the peer matched no trusted entry, the header was ignored, and every session cookie set behind such a proxy silently lost its `Secure` attribute — the same unmapping the client-IP resolver already does.
- `http/middleware/client_ip.go` — `NewForwardedClientIpResolver` strips a `host:port` suffix from the untrusted `X-Forwarded-For` hop it returns. Proxies such as IIS/ARR and Azure Application Gateway append the client's source port, so the resolver read `1.2.3.4:52122` as unparseable and fell back to the trusted proxy's own address, collapsing every client onto one per-IP rate-limit bucket.
- `http/middleware/client_ip.go` — a trusted-proxy entry written in IPv4-mapped IPv6 form (an exact `::ffff:10.0.0.5`, or a `::ffff:.../104` prefix) again matches its unmapped host. The comparison unmaps both sides, so a dual-stack front proxy configured this way resumes hop-skipping instead of being treated as untrusted.
- `http/middleware/rate_limit.go` — the `TokenBucket` and `SlidingWindow` limiters clamp a non-positive window and a non-positive rate/limit. A missing or zero window otherwise disabled rate limiting entirely — a silent fail- **open** — and a negative rate denied every request; both now fall back to a sane floor.
- `http/middleware/rate_limit.go` — the default bucket key normalizes the request path exactly as the router does (trailing slashes trimmed), so `/login`, `/login/` and `/login//` share one bucket. Previously each trailing-slash variant keyed its own bucket, handing a caller a fresh allowance per spelling of the same route.
- `httpclient/stream_response.go` — `StreamResponse.Body` and `Close` are mutex-guarded. A watchdog goroutine aborting an indefinite stream while the consumer read it raced the body field, and a `Close` that nilled it could leave the reader dereferencing a nil body; access is now serialized.
- `http/middleware/compression.go` — the gzip middleware ties its compression pipe to the request context and releases it when the request unwinds. A middleware sitting outside compression that panicked after the handler returned skipped the pipe's normal close, leaking the writer goroutine and the original response body's file descriptor on every such request.
- `debug/command_container.go` — `debug:container` truncates and wraps error/context cells on rune boundaries, so multibyte UTF-8 is no longer sliced mid-character and the table and JSON output stay valid UTF-8.
- `config/environment_source.go` — the dotenv preprocessor now follows godotenv's escaped-quote and value-start quoting rules. A backslash-escaped quote inside a quoted value no longer ends the quote early, so an interior `#` is not mistaken for a comment and the value is not truncated; and a stray quote or apostrophe in an *unquoted* value no longer flips cross-line quote state, which had dropped the comment-prefixed lines of a later quoted multiline value.
- `application/cli.go` — runtime `--role`/`--mode` parsing and stripping stop at a bare `--` end-of-options terminator, passing every token after it to the command verbatim. Previously the scan ran past `--` and could consume a following token as a runtime flag, panicking on an invalid value or stealing the command's own argument.
- `application/cli.go` — an explicitly present but empty `--role` (an `--role=` expanded from an unset env var, or a bare `--role` that cannot consume a dash-leading next token) now fails closed like any other invalid role instead of falling back to the most permissive `all`, which had silently widened the process.
- `httpclient/http_client.go` — concurrent `Post`/`Put`/`Patch` calls that share one caller-supplied options slice no longer race or swap bodies. `append`-ing the JSON body option to a slice with spare capacity wrote into shared backing storage, so two in-flight requests could hand each other's body to the wrong destination; the capacity is clamped before the append so each call gets its own array.
- `httpclient/http_client.go` — a negative configured timeout falls back to the 30-second default. A negative value reached `http.Client.Timeout` as-is, which `net/http` treats as no deadline at all, so a misconfiguration silently produced an unbounded client.
- `httpclient/http_client.go` — the credential-stripping redirect policy also removes the auto-generated `Referer` header on a cross-origin redirect. `net/http` derives the `Referer` from the previous request URL, so a secret carried in that URL's query string leaked to the redirect target even as the header credentials were being stripped.
- `http/url_generator.go` — URL generation rejects a slash inside a single-segment `:param` value instead of percent-encoding it. An emitted `%2F` decoded back to `/` at the server, so the generated path matched a different route or 404'd rather than round-tripping to the route it named.
- `http/url_generator.go` — a catch-all parameter's requirement is tested against the collapsed remainder actually emitted (non-empty segments joined by `/`), not the raw value. A value with interior double slashes the router accepts was otherwise refused by generation, which checked the requirement against a form the router never sees.
- `http/url_generator.go` — URL generation stops at a `*name...` catch-all and no longer appends trailing literal pattern segments, matching registration and routing, which treat the catch-all as terminal. Appending them minted URLs this very router answered with a 404.
- `cache/remember_in_flight.go`, `cache/remember.go` — a `Remember` single-flight late joiner no longer inherits a spurious `context canceled` error. The waiter-count decrement and the decision to cancel the shared call raced when the last waiter of a cancelable call timed out just as another joined; both now happen under the shard mutex, so the call is cancelled only when truly no waiter remains.
- `validation/validation_rule.go` — the tag scanner recognizes a POSIX named character class, so a comma inside one (`regex=[[:alpha:],]`) no longer splits the tag. Such a pattern previously parsed as a truncated, invalid rule that rejected every value; it now compiles and validates as written.
- `config/configuration_resolve.go`, `config/configuration_validate.go` — placeholder resolution reports a circular reference instead of hanging. A self-referential or mutually-referential value (`APP_A=x%env(APP_A)%`) drove the resolver into unbounded recursion at boot; the cycle is now detected and surfaced as an error.
- `config/configuration_resolve.go`, `config/configuration_validate.go` — a placeholder-resolution boot error reports only the parameter name and the offending placeholder key, never the raw parameter value. The value can carry inline DSN credentials, which the previous message printed straight into the boot log.
- `container/resolver_context.go` — resolving a not-yet-created service by name or type snapshots the provider under the container lock. It previously read the provider registry with the lock released, so a concurrent registration racing the read could abort the whole process with Go's fatal `concurrent map read and map write`.
- `http/cors/service.go` — a scheme-qualified wildcard allowed-origin (`<scheme>://*.suffix`, for example `https://*.example.com`) is recognized as a wildcard and matches an `Origin` only when the scheme is identical and the host is a subdomain of the suffix. Such patterns were previously not treated as wildcards and matched nothing; scheme-less patterns keep their existing scheme-agnostic host matching.
- `validation/validator.go` — `validate` tags on nested struct fields, slice/array elements, map values and embedded structs are now enforced rather than silently ignored; only top-level fields were validated before. A nested violation fails the whole validation with a path naming the offending field (`items[0].sku`, `bill.sku`). The descent is depth-bounded and cycle-guarded so a self-referential payload terminates, and nil pointers/interfaces and unexported fields are skipped, so a previously-valid flat payload is unaffected.

## [v1.15.0] - 2026-07-06 - Kernel Fail-Closed Dispatch, Non-Panicking Response Write and Closed-Scope Errors

### Added

- `event/contract/event_dispatcher.go`, `event/event_dispatcher.go`, `security/access_control_listener.go` — `RequiredListenerRegistrar`: an optional event-dispatcher capability to mark a listener *required*. When a listener stops event propagation before a required listener behind it (lower priority) has run, `Dispatch`/`DispatchName` now return an error so the http kernel fails closed (its existing `kernel.request` error path) instead of proceeding as if that listener had run — closing a foot-gun where a custom `kernel.request` listener that stops propagation without producing a response could silently skip the access-control listener and reach the handler unauthenticated. The security firewall marks its access-control listener required automatically, so any application using it is protected with no code change. A listener that deliberately short-circuits past required listeners opts out via `MarkListenerMaySkipRequiredListeners`. Both marks default off and the first listener error
  already aborted dispatch, so an unmarked dispatch is byte-for-byte backward-compatible. Added in lockstep across `v1`/`v2`/`v3`.
- `validation` — the `lessThan` constraint (`lessThan(value=N)` / `lessThan=N`), mirroring `greaterThan`, is now registered by default (back-port from `v3`); a `lessThan` tag was previously rejected as an unknown validation rule.

### Changed

- `version/version.go` — the ldflags-overridable `buildVersion` default is raised to `v1.15.0`; keeping it in step with the released tag is now a standing release-procedure step (builds without `-ldflags` previously reported a stale default).
- `container/scope.go` — resolving from a closed scope through the error-returning methods (`Get`, `GetByType`, `OverrideProtectedInstance`) now returns the `scope is closed` error instead of panicking, aligning them with the package's Must/non-Must convention. The `Must*` variants keep panicking. A panic here was fatal in handler-spawned goroutines that outlive the request (the kernel closes the request scope when `ServeHttp` returns and no recover covers those goroutines). Aligned in lockstep with `v2`/`v3`.

### Fixed

- `http/kernel.go` — the kernel now also fails closed when the `kernel.controller` event dispatch aborts with an error and no listener produced a response, mirroring the existing `kernel.request` fail-closed path: the dispatcher stops at the first failing listener, so a listener marked required through `RequiredListenerRegistrar` sitting behind a failing higher-priority `kernel.controller` listener never ran, yet the kernel logged the error and proceeded to the handler — a silent fail-open one lifecycle event past the `kernel.request` gate the primitive already closed. It now synthesizes a 500 instead. Fixed in lockstep across `v1`/`v2`/`v3`.
- `http/request.go` — the request wrapper now preserves the raw body of an `application/x-www-form-urlencoded` request across its automatic form parse: it buffers the body and restores `Body`/`GetBody` around `ParseForm` (which consumes a urlencoded body), so a consumer that reads the raw body after the request is built still sees the true bytes instead of an empty one; multipart bodies stay streamed. Fixed in lockstep with `v3`, where it restores the HMAC internal-auth source's body-hash tamper-evidence for form-encoded requests (this module has no such consumer, so the change is a forward-looking parity back-port).
- `http/kernel.go` — the kernel now fails closed when the `kernel.request` event dispatch aborts with an error and no listener produced a response: it synthesizes a 500 instead of proceeding to the handler with partially-run listeners (the dispatcher stops at the first failing listener — now documented on `event/contract.EventDispatcher` — so e.g. the access-control listener behind a failing higher-priority listener never ran, and the request continued fail-open). A response set by an earlier listener still wins. Fixed in lockstep with `v2`/`v3`.
- `http/kernel.go` — the four response-finalization blocks now share one dispatch-error logging policy (`logEventDispatchError`, `AlreadyLogged`-aware): the controller-event and handler-response blocks logged the `EventKernelResponse` dispatch error inline, producing a duplicate log line for one listener failure. The not-found-handler error fallback also gains the `PrefersHtml` HTML branch the other error fallbacks already had. Fixed in lockstep with `v2`/`v3`.
- `http/router_utility.go` — `writeResponse` no longer panics on session persistence or response-write failures: it also runs inside the kernel's already-consumed panic-recovery defer, where a second `SaveSession` panic escaped `ServeHttp` and reset the connection instead of delivering the built response — a session-backend outage degraded to connection aborts instead of 500s. Session failures now log once and send the response without the session cookie; a write failure logs instead of panicking. Fixed in lockstep with `v2`/`v3`.
- `http/router_utility.go` — logout now always expires the browser session cookie even when the session-backend delete fails. On the cleared-session path the `Max-Age: -1` clearing cookie was emitted only when `DeleteSession` succeeded, so a session-store outage during logout returned a normal response while the client kept a still-valid cookie pointing at a still-live server-side session — a fail-open logout. Clearing the cookie is independent of and strictly safer than the backend delete (it can only end a session, never resurrect an unpersisted one), so it is now sent regardless (the session is still not marked persisted); the save path, where suppressing the cookie on a failed `SaveSession` is correct, is unchanged. Fixed in lockstep with `v2`/`v3`.
- `logging/recover.go` — a fatal non-zero exit now always leaves one final line on stderr (`melody: exiting with code N after unrecovered error: ...`), even when the error was already logged or the configured logger writes to a file: previously a startup failure such as an http bind error could terminate the process with no visible trace on the standard streams. Fixed in lockstep with `v2`/`v3`.
- `http/kernel.go` — the synthetic `Allow` header on an automatic `405`/`OPTIONS` response now reflects the configured `MethodPolicy`: it advertises `OPTIONS` only when `AutomaticOptions` is enabled and the synthetic `HEAD` only when `HeadFallbackToGet` is enabled. Previously both were listed unconditionally, so under a non-default policy the `Allow` header promised `OPTIONS`/`HEAD` that in fact return `405`. A method the route declares explicitly is unaffected. Fixed in lockstep with `v2`/`v3`.
- `validation` — a parameterized validation tag on a constraint outside the four built-ins (`min`/`max`/`regex`/`greaterThan`) — for example an application-registered `between(min=1,max=5)` — no longer silently discards its parameters and validates against the registered singleton's baked-in configuration (a fail-open in which the tag's declared bound went unenforced). `createConstraintWithParams` now mirrors `v3`'s generic contract: a constraint that accepts parameters implements the new `validation/contract.ParameterizedConstraint` (`WithParams`), and a tag carrying parameters the constraint cannot consume fails closed (the field is rejected as an invalid rule) instead of being validated permissively. The built-in `min`/`max`/`regex`/`greaterThan` parameterized tags are unaffected. Back-ported from `v3`.
- `validation/constraint_greater_than.go`, `constraint_less_than.go`, `constraint_min_length.go`, `constraint_max_length.go`, `constraint_regex.go` — the built-in parameterized constraints now also fail the rule closed when a parenthesized tag carries parameters without the key they consume (`value`, or `pattern`/`value` for `regex`), instead of silently falling back to their registered default bound. A mistyped key such as `greaterThan(min=18)` validated as `> 0`, `min(len=8)` as `minLength 1`, and — worst — `regex(re=^\d{4}$)` fell back to the match-all `.*` default, leaving the field effectively unvalidated; each now returns `invalid validation rule parameter`. The shorthand form (`greaterThan=18`) is unaffected — the parser maps it to the `value` key — and a bare value-less constraint (`min`) still resolves to its default. Extends the fail-closed parameterized-constraint contract above (which had left the built-ins unaffected) to the built-ins themselves. Back-ported from `v3`.
- `validation/constraint_min_length.go`, `validation/constraint_max_length.go` — the min/max length constraints now count Unicode code points (runes) instead of UTF-8 bytes, matching the error text ("characters") and, in `v3`, the OpenAPI `minLength`/`maxLength` facets (code-point based). Previously a multibyte value passed a byte-based minimum with fewer characters than required (a fail-open for a minimum-length check) and a code-point-valid value could be rejected server-side. Fixed in lockstep across `v1`/`v2`/`v3`.
- `security/matcher.go` — `PathPrefixMatcher.Matches` dereferenced the request (`request.HttpRequest()`) without first checking that the `httpcontract.Request` interface itself is non-nil, so a nil request triggered a nil-pointer dereference panic. Both `Firewall.Check` and `ApiKeyHeaderRule.Check` reach the matcher through `Applies` *before* any request nil-check (the `nil == request` guards inside `ApiKeyHeaderRule.Check` run only after `Applies`, leaving them unreachable dead code for a nil request), so a nil request reaching the firewall crashed the request rather than being treated as non-matching. `Matches` now returns `false` for a nil request, mirroring its existing `nil == request.HttpRequest()` guard, so the rule cleanly does not apply. Latent hardening: the request is always non-nil through the normal request-event flow, so this was not reachable in production. Fixed in lockstep with `v2`/`v3`.

## [v1.14.1] - 2026-06-25 - Cross-Version Security and Correctness Back-ports

### Fixed

- `internal/copy.go` — the session deep-copy (`CopyAnyMap`/`CopyAnySlice`, reached through the public `Session.Set`/`Save` API which take `any`) recursed into nested maps and slices with no depth bound, so a cyclic value (for example a map that contains itself) recursed until the goroutine stack overflowed — a fatal error no deferred `recover()` can catch, taking down the whole process. The recursion is now depth-bounded (returning the value as-is at the bound), which both halts a cyclic structure and leaves legitimate, far-shallower data fully deep-copied. Fixed in lockstep with `v2`/`v3`.
- `session/file_storage.go` — `FileStorage.Save`/`Delete` mutated the in-memory `sessionById` map before flushing and did not undo the change when the flush failed, so a `Save`/`Delete` that returned an error was still observable through a later `Load` in the same process (and diverged from the on-disk state after a restart). The in-memory entry is now rolled back on a flush failure, keeping the returned error consistent with both the in-memory and persisted state. Fixed in lockstep with `v2`/`v3`.
- `config/configuration.go` — `Configuration.Get`/`MustGet`/`Names`/`Parameters` read the shared `parameters` map without holding the lock that `RegisterRuntime` takes to write it, so calling `RegisterRuntime` (exposed at runtime via `kernel.Config()`) concurrently with any of those readers tripped Go's non-recoverable `fatal error: concurrent map read and map write`. The mutex is now a `sync.RWMutex`, the readers take the read lock, and `RegisterRuntime` uses the lock-free `getInternalParameter` internally to avoid a self-deadlock — completing the write-side guard added previously. Fixed in lockstep with `v2`/`v3`.
- `http/kernel.go` — when an `EventKernelResponse` listener replaced the response via `SetResponse`, the kernel wrote the new response but never closed the discarded original's body, leaking an open file descriptor for a file-backed response (`FileResponse` or static `ServeReader`). Each of the four response-dispatch sites now closes the discarded response body when the listener swapped it, matching the cleanup the error-handler swap paths already perform. Fixed in lockstep with `v2`/`v3`.
- `session/file_storage.go` — `writeSessionFileInPlace` (used by a `FileStorage` built from an injected `*os.File` via `NewFileStorageFromFile`) seeked and `Truncate(0)`-d the live file *before* JSON-encoding the session snapshot, so a `Save` whose value cannot be marshaled (for example a session value set to a channel or function — `Session.Set` takes `any`, and the value is only marshaled at flush time) left the file truncated to zero bytes, permanently destroying every previously-persisted session on disk while merely returning an error. It now encodes into an in-memory buffer first and only seeks, truncates and writes once the encode has succeeded, mirroring the validate-before-commit guarantee of the atomic `writeSessionFileAtomically` path. Fixed in lockstep with `v2`/`v3`.
- `container/scope.go` — `scope.MustGetByType(nil)` panicked with an obscure `runtime error: invalid memory address or nil pointer dereference` (and discarded the wrapped `GetByType` cause) instead of the intended descriptive panic: `GetByType(nil)` returns a clean "service type is required" error without dereferencing the type, but the error-reporting branch then called `String()` on the nil `reflect.Type`. It now guards the nil type when building the panic context, matching the sibling `resolverContext.MustGetByType` that already does. Fixed in lockstep with `v2`/`v3`.
- `security/config/access_control_builder.go` — `AllowAnonymous` matched its path prefix with a plain string prefix, so `AllowAnonymous("/api/public")` also opened sibling paths that merely share the string prefix (`/api/public-data`, `/api/publicXYZ/secret`) to unauthenticated access. It now builds the public-access rule with `NewAccessControlRuleWithSegmentPrefix`, matching only on a path-segment boundary (the declared prefix itself and its children). Ported from the `v3` fix.
- `security/api_key_authenticator.go` — `NewApiKeyHeaderAuthenticator` validated only the header name; an empty expected value constructed successfully even though it can never authenticate (a non-empty header never `ConstantTimeCompare`-equals `""`), a defensive gap relative to the sibling `ApiKeyHeaderRule`. It now panics on an empty expected value as well. Ported from the `v3` fix.
- `session/file_storage.go`, `session/in_memory_storage.go`, `internal/copy.go` — the session deep-copy recursed only into `map[string]any` and `[]any`, so any other typed collection stored in a session (e.g. `[]string`, `map[string]int`, `[][]string`) was copied by reference and could be mutated across loads/saves, leaking state between requests. The copy now lives in `internal.CopyAnyMap`/`CopyAnySlice` and deep-copies typed slices and maps reflectively. Ported from the `v3` fix.
- `validation/validator.go` — the `regex=<pattern>` shorthand form stores the pattern under the `value` key, but `createConstraintWithParams` only consulted the `pattern` key and otherwise fell back to `NewRegex(".*")`, which matches anything — a fail-open validation bypass for every shorthand regex rule. It now also honors the `value` key. Ported from the `v3` fix.
- `validation/validation_rule.go` — `splitByTopLevelComma` tracked only parenthesis depth, so a top-level comma inside a regex character class (`regex=^[a,b]$`) or quantifier (`regex=^a{1,2}$`) was mistaken for a rule separator, turning a valid tag into a broken regex plus a bogus "unknown validation rule". It now also tracks character-class and curly-brace state, matching the parenthesized form. Ported from the `v3` fix.
- `container/container.go` — `OverrideProtectedInstance` wrote the overridden value into the by-type instance map even for a service registered `WithoutTypeRegistration()`, creating a phantom type alias that caused a non-comparable value-type service with a value-receiver `Close` to be closed twice at shutdown. The by-type write is now gated on the type actually being registered. Fixed in lockstep with `v3`.
- `container/container_close.go` — `Close` still used the older per-node-key dependency/dedup algorithm that never collapsed the two node keys pointing at the same instance (a type-registered service lives under both `service:<name>` and `type:<T>`). This closed a non-comparable value-type service twice, and closed a type-registered dependency before the named service that depends on it by type (a dependent-after-dependency ordering violation at shutdown). It now uses the representative/alias-collapse algorithm already present in `v2`/`v3`. Ported from the `v2`/`v3` fix.
- `event/event_dispatcher_adapter.go` — `RegisteredEvents` sorted the map-owned listener slice in place while holding only a read lock, so two concurrent callers raced on the same backing array (a data race, with possible slice corruption or a sort panic). It now sorts a per-call copy. Ported from the `v2`/`v3` fix.
- `http/request_body.go` — `BindJson` reported an over-limit body as `400 Bad Request` instead of `413 Request Entity Too Large`: the kernel's `MaxBytesReader` returns its error before the local `LimitReader` cap is reached, so the oversize branch never fired on the normal request path. It now detects `*http.MaxBytesError` and returns `413`. Fixed in lockstep with `v2`/`v3`.
- `config/configuration.go` — `RegisterRuntime` performed an unguarded check-then-write on the shared `parameters` map, so two goroutines registering runtime parameters concurrently (or one registering while `Names()`/`Parameters()` iterated the map) raced on the map and could trigger Go's fatal "concurrent map writes". The read-modify-write is now serialized with a `sync.Mutex`, matching the `v3` field. Ported from the `v3` fix.
- `validation/constraint_greater_than.go` — the non-numeric fallback of `GreaterThan.Validate` reported `"value must be an integer"`, but the constraint accepts integer, unsigned, and floating-point values, so the message misled callers passing a valid float. It now reports `"value must be numeric"`, matching the `v3` wording. Ported from the `v3` fix.
- `logging/json_logger.go` — the `Log` marshal-failure fallback recomputed `time.Now()` for its `time` field instead of reusing the timestamp already captured for the primary entry, so a context value that fails to JSON-encode (for example a channel or function) produced a fallback line whose timestamp could drift from the moment the entry was created. The timestamp is now captured once and reused by both the primary entry and the fallback. Fixed in lockstep with `v2`/`v3`.
- `container/resolver.go` — `MustFromResolverByType` returned a nil value instead of panicking when a `Resolver` resolved the requested type to a nil pointer/interface, violating the `Must*` non-nil contract that the sibling `MustFromResolver` already enforces (a custom `containercontract.Resolver` whose `GetByType` returns `(typed-nil, nil)` slipped a nil through to the caller). It now applies the same `internal.IsNilInterface` guard and panics. Fixed in lockstep with `v2`/`v3`.
- `security/access_control.go` — `NewAccessControlRuleWithSegmentPrefix` (used by `AccessControlBuilder.AllowAnonymous`) accepted an empty path prefix, which normalized to `""` and became a catch-all fallback rule — so `AllowAnonymous("")` silently granted `PUBLIC_ACCESS` to every otherwise-unmatched path (fail-open). It now panics on an empty prefix, matching the existing empty-input guards on the exact and regex rule constructors; a fully public service declares an explicit `"/"` prefix. Fixed in lockstep with `v2`/`v3`.
- `validation/validator.go`, `validation/validation_rule.go` — a malformed numeric constraint parameter (for example `validate:"greaterThan(value=abc)"` or `validate:"min(value=notanumber)"`) silently degraded to the constraint's default bound instead of being reported, so a typo'd tag enforced a bound the author never specified (a fail-open configuration). Constraint creation now parses the value strictly (`parseIntStrict`) and a field whose numeric parameter cannot be parsed fails validation with the `invalidRuleSyntax` code instead. A valid leading integer is still accepted, so `max(value=3.9)` keeps truncating to `3`. **Behavioral note:** a previously-silent bad numeric tag now surfaces as a validation error. Ported from the `v3` fix.

## [v1.14.0] - 2026-06-16 - Configurable Transport & Shutdown Tunables + v3 Security and Correctness Back-ports

### Added

- `security/rule_test.go` — regression coverage for the API-key rule fail-open guards (empty header name and empty expected value both panic at construction); `security/access_control_test.go`, `security/access_control_listener_test.go`, and `security/config/access_control_builder_test.go` extended to cover the access-control matching, `PUBLIC_ACCESS` rejection, and `AllowAnonymous` fixes above
- `validation/value_test.go`, `security/access_control_test.go` — regression coverage for the named-string-type constraint fail-open and the trailing-slash access-control bypass back-ported above
- `validation/validation_rule_internal_test.go` — regression coverage that the shorthand and parenthesized regex tag forms both accept an alternation/capture group, and that unbalanced parentheses are still rejected
- `validation/validation_rule_paren_test.go`, `validation/constraint_greater_than_nan_test.go`, `cache/in_memory_increment_ttl_test.go`, `session/copy_any_slice_test.go`, `http/result_handler_typed_nil_test.go` — regression coverage for the parenthesized-regex comma-in-group parse, the `greaterThan` `NaN` rejection, the cache-increment TTL preservation, the session `[]any` deep-copy, and the typed-nil `*Response` normalization back-ported above
- `validation/constraint_pointer_deref_test.go`, `container/container_close_value_test.go` — regression coverage for the string-constraint `*string` fail-open and the value-type service double-close back-ported above
- `httpclient/transport_config.go` — `TransportConfig` (`DialTimeout`, `KeepAlive`, `MaxIdleConns`, `IdleConnTimeout`, `TlsHandshakeTimeout`, `ExpectContinueTimeout`, `ResponseHeaderTimeout`) with `DefaultTransportConfig()` exposes the previously-hardcoded `net/http.Transport` tuning of the HTTP client. Set it via the new fluent `HttpClientConfig.WithTransport(*TransportConfig)`; zero fields inherit the defaults, and a client built without it keeps the previous behaviour unchanged (backwards compatible). Back-ported from v3.
- `application/` — the HTTP graceful-shutdown grace period (previously a hardcoded `5s`) is now overridable: a `Configuration` that also implements the optional `HttpShutdownConfiguration` (`GetShutdownTimeout() time.Duration`) sets it, mirroring the existing `HttpTimeoutConfiguration` mechanism; a zero or absent value keeps the 5s default (backwards compatible). Back-ported from v3.
- `container/container_resolver_test.go`, `cache/remember_test.go` — regression coverage for the closed-container resolution guard and the cancelable-`Remember` late-joiner fix back-ported below
- `security/compiled_configuration_test.go` — regression coverage for the nil-login-result guard back-ported below
- `application/` — `Application.RegisterModuleProvider(provider)` plus expansion of the (previously dormant) `application/contract.ModuleProvider` inside `RegisterModule`: a module that also implements `ModuleProvider` now contributes its child modules in the same call, so an integration or application can register a whole group of capability-modules at once. Existing single-module registration is unchanged. Back-ported from v3.

### Changed

- `.dev/docker/docker-compose.yml`, `.dev/docker/.env`, `dc` — the development recipe now starts in two categories: `./dc up:minimal` starts only the `dev` container (enough for the build-tag matrix and unit tests), and `./dc up:all` also starts the infrastructure services needed by the live end-to-end tests (`rabbitmq`, `redis`, `mysql`, `minio`, grouped under the compose profile `all`); `./dc down` tears down both categories. Every published host port is now an `.env` variable (`DEV_HTTP_HOST_PORT`, `RABBITMQ_HOST_PORT`, `RABBITMQ_MANAGEMENT_HOST_PORT`, `REDIS_HOST_PORT`, `MYSQL_HOST_PORT`, `MINIO_HOST_PORT`, `MINIO_CONSOLE_HOST_PORT`) with the previous values as defaults, so a machine where another stack already holds a port can override it in `.dev/docker/.env.local`
- `.dev/docker/.gitignore` — `.env.local` is no longer tracked (it is machine-local by design and auto-created by the `dc` wrapper); it is now ignored alongside `.bash_aliases_local`
- `.dev/docker/Dockerfile`, `.dev/docker/entrypoint.sh`, `.dev/docker/docker-compose.yml` — the `dev` container now boots the `v3/.example` application by default with `reflex` hot-reload (rebuild-and-restart on `.go`/`.env`/`.yaml`/`.json`/`.toml` changes), so `./dc up:minimal` brings up a live example on `DEV_HTTP_HOST_PORT` (default `8180`). `github.com/cespare/reflex` is installed in the image. Three environment knobs override the behaviour (defaulted in compose, settable in `.dev/docker/.env.local`): `MELODY_DEV_REFLEX_ENABLED` (`0` runs once without watching), `MELODY_DEV_EXAMPLE_DIR` (point at `./.example` or `v2/.example`), and `MELODY_DEV_RUN_COMMAND` (empty idles the container like before). The example boots in-memory by default and wires the live services when their env vars are set under `./dc up:all`
- `.dev/docker/load-balancer/vhost.conf`, `.dev/docker/docker-compose.yml`, `.dev/docker/.env`, `dc` — a new `load-balancer` service (nginx) reverse-proxies the example over plain HTTP at `http://example.melody.localhost.precision-soft.com` (the `*.localhost.precision-soft.com` wildcard resolves to `127.0.0.1`), so there are no localhost-certificate issues. It starts alongside the example under both `./dc up:minimal` and `./dc up:all`, resolves the `dev` upstream through the docker DNS resolver at request time (so it comes up even before the app), and forwards WebSocket upgrades for the example's `/ws` route. The published host port is the new `LOAD_BALANCER_HTTP_HOST_PORT` `.env` variable (default `80`)

### Fixed

- `http/kernel.go`, `http/router_utility.go`, `http/response_writer.go` — a handler that writes its own response directly to the `ResponseWriter` (a hand-rolled streaming or download handler) and then returns `(nil, nil)` no longer triggers a superfluous `WriteHeader` call. `writeResponse` synthesized a default `204 No Content` for every nil response and wrote it unconditionally, so after such a handler had already committed its status the kernel re-wrote the header — emitting a `net/http` "superfluous response.WriteHeader call" warning. The kernel now wraps the response writer in a recorder that tracks whether the headers were committed, and `writeResponse` skips writing whenever the response headers were already committed, so a streamed response is never followed by a superfluous `WriteHeader` — whether the handler returned no response or failed after committing the stream. The recorder forwards `http.Flusher`, `http.Hijacker` and `io.ReaderFrom` and exposes `Unwrap`, so streaming,
  connection-upgrade handlers (which type-assert the writer to `http.Hijacker`) and the file-serving sendfile fast path keep working through the wrapper. (Under HTTP/2 the underlying writer is not an `http.Hijacker`, so that assertion is optimistic and the `Hijack` call returns an error, handled like a missing capability; `http.Pusher` is deliberately not forwarded, as HTTP/2 server push is deprecated.) Because `net/http`'s `MaxBytesReader` detects the server response through an unexported-method assertion that does not follow `Unwrap`, the per-request body limiter is given the raw writer rather than the recorder, so an oversized request body still triggers the connection-close signal; and `Flush` records the header commit, but only when the underlying writer actually supports flushing, so a flush-only streaming handler is likewise recognised as having committed its response. The recorder also marks the response committed only when `Hijack` actually succeeds, so a handler that attempts
  a hijack which fails (and returns no response) still receives a default response rather than an empty one. When a handler commits its own response yet still returns one — or the kernel synthesizes an error response after a stream-then-panic — `writeResponse` now closes that discarded response body before skipping the write, so a `FileResponse` returned alongside a self-written stream no longer leaks its open file descriptor. Regression coverage in `http/kernel_test.go` and `http/response_writer_test.go`. Ported from the `v3` fix.
- `http/router_utility.go`, `http/response_writer.go` — `writeResponse` no longer persists the session twice when the response write fails after the headers were committed. `writeResponse` persists the session (`SaveSession`/`DeleteSession`) and then writes the response; if the write fails after the headers were committed it panics, the panic-recovery path re-enters `writeResponse`, and because `SaveSession` does not reset the session's modified flag the session store was written a second time. The recorder now tracks whether the session was already persisted for the request (`SessionPersisted`/`MarkSessionPersisted`) and `writeResponse` persists it at most once — the header-commit flag cannot gate this, as a handler that streamed its own response still needs its session persisted on that first, already-committed call. Regression coverage in `http/kernel_test.go` (`TestKernel_DoesNotDoublePersistSessionWhenWriteFailsAfterCommit`). Ported from the `v3` fix.
- `http/response.go` — `FileResponse` (and `AttachmentResponse`, which delegates to it) now resolves a served file's `Content-Type` through the same built-in fallback table the static file server uses, so a file with an extension the operating-system MIME database does not register (for example a `.ico` favicon or a web font on a minimal system such as Alpine) is served with an accurate type rather than no `Content-Type`. Previously only the static `FileServer` carried the fallback; the helper path called `mime.TypeByExtension` directly. Regression coverage in `http/response_test.go`. Ported from the `v3` fix.
- `http/static/file_server.go` — the static file server now resolves an asset's `Content-Type` through a built-in fallback table of common web types (`.ico`, `.svg`, `.css`, `.js`, web fonts, `.wasm`, …) for extensions the operating-system MIME database does not register. On a minimal system (for example Alpine) `mime.TypeByExtension(".ico")` returns empty, so a served favicon previously fell through with no `Content-Type` and defaulted to `text/plain`; it is now served as `image/x-icon`. Regression coverage in `http/static/file_server_test.go`. Ported from the `v3` fix.
- `http/kernel.go` — the per-request service-container scope is now closed even when request-logger setup fails: the `scope.Close()` defer was registered after `requestIdLogger`, so a panic during logger resolution leaked the freshly created scope on every such request. The defer is now registered immediately after `NewScope()`, with the logger reference nil-guarded for the pre-setup failure path. Ported from the `v3` fix.
- `http/kernel.go` — a `kernel.response` (`EventKernelResponse`) listener that replaced the response via `SetResponse` was silently ignored on the two primary paths: the controller success path and the panic-recovery path dispatched the event but never read the (possibly replaced) response back from it, so `writeResponse` always wrote the pre-listener response. Both paths now capture `kernelResponseEvent.Response()` after dispatch, matching the kernel-request and kernel-controller short-circuit paths — and `v2`/`v3`, which were already correct. Found by back-filling the v1 kernel test suite to parity with `v2`/`v3`; regression coverage in `http/kernel_test.go` (`TestKernel_ResponseListenerReplacesResponseOnSuccessPath`, `TestKernel_ResponseListenerReplacesResponseOnPanicRecoveryPath`).
- `application/application_module.go` — `RegisterModule` now guards `ModuleProvider` expansion against a provider cycle: a module that (directly or transitively) provides itself recursed without bound and overflowed the goroutine stack at boot. Expansion depth is now capped (`maxModuleProviderDepth`) and a cycle fails fast with a `module provider expansion exceeded maximum depth, possible provider cycle` panic instead of an unrecoverable stack overflow. Ported from the `v3` fix.
- `validation/validation_rule.go` — the `validate` tag grammar now accepts a regex containing a group. `parseValidationTag` classified a rule as parenthesized-form by counting `(`/`)` anywhere in the fragment, so the documented shorthand `regex=^(a|b)$` (the parens are a regex group) was misrouted to the `name(params)` branch and hard-rejected with `"invalid validation tag syntax"`, and the parenthesized `regex(pattern=^(a|b)$)` failed too — no tag spelling could express an alternation/capture group. Classification is now by position (a fragment is parenthesized only when `(` precedes any `=`), with a new `hasBalancedBrackets` helper validating the inner balance, so both spellings carry a grouped pattern verbatim. Ported from the `v3` fix.
- `validation/validation_rule.go` — the parenthesized constraint form `name(value=…)` now accepts a regex whose pattern contains a comma inside a `()` group (for example `regex(value=^(\d{1,3},){3}\d{1,3}$)`). `splitByCommaOutsideRegexMeta` (which splits a parenthesized rule's parameter list) tracked `[]`/`{}` nesting but not `()` depth, so a comma inside a regex group was treated as a parameter separator, split the value mid-pattern, and failed as `invalid validation tag syntax` — even though the shorthand `regex=…` form accepted the same pattern. The parameter splitter now tracks `()` depth too. Ported from the `v3` fix.
- `validation/constraint_greater_than.go` — `greaterThan` now rejects a floating-point `NaN` instead of silently accepting it. IEEE-754 comparisons against `NaN` are always false, so `NaN <= min` evaluated false and the value passed the bound; the constraint now rejects a non-finite float explicitly. Ported from the `v3` fix.
- `cache/in_memory.go` — `Increment`/`Decrement` no longer clear an existing key's TTL on the in-memory backend, matching the Redis backend (whose `INCRBY` preserves the key's expiry). Both paths fed `ttl=0` into the upsert, which replaced the entry with a non-expiring one, so the first increment of a key created with a TTL turned it permanent. The increment path now reuses the existing item's expiry. Ported from the `v3` fix.
- `session/file_storage.go` — `copyAnyMap` (shared by the in-memory and file session backends) now deep-copies `[]any` slices in addition to nested `map[string]any` values. Previously a slice whose elements were maps was copied by reference, so a caller mutating a map inside a slice returned by `Load` could silently corrupt the stored session data (and vice versa after `Save`). Ported from the `v3` fix.
- `http/result_handler.go` — `NormalizeResultToResponse` no longer turns a typed-nil `*Response` into a non-nil `httpcontract.Response` interface. A `ResultHandler` returning `(*Response)(nil), nil` (the idiomatic "no response" signal) passed the `*Response` type assertion as a nil pointer wrapped in a non-nil interface, so the kernel ran the writer and panicked on the nil receiver (recovered into a 500); the assertion now guards the nil pointer and returns a nil interface. Ported from the `v3` fix.
- `validation/` — the string constraints (`email`, `regex`, `alpha`, `alphanumeric`, `numeric`, `notBlank`, `min`, `max`) now dereference a pointer or interface field before inspecting it, closing a fail-open on optional `*string` fields. The validator hands each field to a constraint through `reflect.Value.Interface()` without dereferencing, so a `*string` field reached the regex-family constraints' `value.(string)` assertion as a pointer — it failed and returned `nil` (a silent PASS for any value, including an invalid email) — while `notBlank`/`min`/`max` stringified the pointer with `fmt.Sprintf("%v", value)` and validated its hexadecimal address (so `notBlank` accepted a nil pointer and `min`/`max` measured the address length). A shared `dereferenceValue` helper now unwraps pointer/interface chains (a nil pointer is treated as absent) before the existing checks, matching `greaterThan`/`lessThan`/`notEmpty`. Ported from the `v3` fix.
- `container/container_close.go` — `Close()` no longer calls `Close()` twice on a value-type (non-pointer) service registered with the default options (registered both by name and by type), and no longer panics with `hash of unhashable type` when such a service holds an unhashable interface value (a slice/map/func). Duplicate suppression was keyed by pointer identity only, so the two close candidates referring to the same value-receiver service were both closed; a comparable value is now deduplicated by value identity, and comparability is decided from the runtime contents (`reflect.ValueOf(value).Comparable()`) rather than the static type, so an unhashable value is routed to the non-deduplicated path instead of panicking when used as a Go map key. Ported from the `v3` fix.
- `validation/validation_rule.go` — a regex `validate` tag whose pattern contained a `)`, `]` or `}` **inside a character class** (for example the parenthesized `regex(value=^[)]$)`) was rejected as "invalid validation tag syntax" because `hasBalancedBrackets` counted those literals as structural delimiters. A shared `charClassScanner` now treats every member of a `[...]` class (including a literal `]` as the class's first character and a leading `^` negation) as a literal across `hasBalancedBrackets` and `splitByCommaOutsideRegexMeta`, so such patterns parse and enforce intact. Ported from the `v3` fix.
- `http/middleware/static.go` — the static file middleware merged the file server's headers onto an `EmptyResponse` (which seeds `Content-Type: text/plain`) with `Header.Add`, emitting two conflicting `Content-Type` values so a CSS/JS asset could be served as `text/plain`. The merge now `Set`s the first value of each header key (and `Add`s the rest), letting the file server's `Content-Type` replace the default. Ported from the `v3` fix.
- `validation/validation_rule.go` — a literal quote (`'` or `"`) inside a regex character class no longer mis-parses the `validate` tag. In `splitByCommaOutsideRegexMeta` the quote handlers ran independent of the character-class scanner, so a quote inside `[...]` toggled the quote state; an odd number of class-literal quotes left the flag stuck on, swallowing the top-level comma and silently dropping every following constraint (fail-open). The quote handlers are now gated on `classScanner.inClass`. Ported from the `v3` fix.
- `validation/value.go` — the string constraints (`email`, `regex`, `alpha`, `alphanumeric`, `numeric`) no longer fail open on a **defined string type** (for example `type Email string`). `dereferenceValue` returned the value with its dynamic type, so the constraints' `value.(string)` assertion failed for a named string type and returned `nil` — a silent PASS for any value, the same fail-open the `*string` fix closed for pointers but reached through a domain-typed request field. `dereferenceValue` now normalizes a string-kind value to a plain `string`. Ported from the `v3` fix.
- `config/environment_source.go` — the `.env` preprocessor no longer truncates an unquoted value at an inline `#` not preceded by whitespace (`COLOR=#ffffff` became empty, `PASSWORD=ab#cd` truncated to `ab`), matching the bundled `godotenv` rule, and the per-value `strings.TrimSpace` that defeated quoted-whitespace preservation was dropped. Ported from the `v3` fix.
- `http/router_utility.go`, `http/kernel.go` — a controller that mutates or clears the session and returns a `nil` response no longer loses the session change (and the clearing `Set-Cookie`) or returns an implicit `200` instead of `204`. Session persistence lived only in `writeResponse`'s non-nil branch and the kernel skipped `writeResponse` entirely on the `(nil, nil)` path; the kernel now calls it and `writeResponse` synthesizes an empty `204`. Ported from the `v3` fix.
- `container/container_close.go` — `Close()` is now safe against a concurrent second `Close()`: `isClosed` is set while still holding the entry lock instead of only after the close loop, so two overlapping calls no longer both snapshot and double-close every service. Ported from the `v3` fix.
- `container/scope.go` — `OverrideProtectedInstance` now checks the closed-scope flag **inside** the mutex (matching the lookup methods), closing a race where a concurrent `Close()` nilling the maps caused an `assignment to entry in nil map` panic. Ported from the `v3` fix.
- `security/compiled_configuration.go` — `CompiledFirewall.Login` no longer panics with a nil-pointer dereference when a userland `LoginHandler` returns `(nil, nil)`. The contract returns `(*LoginResult, error)`, so a handler returning neither a result nor an error is valid Go, but the firewall previously dereferenced `result.Token` unguarded inside the request goroutine; it now fails closed with a `firewall login handler returned nil result` error before the login-success event is dispatched. Ported from the `v3` fix.
- `container/container_resolver.go` — a service resolution that raced `Close()` could store its freshly created instance after the close snapshot was taken, so the instance was never closed (a connection/file-handle leak for standalone container users). The creation guard now fails fast with a `container is closed` error when the container is already closed, and a value whose creation completed while `Close()` ran is closed best-effort instead of being stored; already-created instances remain readable after `Close()`. Ported from the `v3` fix.
- `cache/remember.go` — a **cancelable** `Remember` call whose waiters all timed out cancels the leader's context, but the in-flight entry lingered until the leader's deferred cleanup ran, so a caller that joined in that window inherited the doomed call and received its cancellation error even though a fresh computation would have succeeded. A late joiner now detects the canceled call, replaces the entry, and leads a fresh computation; the leader's cleanup deletes only its own entry so it can no longer evict the replacement. Ported from the `v3` fix.

### Security

- `security/access_control_listener.go` — the access-control listener (the request authorization gate) matched only prefix rules and the empty-prefix fallback, silently ignoring exact (`NewAccessControlExactRule`) and regular-expression (`NewAccessControlRegexRule`) rules; a request could therefore bypass an exact or regular-expression access-control rule entirely. `matchAccessControlRule` now delegates to `AccessControl.matchRuleIndex`, sharing the full exact → prefix → regular-expression → fallback precedence already used by `AccessControl.Match`
- `security/rule.go` — `ApiKeyHeaderRule.Check` compared the configured key against the request header with a plain `==`, which is not constant-time and leaks key length and shared prefix through timing; the comparison now uses `crypto/subtle.ConstantTimeCompare`. `NewApiKeyHeaderRule` additionally panics when the header name or the expected value is empty, closing a fail-open path where a request that omits the header (yielding `""`) would compare equal to an empty expected key and authorize every caller
- `security/access_control.go` — `NewAccessControlRule` and `NewAccessControlRuleWithSegmentPrefix` now reject a rule that combines `PUBLIC_ACCESS` with any other attribute (via `normalizeAccessControlAttributes`); the listener grants `PUBLIC_ACCESS` before any role or voter check, so a rule such as `(PUBLIC_ACCESS, ROLE_ADMIN)` would have silently opened the endpoint to everyone and discarded the role requirement
- `security/config/access_control_builder.go` — `AllowAnonymous` appended a rule with no attributes, which the listener treats as "authentication required", so the helper actually denied anonymous access with a 401; it now carries `securitycontract.AttributePublicAccess` so anonymous requests are granted as intended
- `security/access_control.go` — an exact or anchored-regex access-control rule could be bypassed by appending extra trailing slashes (`/admin//` routes to the `/admin` handler, but `matchRuleIndex` trimmed only one trailing slash and so failed to match the exact `/admin` rule, leaving the request unguarded). `matchRuleIndex` now collapses all trailing slashes like the router. Ported from the `v3` fix.

### Documentation

- `README.md` — added a "Getting started" section (install, a minimal runnable HTTP application, and next steps) and a "Versions & project status" section: the v1/v2/v3 module lines, v3 being the actively maintained version, the security/critical-fix back-port policy, the deprecate-toward-v4 approach, and the rationale for the intentional version duplication. Added an "Integrations" pointer and moved the build-tag reference below the usage guidance.
- `CONTRIBUTING.md` — added a "Versioning and where to make changes" section (features land on v3 only; back-port to v1/v2 only for security or critical correctness fixes; the version duplication is intentional and must not be consolidated), documented the `./dc up:minimal` / `up:all` development shell, and pointed the security guidance at `SECURITY.md`.
- `SECURITY.md` — added: supported version lines and private vulnerability reporting through GitHub.
- `integrations/README.md` — added an integrations index (what each integration provides, supported version lines, and links to per-integration documentation).
- `CODE_OF_CONDUCT.md` — added (Contributor Covenant 2.1 by reference; private reporting through GitHub).
- `.github/` — added issue templates (bug, feature), an issue-template config that links private security reporting and disables blank issues, and a pull-request template that reflects the versioning and back-port rules.
- Comment style — the house comment delimiter changed from `/** ... */` to `/* ... */` across all `.go` files. Single-star block comments render correctly on `pkg.go.dev` and machine-recognize the `Deprecated:` marker, so the previous `// Deprecated:` exception was dropped and existing markers were converted to `/* Deprecated: ... */`. `CONTRIBUTING.md` and the documentation canon were updated accordingly. Comments-only change; no behavior change.

## [v1.13.0] - 2026-05-16 - Cron Integration, Decoupled Cron Configuration, and `.example` Flat Layout

### Added

- `cli/contract/type.go` — `StringSliceFlag` type alias for `urfavecli.StringSliceFlag`; lets commands declare repeatable string-slice flags (consumed by `integrations/cron` for `--heartbeat-command` and `--heartbeat-destination`) via `clicontract.StringSliceFlag` like every other flag type
- `.documentation/package/CLI.md` — listed `clicontract.StringSliceFlag` in the package surface and added a pointer to `integrations/cron/` for users looking for a crontab generator
- `.example/go.mod` — `.example/` is now a standalone Go module (`github.com/precision-soft/melody/.example`) so it can `require` framework integrations (such as `integrations/cron`) without creating a cycle with the framework's own `go.mod`; local `replace` directives keep workspace builds resolving against the in-tree melody and integrations/cron checkouts
- `.example/config/` package — formerly `.example/bootstrap/`, now flat-layout; each Module hook lives in its own file with a matching compile-time interface assertion at the bottom (`module.go` → `Module`, `parameter.go` → `ParameterModule`, `service.go` → `ServiceModule`, `security.go` → `SecurityModule`, `event.go` → `EventModule`, `middleware.go` → `HttpMiddlewareModule`, `http.go` → `HttpModule`, `cli.go` → `CliModule`, plus `cron.go` for the cron registry helper and `configure.go` for the entry point)
- `.example/config/parameter.go` — registers cron parameters (`melody.cron.user`, `melody.cron.heartbeat_path`, `app.cron.product_user`, …) from `APP_CRON_*` env vars so the example demonstrates the env-driven cron configuration pattern
- `.example/config/cron.go` — extracts the cron `Configuration` build into a dedicated helper (`newCronConfiguration(kernel)`) that reads `app.cron.product_user` from the parameter cascade and applies it as a per-command `User` on the `product:list` schedule; pedagogical demonstration of how `.env` → `RegisterParameter` → `kernel.Config().Get(...)` → `cron.EntryConfig` flow works end-to-end
- `.example/config/cli.go` — `RegisterCliCommands` returns the CLI command list plus `melody:cron:generate` constructed from `newCronConfiguration(kernelInstance)`
- `.example/config/service.go` — services are now registered through `(*Module).RegisterServices(kernel, registrar)` implementing `applicationcontract.ServiceModule` (instead of a top-level `registerServices(app)` function called from `Configure`)
- `.example/config/middleware.go` — HTTP middleware is now registered through `(*Module).RegisterHttpMiddlewares(kernel, registrar)` implementing `applicationcontract.HttpMiddlewareModule` (instead of a direct `app.RegisterHttpMiddlewares(NewTimingMiddleware())` call from `Configure`); `NewTimingMiddleware` factory is retained
- `.example/config/configure.go` — simplified to a single `app.RegisterModule(NewExampleModule())` call now that every Module* interface is implemented on `*Module` directly
- `.example/security/default_access_denied_handler.go`, `.example/security/login_redirect_entry_point.go` — added compile-time interface assertions (`var _ AccessDeniedHandler = ...`, `var _ EntryPoint = ...`)
- `application/application_new.go` — `computeProjectDirectory` now prefers the working directory over the closest `go.mod` ancestor when the working directory itself contains `.env` or `.env.local`. This unblocks `go run .` for sub-applications whose `.env` lives next to `main.go` rather than at the parent module's root
- `application/application_test.go` — `TestWorkingDirectoryHasEnvironmentFile_*` covers the new `.env` / `.env.local` detection helper

### Changed

- `http/accept.go` — `PrefersHtml` refactored to short-circuit when `text/html` is absent from the `Accept` header, skipping the `application/json` scan and reducing the common-case complexity from O (2N) to O (N); v1/v2/v3 implementations are now byte-identical apart from the melody import path
- `logging/default_logger.go` — rename abbreviated loop variables `i` and `v` to `index` and `value` in `joinPairs`
- `http/response.go` — rename abbreviated loop and parameter variables `r`, `b` to `runeChar`, `byteChar` in `asciiFallbackFilename`, `rfc5987EncodeFilename`, and `isRfc5987AttrChar`
- `.example/` — flattened `domain/` and `infra/` layers into top-level packages (`cache/`, `cli/`, `entity/`, `event/`, `handler/`, `page/`, `presenter/`, `repository/`, `route/`, `security/`, `service/`, `subscriber/`, `url/`). Renamed `bootstrap/` to `config/`. Flat layout. Domain and in-memory repositories collapsed into a single `repository/` package
- `.example/.env` — adds `APP_CRON_USER`, `APP_CRON_HEARTBEAT_PATH`, and `APP_CRON_PRODUCT_USER` so the cron default user, heartbeat path, and `product:list` per-command user are sourced from the environment rather than hard-coded
- `.example/.gitignore` — ignores `/generated_conf/` (output directory for `melody:cron:generate`)
- `.example/README.md` — documents the new flat layout, the cron `Configuration` registry, the env-driven cron parameters, and `melody:cron:generate` usage
- `go.work` — register the new `.example/`, `v2/.example/`, `v3/.example/` workspace modules

### Removed

- `.example/bootstrap/`, `.example/domain/`, `.example/infra/` — flattened into top-level packages (see "Changed")

## [v1.12.1] - 2026-04-23 - Retract v1.10.0

### Changed

- `go.mod` — retract `v1.10.0`; the tag was placed on the wrong commit (identical to `v1.9.0`); use `v1.10.1` instead

## [v1.12.0] - 2026-04-20 - Harden HTTP Server Timeouts

### Added

- `application/application_http.go` — HTTP server now sets hardened timeout defaults (`ReadTimeout=15s`, `ReadHeaderTimeout=5s`, `WriteTimeout=30s`, `IdleTimeout=60s`, `MaxHeaderBytes=1MiB`) to defend against slowloris / slow-body attacks on exposed servers (MEL-148)
- `application/application_http_timeouts.go` — new optional `HttpTimeoutConfiguration` interface; any `HttpConfiguration` that implements it can override the hardened defaults per timeout without breaking existing configurations (MEL-148)
- `application/application_http_timeouts_test.go` — coverage for default application and interface-driven overrides

## [v1.11.0] - 2026-04-17 - Extract HTTP CORS Subpackage and Harden Request Lifecycle

### Added

- `http/cors/` — new subpackage extracted from `http/middleware/cors.go`. Split into `cors.Service`, `cors.Middleware`, and `cors.RegisterResponseListener` so CORS headers are applied both on the happy path (middleware) and on error-path responses produced by the kernel (`kernel.response` listener, priority `-100`)
- `http/response.go` — `BuildContentDisposition(disposition, filename)` emits RFC 6266 `Content-Disposition` with both `filename="..."` ASCII fallback and `filename*=UTF-8''...` RFC 5987 encoding for non-ASCII filenames; `AttachmentResponse` now routes through it
- `http/middleware/rate_limit.go` — `ClientIpResolver` hook and `DefaultClientIp` for proxy-aware IP resolution; `RateLimitConfig.SetClientIpResolver(...)` lets userland install X-Forwarded-For / X-Real-IP strategies without rewriting key extractors
- `http/request.go` — form auto-parsing now gated on `Content-Type` (`application/x-www-form-urlencoded` or `multipart/form-data`); JSON/XML/binary bodies are no longer consumed by `NewRequest`
- `session/session.go` — `isValidSessionId` enforces 32-char lowercase-hex format; `Manager.Session`/`DeleteSession` reject malformed cookies before hitting storage
- Test coverage: `http/cors/{listener,middleware,service}_test.go`, `http/request_test.go`, `http/response_test.go`, `container/scope_test.go` concurrent Close/resolve test, `logging/json_logger_test.go` concurrent writes, `session/file_storage_test.go` atomic write and reopen coverage

### Changed

- `http/middleware/cors.go` — public CORS API (`CorsConfig`, `NewCorsConfig`, `DefaultCorsConfig`, `RestrictiveCorsConfig`, `CorsMiddleware`, `DefaultCorsMiddleware`, `RestrictiveCors`) moved to `http/cors/`. Old symbols retained in `http/middleware/` as deprecated shims that delegate to `http/cors`; kept for backwards compatibility, no removal scheduled
- `http/middleware/compression.go` — gzip now streams through `io.Pipe` instead of buffering the full body; `Vary: Accept-Encoding` is always emitted; `Accept-Encoding` parsing uses RFC 7231 q-values via `acceptsGzip` (explicit `gzip;q=0` is respected)
- `http/middleware/rate_limit.go` — default `keyExtractor` is now built inside `RateLimitMiddleware` from the configured `ClientIpResolver`; `SimpleRateLimit`/`IpRateLimit` no longer embed the extractor directly
- `http/kernel.go` — incoming request bodies are wrapped with `net/http.MaxBytesReader` when `kernel.http.max_request_body_bytes` is positive; discarded responses replaced by an error handler are now closed via `closeDiscardedResponseBody` to avoid leaking file descriptors / connections
- `container/scope.go` — `scope.container` is now `atomic.Pointer[container]`; `Close` nils the pointer so a concurrent `Get`/`Resolve` returns a clean "scope closed" error instead of racing on a nil deref
- `cache/in_memory.go` — removed `runtime.SetFinalizer` fallback and the `cleanupCancel`/`context.Context` path; cleanup goroutine now terminates solely via `Close`/`stopCleanup`, documented as owner-closed
- `logging/json_logger.go` — writes are serialized through `sync.Mutex` so concurrent `Log` calls produce cleanly separated JSON lines on the shared writer
- `security/api_key_authenticator.go` — credential comparison switched to `crypto/subtle.ConstantTimeCompare` to eliminate the timing-leak on API key length/prefix matches
- `session/file_storage.go` — file writes are now atomic (`os.CreateTemp` + `os.Rename`) instead of truncate-in-place; load path decoupled from a long-lived `*os.File` handle; `ownsFile` retired in favor of path-based ownership
- `.documentation/package/*.md` — full documentation overhaul across APPLICATION/CACHE/CLI/CONFIG/CONTAINER/EVENT/HTTP/HTTPCLIENT/LOGGING/SECURITY/SESSION/VALIDATION: added missing userland types, constructors, container-access helpers, environment key tables, constants, and footgun notes

### Deprecated

- `http/middleware.CorsConfig`, `http/middleware.NewCorsConfig`, `http/middleware.DefaultCorsConfig`, `http/middleware.RestrictiveCorsConfig`, `http/middleware.CorsMiddleware`, `http/middleware.DefaultCorsMiddleware`, `http/middleware.RestrictiveCors` — use the equivalents in `github.com/precision-soft/melody/http/cors` instead. Deprecated symbols are kept for backwards compatibility; no removal scheduled.

## [v1.10.1] - 2026-04-17 - Fix Compression Error Propagation and Concurrent Access Races

### Added

- `http/static/utility_test.go` — symlink traversal rejection, absolute path rejection, parent traversal rejection, symlink within root allowed
- `cli/output/application_version_test.go` — Set/Get coverage and concurrent access race test
- `logging/emergency_logger_test.go` — singleton behavior, `Close`/recreate cycle, concurrent access
- `httpclient/http_client_test.go` — concurrent `SetHeader`/`SetBaseUrl`/`SetTimeout` with in-flight requests, `HttpClientConfig.Headers()` defensive copy
- `http/middleware/compression_test.go` — HuffmanOnly and BestCompression level boundary acceptance, out-of-range fallback to DefaultCompression
- `config/configuration_test.go` — placeholder regex rejects identifiers starting with digits, accepts letter/underscore/dotted identifiers
- `session/in_memory_storage_test.go`, `session/file_storage_test.go` — concurrent `Load`/`Save` race tests

### Changed

- `httpclient/http_client.go` — added `sync.RWMutex` to protect concurrent access to `baseUrl`, `headers`, and `timeout` fields
- `httpclient/http_client_config.go` — `Headers()` now returns a defensive copy of the map
- `cli/output/application_version.go` — application version storage replaced with `sync/atomic.Value` for thread safety
- `logging/emergency_logger.go` — replaced `sync.Once` with `sync.Mutex` so `CloseEmergencyLogger()` can reset the singleton and a subsequent `EmergencyLogger()` call creates a fresh instance
- `http/kernel.go` — `debugMode` variable hoisted to single computation at request entry
- `application/application_http.go` — extracted `httpShutdownTimeout` constant for the HTTP server shutdown deadline
- `cache/in_memory.go` — removed redundant map copy in `SetMultiple`

### Fixed

- `http/middleware/compression.go` — compression middleware now propagates `io.ReadAll` errors instead of silently returning partial data to the client; level validation lower bound corrected from `gzip.DefaultCompression` to `gzip.HuffmanOnly`
- `http/static/utility.go` — static file server now validates resolved symlink targets via `filepath.EvalSymlinks()` and returns 403 for paths escaping the configured root directory; `EvalSymlinks` errors are now propagated directly instead of being mapped to `fs.ErrNotExist`
- `config/configuration.go` — placeholder regex now requires identifiers to start with a letter or underscore, rejecting patterns like `%1invalid%`
- `config/configuration_resolve.go` — fix shadowed `err` variable in `resolveSinglePass` that silently discarded template resolution errors
- `session/file_storage.go` — `flushToFile` no longer redundantly reloads the file after a successful rename-based swap
- `logging/logger.go` — `LogError()` nil-logger check moved after the fallback `log.Printf` path so `AlreadyLogged` is only evaluated when a logger is present
- `session/in_memory_storage.go` — `Load()` now holds `RLock` during the data copy to prevent a race with concurrent `Save()` calls
- `session/file_storage.go` — `Load()` now holds `RLock` during `copyAnyMap()` to prevent a race with concurrent `Save()` calls
- `httpclient/http_client.go` — `SetTimeout()` no longer mutates `http.Client.Timeout` on the shared client (which races with in-flight `Do()` calls); `clientForRequest` now reads the instance timeout under `RLock` and builds a per-request client only when it differs from the shared client's construction timeout
- `logging/emergency_logger.go` — `CloseEmergencyLogger()` now resets the singleton to `nil` so that subsequent `EmergencyLogger()` calls actually create a fresh instance (previously the closed instance was retained)

## [v1.10.0] - 2026-04-13 - Lock-Step Release — Align with v2/v3 Sibling Tags

Lock-step release — no `v1/` changes this cycle. Tag SHA equals `v1.9.0`; published to keep the core `v1` module version aligned with the `v2.4.0` / `v3.3.0` sibling tags. See the v2/v3 CHANGELOGs for the actual content of this cycle.

## [v1.9.0] - 2026-04-13 - Fix Validators, Rate Limiter, and Router; Improve Goroutine Lifecycle

### Changed

- `cache/in_memory.go` — `cleanupLoop` accepts `context.Context`; `NewInMemoryCache` creates a cancel context stored as `cleanupCancel`; `Close()` calls `cleanupCancel()` to stop the goroutine cooperatively
- `session/in_memory_storage.go` — same goroutine lifecycle improvements as `cache/in_memory.go`
- `http/request.go` — replace `log.Printf` fallback (when no runtime instance is available) with `logging.NewDefaultLogger().Warning(...)`; remove unused `"log"` import
- `cli/command.go` — remove block comments and `//nolint:errcheck` directives from `printGreenFullLine`, `printGreenStatusLine`, `printRedStatusLine` closures
- `logging/logger.go` — added GoDoc comment to `causeChainMaxDepth`; removed duplicated `buildCauseChain`/`buildCauseContextChain`, delegating to `exception.BuildCauseChain`/`BuildCauseContextChain`
- `security/compiled_configuration.go` — group string fields in `CompiledFirewall` struct (`name`, `matcherDescription`, `loginPath`, `logoutPath`)
- `file_storage.go` — `copyAnyMap` performs recursive deep copy for nested `map[string]any` values
- `exception/utility.go` — export `BuildCauseChain` and `BuildCauseContextChain` (formerly `buildCauseChain` / `buildCauseContextChain`)
- `router_utility.go` — remove implicit HEAD-to-GET match from `matchesMethod`; kernel `HeadFallbackToGet` policy is now the single control point

### Fixed

- `validator.go` — `createConstraintWithParams` now handles `greaterThan` parameters; `validate:"greaterThan(value=5)"` was silently using `min=0`
- `rate_limit.go` — `getClientIp` strips port via `net.SplitHostPort`; rate limiting was per-connection instead of per-IP
- `url_generator.go` — path parameters now URL-encoded via `url.PathEscape`; special characters produced malformed URLs
- `accept.go` — `PrefersHtml` uses position-based comparison; browsers sending both `text/html` and `application/json` now correctly get HTML
- `compression.go` — `gzip.NoCompression` (level 0) is no longer overridden to default compression
- `constraint_greater_than.go` — added `float32`/`float64` support; float values no longer return "value must be an integer"
- `kernel.go` — `errorHandler` now called for controller errors (was only called on panic recovery path)
- `cors.go` — panic at middleware initialization when `AllowCredentials=true` and origins contain `"*"` to prevent overly permissive CORS

## [v1.8.4] - 2026-04-10 - Fix XSS, Symlink Traversal, and Routing Edge Cases

### Added

- `test_helper_test.go` — shared test runtime helper for exception listener tests
- `exception_listener_test.go`, `request_test.go`, `response_test.go`, `middleware/compression_test.go`, `middleware/cors_test.go`, `middleware/rate_limit_test.go`, `url_generation_route_definition_test.go` — new and expanded test coverage for all fixes

### Changed

- `kernel.go` — remove dead nil checks on `MatchResult` (router `Match()` always returns non-nil)
- `profiling_kernel.go` — simplify request context extraction (remove guard on always-non-nil `Attributes()`)
- `request.go` — log warning when `ParseForm` fails (previously silent)
- `url_generation_route_definition.go` — `Defaults()` and `Requirements()` now return defensive copies
- Rename `security/security_test.go` to `security/test_helper_test.go`
- Remove redundant comments from modified files

### Fixed

- `exception_listener.go` — HTML error response now escapes error messages with `html.EscapeString` preventing XSS
- `exception_listener.go` — use `LoggerFromRuntime` instead of `LoggerMustFromRuntime` to prevent panic when runtime logger is not available
- `router_utility.go` — wildcard locale route attribute used `RouteAttributeName` instead of `RouteAttributeLocale`, causing catch-all wildcards named `_route` to incorrectly write to the `_locale` param
- `middleware/compression.go` — `ReadAll` error discarded partially read data; now preserves whatever was read before the error
- `middleware/cors.go` — origin matching was case-sensitive; now uses `strings.EqualFold` for case-insensitive comparison
- `middleware/rate_limit.go` — `getClientIp` now uses `RemoteAddr` only; ignores `X-Forwarded-For` and `X-Real-IP` headers to prevent IP spoofing

## [v1.8.3] - 2026-03-21 - Refactor Address Colon Check in Config

### Changed

- `config/http.go` — replaced colon-based address check with `strings.Contains` for correct host:port detection

## [v1.8.2] - 2026-03-18 - Fix HTTP HEAD Handling and Update Dev Scripts

### Changed

- `internal/reflect.go` — updated type-reflection utilities
- `.dev/validate/all.sh`, `.dev/validate/mod.sh` — added `-h` help flag to validation scripts
- `.gitignore` — updated patterns

### Fixed

- `http/router_utility.go` — aligned HEAD handling and response contract validation; prevents incorrect responses on HEAD requests

## [v1.8.1] - 2026-03-17 - Fix JSON Logging Level Label Preservation

### Fixed

- `logging/contract/level.go`, `logging/logger.go` — preserved numeric logging level labels in JSON output; `logging/json_logger_test.go` — coverage

## [v1.8.0] - 2026-03-17 - Add Module Configuration Registration and Logging Labels

### Added

- `application/contract/config_module.go` — new `ConfigModule` interface allowing modules to register configuration during application boot
- `logging/contract/config.go`, `logging/logging_config.go` — `LoggingConfig` struct and contract for customizable logging level labels
- `logging/default_logger.go`, `logging/json_logger.go`, `logging/logger.go` — updated to apply level label customization from `LoggingConfig`
- `application/application.go`, `application/application_module.go`, `application/application_new.go` — wired `ConfigModule` into the application boot sequence

### Changed

- `.dev/run-batch.sh`, `.dev/utility.sh`, `.dev/validate/all.sh` — dev scripts optimisation

## [v1.7.3] - 2026-03-05 - Add CLI Table Width Flag and Fix Docker Profile Aliases

### Added

- `cli/output/flag.go`, `cli/output/printer_selector.go` — added `--table-width` flag for table output
- `cli/output/option.go`, `cli/output/option_parser.go`, `cli/output/standard_flag.go` — parsed and propagated new width option

### Fixed

- `.dev/docker/.profile` — fixed Docker `.profile` aliases in interactive shells without recursion

## [v1.7.2] - 2026-02-28 - Add CLI Stdout/Stderr Wiring and Standardize Method Receivers

### Added

- `cli/command.go`, `cli/command_output.go` — wired `stdout`/`stderr` to CLI output; print command errors with failed exit status

### Changed

- All `.go` files in the module — standardized all method receivers to `instance` for consistent style

## [v1.7.1] - 2026-02-23 - Fix RoleVoter Auto-Upgrade to RoleHierarchyVoter

### Fixed

- `security/config/compile.go`, `security/access_decision_manager.go` — auto-upgrade `RoleVoter` to `RoleHierarchyVoter` when role hierarchy is configured

## [v1.7.0] - 2026-02-18 - Add GreaterThan and NotEmpty Validation Constraints

### Added

- `validation/constraint_greater_than.go` — new `greaterThan(value=N)` constraint with support for int, float32, float64; returns per-constraint error codes
- `validation/constraint_not_empty.go` — new `notEmpty` constraint for slices and strings; returns per-constraint error codes
- `validation/const.go`, `validation/validation_rule.go`, `validation/validator.go` — wired new constraints into the validation pipeline
- `exception/utility.go` — context-aware error wrapping helper `Wrap(ctx, err)` for exception chaining

## [v1.6.3] - 2026-02-17 - Lock-Step Release — Align with bunorm Integration Tags

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with sibling integration tags. See the `integrations/bunorm/mysql` and `integrations/bunorm/pgsql` CHANGELOGs for the provider post-build hook work captured in this cycle.

## [v1.6.2] - 2026-02-16 - Add HttpMiddlewareModule Registration Hook

### Added

- `application/contract/http_middleware_module.go` — new `HttpMiddlewareModule` interface for middleware registration
- `application/http_middleware.go`, `application/application_module.go` — wired module registration into the HTTP boot sequence

## [v1.6.1] - 2026-02-13 - Fix Token Source Panic and Add ParameterModule/ServiceModule

### Added

- `application/contract/parameter_module.go`, `application/contract/service_module.go` — new `ParameterModule` and `ServiceModule` interfaces for granular application boot
- `application/application.go`, `application/application_module.go` — split boot around configuration resolve; wired new module contracts into the lifecycle

### Fixed

- `security/security_resolution_listener.go` — make token source resolution panic-safe and always set security context; prevents nil-pointer panics when no token source is configured

## [v1.6.0] - 2026-02-11 - Lock-Step Release — Align with rueidis Integration Tag

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with the new `integrations/rueidis` module. See `integrations/rueidis/CHANGELOG.md` for the actual content.

## [v1.5.1] - 2026-02-07 - Lock-Step Release — Align with bunorm Integration Tags

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with `integrations/bunorm` sibling tags. See `integrations/bunorm/CHANGELOG.md` / `integrations/bunorm/mysql/CHANGELOG.md` / `integrations/bunorm/pgsql/CHANGELOG.md` for the actual content.

## [v1.5.0] - 2026-02-06 - Lock-Step Release — Align with bunorm/migrate Integration Tag

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with the new `integrations/bunorm/migrate` module. See `integrations/bunorm/migrate/CHANGELOG.md` for the actual content.

## [v1.4.0] - 2026-02-05 - Lock-Step Release — Align with bunorm Integration Tags

Lock-step release — no `v1/` changes this cycle. Tag published to keep the core `v1` module aligned with the new `integrations/bunorm`, `integrations/bunorm/mysql`, and `integrations/bunorm/pgsql` modules. See those CHANGELOGs for the actual content.

## [v1.3.2] - 2026-02-03 - Fix Exception CauseChain in LogContext

### Fixed

- `exception/utility.go` — included `causeChain` in `LogContext` output so error causes appear in structured log entries

## [v1.3.1] - 2026-01-30 - Fix Default Presenter Exception Override

### Fixed

- `http/exception_listener.go` — prevented default presenter from overriding exception event response in the error handling path

## [v1.3.0] - 2026-01-30 - Add Stateless Firewall and API Key Authentication

### Added

- `security/config/security_module.go`, `security/config/compile.go` — added stateless firewall support for API key authentication; kept `AddFirewall` for backwards compatibility

## [v1.2.0] - 2026-01-30 - Add Controller Autowiring and Relax Signature Validation

### Added

- `http/router_utility.go` — autowire runtime into controller parameters; relaxed controller signature validation to accept contract interfaces
- `http/request.go` — updated request helpers to support new controller signature patterns

## [v1.1.0] - 2026-01-29 - Add Route Options Contract and Group Routing API

### Added

- `http/contract/route_option.go`, `http/contract/router_group.go` — route options contract and group routing API
- `http/route.go`, `http/route_option.go`, `http/router.go`, `http/router_group.go` — implementation of route options and group routing
- `http/router_group_test.go`, `http/router_utility_test.go` — test coverage

## [v1.0.1] - 2026-01-28 - Fix Panic Cause Logging

### Fixed

- `logging/recover.go`, `logging/logger.go` — log panic causes and context chains on recovery; panics now produce structured log entries with full cause chain

## [v1.0.0] - 2026-01-17 - Initial Release

### Added

- `application/` — application container with dependency injection; `Application.Boot()` orchestrates module registration, configuration resolve, and CLI/HTTP mode dispatch
- `bag/` — parameter bag abstraction (`ParameterBag`, typed value accessors)
- `cache/` — cache abstraction (`Manager`, `InMemoryCache`, `Remember` helper with in-flight deduplication)
- `cli/` — CLI command framework with output formatting (JSON, table, list)
- `clock/` — clock abstraction with `SystemClock` and `FrozenClock` for testing
- `config/` — configuration management with placeholder resolution, environment sources, and typed sub-configs (HTTP, CLI, kernel)
- `event/` — event dispatcher with subscriber registration and priority-ordered listener dispatch
- `exception/` — exception handling with typed errors, cause chain, `LogContext`, and HTTP exception mapping
- `http/` — HTTP kernel with routing, middleware pipeline, and request/response contracts; `cors`, `rate_limit`, `compression`, and `static` middleware included
- `httpclient/` — HTTP client abstraction with per-request options and stream response support
- `logging/` — structured logging with JSON logger, emergency logger, and `recover` helper
- `runtime/` — runtime context providing access to logger, config, and container from within request scope
- `security/` — security framework with authentication, authorization, role hierarchy, firewall, and voter chain
- `serializer/` — serializer abstraction with MIME-type dispatch
- `session/` — session management with file-based and in-memory storage backends
- `validation/` — validation framework with `greaterThan`, `notEmpty`, `notBlank`, `alpha`, `alphanumeric`, `email`, `numeric`, `regex`, `minLength`, `maxLength` constraints

[Unreleased]: https://github.com/precision-soft/melody/compare/v1.19.0...HEAD

[v1.19.0]: https://github.com/precision-soft/melody/compare/v1.18.1...v1.19.0

[v1.18.1]: https://github.com/precision-soft/melody/compare/v1.18.0...v1.18.1

[v1.18.0]: https://github.com/precision-soft/melody/compare/v1.17.0...v1.18.0

[v1.17.0]: https://github.com/precision-soft/melody/compare/v1.16.0...v1.17.0

[v1.16.0]: https://github.com/precision-soft/melody/compare/v1.15.0...v1.16.0

[v1.15.0]: https://github.com/precision-soft/melody/compare/v1.14.1...v1.15.0

[v1.14.1]: https://github.com/precision-soft/melody/compare/v1.14.0...v1.14.1

[v1.14.0]: https://github.com/precision-soft/melody/compare/v1.13.0...v1.14.0

[v1.13.0]: https://github.com/precision-soft/melody/compare/v1.12.1...v1.13.0

[v1.12.1]: https://github.com/precision-soft/melody/compare/v1.12.0...v1.12.1

[v1.12.0]: https://github.com/precision-soft/melody/compare/v1.11.0...v1.12.0

[v1.11.0]: https://github.com/precision-soft/melody/compare/v1.10.1...v1.11.0

[v1.10.1]: https://github.com/precision-soft/melody/compare/v1.10.0...v1.10.1

[v1.10.0]: https://github.com/precision-soft/melody/compare/v1.9.0...v1.10.0

[v1.9.0]: https://github.com/precision-soft/melody/compare/v1.8.4...v1.9.0

[v1.8.4]: https://github.com/precision-soft/melody/compare/v1.8.3...v1.8.4

[v1.8.3]: https://github.com/precision-soft/melody/compare/v1.8.2...v1.8.3

[v1.8.2]: https://github.com/precision-soft/melody/compare/v1.8.1...v1.8.2

[v1.8.1]: https://github.com/precision-soft/melody/compare/v1.8.0...v1.8.1

[v1.8.0]: https://github.com/precision-soft/melody/compare/v1.7.3...v1.8.0

[v1.7.3]: https://github.com/precision-soft/melody/compare/v1.7.2...v1.7.3

[v1.7.2]: https://github.com/precision-soft/melody/compare/v1.7.1...v1.7.2

[v1.7.1]: https://github.com/precision-soft/melody/compare/v1.7.0...v1.7.1

[v1.7.0]: https://github.com/precision-soft/melody/compare/v1.6.3...v1.7.0

[v1.6.3]: https://github.com/precision-soft/melody/compare/v1.6.2...v1.6.3

[v1.6.2]: https://github.com/precision-soft/melody/compare/v1.6.1...v1.6.2

[v1.6.1]: https://github.com/precision-soft/melody/compare/v1.6.0...v1.6.1

[v1.6.0]: https://github.com/precision-soft/melody/compare/v1.5.1...v1.6.0

[v1.5.1]: https://github.com/precision-soft/melody/compare/v1.5.0...v1.5.1

[v1.5.0]: https://github.com/precision-soft/melody/compare/v1.4.0...v1.5.0

[v1.4.0]: https://github.com/precision-soft/melody/compare/v1.3.2...v1.4.0

[v1.3.2]: https://github.com/precision-soft/melody/compare/v1.3.1...v1.3.2

[v1.3.1]: https://github.com/precision-soft/melody/compare/v1.3.0...v1.3.1

[v1.3.0]: https://github.com/precision-soft/melody/compare/v1.2.0...v1.3.0

[v1.2.0]: https://github.com/precision-soft/melody/compare/v1.1.0...v1.2.0

[v1.1.0]: https://github.com/precision-soft/melody/compare/v1.0.1...v1.1.0

[v1.0.1]: https://github.com/precision-soft/melody/compare/v1.0.0...v1.0.1

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/v1.0.0
