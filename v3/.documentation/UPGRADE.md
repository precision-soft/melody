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

### Validation: the string-form constraints operate on strings

**What changed.** `regex`, `email`, `alpha`, `alphanumeric` and `numeric` refuse a value that is not a string instead of silently passing it, and `min`, `max` and `notBlank` no longer measure the fmt rendering of a non-string — all three refuse the type. A nil pointer and the empty string still pass the five format constraints, so optional-field composition is unchanged.

**Symptom.** A string-form rule on a non-string field — `regex` on `[]byte`, `max=10` on an `int`, `notBlank` on a `bool` — now rejects every value with `value must be a string`. It previously either passed everything (the format five, and `notBlank` on `false`/`0`/empty collections) or measured the Go rendering: an empty slice passed `min=1` as the two runes of `[]`, and a five-byte `[]byte` failed `max=5` at a rendered width of twenty-one.

**Remedy.** Move the rule to a string field, or use the constraint built for the type: [`greaterThan`/`lessThan`](../validation/constraint_greater_than.go) for numeric ranges, [`notEmpty`](../validation/constraint_not_empty.go) for collections. A rule that must inspect a non-string type is a custom [`contract.Constraint`](../validation/contract/constraint.go).

### Validation: a parameterized rule needs its parameters, whole and non-negative

**What changed.** Three related refusals in how rule parameters are read. A parameterized rule named without parameters (`regex`, `regex()`, `max()`, `lessThan`) fails closed instead of validating with the registered singleton — the bare `regex` validated with the match-everything `.*` default, the bare `lessThan` meant "less than 0", the bare `max` meant the constructor's 100. A numeric bound must be an integer in its entirety: the parse previously accepted a valid leading integer and dropped the rest, so `lessThan=-0.5` became a bound of 0 and accepted `-0.2`, which the tag as written refuses. And [`Regex.WithParams`](../validation/constraint_regex.go) refuses an empty pattern, while [`MinLength`](../validation/constraint_min_length.go) and [`MaxLength`](../validation/constraint_max_length.go) refuse a negative bound.

**Symptom.** A tag of any of these shapes now reports `invalidRuleSyntax` on every value it reaches, where it previously enforced a configuration nobody wrote — or nothing at all. The refusal reason travels in the error context under `cause`, and because `invalidRuleSyntax` is one of the rule-wiring codes the kernel already classifies, the 400 is recorded at error level and that context stays out of the response body.

**Remedy.** Write the parameter the rule needs: `regex(pattern=^[a-z]+$)`, `max=100`, `lessThan=0`. A pattern genuinely meant to match everything says so explicitly. The contract on [`ParameterizedConstraint`](../validation/contract/constraint.go) now states that the registered instance is a template for `WithParams` and never a fallback configuration.

### Validation: a negative length bound is refused at construction

**What changed.** [`validation.NewMinLength`](../validation/constraint_min_length.go) and [`validation.NewMaxLength`](../validation/constraint_max_length.go) panic on a negative bound, naming it. The tag door (`validate:"min=-1"`) refuses the same value with a parse error rather than a panic, because a tag is data a request path reads while a constructor argument is a declaration.

**Symptom.** A call passing a negative bound — almost always a computed value that came out wrong — now fails at construction instead of returning a constraint.

**Remedy.** Clamp the value at the call site if it can legitimately be negative: `max(0, bound)`. What the refusal replaces is worse than a panic in both directions — `NewMinLength(-1)` built a rule that accepted every value in silence, reading as enforced while validating nothing and leaving no record anywhere, and `NewMaxLength(-1)` built one that refused every value including the empty string with `this field must not exceed -1 characters`, which the client was handed.

### Validation: a tag that parses to no rule at all is a syntax error

**What changed.** A `validate` tag that survives the empty and skip-marker guards and then yields no rule — `validate:","` is the plain case — reports `invalidRuleSyntax` instead of being accepted as a request to validate nothing. The empty tag and the explicit skip marker `-` are unchanged and still skip the field.

**Symptom.** A field whose tag is malformed in this particular way now rejects every value, where it previously declared validation and enforced none.

**Remedy.** Write the rules the field needs, or write the skip marker `-` if the field is deliberately unvalidated.

### OpenAPI: the generated document follows the repaired validator, in the restrictive direction

**What changed.** The schema mirror in [`openapi/schema.go`](../openapi/schema.go) advertises what the repaired validator actually enforces, so the document generated by `melody:openapi:generate` and served by `SpecHandler` changes for every tag class the validation sections above describe. A bare parameterized constraint, an empty `regex` pattern, a malformed or negative `min`/`max` bound and a tag that parses to no rule are advertised as unsatisfiable fields — an impossible facet window, or `not: {}` beside a `$ref` — and such a field is now also listed `required`, because the validator rejects the absent zero value through the same refused rule. The string constraints on a non-string shape are advertised as the refusal they now are: a nullable field accepts exactly `null`, a non-nullable one nothing, and `notBlank` makes any non-string field unconditionally unsatisfiable. The stringified-length reading of `max` on numeric and boolean fields, the `[]byte`/`time.Time` exemptions and the value-embed brace floor are gone with the behaviour they mirrored. The lockstep is held by an executable proof, [`openapi/lockstep_test.go`](../openapi/lockstep_test.go) — the only place `openapi` imports `validation`, test-only — documented in [`OPENAPI.md`](package/OPENAPI.md).

**Symptom.** A published spec regenerated under this release shrinks or contradicts fields whose tags the validator refuses — and a spec-driven client, a contract test or a generated SDK starts failing on those fields at generation or validation time, where the previous document advertised satisfiable bounds the server never honoured and the failures arrived as production 400s instead.

**Remedy.** Treat every newly unsatisfiable field in the regenerated document as the misconfiguration report it is, and fix the tag: give the bare constraint its parameter, move a string rule onto a string field, replace `min`/`max` on numerics with `greaterThan`/`lessThan`. A document with no unsatisfiable fields means every declared rule is one the server can actually enforce.

### Logging: `LogError` anchors the record on the error it was handed

**What changed.** [`LogError`](../logging/logger.go) reads the level, message and context of the error the caller passed in, and reads the already-logged mark at the depth [`exception.MarkLogged`](../exception/utility.go) writes it — the nearest `AlreadyLogged` implementer in the chain. It used to search the chain for the nearest `*exception.Error` for both, which disagreed with the writer on every chain whose markable link is a different type, and anchored the record on whatever exception sat deepest. An error that is not a top-level `*exception.Error` now carries the context of the nearest context provider in its chain plus the cause chain walked from its own wrap link, where it used to be logged with no context at all.

**Symptom.** Records change level and content. An `HttpException` at 500 wrapping an info-level cause used to be filed at info and dropped whole by a production threshold; it is now filed at error with the wrapper's own message. A wrapped-then-marked error stops producing two records. Records for foreign errors gain `cause`, `causeChain` and `causeContextChain`.

**Remedy.** None; tooling that parses log records should expect the richer shape. An alert tuned to the volume of info-level records from wrapped http exceptions will see that volume move to error, which is where those failures always belonged.

### Logging: a record whose level is not one of the five weighs as an error

**What changed.** The json logger gives a record with an unrecognised level the priority of `error` instead of the default lowest priority. The label still carries the raw level, so the record says what it was handed.

**Symptom.** Records that used to vanish under any threshold at or above info now appear at error. The case that produced them is the zero-value `&exception.Error{}` a recovery handler can carry: its level is the empty string, it was filed as debug, and its fatal record was dropped by every production configuration.

**Remedy.** None. A deployment that was unknowingly discarding these records will see them; each one names a value constructed without a level, which is worth finding.

### Logging: the json timestamp carries nanosecond precision, and its order is the write order

**What changed.** The `time` field is `RFC3339Nano` rather than `RFC3339`, and the stamp is taken inside the write mutex together with the encoding, so the order of the stamps is the order of the writes. Taken above the lock, the stamp said when the record was FORMED, and the two orders diverged by however long the encoding took.

**Symptom.** The `time` field gains a fractional part. Whole-second stamps made every record of a busy second indistinguishable; the fraction parses under the RFC 3339 layouts a consumer already uses, so a parser reading `time.RFC3339` is unaffected. Concurrent writers pay for the ordering: the encoding is now serialized across them.

**Remedy.** None for a consumer that parses RFC 3339. A test asserting a whole-second stamp by string equality compares a prefix instead.

### Logging: the json logger reports its own failed write, once, on stderr

**What changed.** When a write to the logger's output fails, the first such failure of that logger's life is echoed to stderr. It is echoed directly rather than through the emergency logger, which is itself a json logger and would re-enter the write that just failed, and it stays silent when this logger's own output already is stderr.

**Symptom.** A new line on stderr — `melody: the json logger failed to write a record to its output: ...` — from a process whose log destination is full, read-only, or on a vanished mount. Previously the entire journal went silent with no signal on any channel and the operator read a healthy-looking empty file.

**Remedy.** None; the echo is once per logger, so a destination that fails on every record does not move the flood from one channel to the other.

### Logging: label maps are copied at every door that takes one

**What changed.** [`NewJsonLoggerWithLabels`](../logging/json_logger.go), [`NewDefaultLoggerWithLabels`](../logging/default_logger.go), [`NewLoggingConfiguration`](../logging/logging_config.go) and `LoggingConfiguration.LevelLabels` copy the label map instead of sharing it by reference.

**Symptom.** A caller that built a logger and then mutated the map it still holds no longer changes that logger's labels.

**Remedy.** Build the map before handing it over. What the copy replaces is a fatal concurrent map access no recover reaches: the map is read lock-free on every `Log` call, so a write after construction raced every record being written.

### Logging: a plain-text record is one line, whatever the payload says

**What changed.** The default logger and `LogError`'s process-logger fallback escape the control characters of the message and of the rendered context before writing them.

**Symptom.** A message or a context value containing a line break, a carriage return or an escape sequence renders escaped rather than being obeyed. Text of unknown origin — a header, a form field, a url — regularly reaches these lines.

**Remedy.** None. What the escaping replaces is a record that could be ended early by its own payload, with a fully-formed fake record started after it at whatever level the payload named; the json sibling has always had the same guarantee from its encoder.

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

### Cache: the key grammar is the contract's, on both backends

**What changed.** `cachecontract.Backend` now states the key grammar — non-empty, no spaces, no newlines, at most 1024 bytes — and the in-memory backend enforces it with the same refusals the redis backend always answered. The two implementations of one promise refused different keys.

**Symptom.** A key carrying a space or a newline — typically built from user input — now fails every operation against the in-memory backend with `cache key contains spaces`/`cache key contains newlines`, where it silently worked in development and failed only in production.

**Remedy.** Sanitize the key at the call site (hash it, or strip the whitespace). The refusal names the key.

### Cache: a payload the serializer cannot decode is a typed miss

**What changed.** [`Manager.Get`](../cache/manager.go) wraps a deserialization failure in the new [`DeserializationError`](../cache/deserialization_error.go), and [`Remember`](../cache/remember.go) treats that type as a miss: the callback recomputes and its `Set` overwrites the corrupt payload, so the key heals instead of staying poisoned until an expiry a ttl of zero postpones forever. [`Manager.Many`](../cache/manager.go) no longer discards the whole answer over one corrupt entry: the entry is left out the way an absent key is, and the error returned beside the good values is a `DeserializationError` naming the culprit keys deterministically. Every other error keeps meaning the cache itself failed.

**Symptom.** A key whose payload was corrupted out-of-band recovers on the next `Remember` instead of erroring forever; `Many` over a corrupt entry returns the good values plus a typed error naming the bad keys, where it used to return nothing and an anonymous encoding error.

**Remedy.** None for most callers. A caller of `Many` that treated any error as "no values" now also has the partial map available; a custom `Cache` implementation that wants the same self-healing under `Remember` wraps its own deserialization failures with `NewDeserializationError`.

### Cache: `Remember` answers one shape on the miss and on every hit

**What changed.** The computing call of [`Remember`](../cache/remember.go) returns the value passed through the manager's serializer round-trip — one local encode and decode, no backend involved — which is the exact shape every cached call answers. Until now one key had two shapes: the callback's own value on the miss, the decoded generic form on every hit, so a type assertion worked on the cold path and failed on the warm one, from the second call on. A `Cache` implementation that does not expose its stored shape answers the callback's value unchanged.

**Symptom.** A callback returning `int` is answered as `float64` on the FIRST call too, and a struct as `map[string]any`, where the miss used to answer the callback's own types. The cost is uniform rather than deferred: an integer beyond 2^53 comes back changed on the computing call as well.

**Remedy.** Type-assert against the decoded shape, or decode into a typed target. Where a cached value carries an integer that large, carry a version inside it or key it so it never decodes through JSON.

### Cache: the zero-value RememberOption reads as the defaults

**What changed.** A `&RememberOption{}` built as a literal used to silently disarm the stampede protection it never asked to configure, and its zero `waitTimeout` made every miss answer an instant timeout; [`Remember`](../cache/remember.go) now reads the exact zero-value option as `NewDefaultRememberOption()` — protection on, wait unbounded. Separately, a `waitTimeout` of zero set through the constructor now means no waiting rather than no answer: a result the in-flight call has already memoized is taken without blocking, and only a flight still in the air answers with the timeout.

**Symptom.** Callers passing a literal option regain the single-flight callback the default promises; callers polling with a zero timeout receive a completed result instead of a guaranteed error.

