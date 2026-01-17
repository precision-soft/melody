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
package example

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

- `ResolveByAcceptHeader("")` and `ResolveByAcceptHeader("*/*")` fall back to the default serializer registered for [`MimeApplicationJson`](../../serializer/mime.go). See [`ResolveByAcceptHeader`](../../serializer/serializer_manager.go).
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
