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

### Runtime integration (`serializer`)

- Service names:
    - [`ServiceSerializer`](../../serializer/service_resolver.go)
    - [`ServiceSerializerManager`](../../serializer/service_resolver.go)
- Runtime resolvers:
    - [`SerializerMustFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerManagerMustFromRuntime`](../../serializer/service_resolver.go)
    - [`SerializerManagerFromRuntime`](../../serializer/service_resolver.go)