**Remedy.** None. A caller that genuinely wants protection off builds the option through `NewDefaultRememberOption().WithStampedeProtectionEnabled(false)`, which carries a non-zero shape and stays what it says.

### Cache: a value-kind Cache no longer coalesces in Remember

**What changed.** [`Remember`](../cache/remember.go) coalesces concurrent callers under one in-flight computation only for pointer-kind `Cache` implementations, whose address tells instances apart. A value-kind implementation used to share one flight per type: two different instances with different backends collapsed onto one leader, and the second caller received — and cached nothing from — the value computed for somebody else's cache. A value-kind instance now runs every callback itself.

**Symptom.** A struct- or map-kind `Cache` implementation loses stampede deduplication and never a correct answer.

**Remedy.** Implement `Cache` on a pointer receiver to regain coalescing.

### Cache: a recovered panic carries its cause

**What changed.** The recovery boundaries of [`cache.Remember`](../cache/remember.go) hand the panic value on as the CAUSE of the error they fabricate, and capture the stack of the goroutine that raised it. The `panic` context key they already wrote is unchanged; `panicStack` is added beside it. A panic raised by the cache itself — the backend or the serializer, on the leader's own reads and writes — is now reported as `cache remember cache access panicked` rather than being blamed on the callback, which the wrapper recovers separately.

**Symptom.** `errors.Is` and `errors.As` on the returned error now reach the failure underneath, where before they stopped at the fabricated wrapper. A diagnosis that followed `cache remember callback panicked` into a callback that never panicked now names the cache access instead.

**Remedy.** None for a reader that only renders the error. A caller that branches on `errors.Is` against a sentinel it also uses for non-panic failures should check whether it means to treat a panicked callback the same way.

### Serializer: a refusal of one type is not a refusal of the negotiation

**What changed.** `Accept: application/json;q=0` against a manager that also holds `text/plain` is answered with plain text rather than `406 Not Acceptable`. A type covered by a `q=0` range is recorded as refused and can never be served — by the negotiation or by the fallback, which steps past json when json is what was refused — and `ErrNotAcceptable` is answered only when every registered type is refused. A manager deliberately configured without json now answers an empty or unmatched accept header with its first configured serializer in lexical MIME order, where it used to refuse with `no default serializer configured` while a serializer sat beside the refusal.

**Symptom.** A request whose accept header names one type it does not want, against an application with more than one serializer registered, receives a representation where it used to receive a 406.

**Remedy.** None: this is what the manager's own comment and `SERIALIZER.md` promised. A client that wants nothing at all still gets its 406 by refusing everything — `*/*;q=0`.

### Serializer: the accept header is read on the one strict grammar

**What changed.** The manager's own header parser reads `q` through [`internal.ParseQualityValue`](../internal/quality_value.go) and splits members and parameters through [`internal.SplitOutsideQuotes`](../internal/split_outside_quotes.go), the grammar the http readers already shared. A `q` outside the RFC 7231 qvalue grammar drops its whole member instead of being scored as full acceptance, clamped into a fabricated refusal, or carried through as a `NaN` weight no comparison could select or refuse; and a comma or semicolon inside a quoted parameter value stays inside its member, so the `q=0` in `text/plain;version="1,2";q=0` keeps covering the type it names instead of being detached from it.

**Symptom.** `q=1e-1` and `q=1.5`, which used to be accepted, now drop their member. A header whose every member is malformed is refused by the manager, and the http result handler answers that refusal with the default serializer.

**Remedy.** Spell `q` within the RFC grammar — a zero with up to three decimal digits, or a one with up to three zero decimals.

### Serializer: two mime keys that normalize to one are refused at construction

**What changed.** `NewSerializerManager` refuses a map in which two spellings collapse onto one normalized key — `application/json` and `Application/JSON; charset=x` — naming the normalized key and both spellings in sorted order. The overwrite used to be silent, with map iteration order deciding which serializer survived, so the winner could change from one boot to the next and the loser vanished. A typed-nil serializer instance is refused exactly like the untyped nil, at construction rather than on the first request the negotiation routes to it.

**Symptom.** An application whose serializer map carries two spellings of one media type fails to build its manager instead of booting with an arbitrary winner.

**Remedy.** Register each media type once, under any spelling.

### Serializer: a plain-text payload is never aliased into the caller's buffer

**What changed.** `PlainTextSerializer.Serialize([]byte)` returns a copy rather than the caller's backing array, and `Deserialize` into a `*[]byte` stores a copy rather than the payload slice itself. A typed-nil pointer target is refused with the same error the untyped nil gets, where it used to pass the plain comparison, match its case and dereference nil on the assignment.

**Symptom.** None for a caller that did not rely on the aliasing. A caller returning a pooled buffer after `Serialize` no longer overwrites the bytes it believed it had snapshotted.

**Remedy.** None.

### Clock: `FrozenClock.Advance` refuses a negative duration

**What changed.** `Advance` accepted a negative duration and silently moved the frozen clock backwards, breaking the monotonic invariants the code under test relies on. It now panics with `invalid advance duration`; zero remains a no-op.

**Symptom.** A test advancing by a negative duration panics at the call site.

**Remedy.** Use `TravelTo`, the deliberate door for backwards motion.

### Clock and runtime: the nil refusals read through the interface

**What changed.** `ClockMustFromContainer`, `ClockMustFromResolver`, `runtime.New` and the `runtime` resolvers judge their arguments through the interface rather than with a plain `nil ==` comparison, so a typed nil is refused by name at the door it was handed to. `selectRuntimeResolver` reads the same way: a custom `Runtime` whose `Scope()` yields a typed nil no longer has its healthy container bypassed.

**Symptom.** A wiring mistake that used to surface as a nil dereference inside the container package — on the request path, with the panic blaming the container — is now refused where it was made, naming the argument.

**Remedy.** None.

### Event: `AddSubscriber` answers a registration, and `RemoveSubscriber` takes it

**What changed.** `AddSubscriber(subscriber)` returns an `event/contract.SubscriberRegistration`, and `RemoveSubscriber` takes that registration where it used to take the subscriber value.

**Symptom.** Code that calls `RemoveSubscriber(mySubscriber)` no longer compiles. Code that only ever ADDS subscribers compiles unchanged, because Go permits a call whose return value is discarded — so an application that registers at boot and never removes feels nothing at all.

**Remedy.** Keep what `AddSubscriber` returns and hand it back:

```go
registration := eventDispatcher.AddSubscriber(subscriber)
// later
removedCount := eventDispatcher.RemoveSubscriber(registration)
```

**Why the value could not stay.** A subscriber filed under its own pointer is not identifiable: a subscriber struct that carries no fields occupies no memory, and every zero-size allocation in Go answers one address, so two instances of such a type were one identity in the dispatcher. `RemoveSubscriber(a)` took down `b`'s listeners as well and reported a plausible count for it, and nothing in either value could ever tell them apart. Two consequences follow, both of them widenings: registering the same subscriber twice is now legal and produces two independent installations, and a subscriber that is not a pointer installs like any other — the refusal that guarded the pointer filing has nothing left to guard. The frozen majors took the narrower repair for the same defect, refusing the second registration of one identity; this major takes the redesign instead.

### Event: a dispatch that skipped a required listener fails closed, with a type you can assert

**What changed.** When a listener ends a dispatch early — by stopping propagation or by failing — and a listener marked required through `RequiredListenerRegistrar` sits behind it, the dispatch returns an `*event.RequiredListenerSkippedError` instead of completing, or instead of returning only the ending listener's own failure. The http kernel refuses such a dispatch on `kernel.request` and `kernel.controller` even when a listener produced a response, dropping that response for the error page.

**Symptom.** A listener that stops propagation and answers the request at a priority ahead of a required listener — an http cache above the access-control listener is the shape this was found in — now yields a 500 error page where it used to deliver its own response. A dispatch that used to complete quietly now returns an error.

**Remedy.** This is the guarantee working; the previous behaviour served pages with access control never consulted. If the listener is legitimately entitled to short-circuit past required listeners, mark it: `MarkListenerMaySkipRequiredListeners(registration)`. Both marks default off, so a dispatcher with no marked listeners behaves exactly as before. Assert the type on the error a dispatch returns DIRECTLY rather than through `errors.As`: a listener may dispatch further events, and a nested dispatch skipping a required listener of its own travels up as the cause of an ordinary listener error, which is a different and wider policy.

### Event: marking an unknown registration panics, and the adapter refuses a dispatcher that cannot mark

**What changed.** `MarkListenerRequired` and `MarkListenerMaySkipRequiredListeners` panic when the registration is not one the dispatcher holds, where an unknown registration used to be a documented silent no-op. `EventDispatcherAdapter` panics when the dispatcher it wraps does not implement `RequiredListenerRegistrar`, where it used to absorb the mark. `NewEventDispatcherAdapter` no longer takes a clock — the parameter was validated and never read, and `EVENT.md` had documented the one-argument signature from the beginning.

**Symptom.** A boot that marked a stale or hand-built registration now panics with `event listener registration is not registered`. An adapter over a custom dispatcher panics with `the wrapped event dispatcher cannot mark required listeners`. A call to `NewEventDispatcherAdapter(dispatcher, clock)` no longer compiles.

**Remedy.** Mark the registration `AddListener` returned, not one assembled by hand. Drop the second argument at the adapter constructor. If you wrap a dispatcher of your own and want the required-listener guarantee, implement `RequiredListenerRegistrar` on it — the silent absorption was the defect: callers probe for that interface precisely to learn whether the guarantee is available, and the adapter satisfies the probe on its own behalf, so swallowing the mark answered the probe yes and left the guarantee unarmed.

### Event and debug: subscriber declarations are validated whole, and `debug:events --verbose` changes its json shape

**What changed.** `AddSubscriber` validates every subscribed event before registering a single listener, and refuses a subscriber that declares no events as well as an event name mapped to an empty list. Under `--verbose`, the json document of `debug:events` carries `data.{events, listeners}` where it carried the event list alone; without `--verbose` the shape is unchanged.

**Symptom.** A subscriber whose declaration is partly malformed now panics before anything is installed, where it used to install the valid part and then panic. A subscriber assembled from configuration that produced an empty declaration now fails the boot instead of registering nothing and reporting success. A machine consumer reading `data.items` from `debug:events --format=json --verbose` finds it under `data.events.items`.

**Remedy.** Fix the declaration — the half-installed subscriber was the defect: its first event's listeners were live and firing under a subscriber the caller had been told was refused. For the json consumer, read `data.events.items` under `--verbose`, or drop the flag; the listener detail, including the marks that say whether the fail-closed guarantee is armed, is now reachable from json at all, which it was not before.

### Debug: `NewMiddlewareCommand` takes a description provider and a build provider

**What changed.** `debug.NewMiddlewareCommand` takes two channels where it took one: `MiddlewareDescriptionProvider`, which reports the pipeline as the selection and the ordering see it with no factory run, and `MiddlewareBuildProvider`, which produces the built chain. `debug.MiddlewareProvider` is removed. The constructor refuses a nil provider by panic, and a zero-value command answers a named refusal instead of calling a nil function.

**Symptom.** `debug.NewMiddlewareCommand(func() []httpcontract.Middleware { … })` no longer compiles. An application that lets `Application.bootCli` register the debug family — which is every application that does not wire the command by hand — feels nothing.

**Remedy.** Hand in the two providers the application already has:

```go
debug.NewMiddlewareCommand(
    func() ([]middlewarepipeline.MiddlewareDescription, *middlewarepipeline.MiddlewareBuildReport, error) {
        return myPipeline.Describe(kernelInstance)
    },
    func() ([]httpcontract.Middleware, error) {
        return myPipeline.BuildForInspection(kernelInstance)
    },
)
```

**Why one provider could not stay.** It returned the built chain, so listing the pipeline meant building it — every factory run and every dependency resolved as the price of a table — and the entries the selection dropped could not be reported at all, because a built chain does not carry them. The two channels are what let the default listing describe and `--build` build.

### Debug: `debug:container` and `debug:middleware` describe by default and build under `--build`

**What changed.** The bare `debug:container` listing runs no provider: it reports names, lifetimes, built state and the provider's declared return type out of the registration records. The resolve sweep that builds services sits behind a new `--build` flag; naming a single service still resolves that one. The bare `debug:middleware` listing describes the pipeline, with the inactive entries carrying their reasons, and `--build` runs the real factories under a recover. The `--build` sweep reports its failures on the envelope, with `error.code = "debug.buildFailed"`, the failure count and the failed names in the details, and the first failure as the cause.

**Symptom.** A script that ran `app debug:container` as a smoke test — relying on the listing to construct every service and exit non-zero when one did not — now sees it pass, because the listing no longer builds anything. The listing itself is faster and no longer opens connections.

**Remedy.** Add the flag where the building was the point: `app debug:container --build --format=json || exit 1`. Note that the `debug:*` family is registered on the development environment alone, `debug:router` excepted, so on a deployed application the invocation falls to the unknown-command path and exits `2` with `cli command not found` — a `|| exit 1` gate fires there for the wrong reason. A gate that must exercise the container has to run against a process booted in the development environment.

**Why the default could not stay.** `Get` was called on every name in the container, so listing a production wiring dialled every pool, opened every connection and built every singleton as the price of a table — the introspection door exists precisely so that listing a container does not mean building it.

