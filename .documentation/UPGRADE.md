# UPGRADE

This document records, per release, every change that can require an action from an application already running Melody: what changed, the symptom an upgrader sees, and the remedy. Releases are listed newest first.

It is a companion to [`CHANGELOG.md`](../CHANGELOG.md), not a replacement: the changelog is the exhaustive record of what moved, this document is the short list of what an upgrader has to do about it.

## Versioning policy for breaking changes

Melody releases a behavioural break as a **MINOR**, with the entry marked `**Behavioural change**` in the changelog and listed here with its symptom and remedy. It does not open a new major for one.

The same decision covers a **method added to an exported contract**, which breaks an out-of-tree implementation of that interface at compile time: it ships as a MINOR with a `**Breaking**` note. A new major would put `/v4` into the import path of every file of every consumer — the cost is paid by everyone, including the majority that implements no framework contract — to spare the one consumer that implements it the addition of a single method. That is the same cost already rejected for behavioural breaks, so it is rejected here too.

An upgrader who needs the old behaviour of any entry below pins the previous patch release; the remedies here are the supported path forward.

## Migrating to v3

v1 is feature-frozen: the major is stabilized, no new feature lands on it, and what still arrives, until v4 is released, is patch-level defect fixes and security work. The recommended move for an application on this major is v3, where development continues.

v3 is a separate import path, so an application moves onto it by rewriting its imports rather than by resolving a new version: `github.com/precision-soft/melody` becomes `github.com/precision-soft/melody/v3`, and each integration module gains the same `/v3` suffix on its module path — a package inside such a module then carries the major mid-path, as in `integrations/rueidis/v3/cache`. The one rewrite that does not compile afterwards — twelve deprecated validation constants that v3 has never carried — is recorded with its replacements in [`v3/.documentation/UPGRADE.md`](../v3/.documentation/UPGRADE.md).

From the move on, that document plays this file's role: it records, per v3 release, every change that can require an action, and an upgrader walks its entries newest-first from the release moved onto.

## Unreleased

Every entry below is the consequence of fixing a defect, not a preference: each one describes behaviour that was wrong, and the changelog entry for it names the failure it produced. The release train's two data-loss fixes are in the v3-only `awss3` object storage integration and are recorded in [`v3/.documentation/UPGRADE.md`](../v3/.documentation/UPGRADE.md).

This section covers the changes currently sitting in the `[Unreleased]` block of [`CHANGELOG.md`](../CHANGELOG.md); they ship as a MINOR release.

### Bunorm: the registry refuses new callers while a pool is still closing

**What changed.** `ManagerRegistry.Close` marked the registry closed and then held the registry lock for the whole teardown, closing every manager and every migration database inside the critical section. It now publishes the flag under the lock, takes a snapshot of the two maps, releases the lock, and closes the pools outside it.

**Symptom.** A call to `Manager`, `Database` or `MigrationDatabase` arriving while `Close` is running no longer waits for the teardown to finish; it is refused at once with `ErrManagerRegistryClosed`. Previously such a call parked on the registry lock, and against a peer that had stopped answering — a network partition at shutdown, where the migration connection's write deadlines are deliberately lifted — it could park for as long as the driver waited, so a graceful-shutdown drain expired with goroutines wedged in the registry. Code that relied on that blocking to serialise its last queries behind the teardown now sees the refusal instead.

**Remedy.** None for the ordinary case: the refusal is what the flag has always meant, and every caller already had to handle `ErrManagerRegistryClosed`, which is what the same call answered a moment later anyway. A caller that genuinely needs its work to finish before the pools close must order that itself — run it before `Close`, or gate `Close` behind it — rather than relying on the lock to do the ordering.

### Bunorm mysql and pgsql: a transient marker inside a word is no longer transient

**What changed.** The providers decide whether to retry a failed open by scanning the lowercased error message for a list of markers. The scan matched them as bare substrings, so the short spellings fired inside ordinary identifiers. The markers are now matched as words: a match counts only where the characters on either side are not letters, digits or underscores.

**Symptom.** A permanent failure whose message happens to contain a marker inside a word now fails on the first attempt instead of being retried for the whole budget. The two measured cases are a missing table whose name contains `eof` — `Table 'app.geofences' doesn't exist` — and an unknown column named `session_timeout`; both were retried to exhaustion and then reported as "database connection failed after max retry attempts" rather than as a non-transient failure. Such a boot now fails faster and under the correct classification.

**Remedy.** None is required, and the change is in the safe direction: the failure was permanent in both cases and the retries only delayed the report. The `io.EOF` and `net.Error` checks that run ahead of the message scan are untouched, so a genuine end-of-file or timeout is classified by type as before, and every marker that appears as its own word — `i/o timeout`, `connection refused`, `bad connection`, a bare `EOF` — matches exactly as it did. An operator who wants a permanent failure retried anyway raises the retry budget rather than relying on a substring collision.

### Security: `NewAccessControlRule` now builds a segment-bounded rule

**What changed.** The plain constructor `NewAccessControlRule` built a raw prefix rule that matched across segment boundaries — `NewAccessControlRule("/admin", …)` governed `/administrator` and `/admin-tools` as readily as `/admin/panel`, and being the longest match it shadowed a correctly bounded rule that would have denied. The plain name now builds the segment-bounded rule: `/admin` governs `/admin` and its descendants under a `/` boundary, never a path that merely shares the prefix text. The cross-segment reach moves to the explicit `NewAccessControlRawPrefixRule`; the previous long-named safe constructor `NewAccessControlRuleWithSegmentPrefix` stays as a deprecated alias of `NewAccessControlRule`. The signature of `NewAccessControlRule` is unchanged, so this is a behavioural change, not a compile break.

**Symptom.** A rule declared with `NewAccessControlRule` matches fewer paths than before: a request whose path only shares the prefix text (`/administrator` under a `/admin` rule) is no longer governed by that rule. Where the rule protected such a path and no other rule covers it, the request is now decided by whatever rule does match — a catch-all, or none — which for a protect rule can mean the path is reached under a weaker decision. An empty prefix, previously a catch-all, now refuses at construction.

**Remedy.** A rule that genuinely needs the cross-segment reach — one deliberately governing every path beginning with the text — moves to `NewAccessControlRawPrefixRule` and keeps its old behaviour exactly. A rule that meant a path segment (the common case) needs no change beyond the stricter, intended matching. An empty-prefix catch-all becomes an explicit `"/"` prefix or `NewAccessControlRawPrefixRule("")`. Audit every `NewAccessControlRule` call whose prefix is a bare mount an attacker could extend (`/admin`, `/internal`): under the old raw rule these governed sibling paths by accident, and the bounded rule is what most such rules always meant.

### Bunorm mysql: the provider negotiates verified TLS by default

**What changed.** The mysql provider set no TLS on its connector, so it connected in plaintext and offered no option to enable TLS. It now builds a verifying `tls.Config` by default — the system roots, the configured host as the name to verify against, `MinVersion` TLS 1.2 — the same posture its pgsql sibling already carried, and refuses the driver spellings that would downgrade silently.

**Symptom.** A mysql server that speaks no TLS fails the dial where it previously connected in plaintext. The example's development mysql is such a server.

**Remedy.** A database reached over a trusted network, or one that speaks no TLS, arms `mysql.WithInsecure(true)` on the provider — the deliberate opt-out spelled the same way as pgsql's. A database with a certificate needs no change; one needing a pinned or client certificate passes `mysql.WithTlsConfig`. The example arms the opt-out through a new `MYSQL_INSECURE` switch in its `.env`, mirroring `PGSQL_INSECURE`.

### HTTP: a request path that folds to a different spelling is refused with 400

**What changed.** The kernel now refuses, with `400`, a request whose path is not canonical — one carrying a `..` or `.` segment, or an empty `//` segment — before it is routed or authorized. A trailing slash is not a fold and still routes as before (`/admin/` reaches the `/admin` route). The router matched the path as sent while the access-control matcher folds it, so a request routed to a protected handler under one spelling could be authorized against the folded spelling's rule: `GET /admin/x/../../login` reached a catch-all `/admin` handler while `/login`'s public rule granted it. The refusal closes that by keeping the router, the firewall matchers and the access control reading one spelling.

**Symptom.** A client — typically a non-browser one, since browsers fold before sending — that sends a path containing `..`, `.` or `//` is answered `400 bad request` where the request was previously routed to a handler.

