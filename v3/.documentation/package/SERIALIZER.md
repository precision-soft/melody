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

- `ResolveByAcceptHeader("")` falls back to the default serializer registered for [`MimeApplicationJson`](../../serializer/mime.go). See [`ResolveByAcceptHeader`](../../serializer/serializer_manager.go).
- Every available media type takes the quality of the **most specific** range that covers it, so an exact range wins over a wildcard whatever the header order. `*/*` therefore selects the default JSON serializer whenever one is registered. See [`acceptQualityFor`](../../serializer/mime.go).
- A range carrying `q=0` **refuses** the types it covers rather than being ignored. A header that refuses every type the manager can produce yields an error wrapping [`ErrNotAcceptable`](../../serializer/serializer_manager.go), which [`NormalizeResultToResponse`](../../http/result_handler.go) answers `406 Not Acceptable`. A header that simply matches nothing available still receives the default representation.
- Wildcard subtypes (for example `text/*`) are supported when resolving against the configured serializers. See [`matchWildcardSubtype`](../../serializer/mime.go).
- The one negotiation grammar lives in [`internal.ParseQualityValue`](../../internal/quality_value.go) — a `q` outside the RFC 7231 qvalue grammar (anything other than a zero with up to three decimal digits or a one with up to three zero decimals) drops its whole member, neither an acceptance nor a refusal — and in [`internal.SplitOutsideQuotes`](../../internal/split_outside_quotes.go), which splits members and parameters with quoted-string awareness so a comma or semicolon inside a quoted parameter value (`text/plain;version="1,2";q=0`) stays inside its member. The http readers — `PrefersHtml`, the compression middleware's `acceptsGzip` and the error-body negotiation — read through both; this package's own header parser still carries its earlier grammar and converges on them with the remaining catch-up.
- MIME values are normalized by stripping parameters (for example `; charset=utf-8`) and lowercasing. See [`normalizeMime`](../../serializer/mime.go).

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
