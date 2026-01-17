# RUNTIME

The [`runtime`](../../runtime) package provides Melody’s runtime handle used during request/command lifecycles. A runtime carries a `context.Context` plus access to a scope overlay and the underlying container.

## Scope

- Package: [`runtime/`](../../runtime)
- Subpackage: [`runtime/contract/`](../../runtime/contract)

A `runtimecontract.Runtime` is passed through framework boundaries (HTTP handlers, event listeners, CLI commands) so code can:

- access lifecycle context (`Context()`),
- resolve services from a scope overlay (scope-first with container fallback),
- keep per-request / per-command overrides isolated via a `Scope`.

## Subpackages

- [`runtime/contract`](../../runtime/contract)  
  Public runtime contract (`Runtime`).

## Responsibilities

- Provide the default runtime implementation and constructor:
    - [`New`](../../runtime/runtime.go)
- Provide typed service resolution helpers (scope-first with container fallback):
    - [`FromRuntime`](../../runtime/resolver.go)
    - [`MustFromRuntime`](../../runtime/resolver.go)

## Usage

The example below demonstrates creating a runtime with a scope and resolving a service via `MustFromRuntime`.

```go
package example

import (
	"context"

	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/exception"
	"github.com/precision-soft/melody/runtime"
)

type Clock interface {
	NowUnix() int64
}

type StaticClock struct{}

func (instance *StaticClock) NowUnix() int64 {
	return 0
}

func registerClock(
	serviceContainer containercontract.Container,
) {
	container.MustRegister[Clock](
		serviceContainer,
		"service.clock",
		func(resolver containercontract.Resolver) (Clock, error) {
			return &StaticClock{}, nil
		},
	)
}

func example() int64 {
	serviceContainer := container.NewContainer()

	registerClock(serviceContainer)

	scope := serviceContainer.NewScope()

	runtimeInstance := runtime.New(
		context.Background(),
		scope,
		serviceContainer,
	)

	clock := runtime.MustFromRuntime[Clock](
		runtimeInstance,
		"service.clock",
	)

	closeErr := scope.Close()
	if nil != closeErr {
		exception.Panic(
			exception.NewError("failed to close scope", nil, closeErr),
		)
	}

	containerCloseErr := serviceContainer.Close()
	if nil != containerCloseErr {
		exception.Panic(
			exception.NewError("failed to close container", nil, containerCloseErr),
		)
	}

	return clock.NowUnix()
}
```

## Footguns & caveats

- `FromRuntime` / `MustFromRuntime` resolve from `runtime.Scope()` when present, and fall back to `runtime.Container()` when the scope does not contain the requested service.
    - Implementation: [`selectRuntimeResolver`](../../runtime/resolver.go)
- `runtime.New(...)` requires a non-nil `context.Context`, `Scope`, and `Container` and fail-fast panics on invalid construction.
    - Implementation: [`New`](../../runtime/runtime.go)
- The runtime itself does not own lifecycle cleanup; the caller is responsible for closing the scope (and container when appropriate).

## Userland API

### Contracts

- [`runtime/contract.Runtime`](../../runtime/contract/runtime.go)

### Constructors

- [`runtime.New`](../../runtime/runtime.go)

### Service resolution helpers

- [`runtime.FromRuntime`](../../runtime/resolver.go)
- [`runtime.MustFromRuntime`](../../runtime/resolver.go)
