# CONTAINER

The [`container`](../../container) package provides Melody’s dependency injection container: service registration, deterministic service creation, scoping, overrides, and deterministic shutdown ordering.

## Scope

The container is responsible for:

- registering services by name (and optionally by concrete type),
- resolving services with deterministic single-instance semantics,
- detecting circular dependencies in a single resolver context,
- creating request-scoped (or operation-scoped) overlays via scopes,
- closing services in a deterministic order (dependents before dependencies).

## Subpackages

- [`container/contract`](../../container/contract)  
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

Services are always registered by name (`serviceName string`). By default a registration also registers the service under its concrete type, strictly — typed resolution works out of the box, and a second service registered under the same type fails.

Registration options (see [`RegisterOptions`](../../container/contract/registrar.go)):

- [`WithTypeRegistration(isStrict bool)`](../../container/register_option.go)  
  Keeps type registration on and sets its strictness; `WithTypeRegistration(false)` relaxes the REGISTRATION, not the resolution: a second registration under the same type is accepted and appended instead of failing, and by-type resolution then refuses every one of them with `service type has multiple registrations`. Nothing wins — the type simply stops being resolvable, and the services stay reachable by name.
- [`WithoutTypeRegistration()`](../../container/register_option.go)  
  Disables type registration for that registration call — the opt-out, since the default is on and strict.

Type registration is **on by default** (strict) for `RegisterService`/`MustRegisterType`, so every registered service is also resolvable by its concrete return type — no extra option needed. Resolve a service by type with the generic [`MustFromResolverByType[T]`](../../container/resolver.go) / [`FromResolverByType[T]`](../../container/resolver.go):

```go
bus := container.MustFromResolverByType[messagebuscontract.Bus](resolver)
```

For a service with a **single** implementation this removes the need to invent a string service-name constant and a per-type `MustGetX` accessor — register it and resolve it by type. Keep the **named** path (`RegisterService(ServiceX, ...)` + `XMustFromResolver`) when a contract has more than one implementation that must coexist: because type registration is strict, registering two services under the same contract type fails at registration (the string name is then the only disambiguator).

### Collecting every implementation of an interface

A component that must act on **all** services of a kind — a dispatcher over every message handler, a scheduler over every cron task — collects them with [`AllImplementing[T]`](../../container/resolver_implementing.go) instead of being handed a hand-maintained list that goes stale when a service is added:

```go
handlers, err := container.AllImplementing[MessageHandler](resolver)
```

`AllImplementing[T]` requires `T` to be an interface and resolves every registered service whose type satisfies it — one registered under the interface type itself included, and every instance of a type registered non-strictly under several names. `MustAllImplementing[T]` is the panicking variant. The order never changes between runs: descending [`WithCollectionPriority(n)`](../../container/register_option.go) (a higher priority is dispatched earlier), then type and name. A provider that fails aborts the whole collection rather than yielding a partial set.

Collect with the **resolver a provider receives**, not the container itself, so the collection participates in the current scope's overrides and so a container blocking on its own in-flight creation is avoided. The service whose provider is doing the collecting is excluded from its own result — the composite dispatcher that is itself one of the handlers it dispatches to collects the others. The enumeration is exposed through [`contract.TypeLister`](../../container/contract/type_lister.go), kept apart from `Resolver` so a resolver written outside the framework keeps compiling; the container, a request scope and the provider resolver all implement it, and a closed scope refuses the collection the way its `Get` does.

## Usage

The example below demonstrates:

- registering a service by name,
- registering a service under its type through the generic door, which is strict by default,
- resolving a dependency inside a provider,
- creating a scope and overriding a service instance.