### Debug: `debug:router` reports the two discriminators the dispatch breaks ties on

**What changed.** Every `debug:router` row carries `priority` and `order`, in the table and in the json document, and under `--verbose` also `requirements`, `defaults` and `attributes`. The listing's comparator is total: equal pattern and methods are broken by registration order.

**Symptom.** A machine consumer reading `data.items` finds four new fields per item, and two more under `--verbose`. A table consumer parsing by column position sees `priority` and `order` inserted after `name`. Rows that used to flip between runs now hold still.

**Remedy.** Read the fields by name. The additions are what makes the command answerable: priority and registration order are the two discriminators the dispatch itself uses, so two routes overlapping on pattern and methods rendered identically and the command that exists to say which one answers could not say it.

### Debug: `debug:version` reads the version rows off the envelope meta

**What changed.** The application row reads `envelope.Meta.Version.Application`, where `NewMeta` has already applied the fallback chain — the command's explicit `ApplicationVersion`, then the process-wide declaration made through `output.SetApplicationVersion`, then nothing.

**Symptom.** An application that declared its version through `output.SetApplicationVersion` — which is where a composition root's `main` declares it — now sees that version in the `application` row and in the json document, where it used to see an empty value.

**Remedy.** None; the row now reports what the process declared. An application that sets `ApplicationVersion` on the command itself is unaffected: the explicit value still wins.

### Container: a scoped registration under the protected `service.` prefix is refused at boot

**What changed.** Both scoped registration doors — `Container.RegisterScoped` and `Scope.RegisterScoped`, and the generic helpers over them — refuse a service name beginning with `service.`, with or without `Replacing()`.

**Symptom.** A boot that used to succeed now panics with `service is protected and cannot be registered as a scoped service`, naming the service. The wiring generator produces the same refusal for a constructor carrying `//melody:scoped` under such a name.

**Remedy.** Rename the scoped service out of the reserved namespace. The prefix is the framework's own, and the override path has always refused to substitute a `service.` name for a reason: a scoped registration of that name performs exactly that substitution, inside every scope the kernel resolves through, for the whole life of the process — the protected singleton is replaced once per request with nothing refused anywhere. A CONTAINER-lifetime registration of a `service.`-prefixed name is unaffected, so only the scoped ones have to move.

### Container: `Has` and `HasType` answer under the same suspension `Get` enforces

**What changed.** On the resolver a provider is handed, `Has` and `HasType` consult the request scope only when the resolution may read it. A provider the CONTAINER owns runs with the scope suspended, so it now hears `false` for a name only the scope carries — the same answer its `Get` gives.

**Symptom.** A container-lifetime provider that branches on `resolver.Has(name)` for a scope-only service takes the other branch. Where it previously went on to `MustGet` the name it had just been told existed, it no longer panics.

**Remedy.** Usually nothing: the previous answer was wrong in the dangerous direction. A container provider that genuinely needs a per-request value must not read it at construction — a process-lifetime singleton assembled out of one request's values holds that request for the life of the process. Take the value per call through the runtime or the request's own resolver instead, or make the service scoped.

### Container: `Scope.RegisterScoped` refuses a closed container, and a scope override must fit the types its name is registered under

**What changed.** Three refusals move earlier. `Scope.RegisterScoped` refuses once the container has begun closing, the answer `Container.Register` and `Container.RegisterScoped` already gave. A scope override propagates to every type its name is registered under — the container's registrations, the plan's and the scope's own — and is refused before anything is written when the value does not fit one of them, the rule the container-level door already applied.

**Symptom.** A registration made during shutdown fails where it used to report success. `Scope.OverrideInstance` / `OverrideProtectedInstance` returns `override value is not assignable to the registered service type` for a value that used to install; a test that substituted an unrelated implementation under a name registered under a concrete type is the usual case.

**Remedy.** For the override, register the name under the interface the substitute also satisfies — the guard admits raw assignability for an interface registration — or opt the registration out of the type with `WithoutTypeRegistration()`, which leaves the name with no registered type to fit. The refusal is what stops the name and the type maps from learning two different answers, which is what a type-keyed resolution then served.

### Container: a lazy handle over a request scope becomes terminal when that scope closes

**What changed.** `container.Lazy` and `container.LazyByType` ask the resolver they captured whether it has closed. The first closed answer makes the handle terminal: the scope-is-closed error on that call and on every later one, with the memoized value, the closure and the resolver dropped. `Scope.Closed()` is the door the question is asked through; a resolver that cannot answer it is read as open.

**Symptom.** A handle built over a request scope and used after the request stops answering the memoized value and returns `lazy service scope is closed`. `LazyService.Get` panics with that error rather than handing back a dead request's state.

**Remedy.** Build the handle over the container where it is meant to outlive the request — every container resolution mints a fresh resolver context, so that is also the form safe for concurrent first uses. Code shared across requests resolves per call through `FromResolver` with the current request's resolver; the value is then keyed to the right scope by the scope's own instance map. A handle built over the container is untouched by this change, because the container does not answer the liveness question.

### Http: the kernel and router configuration doors are boot-only and refuse after serving starts

**What changed.** `Kernel.Use`, `SetNotFoundHandler`, `SetErrorHandler`, `SetForwardedHeadersPolicy`, `SetSessionCookiePolicy`, `SetMethodPolicy` and every route registration door refuse by name once `Kernel.ServeHttp` has built the handler. The contract sentence is written on `httpcontract.Kernel`, `httpcontract.Router` and `httpcontract.RouteHandler`.

**Symptom.** An application that registers a route or installs a middleware lazily — from inside a handler, from a goroutine started after boot, from a hot-reload path — panics with `may not register a route after the http kernel started serving` or `may not configure the http kernel after it started serving`, naming the door.

**Remedy.** Move the call into the composition root or a module's registration hook, which run before the handler is built. There is no opt-out, and that is deliberate: every one of those doors writes state each request goroutine reads without synchronization, so what used to happen instead was a data race — and for the route tree's maps a concurrent write is an unrecoverable fatal error the process cannot recover from. The READING doors are untouched: `RouteDefinitions`, `RouteDefinition`, `RouteRegistry` and the introspection surface stay open for the life of the process, because the openapi document and the route manifest are served from handlers.

### Http: routes match the path as the client spelled it, and hosts fold case

**What changed.** Matching reads `URL.EscapedPath` and unescapes each segment after the split, so `%2F` stays inside a segment instead of separating two. `matchesHost` folds case and ignores the port unless the route declared one.

**Symptom.** A request to `/admin%2Fusers` no longer reaches the `/admin/users` handler; it reaches a single-segment route, binding `admin/users`. A route bound to `example.com` now also matches `Example.com` and `example.com:8443` — which is what makes it reachable in local development at all.

**Remedy.** Usually nothing. If an application relied on the decoded spelling reaching a deeper route, register the route the client actually addresses. A route that must discriminate on a port declares it: `example.com:8443` still matches only that port. Note that the url generator already refused a `/` inside a parameter value for exactly this reason, so no generated url changes.

### Http: a route declaration that contradicts itself is refused at registration

**What changed.** Four registration refusals join the ones already there: a pattern segment written as `{id}` rather than `:id`; a route declaring `Locales` whose pattern and defaults supply no `_locale`; a route whose pattern carries `:_locale` while declaring no `Locales` list; and route defaults now merge before the locale gate reads them.

**Symptom.** A boot that used to succeed panics by name. Each refused shape produced a route that was silently unreachable or silently unvalidated: the brace pattern matched only the literal url `/users/%7Bid%7D` and bound nothing; `Locales` without a locale parameter could never match anything; and `:_locale` without a list bound whatever the client sent and the kernel published it verbatim as the request locale.

**Remedy.** Write `:id` instead of `{id}`. Give a localized route either a `:_locale` segment or a `_locale` default, and declare the `Locales` list beside it — a route whose locale comes from a default is now reachable, which it was not before.

### Http: the access log redacts query values

**What changed.** The access-log record and the kernel's 405 and no-route records keep the query parameter names and replace every value with `xxxxx`.

**Symptom.** Log lines that used to read `"query": "token=abc123"` now read `"query": "token=xxxxx"`. A query that does not parse is redacted whole.

**Remedy.** Nothing to change. Any log pipeline matching on a query VALUE has to stop; matching on parameter names still works. This is also a patch-level fix on v1 and v2, because a credential in a url was being copied into the journal on every request.

### Example: the event routes are behind the firewall

**What changed.** In the reference application, `/events/publish` is a POST behind `RoleEditor` and `/events/stream` is behind `RoleUser`, with the subscribed topic authorized against the caller.

**Symptom.** `GET /events/publish?topic=…&text=…` no longer works; it is a POST and it requires an editor. `GET /events/stream` requires an authenticated caller, and the catalog topic requires `RoleEditor`.

**Remedy.** This is a change to the reference application, not to the framework, and it is here because the shape it replaced is one an integrator may have copied: a public `^/events` rule let an unauthenticated GET inject a frame into every stream open across the cluster — reachable cross-site from an `<img>` tag — and let an anonymous reader watch the product and user writes made behind `RoleEditor` and `RoleAdmin` go by in real time.


### Http: `NewRequirements` takes pointers and refuses an incomplete or duplicated declaration

**What changed.** `http.NewRequirements` moves from `...Requirement` to `...*Requirement` — what the `RequireAlpha`/`RequireNumeric`/`RequireAlphaLowercase`/`RequireAlphaNumeric` helpers have always returned — and refuses three declarations it used to drop in silence: an empty parameter name, an empty pattern, and one parameter named twice.

**Symptom.** A call site written as `NewRequirements(*RequireNumeric("id"))` no longer compiles; and a boot that used to succeed now panics by name if any declared requirement is incomplete or duplicated.

**Remedy.** Drop the `*`: `NewRequirements(RequireNumeric("id"))`. If the boot refuses, the refusal names the parameter and the index — an empty pattern is almost always a constant that was never filled in or a configured pattern that resolved to `""`, and until now it left that route parameter with **no constraint at all**, so a segment declared numeric matched anything.

### Http: `JsonHandlerErrorResponder` receives the failure beside the status and the message

**What changed.** The responder signature gains a trailing `cause error`. `JsonHandler` also binds through the same door `Request.BindJson` uses, so its refusals now carry the decoder's diagnosis as a cause, answer 413 for an oversized body instead of 400, and serve validation detail under the standardized `validationErrors` key.

**Symptom.** A responder passed to `WithJsonHandlerErrorResponder` no longer compiles. Clients of a `JsonHandler` route see a 413 where they saw a 400 for an oversized upload, and the error body for a validation failure changes shape: `error.message` becomes `"validation failed"` and the per-field detail arrives in the `validationErrors` array, exactly as it already did for `BindJsonAndValidate` routes.

**Remedy.** Add the parameter — `func(runtimeInstance, request, status int, message string, cause error)` — and ignore it if you do not need it. A client parsing the flattened message must read `validationErrors` instead; that array carries `field`, `message` and `code` per entry rather than a joined sentence. Note that a responder returning `(nil, nil)` no longer suppresses the refusal: previously that answered the client an empty **204** for a rejected request.

### Http: a route pattern, a route zone and a trusted-proxy entry are refused when they are wrong

**What changed.** Four boot-time refusals replace four silent drops. A pattern naming one parameter twice (`/orgs/:id/members/:id`) or carrying a nameless `:` is refused at registration. A zone that is not one of the `RouteZone*` values is refused, at `ExposedRouteAttributes` and at registration, as are an exposed route with no name, an expose attribute that is not a bool and a zone attribute that is not a string. A trusted-proxy entry that parses as neither a CIDR prefix nor an address is refused by `Kernel.SetForwardedHeadersPolicy` and by `NewForwardedClientIpResolver`.

**Symptom.** A boot that used to succeed now panics, naming the pattern, the zone or the offending entry.

**Remedy.** Each refusal names what to fix. The duplicate parameter was unreadable by the handler anyway — only one of the two values survived extraction, and which one was not the handler's to choose. The misspelled zone produced an artifact no filter could select: `melody:routes:manifest --zone <typo>` wrote an empty manifest over the good one and exited zero. The malformed proxy entry narrowed the trusted list, so `X-Forwarded-For` from that hop stopped being believed and every client behind it shared one rate-limit bucket. An unnamed catch-all (`*...`) is untouched, and an empty trusted-proxy entry is still skipped so a list split from an environment variable keeps working.

### Http: the route manifest carries the host, schemes, locales and priority

**What changed.** `RouteManifestEntry` gains `host`, `schemes`, `locales` and `priority`, all `omitempty`. Requirements are now published as the caller DECLARED them rather than as the anchored, non-capturing form the router compiles. Entries with equal names are ordered by pattern, so the document is stable. `FilterRouteManifestByZone` is exported so an in-process projection can apply the zone gate the cli command applies.

**Symptom.** A consumer that parsed the manifest sees new fields and a changed `requirements` spelling: `en|de` where it previously read `^(?:en|de)$`.

**Remedy.** A consumer applying the requirement as a regular expression should keep anchoring it itself; the published form is the developer's declaration, which is what a browser regular-expression engine can actually compile — the wrapped form leaked RE2-only syntax into ECMA-262 contexts. The three new fields are what a generated url needs to match on the way back in: a route bound to a host, restricted to a scheme, or restricted to a locale set was advertised with nothing saying so, and the url the consumer built was refused by the router that advertised it.

