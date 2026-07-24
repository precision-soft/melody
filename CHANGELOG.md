# Changelog

All notable changes to `precision-soft/melody` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

### Security

- `security/access_control_listener.go` — the access-control listener (the request authorization gate) matched only prefix rules and the empty-prefix fallback, silently ignoring exact (`NewAccessControlExactRule`) and regular-expression (`NewAccessControlRegexRule`) rules; a request could therefore bypass an exact or regular-expression access-control rule entirely. `matchAccessControlRule` now delegates to `AccessControl.matchRuleIndex`, sharing the full exact → prefix → regular-expression → fallback precedence already used by `AccessControl.Match`
- `security/rule.go` — `ApiKeyHeaderRule.Check` compared the configured key against the request header with a plain `==`, which is not constant-time and leaks key length and shared prefix through timing; the comparison now uses `crypto/subtle.ConstantTimeCompare`. `NewApiKeyHeaderRule` additionally panics when the header name or the expected value is empty, closing a fail-open path where a request that omits the header (yielding `""`) would compare equal to an empty expected key and authorize every caller
- `security/access_control.go` — `NewAccessControlRule` and `NewAccessControlRuleWithSegmentPrefix` now reject a rule that combines `PUBLIC_ACCESS` with any other attribute (via `normalizeAccessControlAttributes`); the listener grants `PUBLIC_ACCESS` before any role or voter check, so a rule such as `(PUBLIC_ACCESS, ROLE_ADMIN)` would have silently opened the endpoint to everyone and discarded the role requirement
- `security/config/access_control_builder.go` — `AllowAnonymous` appended a rule with no attributes, which the listener treats as "authentication required", so the helper actually denied anonymous access with a 401; it now carries `securitycontract.AttributePublicAccess` so anonymous requests are granted as intended
- `security/access_control.go` — an exact or anchored-regex access-control rule could be bypassed by appending extra trailing slashes (`/admin//` routes to the `/admin` handler, but `matchRuleIndex` trimmed only one trailing slash and so failed to match the exact `/admin` rule, leaving the request unguarded). `matchRuleIndex` now collapses all trailing slashes like the router. Ported from the `v3` fix.

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

### Added

- `http/cors/` — new subpackage extracted from `http/middleware/cors.go`. Split into `cors.Service`, `cors.Middleware`, and `cors.RegisterResponseListener` so CORS headers are applied both on the happy path (middleware) and on error-path responses produced by the kernel (`kernel.response` listener, priority `-100`)
- `http/response.go` — `BuildContentDisposition(disposition, filename)` emits RFC 6266 `Content-Disposition` with both `filename="..."` ASCII fallback and `filename*=UTF-8''...` RFC 5987 encoding for non-ASCII filenames; `AttachmentResponse` now routes through it
- `http/middleware/rate_limit.go` — `ClientIpResolver` hook and `DefaultClientIp` for proxy-aware IP resolution; `RateLimitConfig.SetClientIpResolver(...)` lets userland install X-Forwarded-For / X-Real-IP strategies without rewriting key extractors
- `http/request.go` — form auto-parsing now gated on `Content-Type` (`application/x-www-form-urlencoded` or `multipart/form-data`); JSON/XML/binary bodies are no longer consumed by `NewRequest`
- `session/session.go` — `isValidSessionId` enforces 32-char lowercase-hex format; `Manager.Session`/`DeleteSession` reject malformed cookies before hitting storage
- Test coverage: `http/cors/{listener,middleware,service}_test.go`, `http/request_test.go`, `http/response_test.go`, `container/scope_test.go` concurrent Close/resolve test, `logging/json_logger_test.go` concurrent writes, `session/file_storage_test.go` atomic write and reopen coverage

### Deprecated

- `http/middleware.CorsConfig`, `http/middleware.NewCorsConfig`, `http/middleware.DefaultCorsConfig`, `http/middleware.RestrictiveCorsConfig`, `http/middleware.CorsMiddleware`, `http/middleware.DefaultCorsMiddleware`, `http/middleware.RestrictiveCors` — use the equivalents in `github.com/precision-soft/melody/http/cors` instead. Deprecated symbols are kept for backwards compatibility; no removal scheduled.