```go
package main

import (
	"fmt"

	"github.com/precision-soft/melody/v3/container"
	containercontract "github.com/precision-soft/melody/v3/container/contract"
	"github.com/precision-soft/melody/v3/exception"
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
	)

	container.MustRegister[string](
		serviceContainer,
		"example.greeting",
		func(resolver containercontract.Resolver) (string, error) {
			logger := container.MustFromResolver[Logger](
				resolver,
				"example.logger",
			)

			logger.Info("building greeting")

			return "hello", nil
		},
	)
}

func example() {
	serviceContainer := container.NewContainer()

	registerServices(serviceContainer)

	scope := serviceContainer.NewScope()
	scope.MustOverrideInstance(
		"example.greeting",
		"hello from scope",
	)

	greeting := container.MustFromResolver[string](
		scope,
		"example.greeting",
	)

	logger := container.MustFromResolver[Logger](
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
- Circular dependency detection works at two levels: the per-resolution stack (see [`Resolver`](../../container/contract/resolver.go) and the stack logic in [`container/resolver_context.go`](../../container/resolver_context.go)) catches a cycle inside one resolution, and a container-wide wait graph ([`container/container_resolver.go`](../../container/container_resolver.go)) refuses two concurrent resolutions waiting on each other's creations with `circular service dependency detected across concurrent resolutions`.
- Closing is deterministic and dependency-aware: dependents are closed before dependencies (see [`container/container_close.go`](../../container/container_close.go)). The edge is recorded from the resolver a service was built through, so it covers resolutions made after construction too: a service that keeps its resolver and reaches through it later — a `container.Lazy` handle at first use, any replay of deferred work — is still closed before what it resolves. The one handle with no owner is one built over the **container itself** rather than over a provider's resolver: there is no node it could belong to, so what it resolves is ordered against it by creation order like any unrelated pair. Give such a service an edge by resolving what it needs inside its provider, or build the handle over the resolver the provider was handed.

- **A provider that CAPTURES a collaborator writes no edge.** The graph is recorded from resolutions, so a provider closing over a value built before the container — an integration's registry assembled during module wiring, a client the composition root already holds — and simply handing it back has declared nothing, and the teardown orders it against everything else by creation order. Nothing looks wrong: the service is registered, it is closed, and the order it is closed in is a coincidence that holds until the day it does not. Resolve the collaborator inside the provider wherever that is possible — it is the same call the service would have made anyway, and an edge derived from the resolution cannot fall out of step with what the provider actually uses. Where it is genuinely not possible, because the value has nothing to resolve, declare the edge with [`WithTeardownDependency(names ...string)`](../../container/register_option.go): it writes into the same graph, in the same key space, so the teardown still reads one graph and cannot order two ways. It orders teardown and nothing else — it does not build the named service, does not make it exist, and takes no part in cycle detection during resolution; an edge naming a service that was never created is dropped by the walk, so naming an optional collaborator is not an error. An empty name is refused where the registration is made, and so is a service naming itself. `RegisterScoped` refuses it outright, because a scope keeps its own teardown graph, built per scope from the resolutions that scope actually made, and a declaration written once at registration would install nothing while reading as an ordering that holds. The same declaration keyed by TYPE is [`WithTeardownDependencyOfType[T]()`](../../container/register_option.go), for the collaborator the declaring code knows as a type rather than as a spelling — everything above holds for it word for word — but for where the edge lives: a declaration keyed by a type is kept beside the graph and expanded onto the name that type stands for when each plan is built, rather than written into the graph — and `T` and `*T` name one node, because the container files them under one canonical type.
- What the graph leaves open is settled by **creation order, latest first**: two services with no edge between them are closed newest first, because a service built during the construction of another was needed by it whether or not the edge was declared. The tie-break this replaced was the node key descending — a string comparison, so whether a worker still had a logger to report its drain through was decided by its own name, and renaming `app.worker` to `zz.worker` was the whole difference between a shutdown record written and a shutdown record dropped. What it is NOT is a dependency: measured on the applications in this repository, the whole teardown graph is a single edge over five to seven services, so creation order is what decides almost every pair — an arrangement that holds until the day two of those services actually need each other, at which point nothing in the graph says so. Where an ordering matters, write it down: resolve the collaborator inside the provider, or declare the edge.
- **Closing has two states, and a service's own `Close` may still resolve.** `IsClosed()` — declared on the concrete container, reached through a type assertion when holding the [`Container`](../../container/contract/container.go) contract — answers true from the moment the teardown begins and refuses every new creation for its whole duration, which is what keeps a service from being built into a container that is going away. Resolutions of what is already built keep answering until the last `Close` returns — a worker reporting its drain is entitled to the logger it reports through — and are refused with "container is closed" from then on. A resolution made after that used to answer the instance it found in the map, already closed, with a nil error, so a caller holding a resolver got a live-looking handle to a dead service.
- **The teardown carries a deadline, and a service can ask for it.** `CloseWithContext(ctx context.Context) error` — declared on the concrete container and reached through a type assertion, the way `IsClosed()` is — closes the container under a deadline its caller declares, and hands that deadline to every service whose value carries a `CloseWithContext(context.Context) error` of its own. A service that carries only `Close() error` is closed with it and is bounded by nothing but itself, which is where every service was before the door existed. The deadline is SHARED rather than handed out afresh: it bounds the teardown as a whole, so each service sees what is left of it and a component that spends the whole budget starves the ones the graph puts after it — and the answer an operator needs is which service ate it and which were left with none. The failure map names only the closes that FAILED, and a spent budget is not a failure: a closer reached with the deadline gone answers nil where it has nothing left to do, so a teardown that ran twice its budget used to exit clean with no trace of the service that ate it. A teardown that runs past its deadline therefore keeps a record — the budget it was given, what it spent, the closers running when the deadline passed (`spentBy`), the closers reached after it (`starved`), and the duration of every close — which travels as the `deadline` context of the error when a close failed, and is read through `TeardownDeadlineOverrun() exceptioncontract.Context` — declared on the concrete container, reached the way `IsClosed()` is, nil when there is nothing to say — when none did; the application writes it to the journal as a warning after a clean close. It is a diagnostic and never a failure, so it changes no exit code. `Close()` is `CloseWithContext` with no deadline, and leaves no record. The budget an application declares through `MELODY_TEARDOWN_TIMEOUT` reaches this door from the exit path; see [CONFIG.md](CONFIG.md). A request scope closes its own services through the same preference: `CloseWithContext(ctx context.Context) error` is declared on the concrete scope too, reached through a type assertion the way the scope's liveness question is asked, and hands its context to every scoped service whose value carries the door — `Scope.Close()` is that under no term, which is how the http kernel and the cli close a request's scope, so a scoped service's deadline is a caller's to declare rather than the framework's.

  `Scope.CloseWithContext` is not on the `Scope` contract, and which caller owns the budget of a request's teardown is deliberately not decided in this major.

- **The teardown can close in dependency WAVES, and it is off until the application says so.** `ArmParallelTeardown() error` — declared on the concrete container and reached through a type assertion, the way `IsClosed()` and `CloseWithContext` are — closes, in one wave, every service the graph proves has no relation to any other service in that wave, and keeps the waves themselves in order, so a dependent is still closed before everything it depends on. The default is unchanged: without it the loop is strictly sequential, service by service, in the order described above.

  Arming it is an ASSERTION about the application's own providers rather than a performance setting, which is why it is a door in the composition root and not a configuration key: whoever turns it on has to be the person who wrote the providers. What is asserted is that every ordering the services need is written down — by a provider that RESOLVES its collaborator, or by `WithTeardownDependency` where there is genuinely nothing to resolve. A provider that captures an already-built collaborator declares nothing, and under waves "no edge" is read as "no relation" rather than as an order nobody chose. Arm it after the wiring and before the application runs: arming validates every declared edge and refuses one that names a service nothing ever registered, which is the misspelling the sequential teardown could afford to drop in silence. A declaration naming a scoped service is refused the same way, with `ErrTeardownDependencyIsScoped`: a scoped service is built and closed by each scope and never has a node in the container's graph. And arming is not a snapshot — every registration made after it is validated at its own door under the same rules, a second registration under a type a declaration already names included; which is also a rule about ORDER once armed: a registration that declares a dependency on a name or a type nothing has registered yet is refused there and then, so after arming a dependency is registered before the service that declares it.

  What it buys is not speed. Measured on the applications here, the whole teardown is about a millisecond and the waves save none of it; what changes is that a service is no longer STARVED — every closer in a wave starts at once and sees the whole of what is left of the deadline, instead of the remainder the component before it did not spend, so "the budget was already gone when this one was reached" stops being the ordinary case for whatever the order happened to put last. Two services the walk sees holding EACH OTHER — or a ring of them — gain no edge, because no ordering between them is true, and are closed one after the other inside their wave rather than at once — the plan names such a group by number in `TeardownPlanEntry.SerialGroup`, and `debug:container` prints it as the `group` column, since "same wave, no dependencies" would otherwise read as "closed together"; the built instances an override evicted close one after the other past everything the graph proved. What the walk reads of a service is the same wherever the service came from: pointer words and struct and array layouts, never an interface, a slice or a string, because every word may be memory somebody else is writing — a fresh value's own fields included, since a provider may hand back an object built long before and already in use. A collaborator held only through an interface field or a slice is therefore not seen, from any door, and keeps the order it had; resolve it, or declare the edge.

  One residue no graph can close, and it is why this is opt-in rather than default: two `Close` methods that touch the same EXTERNAL state — one file, one table, a `sql.Register` name, a system resource — have no relation the container can see, whatever it walks. Declaring the edge between them is the only thing that orders them, and nothing will report that it is missing.
- A closed container refuses writes and keeps serving what it built: `Register` — the scoped registrations made through the container included — the overrides and `ArmParallelTeardown` return the container-is-closed error, a new creation is refused, and a resolution of an already-built instance still answers — which is what lets an in-flight request degrade gracefully during shutdown. A scope is stricter: every entry of a closed scope answers "scope is closed". A resolution racing `Close()` is defined behavior — a creation caught mid-race is refused and the value it built is closed rather than leaked (see [`container/container_close.go`](../../container/container_close.go) and [`container/resolver_context.go`](../../container/resolver_context.go)); the one thing `Close()` does not survive is re-entry from a service's own `Close`, which deadlocks the teardown that is waiting on it.
- `OverrideInstance` / `MustOverrideInstance` reject service names with the `service.` prefix (protected services). If you must override a protected service in userland tests, use `OverrideProtectedInstance` or `MustOverrideProtectedInstance` (see [`OverrideService`](../../container/contract/override.go) and its implementations in [`container/container.go`](../../container/container.go) and [`container/scope.go`](../../container/scope.go)). The scoped registration paths refuse the prefix outright — with or without `Replacing()` — because a scoped registration of a protected name would be the same substitution, one scope wide.
- An override must fit every type its name is registered under; a value the registered type cannot hold is refused, so `GetByType` keeps answering with the registered type. An override that replaces an instance the container itself built hands that instance to the teardown — it is closed with the container, once — while an override evicted by a later override stays whoever installed it's to close.
- When the container itself closes, everything still standing in its maps joins the teardown, installed overrides included — the container's lifetime is the process's, so ownership converges on it and there is no opt-out. A scope is deliberately the opposite: it closes what it built and nothing else, and an outside override joins its teardown only through `ClosedWithScope()` (see the scope section below). The asymmetry follows the two lifetimes: a scope ends while its installer goes on running — the http kernel installs the request logger and then uses it to report the scope's own close failure — while a container ends with the process and has nobody to hand anything to. The one case the reasoning does not cover is a value shared with a SECOND container in the same process: a test suite that boots the application repeatedly over one reused client, or a host embedding melody that closes its own handles. This container closes it for all of them. Install a wrapper whose `Close` does
  nothing and keep the real handle where it belongs.
- A provider is validated where it is registered: the signature, and the function value itself — a typed-nil `Provider[T]` handed in as a variable is refused at the registration line instead of panicking on its first resolution. A provider declared with a concrete error type may return its nil error and is read as the success it means.
- The scope↔container reference is held in an [`atomic.Pointer`](https://pkg.go.dev/sync/atomic#Pointer) (see [`container/scope.go`](../../container/scope.go)), so scope lookups and a concurrent `Scope.Close()` are race-free: after close, reads observe a nil container and callers receive a clean "scope is closed" error — the `Must*` forms keep the panic — instead of a partial read.

## Userland API

### Scoped services

A service registered with `RegisterScoped` belongs to one scope — one http request, one command run — instead of to the process. It is built lazily on the first resolution through a scope, shared by everything inside that scope, and closed when the scope closes. The root container never sees it, and two scopes never share one instance.

```go
/* a process-lifetime service */
container.MustRegister(registrar, ServiceAuditWriter,
    func(resolver containercontract.Resolver) (*AuditWriter, error) {
        return NewAuditWriter(bunorm.RepositoryMustFromResolver(resolver)), nil
    })