**Remedy.** Send the canonical path: the folded form the `..`/`.`/`//` resolves to is the resource the client meant. Browsers already do this; a hand-written client or a proxy that forwards a raw target should fold the path itself (Go's `net/http.ServeMux` does the same by redirecting). There is no opt-out: the previous behaviour let a request reach a handler under an authorization decision made for a different path, so it is a defect, not a preference.

### Application: a shutdown that leaves request scopes open exits non-zero

**What changed.** After `Shutdown` returns, melody waits — under the same `MELODY_HTTP_SHUTDOWN_TIMEOUT` budget — for every request scope the http kernel opened to close. A drain that does not finish is recorded as `http shutdown left request scopes open`, carrying the count, and `Run` ends non-zero.

**Symptom.** A process that serves **hijacked** connections — a websocket, or any handler that takes the socket itself — now takes up to the shutdown budget to exit and leaves a non-zero status, where it used to exit immediately and report a clean stop.

**Remedy.** Close those connections when the shutdown begins, so the drain has something to succeed at. This major has no shutdown-hook door and keeps its `*http.Server` private, so the mechanism is the context: derive each upgraded connection's lifetime from the same context the application hands `Run`, since that context's cancellation is what starts the shutdown. A hub that stops accepting and closes its clients on that cancellation is the shape this takes. If the wait is unwelcome, `MELODY_HTTP_SHUTDOWN_TIMEOUT` bounds it — but the exit status is the point: `net/http`'s own `Shutdown` does not track a connection a handler hijacked, so the clean stop reported before was one melody had not obtained. The handler was still running, and the container was closing under it.

### Validation: a negative length bound is refused at construction

**What changed.** `validation.NewMinLength` and `validation.NewMaxLength` panic on a negative bound, naming it. The tag door (`validate:"min=-1"`) already refused the same value and is unchanged.

**Symptom.** A call passing a negative bound — almost always a computed value that came out wrong — now fails at construction instead of returning a constraint.

**Remedy.** Clamp the value at the call site if it can legitimately be negative: `max(0, bound)`. What the refusal replaces is worse than a panic in both directions — `NewMinLength(-1)` built a rule that accepted every value in silence, reading as enforced while validating nothing and leaving no record anywhere, and `NewMaxLength(-1)` built one that refused every value including the empty string with `this field must not exceed -1 characters`, which the client was handed.

### CLI: `--format=json` writes one document per line

**What changed.** The json printer no longer indents. Every melody command's `--format=json` envelope — the framework's `debug:*` family, the integrations' `melody:cron:*` and `db:*` — is now one compact line terminated by a newline, where it used to be a block of indented lines. `--format=json-pretty` is the same document with the indentation back.

**Symptom.** Output that was read by eye, or a test asserting the rendered text with the spacing `encoding/json` puts after a colon (`"error": null`), sees the compact spelling instead (`"error":null`). Nothing that decodes the document is affected: it is the same document.

**Remedy.** For reading by hand, use `--format=json-pretty`, or pipe through `| jq`, which the documentation already recommended. For an assertion on rendered output, decode the document and assert the value rather than the text — the format the printer chooses is not part of what the command reports. Consumers that read the stream a document at a time, and every `jq` pipeline, need no change at all; the reason for the change is the consumers that could not work before, since `melody:cron:run` documented a stream of one closed document per line and emitted twenty.

### Session: an injected file handle opened for appending is refused

**What changed.** `session.NewFileStorageFromFile` refuses a handle opened with `O_APPEND`, naming it, and the write itself now names offset zero instead of seeking to it.

**Symptom.** A construction that used to succeed now fails at boot with `session storage file is opened for appending`.

**Remedy.** Open the handle without `os.O_APPEND` — `os.O_RDWR|os.O_CREATE` is what this storage needs — or hand the path to `session.NewFileStorageFromPath` and let it own the file. The refusal replaces a silent corruption: an appending write ignores the offset, so every snapshot landed after the document it was replacing and the truncation then cut the pair to the new length. Measured, a growing snapshot left the file readable and lost every save with no error on any path, and a shrinking one left a document the next boot refused to decode, losing every persisted session.

### Session: a userland `Storage` may now be called concurrently for different sessions

**What changed.** `Manager.SaveSession` and `Manager.DeleteSession` hold a lock keyed to the session id across the storage call instead of one lock for the whole manager. The tombstone check and the write are still one critical section for the SAME session, which is the invariant that stops a logout being undone by a request that loaded the session before it; two requests acting on different sessions no longer wait on each other. Sixteen concurrent saves of distinct sessions against a storage with a 2 ms round trip took 35.5 ms and now take 2.8 ms.

**Symptom.** An implementation of `session/contract.Storage` written against the previous behaviour — one that assumed the manager serialised every call and therefore keeps unsynchronised state of its own — can now be entered concurrently for different session ids. Both storages melody ships took a mutex of their own all along and are unaffected; so is any storage that delegates to a client which is already safe for concurrent use, which redis and database clients are.

**Remedy.** Make the implementation safe for concurrent calls naming different session ids — for most storages this is already true and needs no change. Calls naming the same id are still serialised by the manager, so per-session state needs nothing added.

### Container: the teardown breaks a tie on creation order, not on the service name

**What changed.** What the dependency graph leaves open — two services with no edge between them — is now closed newest-created first, where it used to be closed by node key descending. The declared edges still decide everything they cover; only the tie-break moved. The rule is causal rather than lexical: a service built during the construction of another was needed by it whether or not the edge was declared, and the string comparison it replaces was written by nobody. The container logger, resolved first at boot, is now closed last without anything naming it — measured, a worker keeping 219 bytes of shutdown journal under one name and 90 under another was the defect this closes.

**Symptom.** An application whose services close in an order that happened to be right because of how they were named, and is not expressed as a dependency, may now close them in a different order. The change can only move a service that no edge constrains.

**Remedy.** Express the ordering: resolve what a service needs inside its own provider, or build a `container.Lazy` handle over the resolver the provider was handed, and the edge constrains the teardown exactly as before. A service that legitimately depends on nothing is unaffected by which of the two rules applies.

### Container: a resolution after the teardown finished is refused

**What changed.** Closing now has two states. `IsClosed()` answers true from the moment the teardown begins and refuses every new creation for its whole duration, unchanged. Resolutions of what is already built keep answering until the last `Close` returns — a service reporting its drain from inside its own `Close` is entitled to what it reports through — and answer `container is closed` from then on. They used to answer the instance found in the maps, already closed, with a nil error.

**Symptom.** Code that resolves out of a container or a resolver it kept, after the process has finished tearing down, now receives an error where it received a closed service and no indication of it. A goroutine outliving the teardown is the usual shape.

**Remedy.** Handle the error the way every other resolution failure is handled, or stop the goroutine before the teardown — a service's `Close` is the place to do it, and it may still resolve.

### Application: a teardown that hangs is abandoned and exits non-zero

**What changed.** The normal return of `Run` closes the container through the same ten-second shield the panic path has used since the exit-step budget was installed, and takes exit code 1 when the budget runs out. Previously the clean path had no budget at all: one `Close` that never returned parked every service behind it and the process with them, so the healthy shutdown was the one without an emergency exit while the dying one had a way out.

**Symptom.** A process whose teardown blocks for more than ten seconds now prints one line to stderr naming the abandoned step and exits 1, where it used to hang until the supervisor killed it.

**Remedy.** None required — the exit is the intended outcome. A service whose `Close` legitimately takes longer than ten seconds should bound its own work: the shield abandons the step, it does not shorten it.

### Security: a typed-nil firewall dependency is refused at boot

**What changed.** A firewall's access decision manager, entry point and access denied handler are refused at compilation when they hold a typed nil, by firewall name and naming which configuration declared it; `Builder.SetGlobal` refuses the same three at the door. The matcher, the token source and the login and logout handlers were already refused in the same loop.

**Symptom.** A composition root that declares `var manager *myManager` and hands it over — without ever assigning it — now fails the boot with `security firewall access decision manager is a typed nil`. It used to boot green, drop the declared role hierarchy in silence, and answer every request behind the firewall with a recovered panic.

**Remedy.** Assign the dependency, or pass nothing at all: a plain nil still means "not declared" and falls back to the global configuration exactly as before.

### Container: the teardown orders a service against what it resolved through a resolver it kept

**What changed.** The dependency edge is recorded from the resolver a provider was handed, so a resolution made after the provider returned — a `container.Lazy` handle's first use, a replay through the `ContainerCarrier` pattern — is recorded like one made during construction. Previously no edge was recorded for those at all and the two services were closed in descending name order.

**Symptom.** The order in which two services are closed can change, and it changes toward the documented one: the holder is now closed before what it resolved. An application that had come to depend on the previous accidental order — a service whose `Close` assumed the other had already ended — sees the reverse. A handle built over the **container itself**, rather than over the resolver its provider received, still has no owner and keeps the previous ordering.

**Remedy.** None for the ordinary case, which is the one this repairs: a component that drains through a lazily-resolved handle at `Close` now finds it alive. If a service genuinely must close after another, express it as a dependency by resolving that other service inside its provider.

### Rueidis: the rate limiter reports a store failure by default

**What changed.** `rueidis.RateLimiter` writes a record when the store fails and no error observer was given. `Allow` returns a bool and `Reset` returns nothing, so under the fail-closed default a redis outage refused every call and reached no channel at all — measured against a dead store: no record, no error, no metric, and a successful login that should have cleared an account's lockout left it locked with no trace. The record goes through the request's own logger where there is one and through the emergency logger otherwise, at error for a store failure and warning for the caller's own cancellation, and the error is marked already-logged so the http middleware and listener do not file a second copy.

**Symptom.** A deployment whose redis goes down now sees `rate limiter store failure` records where it previously saw silence — one per call, which during an outage is one per request. That is the point of the change, but it is a volume an operator should know about before it arrives. Nothing else changes: the refusals themselves, the failure mode and the returned values are exactly what they were.

**Remedy.** None required. To route these somewhere of your own instead — a counter, a sampled channel — pass `WithRateLimiterOnError`, which replaces the record rather than adding to it and leaves the error unmarked, restoring the previous behaviour of the http path exactly.

### Rueidis: the counter refusals of the cache backend are named

**What changed.** On an open backend with a valid key, three distinct mistakes by the caller — a delta that overflows when negated, a counter driven past the int64 ceiling, an increment over a payload that is not a canonical number — used to arrive under one message, `cache counter operation failed`, which is also the answer to a genuine store outage. They now carry the same three messages the in-memory backend has always used: `delta overflows int64 when negated`, `cache increment overflow`, `cache value is not a valid int64`. The redis error stays the cause, and a store error matching none of them keeps the generic message.

**Symptom.** Code matching on the text of a counter refusal sees the specific message instead of the generic one. `errors.Is` and `errors.As` over the redis error are unaffected — the cause chain is unchanged.

**Remedy.** Match on the specific message, or better, on the cause. The generic message from here on means what it says: the store failed.

### Bunorm migrate: the held-lock refusal names the resource and the remedy

**What changed.** `<prefix>:migrate` wraps bun's lock error in a melody error naming the manager label, the lock table and the `<prefix>:unlock` command, with bun's error kept as the cause. It previously travelled as bun's own error, carrying no melody context at all.

**Symptom.** Code matching the refusal on the text `already locked` no longer matches at the top of the chain: `Error()` answers `migrate: the migration lock is held; another migration is running, or a crashed one left it behind`.

**Remedy.** Match through the chain — `errors.Is` and `errors.As` reach bun's error exactly as before — or read the `manager`, `locksTable` and `unlockCommand` keys of the context, which is what the wrap exists to provide.

### Bunorm: bun's own diagnostics go to the journal

**What changed.** Opening a connection through the mysql or pgsql provider routes bun's package-level logger into the application's journal, once per process, through the new `bunorm.RouteDiagnostics`. Bun's reports of a declaration mistake — an unknown struct tag option, an unknown `on_update` or `on_delete` rule, a query carrying arguments and no placeholders — arrive as warning records under the message `bun diagnostic` with the line in the context.

**Symptom.** Those lines stop appearing on standard error and start appearing in the journal. An operator or a test grepping standard error for `WARN: bun:` finds nothing.

**Remedy.** Read them from the journal, filtering on the `bun diagnostic` message. One line is deliberately unaffected and stays on standard error: the mysql dialect writes `can't discover MySQL version` through the **standard library's** default logger rather than bun's, so routing it would mean taking `log.SetOutput` for the whole process — every dependency and your own `log` calls with it. That is the application's decision; take it in your composition root if you want it, as the mysql readme shows.

### Bunorm: the `bun` requirement moves to v1.2.17, dialects and drivers in lockstep

**What changed.** Every module of the `bunorm` family — the manager, `mysql`, `pgsql` and the three `migrate` modules — requires `github.com/uptrace/bun v1.2.17` and, where they carry one, `dialect/mysqldialect`, `dialect/pgdialect` or `driver/pgdriver` at the same version. v1.2.16 swallowed the failure of a migration read from a `.sql` file: the deferred `conn.Close()` / `tx.Rollback()` overwrote the exec error with its own nil return, so `db:migrate` printed `[success]`, exited 0 and marked a migration applied that never ran.

**Symptom.** If your application pins a bun dialect or driver of its own, the build now selects `bun v1.2.17` through this dependency while your dialect stays where it was, and the process **panics at init**: `mysqldialect and Bun must have the same version: v1.2.16 != v1.2.17`. The dialect packages check this themselves; it is not a melody rule.

**Remedy.** Move your own `github.com/uptrace/bun/...` requirements to `v1.2.17` in the same change — `go get github.com/uptrace/bun@v1.2.17 github.com/uptrace/bun/dialect/mysqldialect@v1.2.17` and the equivalent for `pgdialect` / `pgdriver`. Applications that declare no bun dependency of their own need no action.

### Security: `NewRoleHierarchyVoter` takes any `Voter` as its delegate

**What changed.** The `delegate` parameter widened from `*RoleVoter` to `securitycontract.Voter`. The wrapper calls nothing but `Supports` and `Vote`, so the narrower type bought nothing and cost an integrator's own voter — multi-tenant, ownership — the ability to see the expanded roles at all; the only way out was copying the wrapper, which meant every foreign voter reimplementing the expansion rule.

**Symptom.** Source-breaking only for a caller that leaned on the narrowing — a variable or field declared as the parameter type, or a function value assigned from the constructor. Every call passing a `*RoleVoter` compiles unchanged, because `*RoleVoter` is a `Voter`.

**Remedy.** Widen the declaration to `securitycontract.Voter`. A caller that genuinely needs the concrete type keeps it at the construction site and passes it in as before.

### Security: a substituted `AccessDecisionManager` must accept the role hierarchy

**What changed.** The compilation hands the declared role hierarchy to the access decision manager through the optional `security.RoleHierarchyAware` capability instead of asserting on the concrete `*security.AccessDecisionManager`. A manager that does not implement the capability **and is handed a hierarchy** is now refused at compilation, naming the firewall and the capability.

**Symptom.** A boot that declared both a role hierarchy and a manager of your own now fails with `security access decision manager cannot apply the declared role hierarchy`. Before, it booted and the hierarchy silently stopped applying on the enforcement path: `ROLE_ADMIN: [ROLE_USER]` was refused with `403` by the access control listener while `security.IsGranted(runtime, "ROLE_USER")` kept answering `true` for the same request, with no record on either side. Nothing changes for the built-in manager, or for any configuration that declares no role hierarchy.

**Remedy.** Implement `WithRoleHierarchy(roleHierarchy *security.RoleHierarchy) securitycontract.AccessDecisionManager` on your manager. A wrapper that delegates hands the hierarchy to the manager it wraps and re-wraps the answer; a manager that builds its own voters wraps the ones that read roles with `security.NewRoleHierarchyVoter`, which now takes any `Voter`. A manager that deliberately ignores hierarchies answers itself — the capability is the declaration that the omission is intentional.

### Application: three framework services became substitutable

**What changed.** `serializer.ServiceSerializerManager`, `validation.ServiceValidator` and `http.ServiceUrlGenerator` are registered behind a `Has` gate, like the logger, the cache, the session and the firewall manager. `serializer.ServiceSerializer` — the default serializer, the id the two published `Serializer*FromRuntime` resolvers read — is registered for the first time, behind the same gate.

**Symptom.** None for an application that registers none of these ids. An application or module that registered one of them used to die at boot with `duplicate registrations detected at boot`, exit code 1; it now substitutes the framework's registration, which is what the collision was standing in the way of. If your code registered `service.serializer` to make the documented resolvers answer, that registration still wins — the gate defers to it.

**Remedy.** None required. To add a media type to content negotiation, register `serializer.ServiceSerializerManager` with a manager built by `serializer.NewSerializerManager` carrying the wider map. Note that registering `serializer.ServiceSerializer` changes what the two resolvers answer, not what a request is served: negotiation reads the manager.

### Http: `Kernel` gains `SetMethodPolicy`

**What changed.** `http/contract.Kernel` declares `SetMethodPolicy(policy MethodPolicy)`, and `MethodPolicy` moved to `http/contract` beside the two policies already there, with an alias keeping the name `melodyhttp.MethodPolicy` valid. The type was documented, built by `DefaultKernelOptions()` and read on every request, with no door to hand one to.

**Symptom.** An out-of-tree implementation of `http/contract.Kernel` — a wrapper or a test double — stops compiling: `does not implement …Kernel (missing method SetMethodPolicy)`.

**Remedy.** Add the method. A wrapper delegates it to the kernel it wraps; a double that does not care about the policy takes the argument and does nothing, as it already does for `SetForwardedHeadersPolicy`. Applications that do not implement the interface are unaffected, and the shipped defaults are unchanged.

### Http: `Router.AllowedMethods` honours the path's locale

**What changed.** The door filters routes by locale exactly as the matcher does, so a route restricted to a set of locales contributes nothing for a path carrying another one.

**Symptom.** A `405` handler of your own that builds its `Allow` header from this door stops advertising methods that in fact answer `405`. Nothing changes for a routing table with no locale-restricted routes, which is every table that never passes `locales` to `NewRouteOptions`.

**Remedy.** None. If you were compensating for the old answer by filtering the list yourself, the compensation is now redundant. Note that the kernel adds the synthetic `OPTIONS` and `HEAD` its `MethodPolicy` allows on top of this set when it writes the header itself.

### Cron: a relative path from a parameter is anchored at the project directory

**What changed.** In `melody:cron:generate`, a relative path read from a parameter — `melody.cron.destination_file`, `melody.cron.logs_dir`, `melody.cron.heartbeat_path` — is resolved against `%kernel.project_dir%`, the way `MELODY_LOG_PATH`, `kernel.logs_dir` and `kernel.cache_dir` have always been. A relative path passed as a cli flag still resolves against the working directory.

**Symptom.** A deployment whose cron parameters carry relative paths and whose generator runs from a directory other than the project root writes its crontab and its log directory elsewhere than before — where they were always meant to go. The shipped defaults carry `%kernel.project_dir%` already and are unaffected, as are absolute paths and every flag.

**Remedy.** None to apply if the working directory was the project directory, which is the ordinary case. Otherwise either accept the new location or write the parameter as an absolute path — a value already absolute is never re-anchored. A configuration whose `Kernel()` names no project directory keeps the previous behaviour.

### Http/static: in the embedded mode the cache validators change shape

**What changed.** An `embed.FS` reports the zero instant for every file it holds, so the entity tag used to be built out of that constant — degenerating into size alone, identical across every rebuild — and `Last-Modified` was rendered as `Mon, 01 Jan 0001 00:00:00 GMT`. The tag of an undated file is now derived from its size and `version.BuildVersion()`, and `Last-Modified` is not emitted at all when the filesystem carries no time. Nothing changes for the filesystem mode, where files have real modification times.

**Symptom.** After the upgrade, every embedded asset revalidates once: the tag it carried no longer matches the one caches hold. Responses that used to be `304` are `200` for one round per asset per client. A conditional request carrying `If-Modified-Since` and no `If-None-Match` is now answered `200` rather than `304` — that request used to be answered `304` permanently, because the zero instant is never later than anything, so an asset was served stale for the life of the deployment.

**Remedy.** None to apply, but link the version: a build without `-ldflags` setting `version.buildVersion` produces one version string for every build, and with it the stale-asset behaviour returns. Where per-file precision matters more than the one revalidation per deploy, put a content hash in the asset url.

### Http/static: an embedded public directory that does not exist refuses the boot

**What changed.** In the embedded mode `NewFileServer` proves the configured public directory against the embedded filesystem and panics by name — "the public directory is not present in the embedded file system" — when it is absent or is not a directory.

**Symptom.** A release build whose `MELODY_PUBLIC_DIR` names a directory the `//go:embed` directive did not pack now fails at construction. It used to boot cleanly and answer `404` for every asset in the binary, which is the failure this refusal replaces.

**Remedy.** Align the key with what the build embeds, or leave it unset to serve from the root of the embedded filesystem. The key is deliberately not ignored in this mode: the join with the public directory is what confines a stripped prefix to it.

### Cron: `Configuration.Entries` hands out copies

**What changed.** `Entries` copies all the way down — the list, each `*ScheduledCommand` and each `*EntryConfig` behind it, schedule included. Each entry the generator expands also carries its own `*Schedule`.

**Symptom.** Code that reconfigured a registration by writing through what `Entries` returned — `configuration.Entries()[0].Config.Schedule.Hour = "23"` — no longer changes anything. A `Template.Render` implementation that calls the mutating `Schedule.Defaults()` on the schedule it was handed no longer rewrites the registered one for the rest of the process, which is what it used to do.

**Remedy.** Register the entry with the configuration it should have. `Entries` is an inspector; the registry is written through `Schedule`.

### Debug: the `--build` sweep exits non-zero over the services it could not build

**What changed.** `debug:container --build` reports its failures on the envelope, so `Render` answers a non-zero exit for them: the envelope carries `debug.buildFailed`, the number of failures, the names, and the first failure as its cause. The single-name door (`debug:container app.repository.order`) and `debug:middleware --build` have reported theirs all along; the sweep did not.

**Symptom.** A deploy step spelled `app debug:container --build || exit 1`, or any script reading the exit status of the sweep, now fails on an application with a service that does not resolve. It used to pass, with the failures visible only inside `data.items[].error`.

**Remedy.** None if the gate was meant to fail — that is what it was written for. If a pipeline runs the sweep for information rather than as a gate, read the document and ignore the status, or drop `--build` and list without resolving.

### Debug: two json fields keep one type across every row

**What changed.** In the `debug:container` json document, `errorCauseChain` is always an array — empty on a service that resolved — and `errorContextJson` is always a parseable json string, `{}` where there is nothing to report. The table views are unchanged: an empty cell stays empty.

**Symptom.** A consumer that tested `errorCauseChain == null` to detect a healthy row no longer finds a null, and `errorContextJson` is `"{}"` rather than `""`.

**Remedy.** Test the length instead of the null: `.errorCauseChain | length == 0`. `jq '.data.items[].errorCauseChain[]'` and `.errorContextJson | fromjson`, which used to die at the first healthy row, now work over the whole listing.

### Debug: `debug:events --format=json` declares the serving-process listeners at every verbosity

**What changed.** `data.servingProcessListeners` is present at the default verbosity, beside the listing, which stays exactly where it was on `data.items`. The declaration was previously carried only under `--verbose`, where it sits inside a reparented payload. `--order` now also reaches the listener detail: it orders the events, while the rows inside one event keep the dispatch order their `order` field reports.

**Symptom.** The default-verbosity document grows one key when the composition root declares listeners it wires only in the serving process. Under `--order=desc --verbose`, `data.listeners` is grouped by event in descending order rather than ascending.

**Remedy.** None for a consumer reading `data.items`. A consumer that pinned the exact key set of `data` should accept the added key — it is the answer to "is access control wired?", which an absence could not give.

### Debug: `debug:router --format=json` degrades an unserializable route attribute instead of emptying the document

**What changed.** A route attribute whose value the encoder cannot represent — a closure hung on a route by application code — is rendered as its `%v` text instead of failing the whole envelope. Every value that can be represented keeps its json type, so a `[]string` attribute is still a list.

**Symptom.** A command that answered zero bytes and a marshalling error now answers its document. An attribute that used to break the command appears as a string.

**Remedy.** None. If a value should be readable rather than an address, give the route a serializable attribute.

### Debug: `debug:middleware` items always carry `reason`

**What changed.** The `reason` field is present on every item, empty where there is nothing to say. It used to be omitted from every active row.

**Symptom.** The active rows of `data.items` grow a `"reason": ""` field.

**Remedy.** None. A consumer that keyed on the presence of `reason` to detect an inactive middleware should read `status` instead, which has always carried `active`, `inactive` or `built`.

### Serializer: a refusal of one type is not a refusal of the negotiation

**What changed.** `Accept: application/json;q=0` against a manager that also holds `text/plain` is answered with plain text rather than `406 Not Acceptable`. A type covered by a `q=0` range is recorded as refused and can never be served — by the negotiation or by the fallback, which steps past json when json is what was refused — and `ErrNotAcceptable` is answered only when every registered type is refused.

**Symptom.** A request whose accept header names one type it does not want, against an application with more than one serializer registered, receives a representation where it used to receive a 406.

**Remedy.** None: this is what the manager's own comment and `SERIALIZER.md` promised. A client that wants nothing at all still gets its 406 by refusing everything — `*/*;q=0`.

### Migrate: the json document is not shaped by `--verbose`, and its keys are stable

**What changed.** Under `--format=json`, `db:migrate`, `db:rollback`, `db:status`, `db:init` and `db:unlock` collect every block at any verbosity: verbosity remains a rendering decision about the plain text alone, which is what the readme always said. The document keys are now stable names rather than display headings — `data.migrations.applied`, `.pending`, `.rolledBack` — and `data.database.database` is json `null` when the connection reports no current database, where it used to be the rendered string `"<null>"`. The text blocks keep their headings and their `<null>`.

**Symptom.** `db:migrate --format=json` without `-v` answers a populated `data` where it answered `{}`. A consumer reading `data.migrations["APPLIED MIGRATIONS"]` or `data.migrations.APPLIED` finds nothing under those keys. A json run performs the database-identity query that a text run performs only under `--verbose`.

**Remedy.** Read `data.migrations.applied`, `data.migrations.pending` and `data.migrations.rolledBack`; test `data.database.database` for null rather than for the string `"<null>"`. Nothing needs `--verbose` any more to fill the document.

### Cron: the generator answers a document on every outcome, and a job's output goes to the journal

**What changed.** `melody:cron:generate --format=json` renders one envelope whatever happened: a failure travels as `error.code = "cron.generateFailed"` with its message as the cause, and `data.writes` names the destinations already written before it stopped. Under the in-process runner, a scheduled command's own output no longer reaches the process stdout — it is captured and filed as one record per run that printed anything, naming the command and the run id, capped at 64 KiB.

**Symptom.** A pipeline that read an empty stream from a failed generation now reads a document with an error in it, and the command still exits non-zero. Anything tailing the stdout of `melody:cron:run` for a job's own printed output finds it in the log instead, under `cron: scheduled command output`.

**Remedy.** Read `error` in the generator's document rather than inferring failure from an empty stream. For the runner, read the journal; a job that must write to a stream of its own should open it itself rather than relying on the command writer.

### Validation: a rule-declaration fault is an error, and its context stays out of the response

**What changed.** Three validation codes name a mistake in the DECLARATION rather than in the submitted value: `unknownRule`, `invalidRuleSyntax` and `invalidPattern`. A 400 carrying any of them is now recorded at **error** instead of the warning a deliberate 4xx earns, and the internal context of those entries — `rule`, `params`, `cause` — is stripped from the response body while staying in full in the record. Every other validation error is unchanged in both places, so the bounds a numeric constraint reports still reach the client. The refusal itself is still a field error with the same status and the same code: the validator continues to fail closed on a rule it cannot honour rather than passing the value.

**Symptom.** A struct tag naming a rule that does not exist — `validate:"reqiured"`, a `regex` written without parameters, a pattern that does not compile — starts producing `error` records instead of `warning` ones. A client parsing the 400 body no longer finds a `context` object on those entries; it still finds `field`, `message` and `code`.

**Remedy.** Read the new error records: each one names the route and the misspelled rule, and the route it names has been refusing every request it serves. A client that read `errors[].context` for a wiring code was reading the developer's own typo and should read `code` instead.

### Http: the end of a session is a warning, not a storage outage

**What changed.** When another request ends a session while this one is running, the response path records it at **warning** under its own name. The two genuine failures on that path — a save that could not land and a delete that could not land — keep the error level. All of them now carry the session id, the request method and the path.

**Symptom.** `session was deleted while the request was in flight` moves from `error` to `warning`. An alert counting session errors will see its volume drop, by exactly the traffic that was never a failure: a user logging out in one tab produced one of these per concurrent request in the others.

**Remedy.** If an alert was tuned around that volume, retune it; the records it was counting were the session ending, which `SESSION.md` and `HTTP.md` both describe as the normal outcome.

### Cache and cron: a recovered panic carries its cause

**What changed.** The recovery boundaries of `cache.Remember`, of the cron runner and of the bunorm manager registry hand the panic value on as the CAUSE of the error they fabricate, and capture the stack of the goroutine that raised it. The context keys they already wrote — `panic`, `panicValue` — are unchanged; `panicStack` is added beside them.

**Symptom.** `errors.Is` and `errors.As` on the returned error now reach the failure underneath, where before they stopped at the fabricated wrapper. Code that relied on those calls answering false for a panicked callback will now see them answer true.

**Remedy.** None for a reader that only renders the error. A caller that branches on `errors.Is` against a sentinel it also uses for non-panic failures should check whether it means to treat a panicked callback the same way; the message still says the boundary was a panic.

### Security: a 403 names its branch and files one record

**What changed.** Every refusal the access decision manager produces carries a `reason` — one of the exported `security.RefusalReason*` constants — beside the strategy and the attribute, in the exception context the response never renders. The access control listener files exactly one record for a refusal, naming the reason, the firewall and the matched rule: at warning for a denial, and at error only for `no_voter_supports_attribute`, the wiring fault in which a firewall names an attribute no configured voter looks at. The record is filed on whichever exit the path takes, an access denied handler answering and returning early included, and the error the `kernel.exception` dispatch carries is marked as already logged. `DecideAny` names an empty attribute list as its refusal reason rather than falling through its loop to a generic `forbidden`; it refused that input before this change too, so what is new there is the attribution, not the verdict.

**Symptom.** A 403 leaves an `authorization refused` warning where it used to leave only the exception listener's generic `unhandled exception`; a firewall whose attribute nothing votes on is now the one refusal filed at error, so it surfaces as a wiring fault rather than hiding among ordinary denials. An application calling `DecideAny(token, nil, subject)` directly is refused as it was before, but the refusal now carries `empty_attribute_list` where it used to be an unattributed `forbidden`.

**Remedy.** Point alerting at `reason` rather than at the count of 403s, and fix any firewall that starts filing `no_voter_supports_attribute`: the attribute it names reaches no voter, and every request behind it is being refused for a reason no client can repair. A direct `DecideAny` call with no attributes was already being refused before this change, so nothing an application relied on has flipped — but the refusal is now attributable, and the fix is the same as it always was: pass the attributes.

### Httpclient: `SetHeader` stores under the canonical spelling

**What changed.** `SetHeader` canonicalizes the key the way the constructor does, so a header rotated under a spelling other than the configured one overwrites the entry it means to instead of adding a second live one.

**Symptom.** A client configured with `X-Api-Key` and rotated through `SetHeader("x-api-key", …)` now sends the rotated value on every request. It used to send a per-request coin flip between the two, decided by map iteration order — measured at 166 of 200 requests carrying one value and 34 the other. A caller that read the header map expecting its own spelling back reads the canonical one.

**Remedy.** None for the rotation itself, which is now correct. If code round-trips through the map by raw key, spell the key canonically.

### Http: a panic in the request setup answers 500 instead of resetting the connection

**What changed.** A last recovery guard covers the window the main one cannot — everything between the request scope opening and the main guard's own installation (the request logger, the request context override, the config and dispatcher resolutions, the routing), plus a panic raised inside the main guard after it has already recovered once. It files one record carrying the method, the path and the stack, and answers a rendered 500 while the scope is still open. `nethttp.ErrAbortHandler` still travels by identity and still drops the connection with no response.

**Symptom.** A wiring fault that used to reset the connection with nothing in the journal now answers 500 and leaves an `unhandled http error before the kernel recovery guard` record. There is still no terminate dispatch for it, hence no access-log line, and no `kernel.exception` dispatch, hence no application error page — the record is that line's degraded substitute.

**Remedy.** None. A test asserting that `ServeHttp` panics on a broken request setup must assert the 500 instead.

### Session: the contract gains an atomic Snapshot

**What changed.** `session/contract.Session` carries `Snapshot() (values map[string]any, modified bool, cleared bool)` — the three answers read under one lock acquisition — and the response path decides between deleting and saving through it. Reading the flags and the values through the individual accessors let a `Clear` racing the response land between the reads and write the pre-logout state back under a live id.

**Symptom.** An out-of-tree implementation of the `Session` contract stops compiling with "missing method Snapshot".

**Remedy.** Implement `Snapshot` as one critical section over the same three answers the accessors give; a single-threaded implementation can simply return `instance.All(), instance.IsModified(), instance.IsCleared()`.

### Remember: the computing call answers the stored shape

**What changed.** `Remember` over a `cache.Manager` passes the callback's value through one local serializer round-trip before returning it, so the miss and every later hit answer the same shape.

**Symptom.** A caller that type-asserted the miss return to the callback's own type — `value.(int)`, `value.(MyStruct)` — sees the assertion fail now on every call instead of only from the second call on: the value is the decoded generic form (`float64`, `map[string]any`), exactly what the hits always answered.

**Remedy.** Decode the returned value the way the hit path always required — or, where the concrete type matters, re-fetch through a typed unmarshal of your own. The change turns a warm-path-only failure into a deterministic one, which is the fix.

### Httpclient: two spellings of one header are refused

**What changed.** Client-config and request-option header maps are canonicalized, and a map carrying two spellings that collapse onto one header — `x-api-key` beside `X-Api-Key` — is refused with a named panic at construction.

**Symptom.** A configuration that carried both spellings — and was silently sending a per-request coin flip of the two values until now — fails at the constructor naming the collision.

**Remedy.** Keep one spelling. Sequential `SetHeader` calls stay legal and last-write-wins on the canonical key.

### Http: an out-of-range response status code answers 500

**What changed.** A response status code outside `[100, 999]` is refused by name in the write path and the client receives the rendered 500; the header-commit flag is raised only after the delegate returns.

**Symptom.** A handler bug that produced such a code — previously answered as an implicit empty 200 with an unrelated-looking panic in the log — now answers 500, with a log record naming the invalid code.

**Remedy.** None: fix the handler the record now points at.

### Remember: an option assembled from the zero value reads unset fields as the defaults

**What changed.** A `With` setter called on the exact zero-value `RememberOption` first reads the receiver as `NewDefaultRememberOption`, then applies its own field. Until now a zero option plus one setter carried `waitTimeout: 0`, so every cache miss answered "cache remember callback timed out" while the callback computed in the background, and setting cancelability alone silently disabled the stampede protection.

**Symptom.** An option built as `(&RememberOption{}).WithStampedeProtectionEnabled(true)` now waits for the leader (the constructor's `-1`) instead of timing out instantly; one built with `WithCancelable(true)` alone keeps the protection on.

**Remedy.** Nothing for the ordinary caller — the constructor path is unchanged. A caller who genuinely wants no waiting spells it `NewDefaultRememberOption().WithWaitTimeout(0)`, and one who wants the protection off spells it through the constructor too.

### Container: a failing MustGet panics with the original error

**What changed.** `MustGet` and `MustGetByType` — on the container and the resolver context — panic with the melody error the resolution produced, enriched in place with the service name or type, instead of wrapping it in a fresh `"failed to get service instance"` error. The wrapper shed the log level, the already-logged mark and the capture stack, so one logged provider failure filed a second record at the recovery site.

**Symptom.** Code matching the panic value's message against `"failed to get service instance"`/`"failed to get service instance by type"` no longer matches; the message is the cause's own (for a missing registration, `"service is not registered"`/`"service type is not registered"`) and the name or type sits in the error context.

**Remedy.** Match on the original failure or read the context keys `serviceName`/`type`; a foreign (non-melody) cause still arrives wrapped under the old message.

### Rate limit: a nil user-id callback is refused at construction

**What changed.** `UserRateLimit` and `UserRateLimitWithResolver` panic at construction when `getUserId` is nil, the way `RateLimitMiddleware` refuses its missing limiter. Accepted, the nil callback panicked on every request at serve time.

**Symptom.** A wiring that passed nil — perhaps reading the anonymous-fallback GoDoc as "nil means always anonymous" — now fails at boot with `get user id callback is required for user rate limit middleware`.

**Remedy.** Pass a callback; for a purely address-keyed budget use `IpRateLimit`/`IpRateLimitWithResolver`, which is what nil was being used to mean.

### CORS: one default header list, and an empty list stays a preference

**What changed.** `cors.NewService` reads a nil `AllowMethods`/`AllowHeaders` as the single default `DefaultService` grants — `Authorization` included — and keeps an explicitly empty list as the expressed preference, the reading `AllowOrigins` always had.

**Symptom.** A config that set only origins now advertises `Authorization` on preflight (previously the fallback list lacked it, so an SPA sending it broke the moment origins were narrowed). A config that passed `[]string{}` for methods or headers now advertises none where it silently got the defaults.

**Remedy.** A deployment that relied on the empty-slice-means-default reading passes nil (or omits the field); one that must not advertise `Authorization` names its header list explicitly.

### Session: a sub-second positive ttl is refused by the manual constructor too

**What changed.** `session.NewManager` and its siblings refuse a positive ttl below one second, the refusal `MELODY_HTTP_SESSION_TTL` validation has always given: such a lifetime stores no usable session — `SaveSession` reports success and the entry lapses before the client returns.

**Symptom.** A hand-wired manager built with, say, `500*time.Millisecond` now panics at construction naming the rule; zero keeps meaning no expiry.

**Remedy.** Use a lifetime of at least one second, or zero for no expiry.

### Static files: an explicit cache max age of zero means always revalidate

**What changed.** With the cache enabled, a max age of `0` — from `MELODY_STATIC_CACHE_MAX_AGE=0` or passed to `NewFileServerConfig` — now ships `Cache-Control: public, max-age=0` with the ETag/Last-Modified machinery intact. It used to be silently coerced to `3600`, an hour of freshness for the operator who asked for none; only a negative value now reads as unset and takes the default.

**Symptom.** Deployments that set `0` expecting the default start serving `max-age=0`; clients revalidate every request (mostly answered `304`).

**Remedy.** A deployment that wants the hour says `3600`; the value now means what it reads like.

### Cron: a module with runner commands and no configuration refuses the boot

**What changed.** `cron.NewModule`'s `RegisterCliCommands` panics when `RunnerCommands` are supplied without a `Configuration`/`ConfigurationFactory`, and when a factory returns nil. Until now the module silently registered nothing and the wiring error surfaced as "unknown command" at invocation.

**Symptom.** A wiring that carried runner commands but never set the configuration now fails at boot naming the missing configuration.

**Remedy.** Set `Configuration` (or a factory that returns one); a parameters-only module — no runner commands, no generator — keeps working without one.

### Cache: the key grammar is the contract's, on both backends

**What changed.** `cachecontract.Backend` now states the key grammar — non-empty, no spaces, no newlines, at most 1024 bytes — and the in-memory backend enforces it with the same refusals the redis backend always answered. The two implementations of one promise refused different keys.

**Symptom.** A key carrying a space or a newline — typically built from user input — now fails every operation against the in-memory backend with `cache key contains spaces`/`cache key contains newlines`, where it silently worked in development and failed only in production.

**Remedy.** Sanitize the key at the call site (hash it, or strip the whitespace). The refusal names the key.

### pgsql: every driver deadline is named, configured and lifted for migrations

**What changed.** `pgsql.TimeoutConfig` carries `ReadTimeout` and `WriteTimeout` beside `ConnectTimeout`, the connector receives all three (the dial included), and the provider implements `bunorm.MigrationProvider`. Until now the dial ran under pgdriver's internal 5s default whatever `ConnectTimeout` said, every query ran under invisible 10s read / 5s write deadlines, and `db:migrate` ran on the request pool — an 11-second DDL statement died at 10.004s, measured.

**Symptom.** `pgsql.NewTimeoutConfig(connect)` no longer compiles — the constructor takes the three durations, the mysql signature. Behaviourally, the effective read/write deadlines move from 10s/5s to the documented 30s/30s.

**Remedy.** `NewTimeoutConfig(connect, 0, 0)` keeps the connect timeout and takes the 30s/30s defaults; name tighter deadlines where request traffic needs them. Migrations need nothing: `db:migrate` now prefers the dedicated lifted connection automatically.

### rueidis: a wrong-typed credential refuses the boot, an empty prefix refuses the wipe, a closed backend refuses the call

**What changed.** The provider reads user and password through `MustString`, so a wrong-typed credential panics at boot naming the parameter instead of silently connecting with no credential at all. `ClearByPrefixCtx("")` is refused like the empty key everywhere else, instead of wiping the whole namespace. `Backend.Close`/`BackendService.Close` mark the backend closed and later operations refuse — the in-memory backend's answer — while the shared client stays open.

**Symptom.** A boot that connected with a silently-empty password now panics `cannot convert parameter value to string` naming the parameter. A caller using `ClearByPrefix("")` as a synonym of `Clear` now receives `cache key is empty`. An operation after `Close` now receives `cache backend is closed`.

**Remedy.** Register the credential as a string; call `Clear` where the whole namespace is meant; order teardown so nothing uses a backend after closing it — the wrapped client, if shared, is unaffected.

### cron: an entry routed to another crontab file refuses the in-process runner

**What changed.** `EntryConfig.DestinationFile` joins `Command` and `Instances` in `NewRunnerCommand`'s construction refusal: an entry routed to another crontab addresses an external scheduler, and accepted by the runner as well it executed twice whenever the generated manifests were live.

**Symptom.** A boot that used to succeed panics with `cron: the in-process runner supports only name-scheduled single-instance entries; the entry routes to another crontab file`.

**Remedy.** Keep the routed entry out of the runner's `Configuration` (schedule it only for the generator), or drop its `DestinationFile` if in-process execution is the intent.

### Security: a typed nil is refused where a nil is refused

**What changed.** An interface holding a nil pointer is not equal to nil, so a typed-nil matcher, token source, login handler, logout handler, rule, authenticator, decision voter or wrapped token passed the eager validation at the definition site, passed `Compile`, and was first dereferenced on the request path — inside no recovery, taking the process down on a wiring mistake that had two chances to be caught at boot. Both validation walls now use the reflective check the rest of the framework uses. A stateless firewall handed a typed-nil login handler reads it as the absent handler it is, rather than refusing the firewall as contradictory.

**Symptom.** A boot that used to succeed now panics with the piece and the firewall named — `security firewall matcher is nil`, `security firewall token source is nil`, `security firewall login handler is nil`, `security firewall logout handler is nil`, `security firewall rule is nil`, `authenticator at index N is nil`, `security voter is nil`, or `can not create a security token from nil`.

**Remedy.** The panic names the piece that is nil. A variable declared as a concrete pointer type and never assigned — `var handler *MyLoginHandler` — is the usual source; assign it, or pass an untyped `nil` where the piece is genuinely optional.

### Security: `DecideAll` refuses an empty attribute list

**What changed.** `AccessDecisionManager.DecideAll` read an empty attribute list as an AND over nothing and granted it, while `DecideAny` on the same contract refused the identical input. A caller that asked for no attribute — a list a configuration value resolved away, or a variadic call that lost its argument — was granted. The compiled access control cannot produce an empty list, so the change is reached only by a caller going straight to the decision manager.

**Symptom.** `DecideAll(token, nil, subject)` and `DecideAll(token, []string{}, subject)` return a `403` error where they returned nil.

**Remedy.** Pass the attributes the decision is about. A call site that legitimately means "no authorization required" should not consult the decision manager at all.

### Internal conversions: a duration spelled as a bare integer is refused

**What changed.** `internal.Duration` — reached through `config.Parameter.Duration()` and `bag.Duration` — read a bare `int`/`int64` as **nanoseconds**. A runtime parameter registered as `30` and meant as seconds became a timeout that fired instantly, with no error anywhere, while the same value spelled `"30"` was already refused for missing its unit. A bare integer is now refused on both paths.

**Symptom.** A `Duration()` conversion over a numeric value fails with `parameter is not a valid 'duration'` and the cause `a bare integer carries no unit`.

**Remedy.** Register the value as a `time.Duration`, or spell it as a string with a unit — `"30s"`.

### Internal conversions: a non-finite float is refused

**What changed.** `internal.Float64` — reached through `config.Parameter.Float()` and `bag.Float64` — accepted `"NaN"`, `"Inf"` and `"Infinity"` (which `strconv.ParseFloat` parses without an error) and passed a typed non-finite `float64`/`float32` through untouched. Every ordered comparison against NaN is false, so a threshold guard written the normal way silently stopped guarding. Non-finite values are refused on every branch now.

**Symptom.** A float conversion over `NaN`/`Inf` (spelled or typed) fails with `parameter is not a valid 'float64'` and the cause `value is not finite`.

**Remedy.** Fix the configuration value; a parameter that legitimately needs "unbounded" spells it as a sentinel the application defines, not as an infinity.

### Bag: a typed-nil `map[string]string` reads as absent

**What changed.** `bag.StringMapStringString` reported an interface holding a nil `map[string]string` as **present**, answering an empty non-nil map, where the strict accessors beside it answer absent for a nil value. It now reports absent.

**Symptom.** A caller branching on the presence flag receives `false` for a nil map and applies its default, where it used to receive an empty map.

**Remedy.** None for the common case — the default branch is almost always what was meant. A caller that deliberately stored a nil map to mean "present and empty" stores an empty map instead.

### Clock: `FrozenClock.Advance` refuses a negative duration

**What changed.** `Advance` accepted a negative duration and silently moved the frozen clock backwards, breaking the monotonic invariants the code under test relies on. It now panics with `invalid advance duration`; zero remains a no-op.

**Symptom.** A test advancing by a negative duration panics at the call site.

**Remedy.** Use `TravelTo`, the deliberate door for backwards motion.

### Internal test helper: `AssertPanics` is removed

**What changed.** `internal/testhelper.AssertPanics` passed on any recovered value, so it could not tell the guard under test firing from the code crashing for an unrelated reason; it had no callers on any major. `AssertPanicsWithError` — which pins the panic's identity by message — is the remaining form.

**Symptom.** A consumer of the internal helper fails to compile. The `internal` package documentation marks its APIs as free to change without notice.

**Remedy.** Use `AssertPanicsWithError` with the guard's message.

### Httpclient: a client with a base url refuses an absolute url that leaves its origin

**What changed.** `HttpClient` applied its base url only to a relative target; an absolute one bypassed it silently and travelled with the headers and the authorization the client was configured with. A client built with a base url now refuses an absolute url whose scheme, host or effective port differ from that base url. A client built without a base url — `NewDefaultHttpClient`, or `NewHttpClientConfig("", …)` — is unchanged and reaches any host.

**Symptom.** A call passing an absolute url to a client configured with a base url fails with `the absolute url leaves the origin of the configured base url`, naming both origins.

**Remedy.** Pass the path and let the base url supply the origin, or build a second client — one without a base url — for the calls that go elsewhere. A client that carries credentials should not be the one that talks to arbitrary hosts.

### Httpclient: a basic credential with an empty username is sent instead of dropped

**What changed.** `WithBasicAuth` attached the credential only when the username was non-empty, so `WithBasicAuth("", key)` sent an unauthenticated request with no error. The credential now travels whenever it was asked for, empty halves included. A bearer token still wins over a basic credential when both are set.

**Symptom.** A request that previously went out with no `Authorization` header now carries `Basic` with an empty user. An endpoint that answered anonymously will now see an authenticated caller.

**Remedy.** None if the credential was meant to be sent — that is the fix. A caller that deliberately relied on the credential being dropped stops setting it.

### Httpclient: an empty request target names the base resource rather than its trailing slash

**What changed.** The join between the base url and the target appended a slash unconditionally, so a client based at `https://host/v1` could not request `/v1`. An empty target now names the base resource with nothing appended; an explicit `"/"` still names the trailing slash. The base url remains a prefix — `https://host/v1` plus `/users` is `https://host/v1/users` — deliberately unlike the RFC 3986 reference resolution Symfony and Guzzle implement.

**Symptom.** A call passing `""` as the target now requests the base url without a trailing slash.

**Remedy.** Pass `"/"` where the trailing slash was wanted.

### Httpclient: a nil request option, and a nil client configuration, are refused where the mistake is made

**What changed.** A nil `RequestOption` in the variadic list was invoked and panicked with a nil function call on the request path; it is now refused with an error naming its position. `NewHttpClient(nil)` dereferenced its argument; it now panics naming the argument, as the sibling constructors of this framework do.

**Symptom.** An option built by a condition whose other branch yields nothing now produces `nil request option` instead of a panic. A nil configuration fails at the construction line with `http client configuration is nil`.

**Remedy.** Leave the option out rather than passing nil, and use `NewDefaultHttpClient` where the defaults were what was wanted.

### Httpclient: the response body cap is checked before the request is sent, and applies to streams the caller capped

**What changed.** A non-positive `WithMaxResponseBodyBytes` was reported only after the exchange had happened, so the request — and its side effect — had already reached the server. It is now refused before anything is dialled. On the streaming path the option was ignored entirely; a cap the caller names is now enforced there too, while a stream with no cap named stays unbounded, because the default is sized for a body held whole in memory.

**Symptom.** A caller passing a computed cap that reached zero now gets `invalid max response body bytes` without the request having been sent. A streaming caller who set a cap now sees `response body exceeded max size` at the cap instead of receiving the whole body.

**Remedy.** None where the cap was meant to hold. A streaming caller that set a cap without wanting one stops setting it.

### Httpclient: a negative per-request timeout bounds a stream instead of unbounding it

**What changed.** `RequestStream` treated only zero as "no explicit timeout" and passed a negative duration through to a client with no deadline at all. A non-positive duration now falls back to the client's configured timeout on both paths.

**Symptom.** A stream started with a negative `WithTimeout` — the shape produced by computing what is left of a deadline that has already passed — is now cut at the client timeout instead of running indefinitely.

**Remedy.** Check the remaining budget before the call, and pass no timeout where an unbounded stream is what is wanted.

### Exception: `NewExitError` and the http exception constructors refuse a value the process boundary betrays

**What changed.** `NewExitError` panics on an exit code outside `[1, 255]`: `os.Exit` hands the code to the operating system, which keeps its low 8 bits, so 256 reported success from a dying process, a negative read as 255, and 0 contradicted the error the constructor requires. `NewHttpException` and `NewHttpExceptionWithCause` panic on a status code outside `[100, 599]`: `net/http`'s `WriteHeader` panics below 100 and above 999 deep in the response path, and a status the writer clamps to 200 served an exception as success.

**Symptom.** A call constructing an exit error with a computed code that leaves `[1, 255]`, or an http exception with a status that leaves `[100, 599]`, panics at construction instead of failing later — or never visibly.

**Remedy.** Clamp or validate the computed value before constructing. The codes inside the ranges behave exactly as before.

### Exception: `ValidationFailed` carries its detail under the key the response path serves

**What changed.** `ValidationFailed` stores the validation detail under the `errors` context key — the one the kernel exception listener copies into the json error payload — instead of `validationErrors`, a key nothing on the response path reads.

**Symptom.** A caller that read the exception's context under `validationErrors` finds it under `errors` now. A client of an endpoint using `ValidationFailed` starts receiving the detail in the 422 payload, where before it received only the message.

**Remedy.** Read the context under `errors`. Nothing else is required; the status stays 422.

### Exception: `MarkLogged` marks at the depth the reader searches

**What changed.** `MarkLogged` finds the nearest markable error through the chain, matching `logging.LogError`, which reads the mark back the same way. The mark used to be written on the top value alone, so marking a wrapped error was a silent no-op and the one failure produced two records. `LogContext` also anchors the cause chain on the error's own wrap link rather than on the nearest deep `*exception.Error`, so a wrapping http exception no longer swallows the wrapped error's context from the log record, and every extra context map passed to it is merged in order rather than only the first.

**Symptom.** Log records gain detail: a wrapped-then-marked error stops being logged twice, and the record of an `HttpException` wrapping an `*Error` now carries that error's message and context in `cause`/`causeChain`/`causeContextChain`.

**Remedy.** None; tooling that parses log records should expect the richer shape.

### Debug: `debug:events --format=json --verbose` carries the listener detail

**What changed.** Under `--verbose`, the json document's `data` becomes `{"events": <previous list payload>, "listeners": [...]}`, exposing per-listener priority, source, owner and the required / may-skip marks that were previously table-only. Without `--verbose` the shape is unchanged.

**Symptom.** A json consumer that passed `--verbose` and read `data.items` finds the list under `data.events.items` now.

**Remedy.** Drop `--verbose` for the old shape, or read `data.events` and gain `data.listeners`.

### Debug: `NewMiddlewareCommand` refuses a nil provider

**What changed.** `NewMiddlewareCommand(descriptionProvider, buildProvider)` panics at construction when either provider is nil; a zero-value `MiddlewareCommand` run without providers returns a named error through the report instead of a nil-function call.

**Symptom.** Wiring that handed a nil provider panics at registration time instead of when the command runs.

**Remedy.** Pass both providers; return empty results from them when there is nothing to list.

### Event: a skipped required listener refuses the response the stopping listener produced

**What changed.** A `kernel.request` or `kernel.controller` dispatch that stopped propagation before a listener marked required had run now fails the request closed even when the stopping listener set a response; the response it produced is closed rather than written. The refusal is carried by `event.RequiredListenerSkippedError`, which the kernel type-asserts on the returned error itself rather than through its cause chain. A listener that stops propagation with nothing required behind it still answers the request unchanged.

**Symptom.** A listener registered above `security.KernelAccessControlListenerPriority` (20) that stops propagation and serves its own response — an http cache, a maintenance page, a short-circuiting redirect — now returns `500` instead of that response. Before this change the response was served with access control never consulted.

**Remedy.** Register the listener *below* the access-control listener so it runs after authorization, which is almost always what such a listener means. A listener that genuinely must short-circuit past authorization opts out explicitly with `MarkListenerMaySkipRequiredListeners` on its registration.

### Event: a stopping listener that also fails, and an event that arrives stopped

**What changed.** A listener that stops propagation *and* returns an error is now weighed against the required listeners behind it before its own failure is returned, so the skipped required listener is what the dispatch reports. An event whose propagation was already stopped when it reached `Dispatch` now runs no listener at all; it previously ran exactly the first one and then named that listener as the one that stopped propagation.

**Symptom.** A dispatch of the event object a previous dispatch returned runs nothing instead of one listener. A stopping-and-failing listener produces a skipped-required-listener error rather than its own.

**Remedy.** Build a fresh event per dispatch — `DispatchName` does this for you. Nothing else is required.

### Event: subscribers are registered once, validated whole, and never empty

**What changed.** `AddSubscriber` refuses a subscriber whose identity is already registered, refuses a subscriber declaring no subscribed events, refuses an event name mapped to an empty list, and validates every subscribed event before registering any listener. `MarkListenerRequired` and `MarkListenerMaySkipRequiredListeners` refuse a registration the dispatcher does not hold.

**Symptom.** Registering two instances of the same field-less subscriber type panics at boot — they share one address and therefore one identity, so removing either used to remove both. A subscriber built from configuration that produced no events panics instead of registering nothing silently. A stale or foreign `ListenerRegistration` handed to a mark panics instead of leaving the guarantee unarmed.

**Remedy.** Register each subscriber once. A type that genuinely needs two live instances must carry a field so the two can be told apart. Pass marks the registration that `AddListener` returned, from the same dispatcher.

### Event: `NewEventDispatcherAdapter` takes the dispatcher alone

**What changed.** The clock argument is gone: it was validated at construction and never read, and the documentation described the one-argument signature already. The adapter also panics when asked to mark a required listener over a wrapped dispatcher that does not implement `RequiredListenerRegistrar`, instead of absorbing the mark.

**Symptom.** A call to `NewEventDispatcherAdapter(dispatcher, clock)` no longer compiles. `contract.RegisteredListener` gained `Required` and `MaySkipRequiredListeners`, so an out-of-tree composite literal written without field names no longer compiles either.

**Remedy.** Drop the second argument. Write `RegisteredListener` literals with field names. Wrap a dispatcher that supports required listeners, or do not mark them.

### Cli: a duplicated flag name and a mismatched table row fail fast

**What changed.** `output.MergeFlags` panics on a flag name declared twice — the parser resolves a name to the first declaration, so a command-specific flag reusing a standard name was silently inert. `TableBlockBuilder.AddRow` panics on a row whose cell count disagrees with the block's declared columns — a surplus cell silently never rendered; the single-token separator row stays admitted.

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

### Http/Bag: request values are delivered, and a repeated key read as one string is refused

**What changed.** The request bags keep the single and the repeated key apart by type: a query or form key that appeared once is stored as the string it is, a genuinely repeated key (`?a=1&a=2`) stays a string slice. `Request.Input` and the lax accessors (`bag.String`, `bag.StringOrDefault`, `bag.HasNonEmptyString`) deliver the single value they used to silently lose — every value was stored as a slice and the lax accessor answered the empty string for it, so `Input("term")` on `?term=melody` returned `""` with nothing said. Reading a repeated key as one string now panics, naming the key and pointing to `bag.StringSlice`/`bag.StringAt` — never an empty-string guess, never the first element silently hiding the rest.

**Symptom.** Handlers reading `Input`/`StringOrDefault` start receiving the values clients always sent. A request that repeats a key read as a single string answers 500 through the kernel's recovery instead of an empty field.

**Remedy.** None for the ordinary handler — this is the behaviour everyone assumed. Code that genuinely reads multi-value keys uses `bag.StringSlice` (the `all()` analogue), or `bag.StringStrict` for an explicit error.

### Http: a form that does not parse is refused

**What changed.** A `ParseForm` failure (an invalid percent escape, a malformed urlencoded body) is recorded on the request the way a failed body read is, and the kernel refuses the request — 400 — before the handler runs. It used to log one warning and continue with an empty form, so a real submission was processed as "field missing".

**Symptom.** Malformed form submissions answer 400 instead of whatever the handler made of an empty form.

**Remedy.** None; clients sending valid forms are unaffected.

### Config: the template grammar refuses what silently survived

**What changed.** Three constructs that survived as literal text now fail the boot with a named error: an `%env(...)%` whose closing `)%` is malformed or missing (`%env(A))%`, `postgres://user:%env(DB_PASS)@db` with the forgotten percent), a `%name` reference a percent opened and nothing closed (`%app-name%`), and — in `.env` values — a braced `${...}` reference whose name breaks the key grammar (`${DB-PASS}`). Each used to keep its literal spelling in the resolved value, so the application connected with `%env(DB_PASS)` as its password and nothing said so. A literal percent is written doubled (`pa%%ss`), a literal dollar as `\$` — both already documented; the bare-dollar grammar (`pa$sword`, `$1.50`) is untouched.

**Symptom.** A boot that used to come up with placeholder text in a value now fails at the line naming the parameter (content is redacted where it may hold a credential).

**Remedy.** Fix the placeholder, or escape the literal percent/dollar as documented.

### Config: `.env` reading matches godotenv where it silently diverged

**What changed.** Two divergences from godotenv's own reading are gone. The trailing-comment cut now happens once, by godotenv's countback — the value ends at the LAST whitespace-preceded `#` — where the preprocessor's own first-`#` cut read `GREETING=hello # world # x` as `hello` instead of godotenv's `hello # world`. And the preprocessor walks bytes instead of runes, so a `.env` saved in a non-UTF-8 encoding keeps its bytes exactly — a Latin-1 password was silently re-encoded through U+FFFD and the credential sent to the database differed from the one in the file.

**Symptom.** Values with multiple hash marks or non-UTF-8 bytes read as godotenv alone would read them.

**Remedy.** None for UTF-8 files with single comments — the overwhelming case is byte-identical.

### Config: credentials stay out of failure logs

**What changed.** Three paths that carried values into the rendered log are sealed. A godotenv parse failure quoted the remaining tail of the `.env` file — the neighborhood where the credentials live, marker bytes included — into the cause chain; the error now carries a content-free description plus the file path. The typed accessors (`Int`, `Bool`, `Float`, `Duration`) withhold the value-quoting cause for a parameter marked secret, keeping it for ordinary parameters whose mistyped pool size deserves the diagnostic. And a template referencing a non-string parameter reports the type, never the raw value.

**Symptom.** Boot failures on broken `.env` files and secret conversions log what failed and where, without the values.

**Remedy.** None.

### Config: late secret markings propagate, registrations are atomic, names are judged trimmed

**What changed.** `MarkSecret` after the boot resolution now travels to every parameter whose template directly reads the marked key — the late marking used to redact the key while the dsn assembled from it printed in full. `RegisterRuntime`/`RegisterRuntimeSecret` publish the parameter and then resolve it, rolling the publication back if the template fails, so a registration whose template fails still leaves nothing behind — it used to leave the raw template served to every reader that outlived the recovered panic, with the name burnt for the corrected retry. Publishing first is what lets a parameter registered after boot inherit the secret marking of the credential its template reads: the propagation marks the reader it finds by name in the parameter map, and a parameter resolved before it was published was absent at the only moment the propagation looks — so a dsn assembled from a marked password printed in full in `debug:parameters`. A self-referential runtime registration now
names the cycle rather than reporting the key as undefined. And a name is judged trimmed: whitespace-only is refused as empty, a padded name is refused outright instead of registering a parameter no exact-match lookup ever names.

**Symptom.** `debug:parameters` redacts derived values after a late `MarkSecret`; a failed late registration can be retried; padded names fail at the registration line.

**Remedy.** None for correct code.

### Config: `IntWithDefault` answers the default only for absence

**What changed.** A parameter that exists but does not parse panics instead of silently becoming the default — `MELODY_APP_POOL_SIZE=1O0` ran with the default while the operator believed the configured value was live. A nil (or typed-nil) parameter still answers the default.

**Symptom.** A mistyped numeric parameter fails the boot naming the parameter, instead of running in disguise.

**Remedy.** Fix the value, or remove it to genuinely choose the default.

### Bag: `All` copies deep, `AppendString` is atomic

**What changed.** `ParameterBag.All` copies the shapes the bag's own writers produce (`[]string`, `map[string]string`), so mutating the returned map no longer writes into the bag behind its lock. `bag.AppendString` appends through a new critical-section method on the concrete bag — the contract-level read-modify-write lost one of two concurrent appends with no error and nothing the race detector could see; the fallback for foreign `ParameterBag` implementations keeps the old two-step behaviour.

**Symptom.** None for single-threaded use; concurrent appenders stop losing values.

**Remedy.** None. Code that relied on `All` aliasing live state reads the bag directly.

### Cache: the manager no longer closes a backend it was handed

**What changed.** [`NewManager`](../cache/manager.go) builds a manager that does not own its backend: `Close` leaves the backend open, because on the container path both are registered services and the container closes each one itself — the cascade closed the backend twice, which a backend wrapping a connection typically reports as a failure on the second call. `NewManagerOwningBackend` keeps the cascade for the caller that builds both by hand.

**Symptom.** A manager built directly with `NewManager` over a hand-built backend no longer stops that backend's cleanup goroutine on `Close`; the backend outlives the manager until its own `Close`.

**Remedy.** A caller that builds both by hand and wants one `Close` to end both switches to `NewManagerOwningBackend`. The container path needs no action.

### Cache: a closed in-memory backend refuses every operation

**What changed.** [`InMemoryBackend`](../cache/in_memory.go) carries a closed flag: after `Close`, every operation answers `cache backend is closed` instead of silently succeeding against a map whose cleanup goroutine is gone — an entry written after `Close` was never reclaimed by anything and grew the map for the rest of the process. `Close` itself stays idempotent.

**Symptom.** A write or read that races a shutdown past the backend's `Close` receives an error instead of a silent success that nothing would ever sweep.

**Remedy.** None for the ordinary lifecycle. Code that deliberately used a closed backend as a plain map keeps a backend it does not close, or its own map.

### Cache: the in-memory backend refuses what it silently accepted

**What changed.** Four degenerate inputs that used to be absorbed are refused. The empty key answers `cache key is empty` on every operation — it used to be a real key, which the `rueidis` backend refuses, so every caller whose key came up empty silently shared one entry until the deployment switched backends. A negative ttl on `Set`/`SetMultiple` answers `cache ttl is negative` — it used to store an immortal entry, the exact opposite of a ttl computed from an already-passed deadline; zero keeps meaning no expiry. `Increment`/`Decrement` on an existing empty or blank payload answer `cache value is not a valid int64` the way any textual payload does — it used to be adopted as a zero counter and overwritten, destroying the entry, where redis `INCRBY` errors. A negative `maxItems` panics at construction — it used to silently mean unbounded, disarming eviction the operator believed was armed.

**Symptom.** Each refused input surfaces as an error (or, for `maxItems`, a boot-time panic) at the call that produced it, instead of a silently wrong cache.

**Remedy.** Fix the calling code: supply a non-empty key, clamp a computed ttl at zero, keep counter keys away from non-counter writes, and pass `0` for an explicitly unbounded backend.

### Cache: a payload the serializer cannot decode is a typed miss

**What changed.** [`Manager.Get`](../cache/manager.go) wraps a deserialization failure in the new [`DeserializationError`](../cache/deserialization_error.go), and [`Remember`](../cache/remember.go) treats that type as a miss: the callback recomputes and its `Set` overwrites the corrupt payload, so the key heals instead of staying poisoned until an expiry a ttl of zero postpones forever. [`Manager.Many`](../cache/manager.go) no longer discards the whole answer over one corrupt entry: the entry is left out the way an absent key is, and the error returned beside the good values is a `DeserializationError` naming the culprit keys deterministically. Every other error keeps meaning the cache itself failed.

**Symptom.** A key whose payload was corrupted out-of-band recovers on the next `Remember` instead of erroring forever; `Many` over a corrupt entry returns the good values plus a typed error naming the bad keys, where it used to return nothing and an anonymous encoding error.

**Remedy.** None for most callers. A caller of `Many` that treated any error as "no values" now also has the partial map available; a custom `Cache` implementation that wants the same self-healing under `Remember` wraps its own deserialization failures with `NewDeserializationError`.

### Cache: the zero-value RememberOption reads as the defaults

**What changed.** A `&RememberOption{}` built as a literal used to silently disarm the stampede protection it never asked to configure, and its zero `waitTimeout` made every miss answer an instant timeout; [`Remember`](../cache/remember.go) now reads the exact zero-value option as `NewDefaultRememberOption()` — protection on, wait unbounded. Separately, a `waitTimeout` of zero set through the constructor now means no waiting rather than no answer: a result the in-flight call has already memoized is taken without blocking, and only a flight still in the air answers with the timeout.

**Symptom.** Callers passing a literal option regain the single-flight callback the default promises; callers polling with a zero timeout receive a completed result instead of a guaranteed error.

**Remedy.** None. A caller that genuinely wants protection off builds the option through `NewDefaultRememberOption().WithStampedeProtectionEnabled(false)`, which carries a non-zero shape and stays what it says.

### Cache: a value-kind Cache no longer coalesces in Remember

**What changed.** [`Remember`](../cache/remember.go) coalesces concurrent callers under one in-flight computation only for pointer-kind `Cache` implementations, whose address tells instances apart. A value-kind implementation used to share one flight per type: two different instances with different backends collapsed onto one leader, and the second caller received — and cached nothing from — the value computed for somebody else's cache. A value-kind instance now runs every callback itself.

**Symptom.** A struct- or map-kind `Cache` implementation loses stampede deduplication and never a correct answer.

**Remedy.** Implement `Cache` on a pointer receiver to regain coalescing.

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

**What changed.** `HttpTimeoutConfiguration` and `HttpShutdownConfiguration` are deleted from the application package. Nothing implemented them and nothing could: the configuration the application consults is always the one it builds itself, so the overrides were unreachable through any api. The server limits are the fixed defaults — read 15s, read-header 5s, write 30s, idle 60s, max header 1 MiB; the shutdown timeout alone is configurable through `MELODY_HTTP_SHUTDOWN_TIMEOUT` (`kernel.http.shutdown_timeout`), five seconds by default.

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

**What changed.** [`Register`](../container/container_registrar.go) and `OverrideInstance`/`OverrideProtectedInstance` on a closed container return the container-is-closed error, the way `RegisterScoped` always has. The read paths are untouched: already-built instances keep being served during shutdown.

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

### Behavioural: `Session.Get` hands out a copy

**What changed.** [`session.Session.Get`](../session/session.go) returns a copy at the depth `All` copies at. The live nested value it used to hand out, mutated in place, changed the session without passing through `Set`: `modified` stayed false, `SaveSession` skipped the write and reported success, and the mutation silently never persisted.

**Symptom.** Code that mutated the map or slice returned by `Get` and relied on the live session object changing underneath — within the same request only, since the mutation never reached the storage — now works on its own copy.

**Remedy.** Read, mutate the copy, `Set` it back — the pattern that was always the correct one is unaffected:

```go
profile := sessionInstance.Get("profile").(map[string]any)
profile["role"] = "admin"
sessionInstance.Set("profile", profile)
```

### Behavioural: the static file server copies its configuration at construction

**What changed.** [`static.NewFileServer`](../http/static/file_server.go) copies the `FileServerConfig` it is given — struct and both lists — and applies its defaults to the copy. It used to retain the caller's pointer, write `index.html` and `3600` into the caller's own struct, and read the exclusion lists live, so `SetExcludedPathList`/`SetAllowedDotPrefixList` called after construction raced the in-flight requests reading them with no lock.

**Symptom.** A setter called after `NewFileServer` no longer affects the server already built, and the caller's config struct keeps its zero values after construction.

**Remedy.** Configure before constructing — the order the framework's own wiring always used. A deployment that genuinely reconfigures at runtime builds a new server from the updated config and swaps it where it is consulted.

### Behavioural: an installed error handler takes the exception listener's place

**What changed.** The application registers the framework exception listener only when [`Kernel.SetErrorHandler`](../http/kernel.go) installed nothing by boot. The listener answers every kernel.exception dispatch and the kernel consults the handler only when the dispatch produced no response, so an installed handler could never run.

**Symptom.** An application that had installed an error handler — dead code until now — finds it answering its error responses, and the listener's rendering (negotiation, the request-id header, the validation errors payload, the warning-versus-error log grading) replaced by the handler's own.

**Remedy.** None for the ordinary application. One that installed a handler experimentally and prefers the framework rendering removes the `SetErrorHandler` call; one that keeps its handler and wants a detail of the framework rendering back renders it through the same door the framework uses, a kernel.exception listener at a priority above `-1000`.

### Behavioural: error bodies negotiate their representation, and `X-Request-Id` arrives once

**What changed.** Every default error rendering — the exception listener and the kernel's six fallback paths — goes through one renderer that honours the accept header the way the success path does, with one deliberate asymmetry: an accept header that refuses every available media type keeps the error's own status over the default json body instead of answering `406`, because a `401` masked as not-acceptable hides the refusal from the client that has to react to it. And [`WriteToHttpResponseWriter`](../http/response_writer.go) now gives a header key named by the response to the response — the writer's values for it are replaced, not appended to — which is what ends the `X-Request-Id` duplicate on every error response.

**Symptom.** With a non-json serializer registered, a client that negotiated it receives error bodies in that representation where it always received json. Error responses carry `X-Request-Id` once where they carried it twice. The kernel's own fallback html page gained the status and request-id lines the listener's page always had.

**Remedy.** None for a client reading the fields; a client that hardcoded the error content type sends an accept header for it, or none at all.

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

### Compile-level: `config/contract.HttpConfiguration` gained `SessionTombstoneRetention`

**What changed.** [`config/contract.HttpConfiguration`](../config/contract/http.go) declares `SessionTombstoneRetention() time.Duration`, how long a deleted session id keeps refusing a write-back. The framework's own implementation reads it from `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` (`kernel.http.session_tombstone_retention`), five minutes by default — the constant the window used to be — and refuses zero and negative at boot, because a window that refuses nothing is not a shorter window but a disarmed logout defence.

**Symptom.** A type of your own implementing `config/contract.HttpConfiguration` no longer satisfies the interface, and the assignment fails to compile with `missing method SessionTombstoneRetention`.

**Remedy.** Implement it. Returning `config.DefaultSessionTombstoneRetention` keeps the behaviour the interface had without the method:

```go
func (instance *CustomHttpConfiguration) SessionTombstoneRetention() time.Duration {
	return config.DefaultSessionTombstoneRetention
}
```

### Compile-level: `config/contract.HttpConfiguration` gained `ShutdownTimeout`

**What changed.** [`config/contract.HttpConfiguration`](../config/contract/http.go) declares `ShutdownTimeout() time.Duration`, how long a stopping http server waits for the requests it has already admitted before cutting them. The framework's own implementation reads it from `MELODY_HTTP_SHUTDOWN_TIMEOUT` (`kernel.http.shutdown_timeout`), five seconds by default — the constant the wait used to be — and refuses zero and negative at boot, because only a positive value can describe a wait.

**Symptom.** A type of your own implementing `config/contract.HttpConfiguration` no longer satisfies the interface, and the assignment fails to compile with `missing method ShutdownTimeout`.

**Remedy.** Implement it. Returning `config.DefaultHttpShutdownTimeout` keeps the behaviour the interface had without the method:

```go
func (instance *CustomHttpConfiguration) ShutdownTimeout() time.Duration {
	return config.DefaultHttpShutdownTimeout
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

`before`/`after` edges live on [`pipeline.NewHttpMiddlewareDefinition`](../http/middleware/pipeline/definition.go) for a pipeline assembled directly through [`pipeline.NewBuilder`](../http/middleware/pipeline/builder.go); the module registrar exposes priority. [`(*HttpMiddleware).LastBuildReport`](../application/http_middleware.go) reports the order a serving process actually built; `debug:middleware` lists the pipeline through its own description pass, leaving that report alone.

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

**Remedy.** Read the envelope. `meta` already reports the command, its arguments, the start time and the duration, and `error` reports the failure the command itself returned. The **final** status is the exit code, not the document: the scope and the container are closed after the document has been written, so a teardown failure can no longer enter it and is visible only in the exit code.

### CLI: a command whose envelope reports an error exits non-zero

**What changed.** [`output.Render`](../cli/output/renderer.go) returns an exit-coded error after writing the envelope. A registered service that errors or panics while being constructed is reported as `debug.buildFailed` rather than `debug.notFound`.

**Symptom.** A command that reported an error in its payload while exiting `0` now exits `1`. `debug:container <name>` fails when the service cannot be resolved instead of printing `[success]`.

**Remedy.** Nothing to change in the framework. A wrapper script that treated a zero exit as success was reading a status that was never true; a deployment gate such as `app debug:container app.repository.order || exit 1` now works as written. A command of your own that renders a non-nil `Envelope.Error` deliberately and still wants a zero exit must not put the failure in the envelope.

### CLI: `--format` and `--order` reject an unrecognised value

**What changed.** Both flags carry a validator ([`StandardFlags`](../cli/output/standard_flag.go)), so `--format=JSON`, `--format=yaml` and `--order=ascending` fail during flag parsing with a message naming the accepted values, matching how `--limit=abc` already behaved.

**Symptom.** A script passing an unsupported value now fails with a non-zero exit instead of quietly receiving the human table.

**Remedy.** Pass `table`, `json` or `json-pretty`, and `asc` or `desc` — the same three format spellings the validator's own message names. `json-pretty` is the one that restores a document a person can read by hand; `json` stays one document per line so a line-framed consumer can follow a long-running command live. Omitting either flag still defaults to `table` and `asc`.

### CLI: `--limit`, `--offset` and `--order` are applied to the rendered items

**What changed.** `debug:router`, `debug:events`, `debug:parameters`, `debug:middleware` and `debug:container` apply the window through [`output.WindowItems`](../cli/output/list_payload.go) and the order through [`output.ApplySortOrder`](../cli/output/list_payload.go), reversal running before the window so a descending window returns the end of the list. `total` continues to report the unwindowed count.

**Symptom.** An invocation already passing `--limit` or `--offset` received the full list and now receives a window; with `--verbose`, `debug:events` also narrows its listeners block to the windowed events. `--order=desc` was accepted and ignored before, so an invocation that passed it now gets different output.

**Remedy.** Nothing for a client that paged with `offset += limit` — it now walks each item exactly once instead of re-reading the whole list on every page. A consumer that passed `--limit` while expecting everything must drop the flag.

### Debug: `NewMiddlewareCommand` takes a description provider and a build provider

**What changed.** `debug:middleware` describes the pipeline by default — selection, ordering and the function names captured at registration, no factory run — and builds the real chain only under the new `--build` flag, with a recover that renders a failing or panicking factory as a `debug.buildFailed` envelope instead of a dead process. The constructor therefore takes the two channels: `debug.NewMiddlewareCommand(descriptionProvider, buildProvider)`, typed `MiddlewareDescriptionProvider` and `MiddlewareBuildProvider`.

**Symptom.** An out-of-tree caller of `debug.NewMiddlewareCommand` with the old single chain provider stops compiling.

**Remedy.** Hand the two providers. For a pipeline assembled through `pipeline.Builder`, `Describe` answers the description half without building and `Build` remains the build half; a framework-booted application wires both automatically.

### Debug: `debug:container` lists without building; `--build` is the sweep

**What changed.** The bare listing runs no provider — names, lifetimes, built state and declared types, grouped into container and scoped blocks — where it used to resolve every windowed service, opening every pool and client of the application from a console command. The full diagnostic sweep — every service built, failures with cause chains — moved behind `--build`; `debug:container <name>` still builds, and a scoped name now resolves through the run's own scope instead of answering `debug.notFound`.

**Symptom.** A consumer of the bare listing's json no longer receives `typeName`-with-error items — the items carry `name`, `lifetime`, `isBuilt`, `typeName` — and a table consumer sees two blocks. A pipeline that used the bare listing as a health sweep no longer probes anything.

**Remedy.** Pass `--build` where the sweep was the point; keep the bare listing where the question was "what is registered".

### Application: the kernel's default listeners register at the end of `Boot`

**What changed.** The profiler (debug mode), the response normalizer, the terminate access log and the exception listener (when no error handler was installed by boot) register at the end of `Boot` in every process shape, not inside the http run. They are inert where no kernel event is dispatched, and `debug:events` now shows them in a console process.

**Symptom.** A kernel event dispatched between boot and the http run — or from a console process — now reaches the default listeners; `debug:events` output grew the kernel listeners it used to miss.

**Remedy.** Nothing for the ordinary application. A process that dispatched kernel events manually before `runHttp` and relied on nothing listening must account for the listeners existing from `Boot` on.

### Debug: `debug:version` no longer reports melody's version as the application's

**What changed.** The framework wiring stops filling the `application` row with `melody`'s own build version; the row reads the process-wide declaration made through `cli/output.SetApplicationVersion` and prints `<unknown>` without one.

**Symptom.** The `application` row prints `<unknown>` where it printed the framework version.

**Remedy.** Call `output.SetApplicationVersion(version)` once in the composition root's main, with the version the application keeps wherever it keeps it — its own ldflags variable or its environment.

### Http: one record per handler failure, at the level of its status class

**What changed.** The kernel's handler-error writers read and set the already-logged mark and record a deliberate 4xx at warning, so a handler failure files one record instead of two; a handler returning its request context's own cancellation is recorded once at warning as `request cancelled by client`; a response-write failure the client caused is `failed to write response; client disconnected` at warning; the access-control listener's three direct 401 branches file one warning naming the refusal reason; and the static file server's two per-request exits drop from info to debug. The mark now lands on every shape of failure: `MarkLogged` marks the nearest `AlreadyLogged` implementer in the chain, and a handler's plain `errors.New` or a runtime panic implements none, so those two still filed twice — the writers hand the exception dispatch a marked carrier keeping the original as its cause, while the application's error handler still receives the error the handler returned. The
handler-error record also names the request method beside the path.

**Symptom.** Dashboards counting error records see handler failures once instead of twice — foreign errors and runtime panics included — and 4xx refusals at warning; log queries matching `controller handler error` no longer match client disconnects; a 401 now leaves an `authorization refused` warning; the two static info messages disappear from info-level journals. An application error handler that type-asserted the error it receives is unaffected; a `kernel.exception` listener that did so sees the carrier and must reach the original through `errors.As` or `Unwrap`.

**Remedy.** Point alerting at the level that means what it says: error is now a server fault, warning a refusal or a client-caused condition. Update any query that keyed on the duplicated record or the old levels.

### Http: the access log reports the status a stream actually carried

**What changed.** For a handler that committed its own response, the terminate event and the access log report the status code recorded on the connection instead of the synthetic response the kernel substituted; a streamed source that fails before its first byte now answers the rendered 500 instead of an implicit empty 200.

**Symptom.** Streaming routes stop logging `statusCode=204`; a panic mid-stream logs the committed 200 instead of the never-written 500; clients of a failing stream receive 500.

**Remedy.** Update status-distribution queries over the access log for streaming routes; they now read the wire's truth.

### Ownership: configuration handed across a boundary stays what was handed

**What changed.** `Kernel.SetForwardedHeadersPolicy`, `NewForwardedClientIpResolver`, `CompressionMiddleware`, `NewHttpMiddlewareDefinition`, `MiddlewareBuildReport.SetInactive`, `httpclient.RequestOptions.Headers`/`Query` and cron's `Configuration.Schedule`/`Entries` copy what crosses the boundary, in whichever direction it crosses.

**Symptom.** Code that mutated a slice, map or struct after handing it over — or wrote through a getter's returned map — no longer changes the receiver's behaviour; `CompressionMiddleware` no longer rewrites the caller's config with normalized values.

**Remedy.** Mutate before handing over, or use the setters that exist for the purpose; read normalized values from behaviour, not from the caller's own object.

### Middleware: nil configurations read as defaults where defaults exist

**What changed.** `CompressionMiddleware(nil)` builds over `DefaultCompressionConfig()` and the deprecated `CorsMiddleware(nil)` over the default cors service, the reading their siblings give the same absence; `static.NewFileServer(nil)` refuses by name, because its default would be a live file server over `public`.

**Symptom.** A nil that used to panic with a raw dereference now serves defaults (compression, cors) or panics naming the rule (static).

**Remedy.** Nothing, unless a boot script keyed on the raw panic text.

### bunorm: the empty migrate module refuses the boot

**What changed.** `migrate.NewModule(migrate.ModuleConfig{})` — neither `Migrations` nor `Contexts` — is refused at command registration with `bunorm migrate module requires migrations or contexts`.

**Symptom.** An application that registered the empty module booted with no migration commands; it now fails the boot naming the rule.

**Remedy.** Pass the migrations the module exists to register, or remove the registration.

### Cron: a clean shutdown is not a job failure

**What changed.** A run the runner's own shutdown cancelled is recorded at warning as `cron: scheduled command cancelled by shutdown` and excluded from the failure aggregate; the runner's failure and abandon records carry the run's `cronRunId`.

**Symptom.** `melody:cron:run --once` interrupted by SIGTERM exits 0 with a warning instead of non-zero with an error record; alerting keyed on `cron runner command failed` stops firing on deploys.

**Remedy.** Key deploy-time alerting on the new warning if the old signal was load-bearing; genuine failures keep the error record, now attributable by `cronRunId`.
