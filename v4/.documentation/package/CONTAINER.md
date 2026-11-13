# CONTAINER

The [`container`](../../container) package provides Melody’s dependency injection container: service registration, deterministic service creation, scoping, overrides, and deterministic shutdown ordering.

## Scope

The container is responsible for:

- registering services by name (and optionally by concrete type),
- resolving services with deterministic single-instance semantics,
- refusing circular dependencies — inside one resolution, across concurrent ones, and through a Lazy handle or a retained resolver — with one sentinel,
- creating request-scoped (or operation-scoped) overlays via scopes,
- closing services in a deterministic order (dependents before dependencies).

## Subpackages

- [`container/contract`](../../container/contract)\
  Public contracts (`Container`, `Resolver`, `Scope`, `Registrar`, provider and registration options).

## Responsibilities

- Provide the container implementation and constructor:
    - [`NewContainer`](../../container/container.go)
- Provide typed registration helpers:
    - [`Register`](../../container/container_register.go)
    - [`MustRegister`](../../container/container_register.go)
    - [`RegisterType`](../../container/container_register.go)
    - [`MustRegisterType`](../../container/container_register.go)
- Provide typed resolution helpers:
    - [`FromResolver`](../../container/resolver.go)
    - [`MustFromResolver`](../../container/resolver.go)
    - [`FromResolverByType`](../../container/resolver.go)
    - [`MustFromResolverByType`](../../container/resolver.go)
- Provide typed scoped registration helpers:
    - [`RegisterScoped`](../../container/container_register_scoped.go)
    - [`MustRegisterScoped`](../../container/container_register_scoped.go)
    - [`RegisterScopedType`](../../container/container_register_scoped.go)
    - [`MustRegisterScopedType`](../../container/container_register_scoped.go)
- Provide scope overlays:
    - [`Container.NewScope`](../../container/container.go)
- Provide deterministic shutdown:
    - [`Close`](../../container/container_close.go)

## Configuration

### Type registration

Services are always registered by name (`serviceName string`). By default a registration also registers the service under the type its provider declares as its result — an interface when the provider returns one —, strictly — typed resolution works out of the box, and a second service registered under the same type fails.

Every registration door takes a [`RegisterOptions`](../../container/contract/registrar.go) struct as its last argument, whose zero value is the default; `container.RegisterOptions` names the same type. The two type-registration fields:

- `TypeRegistrationRelaxed`\
  Relaxes the REGISTRATION, not the resolution: a second registration under the same type is accepted and appended instead of failing, and by-type resolution then refuses every one of them with `service type has multiple registrations`. Nothing wins — the type simply stops being resolvable, and the services stay reachable by name. It is read only while the type is registered.
- `TypeRegistrationDisabled`\
  Keeps that registration off the by-type index — the opt-out, since the default is on and strict.

Type registration is **on by default** (strict) for the application's `RegisterService` and the container's `MustRegisterType`, so every registered service is also resolvable by its concrete return type — the zero `RegisterOptions{}` does it. Resolve a service by type with the generic [`MustFromResolverByType[T]`](../../container/resolver.go) / [`FromResolverByType[T]`](../../container/resolver.go):

```go
bus := container.MustFromResolverByType[messagebuscontract.Bus](container.ContextOf(resolver), resolver)
```

For a service with a **single** implementation this removes the need to invent a string service-name constant and a per-type `MustGetX` accessor — register it and resolve it by type. Keep the **named** path (`RegisterService(ServiceX, ...)` + `XMustFromResolver`) when a contract has more than one implementation that must coexist: because type registration is strict, registering two services under the same contract type fails at registration (the string name is then the only disambiguator).

### Collecting every implementation of an interface

A component that must act on **all** services of a kind — a dispatcher over every message handler, a scheduler over every cron task — collects them with [`AllImplementing[T]`](../../container/resolver_implementing.go) instead of being handed a hand-maintained list that goes stale when a service is added:

```go
handlers, err := container.AllImplementing[MessageHandler](container.ContextOf(resolver), resolver)
```

`AllImplementing[T]` requires `T` to be an interface and resolves every registered service whose type satisfies it — one registered under the interface type itself included, and every instance of a type registered non-strictly under several names. `MustAllImplementing[T]` is the panicking variant. The order never changes between runs: descending `RegisterOptions.CollectionPriority` (a higher priority is dispatched earlier), then type and name. A provider that fails aborts the whole collection rather than yielding a partial set.

Collect with the **resolver a provider receives**, not the container itself, so the collection participates in the current scope's overrides and so a container blocking on its own in-flight creation is avoided. The service whose provider is doing the collecting is excluded from its own result — the composite dispatcher that is itself one of the handlers it dispatches to collects the others, a scoped one as a container one. On a scope, a name the scope registers hides the container's registration of that name from the collection, whether or not the scoped registration satisfies the interface. The enumeration is exposed through [`contract.TypeLister`](../../container/contract/type_lister.go), kept apart from `Resolver` so a resolver written outside the framework keeps compiling; the container, a request scope and the provider resolver all implement it, and a closed scope refuses the collection the way its `Get` does.

### The caller's context

Every door that can wait on a creation another goroutine runs takes the caller's context first: `Get`, `MustGet`, `GetByType` and `MustGetByType` on the container, a scope and the resolver a provider receives, the typed helpers `FromResolver`, `MustFromResolver`, `FromResolverByType`, `MustFromResolverByType`, `AllImplementing` and `MustAllImplementing`, and a [`LazyService`](../../container/lazy.go)'s `Resolve(ctx)` and `Get(ctx)`. `Has` and `HasType` wait on nothing and take none. A nil context, a typed nil included, is refused at every one of these doors with `context is required in …` naming the door, never read as `context.Background()`.