## [v1.10.1] - 2026-04-17 - Fix Compression Error Propagation and Concurrent Access Races

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

### Changed

- `httpclient/http_client.go` — added `sync.RWMutex` to protect concurrent access to `baseUrl`, `headers`, and `timeout` fields
- `httpclient/http_client_config.go` — `Headers()` now returns a defensive copy of the map
- `cli/output/application_version.go` — application version storage replaced with `sync/atomic.Value` for thread safety
- `logging/emergency_logger.go` — replaced `sync.Once` with `sync.Mutex` so `CloseEmergencyLogger()` can reset the singleton and a subsequent `EmergencyLogger()` call creates a fresh instance
- `http/kernel.go` — `debugMode` variable hoisted to single computation at request entry
- `application/application_http.go` — extracted `httpShutdownTimeout` constant for the HTTP server shutdown deadline
- `cache/in_memory.go` — removed redundant map copy in `SetMultiple`

### Added

- `http/static/utility_test.go` — symlink traversal rejection, absolute path rejection, parent traversal rejection, symlink within root allowed
- `cli/output/application_version_test.go` — Set/Get coverage and concurrent access race test
- `logging/emergency_logger_test.go` — singleton behavior, `Close`/recreate cycle, concurrent access
- `httpclient/http_client_test.go` — concurrent `SetHeader`/`SetBaseUrl`/`SetTimeout` with in-flight requests, `HttpClientConfig.Headers()` defensive copy
- `http/middleware/compression_test.go` — HuffmanOnly and BestCompression level boundary acceptance, out-of-range fallback to DefaultCompression
- `config/configuration_test.go` — placeholder regex rejects identifiers starting with digits, accepts letter/underscore/dotted identifiers
- `session/in_memory_storage_test.go`, `session/file_storage_test.go` — concurrent `Load`/`Save` race tests

## [v1.10.0] - 2026-04-13 - Lock-Step Release — Align with v2/v3 Sibling Tags

Lock-step release — no `v1/` changes this cycle. Tag SHA equals `v1.9.0`; published to keep the core `v1` module version aligned with the `v2.4.0` / `v3.3.0` sibling tags. See the v2/v3 CHANGELOGs for the actual content of this cycle.

## [v1.9.0] - 2026-04-13 - Fix Validators, Rate Limiter, and Router; Improve Goroutine Lifecycle

### Fixed

- `validator.go` — `createConstraintWithParams` now handles `greaterThan` parameters; `validate:"greaterThan(value=5)"` was silently using `min=0`
- `rate_limit.go` — `getClientIp` strips port via `net.SplitHostPort`; rate limiting was per-connection instead of per-IP
- `url_generator.go` — path parameters now URL-encoded via `url.PathEscape`; special characters produced malformed URLs
- `accept.go` — `PrefersHtml` uses position-based comparison; browsers sending both `text/html` and `application/json` now correctly get HTML
- `compression.go` — `gzip.NoCompression` (level 0) is no longer overridden to default compression
- `constraint_greater_than.go` — added `float32`/`float64` support; float values no longer return "value must be an integer"
- `kernel.go` — `errorHandler` now called for controller errors (was only called on panic recovery path)
- `cors.go` — panic at middleware initialization when `AllowCredentials=true` and origins contain `"*"` to prevent overly permissive CORS

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

## [v1.8.4] - 2026-04-10 - Fix XSS, Symlink Traversal, and Routing Edge Cases

### Fixed

- `exception_listener.go` — HTML error response now escapes error messages with `html.EscapeString` preventing XSS
- `exception_listener.go` — use `LoggerFromRuntime` instead of `LoggerMustFromRuntime` to prevent panic when runtime logger is not available
- `router_utility.go` — wildcard locale route attribute used `RouteAttributeName` instead of `RouteAttributeLocale`, causing catch-all wildcards named `_route` to incorrectly write to the `_locale` param
- `middleware/compression.go` — `ReadAll` error discarded partially read data; now preserves whatever was read before the error
- `middleware/cors.go` — origin matching was case-sensitive; now uses `strings.EqualFold` for case-insensitive comparison
- `middleware/rate_limit.go` — `getClientIp` now uses `RemoteAddr` only; ignores `X-Forwarded-For` and `X-Real-IP` headers to prevent IP spoofing