### Http: the server-sent-event writer refuses what it used to emit, and the hub closes what it owns

**What changed.** `NewServerSentEventWriter` refuses an already-committed response and a connection that cannot really flush (the probe now reads through the kernel's response-writer wrapper instead of at it). `Send` refuses an event carrying a name and no data, an id or name that would collapse to empty once its control bytes are removed, and a negative retry; a frame that failed partway poisons the writer, so every later frame is refused rather than appended onto torn bytes. The writer serializes its own frames, so a handler and a keepalive ticker may share it. `ServerSentEventHub` closes the backplane it owns on `Shutdown`, refuses to install one over a live one or into a hub that has already shut down, reads a typed nil as nothing, gains `Close() error` so the container's teardown can see it, and takes a logger through `SetLogger`.

**Symptom.** A `Send` call that used to return nil may now return an error — most often `ServerSentEvent{Event: "x"}` with no `Data`, which the event-stream grammar discards without dispatching anything, so the listener that named `x` never fired. A composition root that installed a second backplane over a live hub now gets a refusal naming the situation, where the first one used to be orphaned in silence. An application that resolved the hub from the container will now have it closed by the ordered teardown.

**Remedy.** Clear the hub and close what you took out before installing a replacement. Give every named event a `Data` payload, or send a comment instead. Handle the error `Send` returns — a stream whose write failed is finished, and the writer says so. If a hub was being kept alive past the container teardown deliberately, do not register it as a service.

### Http: a request path that folds to a different spelling is refused with a 400 before the handler

**What changed.** The kernel refuses any request path that `path.Clean` would fold to another spelling — `//admin/panel`, `/open/../admin/panel`, `/a/./b` — before the security dispatch and before the handler, with a 400 and a warning naming the method and path. A trailing slash is not a fold and is served as before; a target that does not begin with `/` (the asterisk-form of OPTIONS, an authority-form CONNECT) is left to the router.

**Symptom.** A client that spelled a working url with a doubled slash or a dot segment — some crawlers, hand-built paths concatenated with a stray `/` — starts receiving 400 where the route used to answer. Every such request was previously routed under its sent spelling but authorized against the folded spelling's rule, which is the authorization bypass the refusal closes.

**Remedy.** Fix the client to emit the canonical spelling; the refusal is deliberately not a redirect, so nothing teaches the client the working spelling while sidestepping a firewall rule. There is no opt-out.

### Http: a urlencoded body that failed to read or parse is refused, and a session save outage answers 500

**What changed.** A `application/x-www-form-urlencoded` body whose read failed answers 413 when the size limit stopped it and 400 when the client broke it, before the handler — it used to be a warning beside an empty form, with the handler running and often answering 200. A multipart upload past `MaxRequestBodyBytes` surfacing from `ParseMultipartForm` is answered 413 instead of a 500 at error level. Separately, a session-storage outage on the save path replaces the handler's response with an empty 500 and suppresses the session cookie; the handler's success used to be delivered with the session write silently lost.

**Symptom.** Endpoints that previously "accepted" oversized or truncated form submissions as empty forms now refuse them; a login or counter endpoint in front of a down session backend answers 500 instead of a success whose session never landed.

**Remedy.** None for the refusals — they surface failures that were being mis-answered. A handler that must serve degraded responses during a session-backend outage stops writing to the session on that path.

### Http: repeated request parameters are typed, and reading one as a single string refuses loudly

**What changed.** A request parameter that appeared once is stored in the bags as a string; a genuinely repeated key (`?tag=a&tag=b`) stays a `[]string`, and `bag.String`/`bag.StringOrDefault` on it panic toward `StringSlice`/`StringAt` instead of guessing. `Request.Input` answers the first value of a repeated key.

**Symptom.** Handlers that read single-occurrence query or post parameters through `Input`, `bag.String` or `StringOrDefault` start seeing the real values — they used to receive the empty string for every present parameter. Code that read a repeated key through `bag.String` now panics with a message naming the parameter, where it used to receive `("", true)`.

**Remedy.** Read repeated keys with `bag.StringSlice` or `bag.StringAt`; nothing else changes for well-typed readers.

### Http: the kernel contract gains `SetMethodPolicy`, and duplicate routes are refused at registration

**What changed.** `http/contract.Kernel` declares `SetMethodPolicy(MethodPolicy)` and the policy type moves onto the contract — a compile-time addition for out-of-tree implementations of the interface. The route registry refuses a second route identical on everything the matcher discriminates (pattern, methods, host, schemes, locales, requirements, priority), because the later one could never be dispatched and was silently shadowed.

**Symptom.** An out-of-tree `Kernel` implementation stops compiling until it adds the method. A boot that registered the same route twice — usually a module wired twice, or a copy-pasted registration — now panics naming the pattern, the methods and the route name.

**Remedy.** Add the one method to the custom kernel. Remove the duplicate registration; a route that must coexist differs in at least one discriminator. An application aggregating boot collisions arms `RouteRegistry.SetBootCollisionRecorder` for the boot window.

### Http: every framework error body is one standardized envelope, and the validation detail is under `validationErrors`

**What changed.** Every error the framework renders — the exception listener, the kernel's own fallback paths, and `JsonErrorResponse`, which the security entry points and the examples answer through — carries the one envelope `{"status": ..., "time": ..., "error": {"message": ...}}`, with `requestId` beside them where the request is known and the debug-only `context` and `cause` inside the error object. The body is also negotiated: a client that prefers html gets the html error page with the status and the request id, anything else goes through the serializer manager against the joined Accept lines, and an Accept header that refuses every available type keeps the error's own status with the json body — an error status is the signal itself, and masking a refusal behind 406 would hide it. The per-field validation detail reaches the body under the `validationErrors` key, projected so an entry blaming the rule DECLARATION keeps its field, message and code but loses the context naming the developer's own typo. v1/v2 spell that key `errors`; v3 diverges deliberately, because the key names what the detail is.

**Symptom.** A client parsing the flat `{"error": "<message>"}` body stops finding the message at the top level; a reader of the exception context keyed on `errors` finds `validationErrors`; a client of a failed validation starts receiving the per-field detail it never received.

**Remedy.** Read the message at `error.message` and the status inside the body at `status`; read the validation detail at `validationErrors`. The success half of the standardization — a data envelope over handler results, with a machine-readable error `code` — deliberately ships with the feature train, so nothing about successful responses changes here.

### Http static: the folded root spellings are refused, the excluded index stays excluded, and embedded assets get real validators

**What changed.** The file server refuses the spellings `path.Clean` folds into the mount root — `/..`, `/.`, `//`, `/open/..` — the same refusal every other path already earned, because the matchers in front of the application compare the raw path and those spellings reach the index page from behind whatever rule the other prefix carries. The exclusion list is also consulted against the index file the root resolves to, so excluding `/index.html` excludes it for `/` too. On an embedded filesystem — where files carry no modification time — `Last-Modified` is no longer emitted as the year-one instant and `If-Modified-Since` is not consulted, the entity tag carries the build version instead of the zero timestamp, and the tag reads the modification time at nanosecond resolution. `NewFileServer` copies its configuration, refuses nil options by name, proves an embedded public directory at construction, and honours an explicit `cacheMaxAge` of zero as always-revalidate instead of coercing it to an hour.

**Symptom.** Requests for the folded spellings fall through to the application (usually a 404) instead of answering the index page; clients that conditionally revalidated embedded assets stop being answered 304 forever; a deployment that set `MELODY_STATIC_CACHE_MAX_AGE=0` with cache enabled starts serving `max-age=0`; a boot with `MELODY_PUBLIC_DIR` naming a directory the build did not embed panics instead of serving 404s.

**Remedy.** None for the refusals. A deployment that relied on the hour of freshness sets `MELODY_STATIC_CACHE_MAX_AGE=3600` explicitly; a setter called after `NewFileServer` now configures the next server, so configuration lands before construction.

### Http cors: an empty origin list denies, entries are port-significant, and the preflight can be answered ahead of security

**What changed.** An explicitly empty (non-nil) `AllowOrigins` list denies every origin instead of silently becoming the wildcard, and credentials beside it are refused at boot under their own name. A schemeless allow entry is port-significant: `app.example.com` no longer matches every port of that host. `NewService` reads a nil method or header list as the default `DefaultService` grants — Authorization included. `cors.RegisterRequestListener` and the `RegisterListeners` façade arrive, answering a preflight at priority 100, ahead of token resolution — a preflight carries neither cookie nor Authorization by spec, so aimed at an access-controlled path through the middleware form it was refused opaquely. `CorsMiddleware(nil)` reads as the default service instead of panicking.

**Symptom.** A configuration whose origins variable arrives empty stops allowing everybody; an entry that relied on matching any port of its host stops matching the other ports; a SPA calling an access-controlled endpoint cross-origin starts working once the listeners are registered.

**Remedy.** Name the ports (or the scheme) in the allow entries; register `cors.RegisterListeners(eventDispatcher, service)` for applications with access-controlled cross-origin endpoints; keep the middleware form for applications without them.

### Http: the negotiation readers share one strict grammar

**What changed.** `PrefersHtml`, the compression middleware's `acceptsGzip` and the error-body negotiation read their headers under the serializer's rules: every line of a repeated field joined, members and parameters split outside quoted sections, the `q` parameter compared case-insensitively, and a member whose q falls outside the RFC 7231 qvalue grammar dropped whole. A repeated Accept-Encoding coding resolves to its higher quality.

**Symptom.** `gzip;Q=0` stops being compressed; `q=Inf`, `q=NaN`, `q=5` and `q=-1` stop being honoured as weights and drop their member; a refusal carried in a quoted parameter (`text/html;p="a,b";q=0`) or on a second header line starts being honoured.

**Remedy.** None; clients emitting grammar-conforming headers see no change.

### Exception: an out-of-range status is refused at construction

**What changed.** `NewHttpException` and `NewHttpExceptionWithCause` panic on a status below 100 or above 599 — net/http's `WriteHeader` panics on such a status deep in the response path, and a status the writer clamps serves an exception as success.

**Symptom.** A constructor call with a mistyped status (a `4004`, a `0`) panics where it is made instead of one response write away from it.

**Remedy.** Fix the status at the call site.

### Security: `NewAccessControlRule` is now segment-bounded, and the cross-segment form has its own name

**What changed.** `NewAccessControlRule` built a rule that matched every path merely beginning with the prefix — `/admin` governed `/administrator` as readily as `/admin/panel`. It now builds a rule bounded to a path SEGMENT: `/admin` governs `/admin` and `/admin/panel` but not `/administrator`. The cross-segment behaviour moves to the explicit `NewAccessControlRawPrefixRule`, on which `PUBLIC_ACCESS` is refused because a raw public rule, being the longest match, shadows a correctly bounded denial. An empty prefix is now refused rather than made a catch-all fallback, and `NewAccessControlRuleWithSegmentPrefix` becomes a deprecated alias for the plain name.

**Symptom.** An existing rule matches fewer requests than before — only its own segment and descendants under a `/` boundary — which can only refuse a request the old raw form would have granted, never grant one it would have refused. A rule that genuinely meant to reach across segment boundaries (a rule for `/admin` that was relied on to also govern `/admin-tools`) stops governing the sibling. A `NewAccessControlRule("")` used as a catch-all fallback now panics at construction.

**Remedy.** For a rule that must reach across the segment boundary, call `NewAccessControlRawPrefixRule`. For a catch-all fallback, declare an explicit `"/"` prefix, or use `NewAccessControlRawPrefixRule("")`. Most rules want the bounded form and need no change.

### Security: a global access control without a firewall now enforces, and a zero-value override inherits it

**What changed.** A `SetGlobal` access control declared without any firewall was silently dropped — `BuildAndCompile` returned nil the moment no firewall was registered — so the global rules enforced nothing. They now enforce: the compiled configuration carries the global control and the listeners fall back to it. Separately, a zero-value `FirewallOverrideConfiguration{}` now inherits the global access control the way `NewFirewallOverrideConfiguration()` does; it previously carried `inheritGlobalAccessControl=false` and compiled an empty control that opened every route behind the firewall.

**Symptom.** A deployment that declared global rules while every firewall sat behind a turned-off feature flag now has those rules apply — requests that used to reach handlers unauthenticated are refused. A firewall built with a bare `FirewallOverrideConfiguration{}` (rather than the constructor) now inherits and applies the global policy instead of leaving its routes open. `WithMergeStrategy` refuses an unrecognised strategy by name where it used to fall back to `localFirst` silently.

**Remedy.** None if the enforcement is what you meant; this closes a fail-open. A firewall that genuinely wants no global inheritance calls `WithInheritGlobalAccessControl(false)` paired with `WithAccessControl`. Correct any merge strategy string the boot now rejects.

### Security: the role voters and `DecideAll` refuse where they used to grant

**What changed.** `RoleVoter` and `RoleHierarchyVoter` granted a token that carried the role even when it answered `IsAuthenticated()` false; they now deny it, matching the access control listener, which had always checked authentication first. `AccessDecisionManager.DecideAll` over an empty attribute list now refuses instead of granting vacuously, matching `DecideAny`.

**Symptom.** A handler-level `IsGranted` check on an unauthenticated token that carries roles — a "remembered" or half-logged-in token — now returns false where it used to return true. A direct `DecideAll(token, nil, subject)` caller now receives a refusal.

**Remedy.** None if you relied on authentication being required, which is the safe reading. A caller that intentionally passed an empty attribute list to `DecideAll` expecting a grant must pass the attributes it means to check.

### Security: the internal-auth envelope carries its own `typ`, and the JWT validator refuses it

**What changed.** The internal-auth envelope and the JSON web token were both HS256 over the same signing primitive in a byte-identical three-part shape, with nothing but secret disjointness — which nothing enforces — telling them apart. The envelope now signs under `typ: "melody-internal"`, the decoder REQUIRES that type, and the JWT validator refuses any `typ` that is not absent or `JWT` (case-insensitive).

**Symptom.** An envelope minted by an earlier build of the signer is rejected by an upgraded verifier ("internal-auth type is not accepted" in the journal at Info). With the default thirty-second envelope ttl, the exposure is one rolling-deploy window in a fleet where signers and verifiers upgrade at different moments. A JWT minted with an exotic `typ` (anything other than absent or `JWT`) is now refused.

**Remedy.** Upgrade verifiers and signers within the envelope ttl of one another, or tolerate the one-window rejections — they fail closed to anonymous, exactly as an expired envelope does. A JWT issuer that stamps a non-standard `typ` must mint with `JWT` or drop the field.

### Security: both token stores refuse a non-positive ttl on `PutWithTtl`

**What changed.** `InMemoryTokenStore.PutWithTtl` and `RedisTokenStore.PutWithTtl` stored the token with NO expiry when the ttl was zero or negative. Both now panic on `ttl <= 0`, naming the value; the contract sentence on `RevocableTokenStore` says so.

**Symptom.** A caller computing a token's remaining lifetime that has already elapsed — the likeliest source of a non-positive ttl — used to leave a permanent entry; it now fails loudly at the call site.

**Remedy.** A caller that genuinely means "no expiry" calls `Put`, which is that contract's name. A caller passing a computed remainder guards the sign and treats an elapsed lifetime as the expired token it is.

### Security: the in-memory store keeps revocation boundaries until their retention passes

**What changed.** `InMemoryTokenStore.PurgeExpired` dropped every revocation boundary of a user who held no stored tokens; stateless JWTs are never stored, so a JWT-only deployment lost its boundaries on the first purge. Boundaries are now kept forever by default, and the new `WithRevocationEpochRetention` bounds their life the way the rueidis store's option of the same name does. Separately, a negative `JwtConfig.RevocationEpochSkew` and negative values passed to the rueidis `WithTokenStoreMaximumClockSkew`/`WithRevocationEpochRetention` options are refused at construction instead of narrowing a revocation or being silently ignored.

**Symptom.** A long-lived process using the in-memory store as its epoch store retains one entry per revoked user indefinitely unless a retention is configured. A boot passing a negative skew or retention now fails at construction where it used to run a different policy in silence.

**Remedy.** Configure `WithRevocationEpochRetention` to at least the longest token lifetime you mint — a boundary outliving every token it can refuse changes nothing thereafter. Fix any negative duration the boot now rejects; the sign was never doing what its author meant.

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

### Bunorm migrate: the held-lock refusal names the resource and the remedy

**What changed.** `<prefix>:migrate` and `<prefix>:rollback` wrap bun's lock error in a melody error naming the manager label, the lock table and the `<prefix>:unlock` command, with bun's error kept as the cause. It previously travelled as bun's own error, carrying no melody context at all.

**Symptom.** Code matching the refusal on the text `already locked` no longer matches at the top of the chain: `Error()` answers `migrate: the migration lock is held; another migration is running, or a crashed one left it behind`.

**Remedy.** Match through the chain — `errors.Is` and `errors.As` reach bun's error exactly as before — or read the `manager`, `locksTable` and `unlockCommand` keys of the context, which is what the wrap exists to provide.

### Bunorm migrate: a failed lock release fails the command

**What changed.** A `db:migrate` or `db:rollback` whose unlock fails now returns that failure instead of printing it and exiting 0. A migration that itself failed keeps its own error as the verdict, with the unlock failure printed beside it. The release also runs on a context detached from the command's own, so an interrupted migration no longer leaves the lock row behind.

**Symptom.** A deploy step that read exit 0 over a surviving lock row — and then found every later migration refused on every replica — now fails at the step that caused it.

**Remedy.** None for a healthy run. For the failure, `<prefix>:unlock` clears a lock a crashed process left behind, and the refusal above names it.

### Bunorm migrate: the json document is not shaped by `--verbose`, and its keys are stable

**What changed.** Under `--format=json`, `db:migrate`, `db:rollback`, `db:status`, `db:init` and `db:unlock` render one machine-readable envelope where they used to print the plain-text blocks, and they collect every block at any verbosity: verbosity remains a rendering decision about the plain text alone, which is what the readme always said. The document keys are stable names rather than display headings — `data.migrations.applied`, `.pending`, `.rolledBack` — and `data.database.database` is json `null` when the connection reports no current database, where the text block renders `<null>`. `--format=json-pretty` is the same document indented for reading by hand.

**Symptom.** `db:status --format=json | jq` used to fail on the first byte of a plain-text table; it now decodes. A json run performs the database-identity query that a text run performs only under `--verbose`.

**Remedy.** Read `data.migrations.applied`, `data.migrations.pending` and `data.migrations.rolledBack`; test `data.database.database` for null rather than for the string `"<null>"`. Anything that parsed the plain text under `--format=json` must decode the document instead — which is what the flag always promised.

### Bunorm migrate: a nil migration set and an empty module configuration are refused

**What changed.** `RegisterCommands(nil, ...)` panics at registration instead of answering no commands, and `NewModule(ModuleConfig{})` — neither `Migrations` nor `Contexts` — is refused by name when the kernel asks it for its commands. Both used to be silent.

**Symptom.** A binary whose wiring passed a nil set, or registered the module with an empty configuration, fails at boot with a named refusal where it used to boot and answer `unknown command` at the first `db:migrate`.

**Remedy.** Pass the migration set, or the contexts, that the registration was meant to carry. A binary that registers only `Contexts` is unaffected: the module gates its own optional set before calling `RegisterCommands`.

### Bunorm migrate: the plain text escapes control characters, and the commands stop pre-printing their failure

**What changed.** Every string the commands did not write themselves — the error text off the wire, the failed statement, the query names, the identity block the server answers and the migration names — is escaped visibly (`\n`, `\r`, `\t`, the rest as `\xNN`) before the terminal sees it, and before the table cells are measured, so the alignment counts the escaped spelling. The failed statement alone keeps its real line breaks. Separately, the commands no longer pre-print the failure they return: the cli runner's `[error]` line and the log record already report it. The json rendering is untouched — its encoder escapes on its own.

**Symptom.** A test asserting an exact rendered line that contained a raw control byte sees its escaped spelling. A console that showed the same failure three times shows it twice.

**Remedy.** Assert on the escaped spelling, which is what an operator's terminal now receives. The deliberate exception stays: an unlock failure beside a failed migration is still printed, because the return keeps the migration's error and would otherwise lose it.

### Bunorm migrate: the verbose DATABASE block answers for PostgreSQL, and cells are cut by runes

**What changed.** The `--verbose` DATABASE block was MySQL-only, so on a pgsql-backed manager the operator's confirmation of which database a migration was about to touch was silently absent rather than reported as unavailable; it is answered now through `current_database()`, `inet_server_addr()`, `inet_server_port()`, `current_user` and `version()`. Table cells are truncated by rune count rather than by bytes, matching the format widths that pad by runes.

**Symptom.** `db:migrate --verbose` against postgres prints a DATABASE block where it printed none. Over a unix socket — the connection a local migration is most likely to take — the host reads `<local socket>`, because `inet_server_addr()` is NULL there. A multi-byte value that fit its column is no longer truncated, and no cut lands mid-rune.

**Remedy.** None. Read the block; it reports the same five fields on both dialects.

### Rueidis: the boot ping is bounded even when the connect timeout is left at zero

**What changed.** `Provider.Open` runs its boot ping under the connect timeout resolved through the same rule the rest of this package reads its zeros by: a non-positive `TimeoutConfig.ConnectTimeout` takes the default (3s) rather than meaning "no bound". It previously built the ping context only for a positive value and ran the ping on `context.Background()` otherwise — with no deadline at all.

**Symptom.** A `TimeoutConfig` naming only the command timeout, the ordinary shape of a partial literal, put the boot ping on an unbounded context. A store that accepted the connection and then stopped answering hung the boot forever, holding a client no one could close yet. That boot now fails after the default bound with `redis connection failed`.

**Remedy.** None for a wiring that meant the ping to be bounded — this is the bound it always documented. A wiring that genuinely wanted an unbounded ping must now say so by turning the ping off (`ClientConfig.PingOnStart = false`) and pinging itself; a value large enough to stand in for "unbounded" also works, and reads as the deadline it is.

### Rueidis cache: `Backend.Close` ends the backend, and `ClearByPrefix` refuses the empty prefix

**What changed.** Two refusals the in-memory backend behind the same contract already gave. `Backend.Close` marks the backend closed and every later operation answers `cache backend is closed`; a handle minted by `BackendService.WithContext` reads its owner's flag, so the service's `Close` reaches the per-request handles the runtime door mints. And `ClearByPrefix("")` is refused as an empty key instead of being read as the whole namespace. The shared `rueidis.Client` is still not closed by the backend: it belongs to whoever built it.

**Symptom.** Code that used a backend after closing it — a teardown-ordering bug — used to keep serving through the client and now fails immediately with a named refusal. Code that passed an empty prefix to `ClearByPrefix`, deliberately or because a prefix assembled at run time came out empty, used to wipe every entry under the backend's own prefix and now gets `cache key is empty`.

**Remedy.** Fix the teardown order so nothing reads through a closed backend, which is what the refusal is for. Where the whole namespace really is meant, call `Clear`, which says so. `WithCommandTimeout` is available for the other half of the same problem: it bounds the operations that carry no caller context, which is the whole ctx-less half of `cache/contract.Backend`.

### Rueidis: a rate limiter store failure is recorded when no observer is wired

**What changed.** With no `WithRateLimiterOnError` observer, a store failure is now recorded by the limiter itself — at error for an outage, at warning for the caller's own cancellation — and the error is marked already-logged so the http sites do not file it a second time. An observer given by the application still replaces that record rather than adding to it, and receives the error untouched and unmarked. The limiter also names the caller's cancellation apart from a store failure, and re-arms the window on a key that reached the store carrying no expiry.

**Symptom.** New records appear in the journal from applications that wire no observer, where a redis outage previously refused every call and reported nowhere at all: `Allow` answers a bool and `Reset` answers nothing, so there was no channel for it to reach. A counter on a key that had lost its ttl — a `PERSIST`, an eviction, a hand-written key — used to climb past the limit and stay there, refusing every request keyed on it permanently under the fail-closed default; it now lapses.

**Remedy.** None required. An application that wants the failures on its own channel and nowhere else wires `WithRateLimiterOnError`, which is what it was always for.

### Rueidis cache: a negative ttl is refused, and a batch failure names one key deterministically

**What changed.** `Set` and `SetMultiple` refuse a negative ttl instead of falling into the branch that writes no expiry at all, and `SetMultiple` judges the ttl before its empty-batch early return. A batch that fails part-way collects every failing key and names the sorted first, with the failed and requested counts beside it, rather than returning whichever failure the map iteration reached first. `Clear` and `ClearByPrefix` scan every node of the client rather than the one connection a single command reaches.

**Symptom.** A negative ttl used to store the entry **forever** — the one value the caller meant to be short-lived was the one value stored without end. A batch that failed named a different key on every call for the same wrong batch, and hid that the entries after it failed too. Against a redis cluster, a wipe deleted one node's share of the matching keys and reported success.

**Remedy.** Zero still means no expiry, as both backends document, so only a genuinely negative duration is affected — usually a subtraction that went past the present. Anything that parsed the batch failure message should read `key`, `failedKeyCount` and `requestedCount` from the error context instead.

### Cron: a module with runner commands and no configuration refuses the boot

**What changed.** `cron.NewModule`'s `RegisterCliCommands` panics when `RunnerCommands` are supplied without a `Configuration`/`ConfigurationFactory`, and when a factory returns nil. Until now the module silently registered nothing and the wiring error surfaced as "unknown command" at invocation.

**Symptom.** A wiring that carried runner commands but never set the configuration now fails at boot naming the missing configuration.

**Remedy.** Set `Configuration` (or a factory that returns one); a parameters-only module — no runner commands, no generator — keeps working without one.

### Cron: an entry routed to another crontab file refuses the in-process runner

**What changed.** `EntryConfig.DestinationFile` joins `Command` and `Instances` in `NewRunnerCommand`'s construction refusal: an entry routed to another crontab addresses an external scheduler, and accepted by the runner as well it executed twice whenever the generated manifests were live.

**Symptom.** A boot that used to succeed panics with `cron: the in-process runner supports only name-scheduled single-instance entries; the entry routes to another crontab file`.

**Remedy.** Keep the routed entry out of the runner's `Configuration` (schedule it only for the generator), or drop its `DestinationFile` if in-process execution is the intent.

### Cron: a clean shutdown is not a job failure

**What changed.** A run the runner's own shutdown cancelled is recorded at warning as `cron: scheduled command cancelled by shutdown` and excluded from the failure aggregate; the runner's failure and abandon records carry the run's `cronRunId`.

**Symptom.** `melody:cron:run --once` interrupted by SIGTERM exits 0 with a warning instead of non-zero with an error record; alerting keyed on `cron runner command failed` stops firing on deploys.

**Remedy.** Key deploy-time alerting on the new warning if the old signal was load-bearing; genuine failures keep the error record, now attributable by `cronRunId`.

### Cron: `Configuration` hands out copies

**What changed.** `Schedule` copies the entry configuration it is handed, and `Entries` copies all the way down — the list, each `*ScheduledCommand` and each `*EntryConfig` behind it, schedule included.

**Symptom.** Code that reconfigured a registration by writing through its own struct after `Schedule(...)`, or through what `Entries` returned — `configuration.Entries()[0].Config.Schedule.Hour = "23"` — no longer changes anything.

**Remedy.** Register the entry with the configuration it should have. `Entries` is an inspector; the registry is written through `Schedule`.

### Cron: the runner writes the machine document, and a job's output goes to the journal

**What changed.** `melody:cron:run` accepts the standard flag set every melody command accepts — the framework rewrites `-v`/`-vv` into `--verbosity` for every command, which used to kill the runner at parse — and under `--format=json` renders one envelope per dispatched minute (`--once` writes exactly one and exits). A scheduled command's own output no longer reaches the process stdout: it is captured and filed as one record per run that printed anything, naming the command and the run id, capped at 64 KiB.

**Symptom.** `melody:cron:run --once --format=json` answers a document instead of an empty stream, and `-v` no longer fails with "flag provided but not defined". Anything tailing the stdout of `melody:cron:run` for a job's own printed output finds it in the log instead, under `cron: scheduled command output`.

**Remedy.** Read the document rather than inferring the outcome from an empty stream. For a job's own output, read the journal; a job that must write to a stream of its own should open it itself rather than relying on the command writer.

### Cron: a recovered panic carries its cause

**What changed.** The cron runner's recovery boundary hands the panic value on as the CAUSE of the error it fabricates, and captures the stack of the goroutine that raised it. The `panicValue` context key it already wrote is unchanged; `panicStack` is added beside it.

**Symptom.** `errors.Is` and `errors.As` on the run's error now reach the failure underneath, where before they stopped at the fabricated wrapper. Code that relied on those calls answering false for a panicked job will now see them answer true.

**Remedy.** None for a reader that only renders the error. A caller that branches on `errors.Is` against a sentinel it also uses for non-panic failures should check whether it means to treat a panicked job the same way; the message still says the boundary was a panic.

### Cron: the deprecated abbreviated validation aliases are removed

**What changed.** `ForbiddenChar`, `CrontabForbiddenChars` and `ValidateNoForbiddenChars` are gone from the cron binding. They were deprecated aliases of `ForbiddenCharacter`, `CrontabForbiddenCharacters` and `ValidateNoForbiddenCharacters`, which are unchanged.

**Symptom.** Code naming an alias stops compiling with an undefined-identifier error.

**Remedy.** Spell the name out; the replacement is a rename, signature-identical. The templates have read `CrontabForbiddenCharacters` since the aliases were deprecated, so nothing behavioural changes.

### Cron: the generated k8s manifests open with the ownership marker

**What changed.** Every file the builtin `k8s` template renders starts with three comment lines carrying `# owned by melody:cron:generate`, the same marker the crontab dialects carry in their header block, and the template renders the marker header alone — demanding no container image — when it has no entries. That is what lets `--prune` reconcile a k8s output directory: the sweep empties only a file whose first bytes prove this generator wrote it.

**Symptom.** Manifests gain three leading YAML comment lines; a byte-exact comparison against previously generated files sees the difference. `kubectl apply` reads comments as comments — the documents are unchanged.

**Remedy.** Regenerate; anything diffing manifests byte-for-byte should regenerate its baseline. Nothing to do for the cluster.

### Cron: a malformed heartbeat opt-in fails the generation

**What changed.** A `melody.cron.heartbeat.enabled` value that does not hold a boolean fails `melody:cron:generate` with an error naming the parameter, under every template. It used to be read as "not enabled".

**Symptom.** A generation that silently produced a crontab without the liveness line the operator asked for — a misspelling indistinguishable from never having asked — now exits non-zero naming `melody.cron.heartbeat.enabled`.

**Remedy.** Fix the value; `true`/`false` in any spelling the parameter conversion accepts. Removing the parameter keeps the opt-in unset, which stays legal.

### Cron: a relative parameter path anchors at the project directory

**What changed.** A relative path read from a parameter — `melody.cron.destination_file`, `melody.cron.logs_dir`, `melody.cron.heartbeat_path` — is anchored at the project directory, the way melody resolves `kernel.logs_dir` and its siblings. A relative path typed as a cli flag stays relative to the working directory, as a shell path should.

**Symptom.** `melody.cron.logs_dir = var/log/cron` used to mean a different directory depending on where the binary was invoked from — under a supervisor starting from `/`, the generated crontab baked `/var/log/cron` into itself. The shipped defaults carry `%kernel.project_dir%` and never moved.

**Remedy.** None for parameters using the shipped `%kernel.project_dir%`-anchored defaults or absolute paths. A deployment that relied on a relative parameter path following the working directory should either make the path absolute or run the generator from the directory it means.

### Cron: the generator writes the machine document and names what a failed run left on disk

**What changed.** `melody:cron:generate` accepts the standard flag set every melody command accepts — the framework rewrites `-v`/`-vv` into `--verbosity`, which used to kill the generator at parse — and under `--format=json` renders one closed envelope: `data.writes` and `data.pruned` as lists on every run, the failure inside the envelope's error with its context and cause chain. In text mode a failed run prints the writes and sweeps that had already happened before returning the failure.

**Symptom.** `melody:cron:generate --format=json | jq` answers a document instead of an empty stream, and `-v` no longer fails with "flag provided but not defined". The heartbeat-only text line now says `wrote heartbeat-only crontab to …`, as the frozen majors' does.

**Remedy.** Read the document rather than inferring the outcome from the exit code alone; anything matching the old `heartbeat-only file` text updates the one word.

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

### Application: a teardown failure on Run's normal return exits non-zero

**What changed.** When `Run` returns without a panic, the teardown it performs turns a failure it discovered itself — a container close that errored, a flush that failed — into exit code 1. Previously the failure was logged at emergency and the process exited 0.

**Symptom.** A supervisor that restarts on non-zero now sees a shutdown that lost something, where it used to record a clean stop whose only trace was one log line. A close somebody else already performed keeps reporting through its own channel and its own exit code, so nothing is reported twice.

**Remedy.** None for a healthy teardown — exit 0 is unchanged. A deployment that alerted on the emergency record alone can keep doing so; the exit code is an additional signal, not a replacement.

### Application: a teardown that hangs is abandoned and exits non-zero

**What changed.** The normal return of `Run` closes the container through the same ten-second shield the panic path now uses, and takes exit code 1 when the budget runs out. Previously the clean path had no budget at all: one `Close` that never returned parked every service behind it and the process with them, so the healthy shutdown was the one without an emergency exit while the dying one had a way out.

**Symptom.** A process whose teardown blocks for more than ten seconds now prints one line to stderr naming the abandoned step and exits 1, where it used to hang until the supervisor killed it.

**Remedy.** None required — the exit is the intended outcome. A service whose `Close` legitimately takes longer than ten seconds should bound its own work: the shield abandons the step, it does not shorten it.

### Logging: every fatal exit writes a certificate record at emergency level

**What changed.** The process-boundary exit handler writes one record at emergency level — "process exiting after unrecovered error", with the exit code and the error in its context — through whatever logger it resolved, always. The detailed record of the failure is still written at the error's own level, which an operator threshold can silently discard; the certificate is the one record no threshold drops.

**Symptom.** A log pipeline sees one new emergency record per fatal exit, beside the detailed record it may or may not have been keeping. A pipeline alerting on emergency level now fires on every fatal exit, which is what the level exists for.

**Remedy.** None for most deployments — the record is the signal that was missing. A pipeline that must not see it filters on the record's message, which is stable.

### Logging: the correlation id wins the context key

**What changed.** The correlation decorator — `NewRequestLogger` on the request path, the new `NewProcessLogger` on the console path — writes the real id under the context key unconditionally. A non-empty string already sitting under the key used to make the decorator keep it and drop the real id. On the request path a different non-empty string claim now survives under the key suffixed `Claimed`; on the console path the caller's value survives verbatim, whatever its type, under the key suffixed `Provided`.

**Symptom.** A record whose context carried its own `requestId`/`processId` value now shows the generated id under that key and the caller's value under the suffixed one. Anything grepping the log by the id melody generated now finds every record of the run, including the ones that used to escape the chain.

**Remedy.** A consumer that deliberately overrode the key should read its value back from the suffixed key; everything else needs no change — the correlation is simply no longer forgeable from a context value.

### Config: a late secret marking travels to the parameters assembled from the key

**What changed.** `MarkSecret` called after the boot resolution now propagates to every parameter whose template reads the marked key, and follows the marking to a fixpoint through derivation chains — exactly what a marking arriving before the resolution has always done. The scan reads the raw templates; a match inside doubled-percent escaped text over-marks, which errs toward redacting more, never less.

**Symptom.** `debug:parameters` redacts values it used to print: the dsn assembled from a password marked late now shows as secret beside the key itself. No stored value changes — the marking governs display, not storage.

**Remedy.** None. A parameter that must stay visible should not read a marked credential in its template.

### Httpclient: a basic credential travels whenever it was asked for

**What changed.** `WithBasicAuth("", password)` now sends the credential. The username guard used to drop the whole authorization silently, so an api key spelled as the password of an empty user — the shape of curl's `-u :key`, legal under RFC 7617 — produced an unauthenticated request presented as a success. A typed-nil authorization or basic half now leaves the header unwritten instead of panicking on the request path.

**Symptom.** A request built with an empty username and a non-empty password reaches the server with an `Authorization` header it never carried before.

**Remedy.** None for a correct caller. A caller that relied on the empty username to mean "no credential" passes no basic authorization at all.

### Httpclient: the response body cap binds the stream, the default included

**What changed.** `RequestStream` enforces `MaxResponseBodyBytes` — the cap was completely inert on the streaming path — and the inherited default of ten mebibytes now binds a stream whose caller never named a cap. Reading past the cap answers "response body exceeded max size" with the cap, the method and the sanitized url in the record. An invalid cap is also refused before anything is dialled, on both paths, so a POST no longer commits its side effect before being told its options were invalid.

**Symptom.** A stream that delivers more than ten mebibytes — a large download, a long-lived server-sent-event feed — errors mid-read where it used to run unbounded.

**Remedy.** Name the cap the stream actually needs through `WithMaxResponseBodyBytes`; a genuinely unbounded consumer sets one sized far above its expected traffic. Callers of the buffered `Request` are unaffected — the default has always bounded them.

### Opaque tokens: a stored token with no issue instant is refused once its user is revoked

**What changed.** A revocation is no longer an enumeration. [`security/contract.RevocationEpochStore`](../security/contract/token_store.go) publishes a boundary per user, and per device of a user, and [`Lookup`](../security/in_memory_token_store.go) refuses a token issued before the boundary that governs it. This closes the window [`DeleteByUser`](../../integrations/rueidis/v3/token_store.go) could never close: it walks an index with `SSCAN`, which does not promise to return a member added while the walk is in progress, so a token issued during a revocation survived it. The comparison needs an issue instant, so [`security/contract.Claims`](../security/contract/token_validator.go) carries `IssuedAt`, stamped by the store on every write.

Nothing breaks at compile time. The new methods live on their own interface, composed into `EpochRevocableTokenStore` rather than added to `RevocableTokenStore`, so an out-of-tree token store still satisfies the interface it was written against — it simply cannot publish boundaries, and a caller that needs one is told so by `EpochRevocableTokenStoreMustFromResolver` at the moment the service is asked for.

**Symptom.** A token stored by an earlier release carries no issue instant. The zero instant precedes every boundary, so the first time `RevokeBefore` is called for a user, that user's pre-upgrade tokens stop resolving — including ones an operator did not mean to end. Users nobody revokes are unaffected: with no boundary there is nothing to compare against and the token resolves exactly as before.

**Remedy.** None is needed in the ordinary case, and the behaviour is the safe direction: the tokens that stop resolving belong to an account somebody deliberately revoked. If an upgrade must not end any pre-upgrade session, do not call `RevokeBefore` until the longest token lifetime has passed since the deploy; every token written after it carries an instant and is compared normally.

Two consequences worth knowing before wiring it up. A token whose issue was in flight across a revocation is refused — the instant is stamped before the write reaches the store, so a token stamped just before the boundary and written just after it is treated as predating it. That is over-strict rather than under-strict, and deliberate. And the instants come from application clocks, so a node whose clock runs ahead of the node a revocation is issued from stamps tokens that read as later than the boundary and survive it: the window is exactly the skew between the two, and a single node whose clock steps backwards — an NTP correction, a restored snapshot, a resumed virtual machine — produces the same thing without any second node. `WithTokenStoreMaximumClockSkew` on the redis store, and `JwtConfig.RevocationEpochSkew` on the json web token path, bound that window: they widen the boundary by the stated amount and, on the store, additionally refuse a stamp further ahead of the verifying
node than the same amount. Both default to zero, which leaves the behaviour of this release unchanged; set them to the worst skew the fleet can carry. The cost is symmetrical and deliberate: a token issued within that window AFTER a revocation is refused too. `WithRevocationEpochRetention` is unrelated to any of this — it floors how long a boundary is kept when there is no index deadline to adopt, and does not affect the comparison.

### Messagebus: an unhandled consumed message fails the dispatch

**What changed.** A message whose type has no registered handler used to pass through the handle middleware with a warning and nil error; the consume command then Acked it. The default now refuses the dispatch, and the opt-in is `HandleOptions.AllowMissingHandler`, which replaces `RequireHandler` (the same switch, inverted, so the zero value is the safe cell).

**Symptom.** On the consume path, a forgotten `RegisterHandler` line — or a handler registered for `T` while the transport decodes `*T` — used to drain the queue one warning at a time: every message Acked and destroyed, the retry, dead-letter and failure-transport machinery never engaging because the pipeline was told the message was handled. The same mistake now nacks into exactly that machinery and is impossible to miss.

**Remedy.** Code that set `RequireHandler: true` deletes the field — that is the default now. A bus that genuinely wants pass-through (a tap that observes some types and forwards the rest) sets `AllowMissingHandler: true` and keeps the old behaviour, warning included.

### Mailer: configured smtp credentials fail closed when the server does not advertise AUTH

**What changed.** With a username configured, a server whose EHLO response does not advertise `AUTH` is refused — whatever `RequireAuth` says. `RequireAuth` keeps its other half: it still refuses the configuration in which authentication is required but no username is set.

**Symptom.** The old default skipped the whole auth branch and delivered the message as anonymous submission while reporting success, so the configured identity went quietly unused — most commonly against a relay that only advertises `AUTH` after `STARTTLS`, on a session where tls was not negotiated. Deployments in that shape now get an error naming the unapplied credentials instead of silent unauthenticated delivery.

**Remedy.** For a relay that genuinely takes unauthenticated submission, remove the credentials from the configuration — they were doing nothing. For a relay that advertises `AUTH` only after `STARTTLS`, set `RequireTls` so the session upgrade is guaranteed before the auth branch is reached.

### Translation: an absent parameter stays visible, and a misnamed catalog file refuses the load

**What changed.** A plain placeholder whose parameter is absent renders as the visible placeholder itself (`Hello, {name}!`) instead of as an empty string; a parameter present with an explicit nil still renders empty. And `JsonDirectoryLoader.Load` answers a hard error naming any `.json` file that does not parse as `<domain>.<locale>.json`, instead of skipping it.

**Symptom.** Rendered empty, a renamed parameter key shipped every message quietly missing its amount, name or count, with nothing anywhere to learn it from; a golden test that asserted the empty rendering sees the placeholder now. A translations directory in the natural-but-unsupported `en.json` layout used to load zero catalogs successfully, with the runtime symptom — raw message ids in production — pointing nowhere near the mis-named files; that directory now fails the boot with the file named.

**Remedy.** Fix the parameter key, or pass the parameter with an explicit nil where the empty rendering was genuinely wanted. Rename catalog files to `<domain>.<locale>.json` — `messages.en.json` for the default domain — and delete stray `.json` files from the translations directory.

### Compile-level: `messagebus/contract.Transport`'s `Close` lost its runtime parameter

**What changed.** The contract method is `Close() error`; the former `Close(runtimeInstance runtimecontract.Runtime) error` is gone, and `RegisterTransports` now registers a `TransportsCloser` the container's ordered teardown reaches.

**Symptom.** A userland transport fails to compile against the interface until the parameter is deleted. The old signature was structurally dead: the teardown recognizes `Close() error` and nothing else, nothing in the framework or any production wiring ever called a transport's `Close`, so every broker connection lived exactly as long as the process and every deploy tore it down abruptly.

**Remedy.** Delete the parameter from the implementation. A transport that used the runtime for a deadline owns its bound now — the builtin amqp transport already carried its own join timeout and ignored the runtime entirely, which is what made the removal free.

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

### Wiring: the generator refuses what it used to drop silently

**What changed.** `melody:wiring:generate` fails, naming the site, on the inputs it used to read as "nothing": an unknown `//melody:` directive (a mistyped `scoped` demoted a request-lifetime service to a never-closed singleton; a mistyped `ignore` registered the constructor it acknowledged), a `//melody:bind` assignment without the equals sign or with an empty half (the override beside the constructor silently fell back to a broader bind), a malformed exclude pattern (`path.Match`'s `ErrBadPattern` was read as "does not match", so the exclusion excluded nothing), an empty import path or directory on a package binding (an empty directory scanned the whole project tree as one package), and two constructors that would register the same container key (the generated file panicked at first boot while the generation had reported success). `//melody:ignore` now accepts a trailing reason, which is the spelling the refusal of unknown directives makes mandatory to honour. An exclude that matched no constructor is reported like an unused bind, `--strict` fails on it, and a strict refusal carries every violation — binds, excludes, skipped constructors — in one error instead of the first found.

**Symptom.** A generation that used to succeed over a tree carrying any of these now fails with an error naming the file and line, and a `--strict` pipeline with a dead exclude goes red.

**Remedy.** Correct the named site: fix the directive spelling, add the equals sign, terminate the character class, split the two constructors or route one through `//melody:ignore`. Every refusal is a defect the generated file would otherwise carry into boot — none of them is a new rule about correct input.

### Wiring and openapi: the `--out` contract hardens, and the openapi anchor moves

**What changed.** Both generate commands write through a temp file and a rename, so an interrupted write leaves the previous artifact intact instead of a torn one; both refuse to replace a file that is not theirs — wiring by the `DO NOT EDIT` marker, openapi by the target not holding a JSON document; wiring refuses an `--out` inside a scanned package directory, which its own documentation always forbade; and a relative `--out` on `melody:openapi:generate` is now anchored at the project directory, exactly as the wiring command has always anchored its own, with the parent directories created on the way.

**Symptom.** `melody:openapi:generate --out openapi.json` run from a working directory other than the project root — a systemd unit, a Makefile in a subdirectory — now writes into the project instead of into that directory; a pipeline that relied on the old CWD anchoring reads the file from the wrong place. A mistyped `--out` pointing at a hand-written file fails instead of destroying it.

**Remedy.** Pass an absolute `--out` to pin any other destination; delete a foreign file deliberately if its path really is the intended output. Nothing changes for the documented invocations run from the project root.

### Openapi: the document's shape becomes faithful to the router, and stable

**What changed.** A route registered with no method list — which the router answers on every verb — is documented on all eight path item verbs instead of as an operation-less `{}`; a verb the format cannot model (`PURGE`) is named in the path item's new `description` instead of vanishing; a catch-all pattern stops the documented path at the catch-all segment, because the router discards everything after it; converging routes no longer overwrite each other's operations (the earlier registration wins, as in the router's match order); and response types are visited in status order, so component names and every `$ref` to them stop depending on map iteration and the generated file is byte-stable across runs.

**Symptom.** A committed `openapi.json` regenerated after this change may differ once — method-less routes gain operations, catch-all paths shorten, colliding component names settle onto the lower status — and then stays byte-identical run over run, which is the property the diff-based pipelines were missing.

**Remedy.** Regenerate the committed document once and review the diff; it is the document moving onto what the router actually serves. Declare explicit method lists on routes that should not advertise all eight verbs.

### Configuration: the shared parsers refuse the values that used to disarm a guard

**What changed.** Three shapes that used to pass through the typed accessors are now refused. A bare integer read as a duration — `RegisterRuntime("ttl", 30)` followed by `Parameter.Duration()` — was interpreted as nanoseconds and produced a timeout that expires instantly with no error anywhere, while the same value spelled `"30"` was already refused for its missing unit; it is now refused the same way. `Float64` refuses NaN and the infinities, which `strconv.ParseFloat` parses from `"NaN"`, `"Inf"` and `"Infinity"` without complaint and which silently disarm every threshold written the normal way, since each ordered comparison against NaN is false. And a typed-nil `map[string]string` reads as absent rather than as an empty present map, so a caller branching on the presence flag to apply a default gets the default.

**Symptom.** A parameter or bag entry carrying one of the three shapes now produces an error where it used to produce a value. In every case the value it used to produce was wrong: an instant timeout, a threshold that stopped guarding, a default that never applied.

**Remedy.** Spell a duration with its unit — `"30s"` or a `time.Duration` — and give a ratio a finite value. A caller that genuinely wants an empty map for a nil one applies that default itself, which is now possible because the absence is reported.

### Configuration: `IntWithDefault` panics on a parameter that exists but does not parse

**What changed.** The helper answers the default only for a parameter that is ABSENT. A parameter that exists and does not parse as an int now panics, naming the failure, instead of silently becoming the default.

**Symptom.** A boot that used to run with a default now fails, naming the parameter whose value could not be read.

**Remedy.** Correct the value, or remove the parameter to fall back to the default deliberately. A mistyped value that quietly turns into a number nobody wrote is a misconfiguration running in disguise, and the panic is what makes it visible at boot rather than in production behaviour nobody can explain.

### Configuration: the `.env` grammar matches godotenv's, and a malformed reference fails the boot

**What changed.** A `.env` line is preprocessed byte by byte rather than through runes, so a file saved as anything other than UTF-8 keeps its bytes — a rune round-trip re-encoded them, rewriting values, a password among them, where godotenv alone passes a quoted value through untouched. The comment cut now matches godotenv's own: a `#` opening before the key separator comments the whole line out, while a `#` after it stays in the produced line and godotenv's countback decides where the value ends, which stops the double cut that read `hello # world # x` as `hello` where godotenv reads `hello # world`. And a `${...}` reference whose closing brace arrived over a name outside the key grammar is refused rather than surviving as literal text — nobody types `${...}` into a password by accident — while an unclosed brace stays data, like the bare dollar it is.

**Symptom.** A value carrying a hash after the separator keeps more of itself than it used to; a non-UTF-8 file stops being rewritten; a `.env` holding a malformed `${...}` fails the boot naming the enclosing key, where it used to load the braces as text.

**Remedy.** Correct the malformed reference, or escape a literal dollar as `\$`. For the comment cut and the encoding there is nothing to do: both moves take the reading onto what godotenv itself does, which is what the surrounding contract always promised.

### Configuration: an unterminated `%env(` and an unclosed `%name%` fail the boot

**What changed.** A `%env(` that never closes with `)%`, and a name-shaped run opened by a percent that nothing closes, are both refused at boot instead of surviving as literal template text. The contract already demanded that a literal percent be written doubled, so a `%app-name%` that reaches a service verbatim is a typo, not data. A percent standing in front of a character no name may start with — `growth of 50% overall` — is still data and still resolves untouched. `RegisterRuntime` additionally refuses a blank name and one carrying leading or trailing whitespace, which used to register a parameter nobody could look up under the name they wrote.

**Symptom.** A boot that used to succeed over a template holding one of these now fails, naming the parameter and the offending reference; a registration with a padded name fails where it used to be accepted.

**Remedy.** Close the placeholder, double the literal percent, or trim the name. Every refusal replaces a template fragment that was being handed to a consuming service as though it were the value.

### Container: registration refuses a typed-nil provider and a colliding identity key

**What changed.** The one gate all three registration paths go through refuses a typed-nil provider function — `var f containercontract.Provider[T]` handed in uninitialized — instead of registering a signature-valid function that panics on its first call. And two DIFFERENT types whose identity keys coincide (pointer-to-unnamed-composite types drop the package path, so `*[]alpha.Bus` and `*[]beta.Bus` of same-short-named packages read identically) refuse to coexist, at the container door and the scoped one alike.

**Symptom.** A boot that used to report success and fail at the first resolution now fails at the registration line, naming the service; a wiring whose two same-keyed types used to produce false circular-dependency reports at resolution and merged nodes at teardown now refuses the second declaration at boot.

**Remedy.** Initialize the provider before registering it. For a genuine identity-key collision, name one of the composite types — a defined type alias (`type buses []contract.Bus`) carries its package path and keys uniquely.

### Container: a closed container refuses writes, and a finished teardown refuses resolutions

**What changed.** `Register` and `OverrideProtectedInstance` on a closed container refuse the way the scoped registrar always has, and a resolution after the teardown finished is refused instead of answered out of the maps. Between the two closing states the memoized instances are still served, so a service's own `Close` keeps resolving what it reports through.

**Symptom.** A late registration that used to report success for a service no resolution could ever build now returns the closed-container error; a resolver kept across shutdown that used to receive a closed service with a nil error now receives the same error.

**Remedy.** Usually nothing — both were silent failure modes. A shutdown path that legitimately resolves during the teardown (a drain reporting through the logger) is unaffected: the refusal starts only after the last `Close` returned.

### Container: an override must fit every registered type of its name, and an override installed mid-creation wins

**What changed.** `OverrideProtectedInstance` refuses, before anything is written, a value that cannot sit under a type its name is registered under — judged the way the readers will judge it, raw assignability for an interface registration and canonical identity for a value-typed one. And an override installed while a provider was still building the same service wins the slot: the creation that lost closes the value it built and the resolution answers the override.

**Symptom.** An override of the wrong type used to be accepted and then served through `GetByType` — a poisoned type cache — and now returns an error naming the registered type and the value's. A test that installed an override during a warm-up race and sometimes had it overwritten now keeps it deterministically.

**Remedy.** Fix the override's type where the refusal points. Nothing else: the race half had no correct outcome before.

### Container: the teardown closes replaced built instances and breaks ties on creation order

**What changed.** An override replacing an instance the container itself built moves the replaced instance to a graveyard the teardown closes — under the same identity marks that keep a pointer shared by several names from closing twice — instead of leaking it; the scope's `ClosedWithScope` filings evict into the same kind of graveyard. And where the dependency graph says nothing, both teardowns now close in **creation order, latest first**, instead of by the node key descending.

**Symptom.** A process whose overrides used to strand container-built values no longer leaks them at shutdown. A teardown whose order between unrelated services was decided by spelling — `app.worker` closed before the logger, `zz.worker` after — now closes what was built later first, so a service built during another's construction closes before it.

**Remedy.** Nothing for the graveyard. If a test pinned the exact close order of services with no declared dependency edge, it was pinning a string comparison; declare the edge or accept the creation order.

### Configuration: the http shutdown wait becomes a parameter, and two dead interfaces are gone

**What changed.** `MELODY_HTTP_SHUTDOWN_TIMEOUT` (`kernel.http.shutdown_timeout`) sets how long a stopping http server waits for the requests it has already admitted, defaulting to five seconds — the value the framework used to hardcode. It reaches the server through `Configuration.Http().ShutdownTimeout()`, a method added to `config/contract.HttpConfiguration`, and the same budget bounds the join of the `OnHttpShutdown` hooks and the drain of the request scopes. `HttpTimeoutConfiguration` and `HttpShutdownConfiguration` are deleted from the application package: nothing implemented either, nothing could inject one, and the branch that read them would have applied their zero values verbatim to the server had it ever become reachable.

**Symptom.** An out-of-tree implementation of `config/contract.HttpConfiguration` no longer compiles until it declares `ShutdownTimeout() time.Duration`. A boot with `MELODY_HTTP_SHUTDOWN_TIMEOUT=0` or a negative value now fails with `http shutdown timeout must be positive`, where before the key did not exist. A type written against either deleted interface no longer compiles.

**Remedy.** Add the accessor to your implementation. Set the key to whatever your supervisor's termination grace allows, or leave it unset for the previous five seconds. There is no replacement for the deleted interfaces: the per-request limits are fixed in this major and the shutdown wait is the parameter.

### Application: three boot doors close, and one refuses a name nothing consumes

**What changed.** An **http** process whose `.env` artifacts contributed no keys at all refuses to boot rather than serve on development defaults; a cli process stays permissive. A module registered from inside a module boot hook is refused, and so is a route queued from inside one. `RegisterConfiguration` refuses any name but `logging`.

**Symptom.** A production binary started from a directory without its `.env` files, which used to serve on `dev` with the profiler and the debug commands enabled behind a single warning, now fails the boot naming the project directory. A composition root that registered a module or a route from inside `RegisterServices` or any other hook — where it never took effect — now sees a refusal. A configuration registered under a misspelled name, which used to be stored and never read, now fails the boot.

**Remedy.** Give the http process its environment files, or set any `MELODY_*` key explicitly. Move module registrations to `ModuleProvider` and route registrations to `RegisterHttpRoutes`, the hooks made for them. Correct the configuration name to `logging`.

### Application: the framework's default services can be substituted, and a middleware factory may not yield nil

**What changed.** The logger, the url generator, the serializer manager, the default serializer and the validator are registered only when the container does not already hold them, the way the cache, the session and the firewall manager already were; `service.serializer`, which two documented resolvers named and nothing registered, is now registered. The logger is additionally resolved once eagerly at boot, so a provider that cannot build fails the boot instead of the first request. A middleware factory returning nil, or a typed nil, fails the pipeline build naming the definition. The firewall manager is registered whatever the process mode.

**Symptom.** An application that used to hit the duplicate-registration exit when bringing its own serializer manager now substitutes it. A logging configuration that cannot build now fails at the boot step that owns it rather than at an arbitrary later resolution. A middleware factory that returned nil to disable itself conditionally now fails the boot.

**Remedy.** A factory that wants to disable itself returns a pass-through middleware instead of nil. Everything else needs no action.

### Application: the boot order puts the application's routes first and the kernel listeners last

**What changed.** `bootHttp` runs before the module phases, so the application's own routes register before any module's and win the router's registration-order tie-break; the duplicate-route refusals are collected into the aggregated boot report rather than panicking one at a time. The kernel's default listeners are wired at the end of `Boot` in every process shape, so a console process's dispatcher answers with the same set the serving process runs, and the framework exception listener steps aside when the application installed its own error handler.

**Symptom.** Where an application route and a module route shared a pattern, the application's now wins where the module's used to. A boot with several duplicate routes reports them together instead of stopping at the first. An error handler installed at boot, which could never run, now runs. `debug:events` in a console process no longer reports an empty dispatcher.

**Remedy.** Nothing, unless a deployment relied on a module route shadowing an application route on the same pattern — declare the intended one and remove the other.

### Httpclient: `buildUrl` is RFC 3986 reference resolution, and a base url wants its trailing slash

**What changed.** The target is resolved against the base url by RFC 3986 reference resolution — the rule Symfony and Guzzle implement — instead of being appended to the base as a prefix: an absolute-path target (`/users`) replaces the base path entirely, a relative one (`users`) merges over the last segment of the base path, and an empty target names the base resource itself, with its trailing slash. Because the merge cuts the last segment of a base spelled without a trailing slash, `NewHttpClientConfig` and `SetBaseUrl` refuse such a base by panic; a base with an empty path (`https://host`) stays legal.

**Symptom.** A client built with `NewHttpClientConfig("https://host/v1", ...)` panics at construction with `the base url path must end with a slash`. A client rebuilt with `"https://host/v1/"` and calling `Get("/users")` reaches `https://host/users` where the old join reached `https://host/v1/users`.

**Remedy.** Spell the base with its trailing slash and the targets relative: `"https://host/v1/"` + `"users"` names `https://host/v1/users` under both rules. Targets that begin with `/` are the ones whose meaning changed — drop the leading slash to keep them under the base path. The panic at construction is deliberate: without it the changed meaning would surface as a 404 in production.

### Httpclient: the `TransportConfig` fields are pointers, built with `TransportDuration` and `TransportCount`

**What changed.** The eight override fields become `*time.Duration`/`*int`. A nil field means "not set" and falls back to the default beside it; a SET value reaches `net/http` verbatim, zero and negative included, carrying the meaning `net/http` and `net.Dialer` give it — `MaxIdleConns: 0` is an unbounded pool, `IdleConnTimeout: 0` waits forever, a negative `KeepAlive` disables the probes. `DefaultTransportConfig()` stays the fully populated statement of the defaults.

**Symptom.** A `TransportConfig` literal spelling bare values no longer compiles.

**Remedy.** Wrap each value: `DialTimeout: httpclient.TransportDuration(5 * time.Second)`, `MaxIdleConns: httpclient.TransportCount(200)`. A field that used to be left zero to mean "inherit the default" is now LEFT NIL to mean it — which is what the zero value of a pointer field already is, so an override that only named some fields carries the same meaning it had.

### Httpclient: the foreign-origin refusal reads the resolved url, and a relative target needs a base url

**What changed.** On a client WITH a base url, the refusal of a target leaving the base origin is judged on the RESOLVED url rather than on the target's spelling, so the network-path form (`//other.example/x`) and any case of the scheme are covered. On a client WITHOUT a base url, a relative target is refused by name instead of failing inside `net/http` with the cause unnamed.

**Symptom.** `Get("//other.example/x")` on a based client answers `the request url leaves the origin of the configured base url` where it used to hang the reference under the base path. `Get("/users")` on a base-less client answers `the request url is relative and the client has no base url` instead of `failed to create request`.

**Remedy.** A caller that talks to more than one origin builds a client without a base url, as before. A caller that hit the second refusal was already failing — the error now names the missing half.

### Httpclient: header maps are canonicalized at every door, the getters hand out copies, and a nil option is refused

**What changed.** `NewHttpClientConfig` and `RequestOptions.SetHeaders` refuse a map carrying two spellings that collapse onto one header; `HttpClient.SetHeader` and `RequestOptions.SetHeader` store the canonical spelling. `RequestOptions.Headers()` and `Query()` hand out copies. A nil `RequestOption` in a call's option list is refused with its index instead of being called.

**Symptom.** A configuration map holding both `x-api-key` and `X-Api-Key` panics naming the collision. A header rotation through `SetHeader("x-api-key", ...)` on a client configured with `X-Api-Key` overwrites the entry it means to instead of leaving two. Code that wrote into the map returned by `Headers()` no longer reaches the option set.

**Remedy.** Keep one spelling per header in each map. Code that mutated the getters' maps moves to `SetHeader`/`SetQuery`, the doors that write.

### Session: an injected file handle opened for appending is refused

**What changed.** `session.NewFileStorageFromFile` refuses a handle opened with `O_APPEND`, naming it, and the write itself now names offset zero instead of seeking to it.

**Symptom.** A construction that used to succeed now fails at boot with `session storage file is opened for appending`.

**Remedy.** Open the handle without `os.O_APPEND` — `os.O_RDWR|os.O_CREATE` is what this storage needs — or hand the path to `session.NewFileStorageFromPath` and let it own the file. The refusal replaces a silent corruption: an appending write ignores the offset, so every snapshot landed after the document it was replacing and the truncation then cut the pair to the new length. Measured, a growing snapshot left the file readable and lost every save with no error on any path, and a shrinking one left a document the next boot refused to decode, losing every persisted session.

### Session: a userland `Storage` may now be called concurrently for different sessions

**What changed.** `Manager.SaveSession` and `Manager.DeleteSession` hold a lock keyed to the session id across the storage call instead of one lock for the whole manager. The tombstone check and the write are still one critical section for the SAME session, which is the invariant that stops a logout being undone by a request that loaded the session before it; two requests acting on different sessions no longer wait on each other. Sixteen concurrent saves of distinct sessions against a storage with a 2 ms round trip took 35.5 ms and now take 2.8 ms.

**Symptom.** An implementation of `session/contract.Storage` written against the previous behaviour — one that assumed the manager serialised every call and therefore keeps unsynchronised state of its own — can now be entered concurrently for different session ids. Both storages melody ships took a mutex of their own all along and are unaffected; so is any storage that delegates to a client which is already safe for concurrent use, which redis and database clients are.

**Remedy.** Make the implementation safe for concurrent calls naming different session ids — for most storages this is already true and needs no change. Calls naming the same id are still serialised by the manager, so per-session state needs nothing added.

### Http: the end of a session is a warning, not a storage outage

**What changed.** When another request ends a session while this one is running, the response path records it at **warning** under its own name. The two genuine failures on that path — a save that could not land and a delete that could not land — keep the error level. All of them now carry the session id, the request method and the path.

**Symptom.** `session was deleted while the request was in flight` moves from `error` to `warning`. An alert counting session errors will see its volume drop, by exactly the traffic that was never a failure: a user logging out in one tab produced one of these per concurrent request in the others.

**Remedy.** If an alert was tuned around that volume, retune it; the records it was counting were the session ending, which `SESSION.md` and `HTTP.md` both describe as the normal outcome.

### Session: the contract gains an atomic Snapshot

**What changed.** `session/contract.Session` carries `Snapshot() (values map[string]any, modified bool, cleared bool)` — the three answers read under one lock acquisition — and the response path decides between deleting and saving through it. Reading the flags and the values through the individual accessors let a `Clear` racing the response land between the reads and write the pre-logout state back under a live id.

**Symptom.** An out-of-tree implementation of the `Session` contract stops compiling with "missing method Snapshot".

**Remedy.** Implement `Snapshot` as one critical section over the same three answers the accessors give; a single-threaded implementation can simply return `instance.All(), instance.IsModified(), instance.IsCleared()`.

### Session: a sub-second positive ttl is refused by the manual constructor too

**What changed.** `session.NewManager` and its siblings refuse a positive ttl below one second, the refusal `MELODY_HTTP_SESSION_TTL` validation has always given: such a lifetime stores no usable session — `SaveSession` reports success and the entry lapses before the client returns.

**Symptom.** A hand-wired manager built with, say, `500*time.Millisecond` now panics at construction naming the rule; zero keeps meaning no expiry.

**Remedy.** Use a lifetime of at least one second, or zero for no expiry.

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

### Compile-level: `config/contract.HttpConfiguration` gained `SessionTombstoneRetention`

**What changed.** [`config/contract.HttpConfiguration`](../config/contract/http.go) declares `SessionTombstoneRetention() time.Duration`, how long a deleted session id keeps refusing a write-back. The framework's own implementation reads it from `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` (`kernel.http.session_tombstone_retention`), five minutes by default — the constant the window used to be — and refuses zero and negative at boot, because a window that refuses nothing is not a shorter window but a disarmed logout defence.

**Symptom.** A type of your own implementing `config/contract.HttpConfiguration` no longer satisfies the interface, and the assignment fails to compile with `missing method SessionTombstoneRetention`.

**Remedy.** Implement it. Returning `config.DefaultSessionTombstoneRetention` keeps the behaviour the interface had without the method:

```go
func (instance *CustomHttpConfiguration) SessionTombstoneRetention() time.Duration {
	return config.DefaultSessionTombstoneRetention
}
```

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