The context bounds the wait, not the build. A resolution parked on another goroutine's creation leaves the moment its context ends, answering an error whose cause is `context.Cause(ctx)`, so `errors.Is` matches the cause set through `context.WithCancelCause`, or `context.Canceled` or `context.DeadlineExceeded` when none was set; it leaves the waiter count and the wait graph as a released waiter does, and a creation that finished in the same instant is answered as its value. The goroutine running the provider is never interrupted: its value is kept for the next resolution.

A provider's own resolutions are another matter, since they wait under the context of the call that started its creation. When that call ends while the provider waits on a nested creation another goroutine runs, the provider's resolution fails, the creation fails, and every resolution coalesced behind it answers that failure, though their own contexts are live; nothing is memoized, so the next resolution builds again. A provider that must not fail with its first caller resolves under a context of its own, such as `context.WithoutCancel(container.ContextOf(resolver))`.

A provider keeps its signature, `func(resolver Resolver) (T, error)`. The resolver it receives carries the context of the call whose resolution started the creation, read through [`container.ContextOf(resolver)`](../../container/resolver.go), so the provider's own resolutions wait under the same deadline as the call that asked for it:

```go
container.MustRegister[*ReportService](registrar, ServiceReportService,
    func(resolver containercontract.Resolver) (*ReportService, error) {
        ctx := container.ContextOf(resolver)

        return NewReportService(
            container.MustFromResolver[Repository](ctx, resolver, ServiceRepository),
        ), nil
    },
    container.RegisterOptions{})
```

The capability behind it is [`contract.ResolutionContext`](../../container/contract/resolution_context.go), kept off `Resolver` so a resolver written outside the framework keeps compiling; `ContextOf` answers `context.Background()` for one without it, and the container and a scope do not implement it, since no call is in flight on them. The resolver a provider receives carries the context only while the provider runs: one the provider retains — in a `Lazy` handle, a closure or a callback — answers `context.Background()` through `ContextOf` once the provider returned, and keeps nothing of the call that built it; a later resolution through it passes its own context. A runtime carries its own context, so `runtime.FromRuntime` and `runtime.MustFromRuntime` keep their shape and resolve under `runtimeInstance.Context()`; every framework `*FromContainer` and `*FromResolver` helper takes the context first, the `*FromRuntime` ones do not. The kernel answers the application's context through `Kernel.Context()`, the one a boot hook resolves under.

A goroutine the provider starts resolves through its own resolver, [`container.ForGoroutine(resolver)`](../../container/resolver.go): every resolution through it — a `Lazy` handle built over it, or a plain `Get` — runs on a chain of its own, even while the provider is still running, and is recorded as the provider's dependency, never as one of the service the provider happens to be building at that moment. It carries no context, so `ContextOf` answers `context.Background()` and the goroutine passes its own to every door. The capability behind it is [`contract.GoroutineResolver`](../../container/contract/goroutine_resolver.go); the container and a scope do not implement it, since no chain is in flight on them, and are answered as they are. While the resolution that started the provider is still building, the container cannot tell whether the provider waits on its goroutine, so a use from the goroutine that would wait on any creation that resolution holds — the provider's own service, a service further up the chain, a dependency it is resolving at that instant, or a collection that includes one of them — is refused with `ErrCircularDependency` rather than risk a deadlock. A goroutine that needs one of those resolves it once the provider has returned: a `Lazy` handle used at the first piece of work, as in the sample, rather than as the goroutine starts.

```go
container.MustRegister[*Worker](registrar, ServiceWorker,
    func(resolver containercontract.Resolver) (*Worker, error) {
        mailer := container.Lazy[*Mailer](container.ForGoroutine(resolver), ServiceMailer)

        worker := NewWorker(mailer)
        go worker.Run()

        return worker, nil
    },
    container.RegisterOptions{})
```

## Usage

The example below demonstrates:

- registering a service by name,
- registering a service under its type through the generic door, which is strict by default,
- resolving a dependency inside a provider, under the context of the call that started its creation,
- resolving through a scope under the caller's context,
- creating a scope and overriding a service instance.

```go
package main

import (
    "context"
    "fmt"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type Logger interface {
    Info(message string)
}

type StdLogger struct{}

func (instance *StdLogger) Info(message string) {
    fmt.Println(message)
}

func registerServices(
    serviceContainer containercontract.Container,
) {
    container.MustRegister[Logger](
        serviceContainer,
        "example.logger",
        func(resolver containercontract.Resolver) (Logger, error) {
            return &StdLogger{}, nil
        },
        container.RegisterOptions{},
    )

    container.MustRegister[string](
        serviceContainer,
        "example.greeting",
        func(resolver containercontract.Resolver) (string, error) {
            logger := container.MustFromResolver[Logger](
                container.ContextOf(resolver),
                resolver,
                "example.logger",
            )

            logger.Info("building greeting")

            return "hello", nil
        },
        container.RegisterOptions{},
    )
}

func example(ctx context.Context) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})

    registerServices(serviceContainer)

    scope := serviceContainer.NewScope()
    scope.MustOverrideInstance(
        "example.greeting",
        "hello from scope",
        container.OverrideOptions{},
    )

    greeting := container.MustFromResolver[string](
        ctx,
        scope,
        "example.greeting",
    )

    logger := container.MustFromResolver[Logger](
        ctx,
        scope,
        "example.logger",
    )

    logger.Info(greeting)

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
}
```

## Footguns & caveats