### Changed

- `kernel.go` — remove dead nil checks on `MatchResult` (router `Match()` always returns non-nil)
- `profiling_kernel.go` — simplify request context extraction (remove guard on always-non-nil `Attributes()`)
- `request.go` — log warning when `ParseForm` fails (previously silent)
- `url_generation_route_definition.go` — `Defaults()` and `Requirements()` now return defensive copies
- Rename `security/security_test.go` to `security/test_helper_test.go`
- Remove redundant comments from modified files

### Added

- `test_helper_test.go` — shared test runtime helper for exception listener tests
- `exception_listener_test.go`, `request_test.go`, `response_test.go`, `middleware/compression_test.go`, `middleware/cors_test.go`, `middleware/rate_limit_test.go`, `url_generation_route_definition_test.go` — new and expanded test coverage for all fixes

## [v1.8.3] - 2026-03-21 - Refactor Address Colon Check in Config

### Changed

- `config/http.go` — replaced colon-based address check with `strings.Contains` for correct host:port detection

## [v1.8.2] - 2026-03-18 - Fix HTTP HEAD Handling and Update Dev Scripts

### Fixed

- `http/router_utility.go` — aligned HEAD handling and response contract validation; prevents incorrect responses on HEAD requests

### Changed

- `internal/reflect.go` — updated type-reflection utilities
- `.dev/validate/all.sh`, `.dev/validate/mod.sh` — added `-h` help flag to validation scripts
- `.gitignore` — updated patterns

## [v1.8.1] - 2026-03-17 - Fix JSON Logging Level Label Preservation

### Fixed

- `logging/contract/level.go`, `logging/logger.go` — preserved numeric logging level labels in JSON output; `logging/json_logger_test.go` — coverage

## [v1.8.0] - 2026-03-17 - Add Module Configuration Registration and Logging Labels

### Changed

- `.dev/run-batch.sh`, `.dev/utility.sh`, `.dev/validate/all.sh` — dev scripts optimisation

### Added

- `application/contract/config_module.go` — new `ConfigModule` interface allowing modules to register configuration during application boot
- `logging/contract/config.go`, `logging/logging_config.go` — `LoggingConfig` struct and contract for customizable logging level labels
- `logging/default_logger.go`, `logging/json_logger.go`, `logging/logger.go` — updated to apply level label customization from `LoggingConfig`
- `application/application.go`, `application/application_module.go`, `application/application_new.go` — wired `ConfigModule` into the application boot sequence

## [v1.7.3] - 2026-03-05 - Add CLI Table Width Flag and Fix Docker Profile Aliases

### Fixed

- `.dev/docker/.profile` — fixed Docker `.profile` aliases in interactive shells without recursion

### Added

- `cli/output/flag.go`, `cli/output/printer_selector.go` — added `--table-width` flag for table output
- `cli/output/option.go`, `cli/output/option_parser.go`, `cli/output/standard_flag.go` — parsed and propagated new width option

## [v1.7.2] - 2026-02-28 - Add CLI Stdout/Stderr Wiring and Standardize Method Receivers

### Changed

- All `.go` files in the module — standardized all method receivers to `instance` for consistent style

### Added

- `cli/command.go`, `cli/command_output.go` — wired `stdout`/`stderr` to CLI output; print command errors with failed exit status

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

### Fixed

- `security/security_resolution_listener.go` — make token source resolution panic-safe and always set security context; prevents nil-pointer panics when no token source is configured

### Added

- `application/contract/parameter_module.go`, `application/contract/service_module.go` — new `ParameterModule` and `ServiceModule` interfaces for granular application boot
- `application/application.go`, `application/application_module.go` — split boot around configuration resolve; wired new module contracts into the lifecycle

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

[Unreleased]: https://github.com/precision-soft/melody/compare/v1.18.1...HEAD

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
