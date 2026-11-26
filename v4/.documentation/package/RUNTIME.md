# RUNTIME

The [`runtime`](../../container/runtime) package builds the runtime of one phase — an http request, a cli run, one message delivery or one cron tick — over its `context.Context` and its scope. The `Runtime` contract lives beside the container's, in [`container/contract`](../../container/contract).

## Scope

- Package: [`container/runtime/`](../../container/runtime)
- Contract: [`container/contract/`](../../container/contract) (`Runtime`, `Kind`)

A `containercontract.Runtime` is passed through framework boundaries (HTTP handlers, event listeners, CLI commands) so code can:

- access lifecycle context (`Context()`),
- resolve services through its scope (`Scope()`): the scope answers its own services and the container's,
- know the phase it stands for (`Kind()`: `KindRequest`, `KindRun`).

A runtime reaches no container: everything it resolves goes through its scope, and code that needs the container itself (a command that describes it) is handed it where it is built.

## Responsibilities

- Build a runtime:
    - [`NewRequest`](../../container/runtime/runtime.go): one http request;
    - [`NewRun`](../../container/runtime/runtime.go): one cli run, one delivery, one tick;
    - [`NewChildRun`](../../container/runtime/runtime.go): a run over a new scope of the same container, for one delivery or one tick inside a run; the caller closes it through `child.Scope().Close()`;
    - [`WithContext`](../../container/runtime/runtime.go): the same runtime, kind and scope, over a derived context.
- Provide typed service resolution helpers, through the scope:
    - [`FromRuntime`](../../container/runtime/resolver.go)
    - [`MustFromRuntime`](../../container/runtime/resolver.go)

## Usage

The example below demonstrates creating a runtime with a scope and resolving a service via `MustFromRuntime`.

```go
package main

import (
    "context"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/container/runtime"
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
        container.RegisterOptions{},
    )
}

func example() int64 {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    registerClock(serviceContainer)

    scope := serviceContainer.NewScope()

    runtimeInstance := runtime.NewRun(
        context.Background(),
        scope,
    )

    clock := runtime.MustFromRuntime[Clock](
        runtimeInstance,
        "service.clock",
    )

    closeErr := scope.CloseWithContext(context.Background())
    if nil != closeErr {
        exception.Panic(
            exception.NewError("failed to close scope", nil, closeErr),
        )
    }

    containerCloseErr := serviceContainer.CloseWithContext(context.Background())
    if nil != containerCloseErr {
        exception.Panic(
            exception.NewError("failed to close container", nil, containerCloseErr),
        )
    }

    return clock.NowUnix()
}
```

## Footguns & caveats

- `FromRuntime` / `MustFromRuntime` resolve through `runtime.Scope()`; a scope resolves the container's own services itself — `scope.Get` builds its resolver over the container it was opened from. A **closed** scope answers `scope is closed`: a runtime holding a dead scope is a wiring fault rather than something to route around.
    - Implementation: [`selectRuntimeResolver`](../../container/runtime/resolver.go), [`(*scope).Get`](../../container/scope.go)
- The constructors require a non-nil `context.Context` and `Scope` and fail-fast panic on invalid construction; `WithContext` keeps the kind, so a runtime derived for a deadline stays the phase it was.
    - Implementation: [`NewRequest`, `NewRun`, `WithContext`](../../container/runtime/runtime.go)
- `NewRequest` and `NewRun` bind their kind to the scope, so a provider whose creation enters through that scope reads it as `resolver.Runtime().Kind()`, and a singleton reads the kind of the call that built it first. One scope serves one kind: a constructor of another kind over a bound scope panics naming both; give each kind its own scope. A scope of another implementation is accepted and not bound. A resolution through the container itself, or through a scope no constructor bound, hands its provider `KindDetached`.
    - Implementation: [`(*scope).BindRuntimeKind`](../../container/scope_runtime_seam.go), [`(*resolverContext).Runtime`](../../container/resolver_context.go)
- `NewBoot(ctx, container)` opens the runtime of the application's boot over `container.NewBootScope()` and answers it with its finish: the finish closes the boot scope, then ends the context with `containercontract.ErrBootFinished` as its cause, answers the close's error and answers nil a second time. The application opens one in `Boot`; a hook reads it through `Kernel.BootRuntime()`. Once finished, every refusal of the boot scope — a resolution by name or type, a collection, an override, a lazy handle built over it — matches both `container.ErrScopeClosed` and `ErrBootFinished`, so a door used under a boot runtime kept past `Boot` names the finished boot; a boot scope closed before the boot finished answers the same. A scoped registration on it is refused as `ErrScopedServiceAtBoot` and a child run of the boot runtime by kind, closed or not.
    - Implementation: [`NewBoot`](../../container/runtime/runtime.go)
- The runtime itself does not own lifecycle cleanup; the caller is responsible for closing the scope (and container when appropriate).

## Userland API

### Contracts

- [`containercontract.Runtime`](../../container/contract/runtime.go)
- [`containercontract.Kind`](../../container/contract/runtime_kind.go)

### Constructors

- [`runtime.NewRequest`](../../container/runtime/runtime.go)
- [`runtime.NewRun`](../../container/runtime/runtime.go)
- [`runtime.NewChildRun`](../../container/runtime/runtime.go)
- [`runtime.WithContext`](../../container/runtime/runtime.go)

### Service resolution helpers

- [`runtime.FromRuntime`](../../container/runtime/resolver.go)
- [`runtime.MustFromRuntime`](../../container/runtime/resolver.go)
- [`runtime.FromRuntimeByType`](../../container/runtime/resolver.go) and [`runtime.MustFromRuntimeByType`](../../container/runtime/resolver.go) — the one service registered under `T`
- [`runtime.FromRuntimeByReflectType`](../../container/runtime/resolver.go) — the same lookup for a type held as a `reflect.Type`, the form an argument resolver needs
- [`runtime.AllImplementingFromRuntime`](../../container/runtime/resolver.go) and [`runtime.MustAllImplementingFromRuntime`](../../container/runtime/resolver.go) — every service implementing `T`, in the container's collection order

With a runtime in hand, resolve through it: `XMustFromRuntime(runtimeInstance)` where the package has that form, or `runtime.FromRuntime[T](runtimeInstance, name)` for a service of your own, `runtime.FromRuntimeByType[T](runtimeInstance)` by type and `runtime.AllImplementingFromRuntime[T](runtimeInstance)` for a collection. `XFromResolver(ctx, resolver)` is the door of a provider, which holds a resolver.