- A request scope layers **over** the container, not underneath it. A provider registered on the container builds from the container alone: the service it produces belongs to the whole process, so assembling it out of one request's substitutes would hold that request forever and let that request's end take the service away from every other one. Only the service a caller actually asks the scope for is looked up through the scope, which is what layering means. A container provider that asks for something only a scope carries — a request context — is told the service does not exist, at the point the wiring mistake was made; one that asks for the logger receives the container's own, which is the logger a process-lifetime service should hold.
- Providers must be functions compatible with [`Provider[T]`](../../container/contract/provider.go). A successful creation is memoized — once per container for a container registration, once per scope for a scoped one; a provider that returned an error, panicked or yielded nil is retried on the next resolution.
- Typed resolution by type is delegated to the underlying name registration when the type maps to a single service name, ensuring that resolving by name and by type returns the same instance (see [`container/resolver_context.go`](../../container/resolver_context.go)).
- No ring of dependencies is ever recorded, and every refusal of one carries [`ErrCircularDependency`](../../container/errors.go) as its cause, so `errors.Is` matches it whatever the form. A cycle inside one resolution is caught by the per-resolution stack (see [`Resolver`](../../container/contract/resolver.go) and the stack logic in [`container/resolver_context.go`](../../container/resolver_context.go)); two concurrent resolutions waiting on each other's creations are refused by a container-wide wait graph ([`container/container_resolver.go`](../../container/container_resolver.go)) with `circular service dependency detected across concurrent resolutions`; and the dependency graph itself refuses, before the lookup, the edge that would close a ring with the edges already recorded — a name and the one name its type stands for are one node there — with `circular service dependency detected`.
- **A resolution waiting on another goroutine's creation is bounded and released.** Concurrent resolutions of one service build it once: the first runs the provider and the others wait for its answer. [`ContainerConfig.CreationWaiters`](../../container/container.go) bounds how many may wait on one creation — 1,024 when left out, at least 1, `bound.UnlimitedCount()` for no bound — and one more is refused at once with [`ErrCreationWaitersExceeded`](../../container/errors.go), writing nothing into the wait graph; an application declares it through `ApplicationConfig.ContainerCreationWaiters` or `MELODY_CONTAINER_CREATION_WAITERS` (see [CONFIG.md](CONFIG.md)). A waiter does not outlive its caller nor what it waits in: its context ending releases it with that context's cause (see [The caller's context](#the-callers-context)), the container's `Close` releases every waiter with `ErrContainerClosed` the moment the teardown begins, and a scope's `Close` releases the waiters on that scope's own creations with `ErrScopeClosed` — a waiter on a container creation reached through the scope keeps waiting, since the scope does
  not own it. A creation that finished in the same instant is answered as its value, and a waiter released by both the container and its scope answers `ErrContainerClosed`. A released waiter leaves the count and the wait graph, so the waiter admitted after it and a later resolution of the same chain see neither; the provider itself is never interrupted, and its value is kept or, the container or the scope being closed, closed at once.
- **A ring closed through a Lazy handle is refused at the resolution that closes it.** A service that holds a [`Lazy`](../../container/lazy.go) handle — or keeps its resolver and resolves later — writes the edge from itself to what it resolves at the first use, after its provider returned. When the other service is built over the holder, its provider resolving the holder would write the edge back and close a ring: that resolution is refused with `ErrCircularDependency` and the handle answers the error unmemoized. Built in that order, the other service is not built; built first — its provider resolving the holder before the handle is used — it stays built, and the handle's resolution is the one refused. The ring is a wiring defect, reported at the first use of the handle rather than at boot, since nothing is built before then. A declaration is an edge of the same graph: `TeardownDependencyNames` or `TeardownDependencyTypes` closing a ring with the edges already recorded is refused at `Register` or `DeclareTeardownDependency`, and a resolution closing a ring with a declaration is refused like any other; a type declaration counts once one service is registered under the type, as the plan expands it, so the registration of that service is refused too when it turns the declaration into a ring. An edge a resolution writes is held while it runs and kept only when it succeeds: a lookup that fails — an optional collaborator the provider finds missing and does without — leaves no dependency behind. The teardown keeps reporting a dependency cycle only as the defensive answer to a ring no refusal could see — two names answering one instance, which the plan folds onto one node.
- Closing is deterministic and dependency-aware: dependents are closed before dependencies (see [`container/container_close.go`](../../container/container_close.go)). The edge is recorded from the resolver a service was built through, so it covers resolutions made after construction too: a service that keeps its resolver and reaches through it later — a `container.Lazy` handle at first use, any replay of deferred work — is still closed before what it resolves. The one handle with no owner is one built over the **container itself** rather than over a provider's resolver: there is no node it could belong to, so what it resolves is ordered against it by creation order like any unrelated pair. Give such a service an edge by resolving what it needs inside its provider, or build the handle over the resolver the provider was handed. A handle over a provider's resolver used while the resolution that built the provider is still building a service, on a path that reaches back to that service, is
  refused as a circular dependency rather than left waiting on its own caller; a use from another goroutine that reaches such a service during that window is refused the same way, since the container cannot tell it from the caller — a goroutine the provider starts resolves through `container.ForGoroutine(resolver)`, under the same window.

- **`CloseWithContext` is the one teardown door, and a value whose own close takes no context closes through a closer.** The teardown closes a service through `CloseWithContext(ctx context.Context) error` alone, handing it what is left of the budget; a value carrying `Close() error` without it is refused with [`ErrCloseDoorRequired`](../../container/errors.go) before it can be filed — at the registration whose provider declares such a concrete type (`Register`, `RegisterType` and both scoped registrations, on the container and on a scope), at an override the container or the scope would close (every container override, a scope's under `ClosedWithScope`), and, for a provider declaring an interface whose concrete type the registration cannot see, at the creation, where the refused value's `Close` is called once and its failure joined to the refusal. A type the application does not own may carry only a `Close` that waits on the network for ever — an amqp connection whose peer stopped reading is one, `*sql.DB` and `*os.File` are others. Register such a service with `RegisterOptions{Closer: container.NewCloser[T](closer)}` ([`NewCloser`](../../container/register_option.go)): the closer is handed the value and the teardown's remaining budget, admits a type carrying `Close` alone and replaces its door, while every resolver keeps reading the service's own type. It applies to the built value, to an instance an override evicted, to a value built after the container began to close and to an override installed under the name when the closer takes the override's type; an override of another type closes through its own door. A closer whose `T` the provider's declared type is not assignable to is refused at the registration with `ErrCloserTypeMismatch`, and so is a `Closer` built by hand with one of its two fields left out, naming `RegisterOptions.Closer`; both scoped
  registrations refuse a `Closer` with `ErrScopedCloserUnsupported`, since a scope closes what it built through the door the values carry: a scoped value of a type the application does not own is wrapped in a type of its own that carries `CloseWithContext`.
- **The container owns a value when its provider returned it; what that value holds, the container does not see.** A service that holds a closable closes it in its own `CloseWithContext`, or registers it as a service of its own and resolves it, so the graph orders the two. A service with nothing to close that holds a closable it neither closes nor registers leaks it, and that leak is its author's.
- **A provider that CAPTURES a collaborator writes no edge.** The graph is recorded from resolutions, so a provider closing over a value built before the container — an integration's registry assembled during module wiring, a client the composition root already holds — and simply handing it back has declared nothing, and the teardown orders it against everything else by creation order. Nothing looks wrong: the service is registered, it is closed, and the order it is closed in is a coincidence that holds until the day it does not. Resolve the collaborator inside the provider wherever that is possible — it is the same call the service would have made anyway, and an edge derived from the resolution cannot fall out of step with what the provider actually uses; the framework's own providers resolve the clock, the configuration and the route registry the same way rather than closing over the values the kernel built. Where it is genuinely not possible, because the value has nothing to resolve, declare the edge in `RegisterOptions.TeardownDependencyNames`: it writes into the same graph, in the same key
  space, so the teardown still reads one graph and cannot order two ways. It orders teardown and nothing else — it does not build the named service and does not make it exist; an edge naming a service registered and never built is dropped by the walk. A name may be written before its service is registered, so the registration admits it; a name NOTHING registers is the misspelling the waves cannot afford (see below), refused with `ErrTeardownDependencyWasNeverRegistered` by `VerifyDeclaredTeardownDependencies() error` — declared on the concrete container and reached through a type assertion, which the application calls once its modules registered their services, failing the boot before `Run` — and reported again by `Close`, which then closes one service after the other. A collaborator that may or may not be wired is declared in `RegisterOptions.OptionalTeardownDependencyNames` (or `OptionalTeardownDependencyTypes`): the edge orders the teardown when the name is registered and is dropped without a refusal when it is not, every other rule holding. An empty name is refused where the registration is made, and so is a service naming itself. `RegisterScoped` refuses the list outright, because a scope keeps its own teardown graph, built per scope from the resolutions that scope actually made, and a declaration written once at registration would install nothing while reading as an ordering that holds. Where the author of a registration is not the one who knows the edge — the framework ordering a service the application registered against one of its own — the same declaration is made after the registration through `DeclareTeardownDependency(serviceName string, dependencyNames []string) error`, nil declaring nothing,
  declared on the concrete container and reached through a type assertion like `VerifyDeclaredTeardownDependencies`; it holds the field's rules and refuses on a closed container. The same declaration keyed by TYPE is `RegisterOptions.TeardownDependencyTypes`, written `reflect.TypeFor[T]()`, for the collaborator the declaring code knows as a type rather than as a spelling — everything above holds for it word for word — but for where the edge lives: a declaration keyed by a type is kept beside the graph and expanded onto the name that type stands for when each plan is built, rather than written into the graph — and `T` and `*T` name one node, because the container files them under one canonical type. It binds to exactly the type it declares: a composite built from two packages' same-named types — `func(alpha.Bus)` and `func(beta.Bus)`, a struct or an interface over them — names two types, both of which can be registered under an explicit name — `RegisterType` derives the name of an unnamed composite
  from its spelling, so the two would collide there as one duplicate name — and a declaration on one never orders against the other's service.
- What the graph leaves open is settled by **creation order, latest first**: two services with no edge between them are closed newest first, because a service built during the construction of another was needed by it whether or not the edge was declared. The tie-break this replaced was the node key descending — a string comparison, so whether a worker still had a logger to report its drain through was decided by its own name, and renaming `app.worker` to `zz.worker` was the whole difference between a shutdown record written and a shutdown record dropped. What it is NOT is a dependency: measured on the applications in this repository, the whole teardown graph is a single edge over five to seven services, so creation order is what decides almost every pair — an arrangement that holds until the day two of those services actually need each other, at which point nothing in the graph says so. Where an ordering matters, write it down: resolve the collaborator inside the provider, or declare
  the edge.
- **Closing has two states, and a service's own `CloseWithContext` may still resolve.** `IsClosed()` — declared on the concrete container, reached through a type assertion when holding the [`Container`](../../container/contract/container.go) contract — answers true from the moment the teardown begins and refuses every new creation for its whole duration, which is what keeps a service from being built into a container that is going away. Resolutions of what is already built keep answering until the last service close returns — a worker reporting its drain is entitled to the logger it reports through — and are refused with "container is closed" from then on. What a `CloseWithContext` resolves must therefore have been resolved before the teardown: a memoized service is answered, but the edge that answer writes lands after the plan was drawn and orders nothing, so a collaborator a service needs while it closes is resolved in its provider or declared; a first resolution during the teardown is a creation, and it is refused. A resolution made after that used to answer the instance it found in the map, already closed, with a nil error, so a caller holding a resolver got a live-looking handle to a dead service.
- **The teardown carries a deadline, and a service can ask for it.** `CloseWithContext(ctx context.Context) error` — declared on the `Container` contract — closes the container under a deadline its caller declares, and hands that deadline to every service whose value carries a `CloseWithContext(context.Context) error` of its own — the door [`ContextCloser`](../../container/contract/context_closer.go) names on the contract package, asked of the value by a type assertion and declared in place of `Close` on the framework's own service contracts — the cache and its backend, the session manager and its storage, a message bus transport, a server-sent-event backplane. Every service closes through it, with the deadline, by the container and by a request scope alike. The deadline is SHARED rather than handed out afresh: it bounds the teardown as a
  whole, so each service sees what is left of it and a component that spends the whole budget starves the ones the graph puts after it — and the answer an operator needs is which service ate it and which were left with none. The failure map names only the closes that FAILED — one line per service, with what the close raised BEYOND that line beside it under `failureDetails`, keyed the same way: for a contained panic the recovered value, its type, the frames that ran and the cause chain, which a line cannot carry. The entry is present only when a close left something the line does not already say, so a close answering a plain error adds none — every melody error seeds its context with its own message, which is the failure line verbatim. The frames of one contained panic are bounded and the cut names itself: the teardown error is one journal record, and an unbounded stack per failed node made a shutdown losing twenty services write tens of kilobytes of it in a single line. The context is
  read under the same containment the line has, so a close error whose `Unwrap` panics leaves a marker rather than ending the teardown — and a spent budget is not a failure: a closer reached with the deadline gone answers nil where it has nothing left to do, so a teardown that ran twice its budget used to exit clean with no trace of the service that ate it. A teardown that runs past its deadline therefore keeps a record — the budget it was given, what it spent, the closers running when the deadline passed (`spentBy`), the closers reached after it (`starved`), and the duration of every close — which travels as the `deadline` context of the error when a close failed, and is read through `TeardownDeadlineOverrun() exceptioncontract.Context` — declared on the concrete container, reached the way `IsClosed()` is, nil when there is nothing to say — when none did; the application writes it to the journal as a warning after a clean close. It is a diagnostic and never a failure, so it changes no
  exit code. `Close()` is `CloseWithContext` with no deadline, and leaves no record. The budget an application declares through `MELODY_TEARDOWN_TIMEOUT` reaches this door from the exit path; see [CONFIG.md](CONFIG.md). A request scope closes its own services through the same door: `CloseWithContext(ctx context.Context) error` is declared on the `Scope` contract too, and hands its context to every scoped service whose value carries the door. `Scope.Close()` is that under [`ContainerConfig.ScopeTeardownTimeout`](../../container/container.go), 3s when left out and no term when unlimited, and it is how the http kernel, the cli and the message consumer close their scopes; see [CONFIG.md](CONFIG.md).


- **The teardown closes in dependency WAVES, unless the container is configured sequential.** The teardown closes, in one wave, every service the graph proves has no relation to any other service in that wave, and keeps the waves themselves in order, so a dependent is still closed before everything it depends on. [`ContainerConfig.TeardownSequential`](../../container/container.go) — `ApplicationConfig.ContainerTeardownSequential` or `MELODY_CONTAINER_TEARDOWN_SEQUENTIAL` for an application, the key winning (see [APPLICATION.md](APPLICATION.md)) — closes service by service instead, in the order described above; `TeardownRunsInWaves()`, on the concrete container, answers which of the two the container is configured for, and `debug:container` prints it in the block's title; a `Close` that finds a declaration naming nothing runs the sequential order whatever the configuration (see below).

  The waves read "no edge" as "no relation" rather than as an order nobody chose, so every ordering the services need has to be written down — by a provider that RESOLVES its collaborator, or by `TeardownDependencyNames` where there is genuinely nothing to resolve. A provider that captures an already-built collaborator declares nothing. A declared edge naming a service nothing ever registered is the misspelling the waves cannot drop in silence: the application refuses it at the end of its boot through `VerifyDeclaredTeardownDependencies`, and a container used without an application reports it at `Close`, which then closes one service after the other rather than in waves and joins the refusal to its answer. A declaration naming a scoped service is refused with `ErrTeardownDependencyIsScoped` — at the registration when the scoped service is already registered, at the verification otherwise: a scoped service is built and closed by each scope and never has a node in the container's graph. A declaration keyed by a type more than one service is registered under is refused with `ErrTeardownDependencyTypeIsAmbiguous`, at the registration that declares it and at the second registration under a type a declaration already names. Under `TeardownSequential` none of the three is refused, since nothing closes at once.

  What it buys is not speed. Measured on the applications here, the whole teardown is about a millisecond and the waves save none of it; what changes is that a service is no longer STARVED — every closer in a wave starts at once and sees the whole of what is left of the deadline, instead of the remainder the component before it did not spend, so "the budget was already gone when this one was reached" stops being the ordinary case for whatever the order happened to put last. Two services the walk sees holding EACH OTHER — or a ring of them — gain no edge, because no ordering between them is true, and are closed one after the other inside their wave rather than at once — the plan names such a group by number in `TeardownPlanEntry.SerialGroup`, and `debug:container` prints it as the `group` column, since "same wave, no dependencies" would otherwise read as "closed together"; the built instances an override evicted close one after the other past everything the graph proved.

  Under the waves the container also WALKS what each service holds, where the service is filed, and orders a service before a collaborator it holds a pointer to, so a capture the author forgot to declare still closes in the right order. What the walk reads of a service is the same wherever the service came from: pointer words and struct and array layouts, never an interface, a slice or a string, because every word may be memory somebody else is writing — a fresh value's own fields included, since a provider may hand back an object built long before and already in use. Reading such a word is still a read without synchronisation, so a service whose own goroutines rewrite its pointer fields — a list head replaced under its mutex, a connection swapped on reconnect — is a data race the moment it is walked, which `go test -race` reports at its filing. Register it with `RegisterOptions{TeardownReflectionDisabled: true}`: its value is not walked, under its name, its type or a value installed over it, and a walk of another service that reaches it records the edge toward it and does not enter it — so it is still closed after what holds it, and what it holds itself is declared in
  `TeardownDependencyNames`. A registration whose provider declares a concrete pointer type shields every value of that type from the moment it is registered, so a holder that captured it and is walked before it is filed does not enter it either; a provider declaring an interface shields only the value it filed, so a holder walked before has already read it — resolve it inside the holder's provider, which files it first. A collaborator held only through an interface field or a slice is therefore not seen, from any door, and keeps the order it had; nor is one held through a bare `unsafe.Pointer` or an `atomic.Value`, because an untyped word names no type to match the identity against. An `atomic.Pointer[T]` is the exception: its type names `T`, and the walk reads it through its own atomic load, so it races no writer swapping it, and orders the holder before what it points at as a plain pointer field would — when the holder is reached through a pointer, as a registered service is; inside a value handed over by value the atomic word is not read, the plain pointer fields around it still are. Resolve it, or declare the edge. The walk is also bounded in cost: it reads at most 65,536 values per service, 12 levels deep (a pointer and the struct behind it are two), and 256 fields or elements of any one value, and the budget is spent level by level, so a wide level can exhaust it before a deeper collaborator is reached. An edge beyond those bounds is not gained; declare it.

  One residue no graph can close: two `CloseWithContext` methods that touch the same EXTERNAL state — one file, one table, a `sql.Register` name, a system resource — have no relation the container can see, whatever it walks. Declare the edge between them, or configure the container sequential; nothing will report that it is missing.
- A container that is closing refuses writes and keeps serving what it built until its teardown finishes: `Register` — the scoped registrations made through the container included — the overrides and `VerifyDeclaredTeardownDependencies` return the container-is-closed error, a new creation is refused, and a resolution of an already-built instance still answers — which is what lets an in-flight request degrade gracefully during shutdown. A scope is stricter: every door of a closed scope answers "scope is closed", except the enumerations `TypesImplementing` and `ReferencesImplementing`, which answer an empty list (`AllImplementing` turns that into the refusal). A resolution racing `Close()` is defined behavior — a creation caught mid-race is refused and the value it built is closed at once, by itself, outside the plan — after the services it depends on may already have closed, since no edge was written for it — rather than leaked, through `CloseWithContext` under the context of the resolution that built it, which waits for that close, a failure of that close joined to the refusal; the same when the scope it was built for closed while its provider ran, and when an override won its name, where the resolution answers the override and a failed close has no carrier (see [`container/container_close.go`](../../container/container_close.go) and [`container/resolver_context.go`](../../container/resolver_context.go)); the one thing `Close()` does not survive is re-entry from a service's own `CloseWithContext`, which deadlocks the teardown that is waiting on it.
- `OverrideInstance` / `MustOverrideInstance` reject service names with the `service.` prefix (protected services). If you must override a protected service in userland tests, use `OverrideProtectedInstance` or `MustOverrideProtectedInstance`. Every override door takes an [`OverrideOptions`](../../container/contract/override.go) struct last, whose zero value is the default; its one field, `ClosedWithScope`, is a scope's, and the root container refuses it by name (see [`OverrideService`](../../container/contract/override.go) and its implementations in [`container/container.go`](../../container/container.go) and [`container/scope.go`](../../container/scope.go)). The scoped registration paths refuse the prefix outright — with or without `ReplacesContainerService` — because a scoped registration of a protected name would be the same substitution, one scope wide.
- An override must fit every type its name is registered under; a value the registered type cannot hold is refused, so `GetByType` keeps answering with the registered type. An override that replaces an instance the container itself built hands that instance to the teardown — it is closed with the container, once, after the ordered walk of the registered services, so its `CloseWithContext` must not reach a service it resolved — while an override evicted by a later override stays whoever installed it's to close.
- When the container itself closes, everything still standing in its maps joins the teardown, installed overrides included — the container's lifetime is the process's, so ownership converges on it and there is no opt-out. A scope is deliberately the opposite: it closes what it built and nothing else, and an outside override joins its teardown only through `OverrideOptions{ClosedWithScope: true}` (see the scope section below), which the container itself refuses. The asymmetry follows the two lifetimes: a scope ends while its installer goes on running — the http kernel installs the request logger and then uses it to report the scope's own close failure — while a container ends with the process and has nobody to hand anything to. The one case the reasoning does not cover is a value shared with a SECOND container in the same process: a test suite that boots the application repeatedly over one reused client, or a host embedding melody that closes its own handles. This container closes it for all of them. Install a wrapper whose
  `CloseWithContext` does nothing and keep the real handle where it belongs.
- A provider is validated where it is registered: the signature, and the function value itself — a typed-nil `Provider[T]` handed in as a variable is refused at the registration line instead of panicking on its first resolution. A provider declared with a concrete error type may return its nil error and is read as the success it means.
- The scope↔container reference is held in an [`atomic.Pointer`](https://pkg.go.dev/sync/atomic#Pointer) (see [`container/scope.go`](../../container/scope.go)), so scope lookups and a concurrent `Scope.Close()` are race-free: after close, reads observe a nil container and callers receive a clean "scope is closed" error — the `Must*` forms keep the panic — instead of a partial read.

## Userland API

### Services are stateless by default

A service registered on the container is ONE instance for the whole life of the process and for every request at once — the provider runs once and the value is memoized (see the caveats above) — so it holds nothing that belongs to a request or to a call: not the current user, not the last response, not a working buffer, not a counter the next request reads. Whatever a request needs travels as a method argument, or lives in a scoped service (next section). The rule is the one a Symfony service follows, and it is measured rather than assumed: on 2026-09-18 every process service of this repository's composition root — the 58 the example resolves, the 19 the framework registers on every application, the doors of the integrations, 66 concrete types classified field by field (69 with the three reached only through them) — held no request or call state. What holds state does so in one of these categories:

- **caches, limiters and stores** — the state IS the product, kept under a lock: the in-memory cache backend, the session storage and manager, the token stores, the lockers, the nonce guard, the event dispatcher's listener lists;
- **connection and pool holders** — a bun registry, an amqp transport, a rueidis client, a tracer provider, a log file writer: the state is a resource, and the teardown closes it;
- **boot registries** — written while the application wires, read on the request path, FROZEN once it serves: the configuration ([`MarkServing`](../../config/configuration.go) refuses a late [`Resolve`](../../config/configuration_resolve.go)), the router and its route registry (a late route is refused at the door), the openapi registry ([`MarkServing`](../../openapi/registry.go), a late [`Describe`](../../openapi/registry.go) refused). A registry that could only document "call this at boot" is one process kill away from a module that did not read the sentence, which is why each of these has a door and not a comment;
- **factories** — the state belongs to the value produced, never to the factory;
- **scoped services** — the state is the request's by construction.

A process service that holds mutable state and is written from here on says WHY in the GoDoc of its type — the category, and what protects the state; of the types the measurement classified, the openapi registry says so and the others are documented by their doors and their locks, not by a category sentence. A service whose state fits none of the five is a request-state leak, whatever it is called: make it scoped, or move the state onto the value it produces or the argument it is handed.

```go
/* WRONG: a process service remembering the caller — every request shares this one instance */
type Exporter struct {
    currentUser string
}

func (instance *Exporter) Export(user string) []byte {
    instance.currentUser = user

    return instance.render()
}

/* RIGHT: the request's data travels with the call, or the service is registered scoped */
func (instance *Exporter) Export(user string) []byte {
    return instance.renderFor(user)
}
```

`debug:container` lists every registered service with its lifetime, and `debug:container --build` the concrete type each one resolves to, which together are the inventory the rule is checked against; the live harness pins the example's inventory — every process service classified by name, concrete type and category, so a service nobody classified fails the band, and so does one whose type changed since its state was measured.

### Scoped services

A service registered with `RegisterScoped` belongs to one scope — one http request, one command run — instead of to the process. It is built lazily on the first resolution through a scope, shared by everything inside that scope, and closed when the scope closes. The root container never sees it, and two scopes never share one instance.

```go
/* a process-lifetime service */
container.MustRegister(registrar, ServiceAuditWriter,
    func(resolver containercontract.Resolver) (*AuditWriter, error) {
        return NewAuditWriter(container.MustFromResolverByType[*AuditRepository](container.ContextOf(resolver), resolver)), nil
    },
    container.RegisterOptions{})

/* a request-lifetime service: it may read both the scope and the container */
container.MustRegisterScoped(registrar, ServiceAuditTrail,
    func(resolver containercontract.Resolver) (*AuditTrail, error) {
        ctx := container.ContextOf(resolver)

        return NewAuditTrail(
            http.RequestContextMustFromResolver(ctx, resolver),
            AuditWriterMustFromResolver(ctx, resolver),
        ), nil
    },
    container.RegisterOptions{})
```

The lifetime is spelled in the verb, and the two registrar interfaces share no method: `Registrar` has `Register`/`MustRegister`, `ScopedRegistrar` has `RegisterScoped`/`MustRegisterScoped`. Handing one where the other is expected does not compile, which is what keeps a container provider from being registered as scoped — a mistake that would otherwise rebuild and close it once per request without ever failing.

The direction stays one-way. A scoped service reads both levels; a container provider is refused anything only a scope carries, with the same "service is not registered" a wiring mistake gets anywhere else. `Has` and `HasType` on the resolver a provider was handed answer under that same suspension, so an existence check can never disagree with the resolution it gates. A container service that needs request data takes it as a method argument.

A name — or a registered type — claimed at both lifetimes is refused where it is made, unless the SCOPED registration declares `RegisterOptions{ReplacesContainerService: true}`; the container-level registration paths do not read the field. Declared where nothing collides yet, the waiver stands: a container registration of the same name arriving later is admitted without declaring anything itself. The waiver covers the name and the registered type together — a registration that means only the name opts out with `TypeRegistrationDisabled`. It admits substitution, not decoration: a scoped provider that resolves the name it replaces re-enters itself and is reported as a circular dependency. Protected `service.` names cannot be registered scoped at all.

The declaration lives on [`ScopedRegistrar`](../../container/contract/scoped_registrar.go), which the container implements as the thing that makes scopes, rather than beside the container's own registrations; `ScopeManager`, and so `Container`, and `Scope` embed it. A scope does not exist until a request arrives, so what a scope will own has to be declared at boot by whatever will be creating them.

A scope's `RegisterScoped` — every scope the container makes implements [`ScopedRegistrar`](../../container/contract/scoped_registrar.go) too — adds a service to one live scope, layered over the plan the container was booted with. It is the rare case; a scoped service is normally declared at boot so every scope gets it. Like the other two registration doors it refuses once the container has begun closing, because a registration accepted on a live scope would report success for a service whose every resolution the creation guard then refuses.

### Scope teardown

`Close` closes what the scope built and nothing else: an override belongs to whoever installed it, and a singleton reached through the scope belongs to the root container. Services the scope built are closed in dependency order, dependents before their dependencies — one instance filed under its name and its type is collapsed into one node before the ordering runs, so the close order holds however the service was resolved, and a value neither comparable nor addressable is closed once rather than once per filing. An override that has nowhere else to be closed can join the teardown with `OverrideOptions{ClosedWithScope: true}` ([`OverrideOptions`](../../container/contract/override.go)) on any of the four override doors; a created instance such an override evicts is still the scope's, and still closes with it.

A scope override answers the type-keyed resolutions of every type its name is registered under, as the container-level override propagates — the container's registrations, the plan's and the scope's own — and, unlike the container's, under the value's own canonical type when no registration at any level claims that type; so a name and a type can never answer two different services inside one request. The same assignability rule guards both levels: a value a registered type cannot hold is refused before anything is written, raw assignability for an interface registration and canonical identity for a value-typed one.

A [`container.Lazy`](../../container/lazy.go) handle built over a scope follows that scope's life. The memoized value is served while the scope lives; once the scope reports itself closed through `Scope.Closed()`, the handle becomes terminal — the scope-is-closed error on that call and every later one, with the value, the closure and the resolver dropped — because a memoized value from a dead request is that request's state and the alternative was serving it to every later caller forever. A handle built over the container, or over the resolver a container provider was handed, follows the container instead: it serves through the teardown — a service's own `Close` may still reach what it depends on through it — and turns terminal once the teardown finished, answering `ErrContainerClosed`, so a caller tells an application shutting down from a request that ended. A resolver that cannot answer the liveness question at all is read as OPEN. Every form is safe for concurrent first uses, since
every container resolution, and every resolution through a provider's resolver once its provider returned, mints a fresh resolution context.

A `Lazy` or `LazyByType` handle built over the container, or over the resolver a container provider was handed, is recorded where it is built, and the application checks every recorded handle at the end of `Boot` through the container's `VerifyLazyNames`. A name nothing registered, a scoped name, a type nothing registered, a type more than one service is registered under, and a service whose built value or declared concrete type is not the handle's type each fail the boot, naming the service or the type, the line that built the handle and its caller; the cause is [`ErrLazyHandleNameNotRegistered`](../../container/errors.go), [`ErrLazyHandleTypeIsAmbiguous`](../../container/errors.go) or [`ErrLazyHandleTypeMismatch`](../../container/errors.go). A provider declaring an interface passes for any handle type, since only the value it returns can answer. A registration that was never built passes, and nothing is resolved by the check. The check ends the recording: a handle built afterwards, at request time or inside a provider resolved later, is not verified, and neither is a handle over a scope or over a resolver of your own. One line that builds many handles for one service, from one caller, is recorded once. A container you use without the application is never checked and keeps its record for its life, one entry per service, line and caller. A handle for an optional service is built after a `Has` check, so the boot does not refuse it.

### Contracts (`container/contract`)

- [`type Container`](../../container/contract/container.go)
- [`type Resolver`](../../container/contract/resolver.go)
- [`type Registrar`](../../container/contract/registrar.go)
- [`type OverrideService`](../../container/contract/override.go)
- [`type ScopeManager`](../../container/contract/scope.go)
- [`type Scope`](../../container/contract/scope.go)
- [`type ScopedRegistrar`](../../container/contract/scoped_registrar.go)
- [`type OverrideOptions`](../../container/contract/override.go)
- [`type Provider[T]`](../../container/contract/provider.go)
- [`type RegisterOptions`](../../container/contract/registrar.go)
- [`type Closer`](../../container/contract/registrar.go)
- [`type TypeLister`](../../container/contract/type_lister.go)
- [`type ResolutionContext`](../../container/contract/resolution_context.go) — the optional capability of a resolver that carries the context of the resolution in flight
- [`type GoroutineResolver`](../../container/contract/goroutine_resolver.go) — the optional capability of a provider's resolver to hand out a resolver for a goroutine the provider starts
- [`type ServiceReference`](../../container/contract/type_lister.go)
- [`type ServiceDescription`](../../container/contract/service_description.go), [`ServiceLifetimeContainer`, `ServiceLifetimeScoped`](../../container/contract/service_description.go) — what a container can say about a registration without running its provider: the name, the owning lifetime, whether an instance already exists, and the type. Answered by `ServiceDescriptions()` on the container, which is what lets an introspection command list a container without building it

### Constructors and helpers (`container`)

- [`NewContainer(config ContainerConfig) containercontract.Container`](../../container/container.go), [`ContainerConfig`](../../container/container.go), [`CreationWaitersRule`, `DefaultCreationWaiters`](../../container/container.go)
- Typed registration:
    - [`Register[T]`](../../container/container_register.go)
    - [`MustRegister[T]`](../../container/container_register.go)
    - [`RegisterType[T]`](../../container/container_register.go)
    - [`MustRegisterType[T]`](../../container/container_register.go)
- Typed scoped registration:
    - [`RegisterScoped[T]`](../../container/container_register_scoped.go)
    - [`MustRegisterScoped[T]`](../../container/container_register_scoped.go)
    - [`RegisterScopedType[T]`](../../container/container_register_scoped.go)
    - [`MustRegisterScopedType[T]`](../../container/container_register_scoped.go)
- Registration options:
    - [`type RegisterOptions`](../../container/register_option.go), the alias of the contract struct
    - [`NewCloser[T](closer func(context.Context, T) error)`](../../container/register_option.go)
- Override options:
    - [`type OverrideOptions`](../../container/override_option.go), the alias of the contract struct
- Interface collection:
    - [`AllImplementing[T]`](../../container/resolver_implementing.go)
    - [`MustAllImplementing[T]`](../../container/resolver_implementing.go)
- Typed resolution:
    - [`FromResolver[T]`](../../container/resolver.go)
    - [`MustFromResolver[T]`](../../container/resolver.go)
    - [`FromResolverByType[T]`](../../container/resolver.go)
    - [`MustFromResolverByType[T]`](../../container/resolver.go)
    - [`ContextOf(resolver) context.Context`](../../container/resolver.go) — the context a provider's resolver carries, `context.Background()` for a resolver without one
    - [`ForGoroutine(resolver) Resolver`](../../container/resolver.go) — the resolver a goroutine started by a provider resolves through, on a chain of its own; any other resolver is answered as it is
- Deferred resolution — the handle a component assembled during the boot phase holds over a service whose provider is registered but not yet safe to resolve at that phase:
    - [`type LazyService[T]`](../../container/lazy.go) — `Resolve(ctx)` and `Get(ctx)` resolve under the caller's context; memoizes success only, so a failed, cancelled or nil resolution is retried on the next call
    - [`Lazy[T](resolver, serviceName)`](../../container/lazy.go) — the deferred form of `FromResolver` / `MustFromResolver`; for a goroutine the provider starts, built over `ForGoroutine(resolver)`
    - [`LazyByType[T](resolver)`](../../container/lazy.go) — the deferred form of `FromResolverByType` / `MustFromResolverByType`
    - `VerifyLazyNames()` on the container — refuses the first recorded handle no registration answers; the application calls it at the end of `Boot`
- Sentinels:
    - [`ErrServiceIdAlreadyRegistered`, `ErrServiceTypeAlreadyRegistered`, `ErrScopedServiceIdAlreadyRegistered`, `ErrScopedServiceTypeAlreadyRegistered`](../../container/errors.go)
- Scopes: created via `Container.NewScope()` (see [`ScopeManager`](../../container/contract/scope.go)).