/* a request-lifetime service: it may read both the scope and the container */
container.MustRegisterScoped(registrar, ServiceAuditTrail,
    func(resolver containercontract.Resolver) (*AuditTrail, error) {
        return NewAuditTrail(
            http.RequestContextMustFromResolver(resolver),
            AuditWriterMustFromResolver(resolver),
        ), nil
    })
```

The lifetime is spelled in the verb, and the two registrar interfaces share no method: `Registrar` has `Register`/`MustRegister`, `ScopedRegistrar` has `RegisterScoped`/`MustRegisterScoped`. Handing one where the other is expected does not compile, which is what keeps a container provider from being registered as scoped — a mistake that would otherwise rebuild and close it once per request without ever failing.

The direction stays one-way. A scoped service reads both levels; a container provider is refused anything only a scope carries, with the same "service is not registered" a wiring mistake gets anywhere else. `Has` and `HasType` on the resolver a provider was handed answer under that same suspension, so an existence check can never disagree with the resolution it gates. A container service that needs request data takes it as a method argument.

A name — or a registered type — claimed at both lifetimes is refused where it is made, unless the SCOPED registration declares [`Replacing()`](../../container/register_option.go); the container-level registration paths do not read the option. Declared where nothing collides yet, the waiver stands: a container registration of the same name arriving later is admitted without declaring anything itself. The waiver covers the name and the registered type together — a registration that means only the name opts out with `WithoutTypeRegistration()`. It admits substitution, not decoration: a scoped provider that resolves the name it replaces re-enters itself and is reported as a circular dependency. Protected `service.` names cannot be registered scoped at all.

The declaration lives on [`ScopeManager`](../../container/contract/scope.go) — the interface the container satisfies by being the thing that makes scopes — rather than beside the container's own registrations. A scope does not exist until a request arrives, so what a scope will own has to be declared at boot by whatever will be creating them.

`Scope.RegisterScoped` adds a service to one live scope, layered over the plan the container was booted with. It is the rare case; a scoped service is normally declared at boot so every scope gets it. Like the other two registration doors it refuses once the container has begun closing, because a registration accepted on a live scope would report success for a service whose every resolution the creation guard then refuses.

### Scope teardown

`Close` closes what the scope built and nothing else: an override belongs to whoever installed it, and a singleton reached through the scope belongs to the root container. Services the scope built are closed in dependency order, dependents before their dependencies — one instance filed under its name and its type is collapsed into one node before the ordering runs, so the close order holds however the service was resolved, and a value neither comparable nor addressable is closed once rather than once per filing. An override that has nowhere else to be closed can join the teardown with [`ClosedWithScope()`](../../container/override_option.go) through `OverrideProtectedInstanceWithOptions`; a created instance such an override evicts is still the scope's, and still closes with it.

A scope override answers the type-keyed resolutions of every type its name is registered under, exactly as the container-level override propagates — the container's registrations, the plan's and the scope's own — so a name and a type can never answer two different services inside one request. The same assignability rule guards both levels: a value a registered type cannot hold is refused before anything is written, raw assignability for an interface registration and canonical identity for a value-typed one.

A [`container.Lazy`](../../container/lazy.go) handle built over a scope follows that scope's life. The memoized value is served while the scope lives; once the scope reports itself closed through `Scope.Closed()`, the handle becomes terminal — the scope-is-closed error on that call and every later one, with the value, the closure and the resolver dropped — because a memoized value from a dead request is that request's state and the alternative was serving it to every later caller forever. A resolver that cannot answer the liveness question is read as OPEN, so a handle built over the container is untouched; that is also the form safe for concurrent first uses, since every container resolution mints a fresh resolution context.

### Contracts (`container/contract`)

- [`type Container`](../../container/contract/container.go)
- [`type Resolver`](../../container/contract/resolver.go)
- [`type Registrar`](../../container/contract/registrar.go)
- [`type OverrideService`](../../container/contract/override.go)
- [`type ScopeManager`](../../container/contract/scope.go)
- [`type Scope`](../../container/contract/scope.go)
- [`type ScopedRegistrar`](../../container/contract/scoped_registrar.go)
- [`type OverrideServiceWithOptions`](../../container/contract/override.go)
- [`type OverrideOption`](../../container/contract/override.go)
- [`type OverrideOptions`](../../container/contract/override.go)
- [`type Provider[T]`](../../container/contract/provider.go)
- [`type RegisterOption`](../../container/contract/registrar.go)
- [`type RegisterOptions`](../../container/contract/registrar.go)
- [`type TypeLister`](../../container/contract/type_lister.go)
- [`type ServiceReference`](../../container/contract/type_lister.go)
- [`type ServiceDescription`](../../container/contract/service_description.go), [`ServiceLifetimeContainer`, `ServiceLifetimeScoped`](../../container/contract/service_description.go) — what a container can say about a registration without running its provider: the name, the owning lifetime, whether an instance already exists, and the type. Answered by `ServiceDescriptions()` on the container, which is what lets an introspection command list a container without building it

### Constructors and helpers (`container`)

- [`NewContainer() containercontract.Container`](../../container/container.go)
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
    - [`WithTypeRegistration(isStrict bool)`](../../container/register_option.go)
    - [`WithoutTypeRegistration()`](../../container/register_option.go)
    - [`Replacing()`](../../container/register_option.go)
    - [`WithTeardownDependency(serviceNames ...string)`](../../container/register_option.go)
- Override options:
    - [`ClosedWithScope()`](../../container/override_option.go)
    - [`WithCollectionPriority(priority int)`](../../container/register_option.go)
- Interface collection:
    - [`AllImplementing[T]`](../../container/resolver_implementing.go)
    - [`MustAllImplementing[T]`](../../container/resolver_implementing.go)
- Typed resolution:
    - [`FromResolver[T]`](../../container/resolver.go)
    - [`MustFromResolver[T]`](../../container/resolver.go)
    - [`FromResolverByType[T]`](../../container/resolver.go)
    - [`MustFromResolverByType[T]`](../../container/resolver.go)
- Deferred resolution — the handle a component assembled during the boot phase holds over a service whose provider is registered but not yet safe to resolve at that phase:
    - [`type LazyService[T]`](../../container/lazy.go) — memoizes success only, so a failed or nil resolution is retried on the next call
    - [`Lazy[T](resolver, serviceName)`](../../container/lazy.go) — the deferred form of `FromResolver` / `MustFromResolver`
    - [`LazyByType[T](resolver)`](../../container/lazy.go) — the deferred form of `FromResolverByType` / `MustFromResolverByType`
- Sentinels:
    - [`ErrServiceIdAlreadyRegistered`, `ErrServiceTypeAlreadyRegistered`, `ErrScopedServiceIdAlreadyRegistered`, `ErrScopedServiceTypeAlreadyRegistered`](../../container/errors.go)
      Scopes are created via `Container.NewScope()` (see [`ScopeManager`](../../container/contract/scope.go)).
