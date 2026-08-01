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
	"github.com/precision-soft/melody/serializer"
	serializercontract "github.com/precision-soft/melody/serializer/contract"
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
- A range carrying `q=0` **refuses** the types it covers rather than being ignored. A header that refuses every type the manager can produce yields an error wrapping [`ErrNotAcceptable`](../../serializer/serializer_manager.go), which [`NormalizeResultToResponse`](../../http/result_handler.go) answers `406 Not Acceptable`. A header that simply matches nothing available still receives the default representation.
- A `q` outside the RFC 7231 qvalue grammar — anything other than a zero with up to three decimal digits or a one with up to three zero decimals — drops its whole member: it is neither an acceptance nor a refusal. A header whose every member is malformed resolves like a header that matches nothing. See [`parseQualityValue`](../../serializer/mime.go).
- The header is split with quoted-string awareness, so a comma or semicolon inside a quoted parameter value (for example `text/plain;version="1,2";q=0`) stays inside its member and the `q` it carries keeps covering the type it names. See [`splitOutsideQuotes`](../../serializer/mime.go).
- Wildcard subtypes (for example `text/*`) are supported when resolving against the configured serializers. See [`matchWildcardSubtype`](../../serializer/mime.go).
- MIME values are normalized by stripping parameters (for example `; charset=utf-8`) and lowercasing. Two keys that collapse into one normalized key are refused at construction — the silent overwrite used to pick its winner by map iteration order. A typed-nil serializer instance is refused exactly like the untyped nil. See [`normalizeMime`](../../serializer/mime.go) and [`NewSerializerManager`](../../serializer/serializer_manager.go).
- Implementations of the [`Serializer`](../../serializer/contract/serializer.go) contract are wired as process-wide singletons and called from every request goroutine at once: they must be safe for concurrent use, `Serialize` must return bytes the caller owns, and `Deserialize` must leave the target owning its bytes.
- The framework consumers read the `Accept` header with every repeated line joined in order ([`Header.Values`](../../http/result_handler.go)); reading it with `Header.Get` silently drops every line after the first of a list-typed field.

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

### Runtime integration (`serializer`)

- Service names:
    - [`ServiceSerializer`](../../serializer/service_resolver.go)
    - [`ServiceSerializerManager`](../../serializer/service_resolver.go)
- Runtime resolvers:
    - [`SerializerMustFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerManagerMustFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerManagerFromRuntime`](../../serializer/service_resolver.go)
