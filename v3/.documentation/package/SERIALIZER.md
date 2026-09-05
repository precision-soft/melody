# SERIALIZER

The [`serializer`](../../serializer) package provides Melody’s serialization infrastructure: serializer implementations, MIME constants, `Accept` header negotiation, and runtime integration helpers.

## Scope

This package is responsible for transforming values to and from `[]byte` representations based on MIME types.

## Subpackages

- [`serializer/contract`](../../serializer/contract)
  Public serializer contracts.

## Responsibilities

- Serializer implementations:
    - [`JsonSerializer`](../../serializer/serializer.go)
    - [`PlainTextSerializer`](../../serializer/plain_text_serializer.go)
- MIME constants:
    - [`MimeApplicationJson`](../../serializer/mime.go)
    - [`MimeTextPlain`](../../serializer/mime.go)
- `Accept` header negotiation:
    - [`SerializerManager`](../../serializer/serializer_manager.go)
    - [`NewSerializerManager`](../../serializer/serializer_manager.go)
- Runtime integration helpers:
    - [`ServiceSerializer`](../../serializer/service_resolver.go)
    - [`ServiceSerializerManager`](../../serializer/service_resolver.go)

## Usage

The example below demonstrates selecting a serializer from an `Accept` header and serializing a value.

```go
package main

import (
	"github.com/precision-soft/melody/v3/serializer"
	serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
)

func serializeByAcceptHeader(acceptHeader string, value any) ([]byte, error) {
	manager, createErr := serializer.NewSerializerManager(
		map[string]serializercontract.Serializer{
			serializer.MimeApplicationJson: serializer.NewJsonSerializer(),
			serializer.MimeTextPlain:       serializer.NewPlainTextSerializer(),
		},
	)
	if nil != createErr {
		return nil, createErr
	}

	serializerInstance, resolveErr := manager.ResolveByAcceptHeader(acceptHeader)
	if nil != resolveErr {
		return nil, resolveErr
	}

	return serializerInstance.Serialize(value)
}
```

## Footguns & caveats

- `ResolveByAcceptHeader("")` falls back to the serializer registered for [`MimeApplicationJson`](../../serializer/mime.go); a manager deliberately configured without one serves its first configured serializer in lexical MIME order, so an empty accept header — the client that takes anything — is always answered while any serializer is configured. Only the empty manager errors. See [`ResolveByAcceptHeader`](../../serializer/serializer_manager.go).
- Every available media type takes the quality of the **most specific** range that covers it, so an exact range wins over a wildcard whatever the header order. `*/*` therefore selects the default JSON serializer whenever one is registered. See [`acceptQualityFor`](../../serializer/mime.go).
- A range carrying `q=0` **refuses** the types it covers rather than being ignored: a refused type is never served by the negotiation. Only a header that refuses **every** type the manager can produce yields an error wrapping [`ErrNotAcceptable`](../../serializer/serializer_manager.go), which [`NormalizeResultToResponse`](../../http/result_handler.go) answers `406 Not Acceptable` — on the **success** path. The error path does not: [`renderErrorResponse`](../../http/error_renderer.go) treats `ErrNotAcceptable` as a reason to fall back to a json body, so a client that refused every type still receives json when the request fails. A `406` is what a refusing client gets for a successful result, and a json error document is what it gets for a failing one. A refusal that leaves another registered type merely unmatched is a preference, not a refusal of the whole manager: `Accept: application/json;q=0` against a manager holding JSON and plain text is answered `text/plain`, the representation
  the client never rejected. A header that simply matches nothing available still receives the default representation.
- A `q` outside the RFC 7231 qvalue grammar — anything other than a zero with up to three decimal digits or a one with up to three zero decimals — drops its whole member: it is neither an acceptance nor a refusal. A header whose every member is malformed is refused by the manager with `no acceptable mime types in accept header`; the http result handler answers that refusal by serializing with the default serializer, so the client still receives the default representation. See [`internal.ParseQualityValue`](../../internal/quality_value.go).
- The header is split with quoted-string awareness, so a comma or semicolon inside a quoted parameter value (for example `text/plain;version="1,2";q=0`) stays inside its member and the `q` it carries keeps covering the type it names. See [`internal.SplitOutsideQuotes`](../../internal/split_outside_quotes.go).
- Wildcard subtypes (for example `text/*`) are supported when resolving against the configured serializers. See [`matchWildcardSubtype`](../../serializer/mime.go).
- MIME values are normalized by stripping parameters (for example `; charset=utf-8`) and lowercasing. Two keys that collapse into one normalized key are refused at construction — the silent overwrite used to pick its winner by map iteration order. A typed-nil serializer instance is refused exactly like the untyped nil. See [`normalizeMime`](../../serializer/mime.go) and [`NewSerializerManager`](../../serializer/serializer_manager.go).
- Implementations of the [`Serializer`](../../serializer/contract/serializer.go) contract are wired as process-wide singletons and called from every request goroutine at once: they must be safe for concurrent use, `Serialize` must return bytes the caller owns, and `Deserialize` must leave the target owning its bytes.
- `JsonSerializer.Deserialize` into an **untyped** target (`*any`, `*map[string]any`) applies `encoding/json`'s defaults: every number decodes as `float64` — an integer beyond 2^53 comes back a **different integer** with no error (`9007199254740993` reads back `9007199254740992`) — an array comes back `[]any` whatever it was marshaled from, and an object `map[string]any`. A typed struct target decodes exactly. This is the same hazard [`CACHE.md`](CACHE.md) records for the cache round-trip and [`SESSION.md`](SESSION.md) for the session file storage; decode ids and amounts into typed fields, or carry them as strings.
- The framework consumers read the `Accept` header with every repeated line joined in order ([`joinedAcceptHeader`](../../http/error_renderer.go), through the standard library's `Header.Values`); reading it with `Header.Get` silently drops every line after the first of a list-typed field.

## Userland API

### Contracts (`serializer/contract`)

- [`type Serializer`](../../serializer/contract/serializer.go)

### Serializers (`serializer`)

- [`NewJsonSerializer`](../../serializer/serializer.go)
- [`NewPrettyJsonSerializer`](../../serializer/serializer.go)
- [`NewPlainTextSerializer`](../../serializer/plain_text_serializer.go)

### Manager (`serializer`)

- [`NewSerializerManager`](../../serializer/serializer_manager.go)
- [`type SerializerManager`](../../serializer/serializer_manager.go)
    - [`Get(mime string) (serializercontract.Serializer, bool)`](../../serializer/serializer_manager.go) — the serializer registered for one media type, with the flag that tells an unregistered type from a nil one
    - [`ResolveByAcceptHeader(acceptHeader string)`](../../serializer/serializer_manager.go)
- [`ErrNotAcceptable`](../../serializer/serializer_manager.go) — reports that the accept header refused every media type the manager can produce, so no representation can be served

### Runtime integration (`serializer`)

- Service names:
    - [`ServiceSerializer`](../../serializer/service_resolver.go)
    - [`ServiceSerializerManager`](../../serializer/service_resolver.go)
- Runtime resolvers:
    - [`SerializerMustFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerManagerMustFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerManagerFromRuntime`](../../serializer/service_resolver.go)

Which id answers what. `ServiceSerializer` is the **default serializer** — the json one, registered by the application boot behind a `Has` gate, so an application or module registering the id first substitutes it; it is what the two `Serializer*FromRuntime` resolvers answer, and nothing else reads it. `ServiceSerializerManager` is what **content negotiation** reads: a response's media type is chosen by the manager from the request's `Accept` header, so registering a serializer of your own under the first id changes what those two resolvers hand a caller, and what a request is served only on the fallback path [`NormalizeResultToResponse`](../../http/result_handler.go) takes when the manager is absent or the negotiation itself failed. To add a media type — xml, msgpack, cbor, `application/vnd.api+json` — register the manager id with a manager built by `NewSerializerManager` carrying the wider map; the boot gates that id too, so the registration is a substitution rather than a
duplicate-registration exit.
