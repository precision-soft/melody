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
  Keeps type registration on and sets its strictness; `WithTypeRegistration(false)` relaxes the default so a later registration under the same type wins instead of failing.
- [`WithoutTypeRegistration()`](../../container/register_option.go)  
  Disables type registration for that registration call — the opt-out, since the default is on and strict.

## Usage

The example below demonstrates:

- registering a service by name,
- spelling out the strict type registration the defaults already give,
- resolving a dependency inside a provider,
- creating a scope and overriding a service instance.

```go
package main

import (
	"fmt"

	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/exception"
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
- Closing is deterministic and dependency-aware: dependents are closed before dependencies (see [`container/container_close.go`](../../container/container_close.go)). The edge is recorded from the resolver a service was built through, so it covers resolutions made after construction too: a service that keeps its resolver and reaches through it later — a `container.Lazy` handle, or the `ContainerCarrier` pattern — is still closed before what it resolves. The one handle with no owner is one built over the **container itself** rather than over a provider's resolver: there is no node it could belong to, so what it resolves is ordered by name against it like any unrelated pair. Give such a service an edge by resolving what it needs inside its provider, or build the handle over the resolver the provider was handed.
- What the graph leaves open is settled by **creation order, latest first**: two services with no edge between them are closed newest first, because a service built during the construction of another was needed by it whether or not the edge was declared. The tie-break this replaced was the node key descending — a string comparison, so whether a worker still had a logger to report its drain through was decided by its own name, and renaming `app.worker` to `zz.worker` was the whole difference between a shutdown record written and a shutdown record dropped. The logger is the case that shows it: the boot resolves it first, so it is now closed last, without anything anywhere naming it.
- **Closing has two states, and a service's own `Close` may still resolve.** `IsClosed()` — declared on the concrete container, reached through a type assertion when holding the [`Container`](../../container/contract/container.go) contract — answers true from the moment the teardown begins and refuses every new creation for its whole duration, which is what keeps a service from being built into a container that is going away. Resolutions of what is already built keep answering until the last `Close` returns — a worker reporting its drain is entitled to the logger it reports through — and are refused with "container is closed" from then on. A resolution made after that used to answer the instance it found in the map, already closed, with a nil error, so a caller holding a resolver got a live-looking handle to a dead service.
- A closed container refuses writes and keeps serving what it built: `Register`, `RegisterScoped` and the overrides return the container-is-closed error, a new creation is refused, and a resolution of an already-built instance still answers — which is what lets an in-flight request degrade gracefully during shutdown. A scope is stricter: every entry of a closed scope answers "scope is closed". A resolution racing `Close()` is defined behavior — a creation caught mid-race is refused and the value it built is closed rather than leaked (see [`container/container_close.go`](../../container/container_close.go) and [`container/resolver_context.go`](../../container/resolver_context.go)); the one thing `Close()` does not survive is re-entry from a service's own `Close`, which deadlocks the teardown that is waiting on it.
- `OverrideInstance` / `MustOverrideInstance` reject service names with the `service.` prefix (protected services). If you must override a protected service in userland tests, use `OverrideProtectedInstance` or `MustOverrideProtectedInstance` (see [`OverrideService`](../../container/contract/override.go) and its implementations in [`container/container.go`](../../container/container.go) and [`container/scope.go`](../../container/scope.go)). The scoped registration paths refuse the prefix outright — with or without `Replacing()` — because a scoped registration of a protected name would be the same substitution, one scope wide.
- An override must fit every type its name is registered under; a value the registered type cannot hold is refused, so `GetByType` keeps answering with the registered type. An override that replaces an instance the container itself built hands that instance to the teardown — it is closed with the container, once — while an override evicted by a later override stays whoever installed it's to close.
- When the container itself closes, everything still standing in its maps joins the teardown, installed overrides included — the container's lifetime is the process's, so ownership converges on it and there is no opt-out. A scope is deliberately the opposite: it closes what it built and nothing else, and an outside override joins its teardown only through `ClosedWithScope()` (see the scope section below). The asymmetry follows the two lifetimes: a scope ends while its installer goes on running — the http kernel installs the request logger and then uses it to report the scope's own close failure — while a container ends with the process and has nobody to hand anything to. The one case the reasoning does not cover is a value shared with a SECOND container in the same process: a test suite that boots the application repeatedly over one reused client, or a host embedding melody that closes its own handles. This container closes it for all of them. Install a wrapper whose `Close` does nothing and keep the real handle where it belongs.
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

The direction stays one-way. A scoped service reads both levels; a container provider is refused anything only a scope carries, with the same "service is not registered" a wiring mistake gets anywhere else. A container service that needs request data takes it as a method argument.

A name — or a registered type — claimed at both lifetimes is refused where it is made, unless the SCOPED registration declares [`Replacing()`](../../container/register_option.go); the container-level registration paths do not read the option. Declared where nothing collides yet, the waiver stands: a container registration of the same name arriving later is admitted without declaring anything itself. The waiver covers the name and the registered type together — a registration that means only the name opts out with `WithoutTypeRegistration()`. It admits substitution, not decoration: a scoped provider that resolves the name it replaces re-enters itself and is reported as a circular dependency. Protected `service.` names cannot be registered scoped at all.

The declaration lives on [`ScopeManager`](../../container/contract/scope.go) — the interface the container satisfies by being the thing that makes scopes — rather than beside the container's own registrations. A scope does not exist until a request arrives, so what a scope will own has to be declared at boot by whatever will be creating them.

`Scope.RegisterScoped` adds a service to one live scope, layered over the plan the container was booted with. It is the rare case; a scoped service is normally declared at boot so every scope gets it.

### Scope teardown

`Close` closes what the scope built and nothing else: an override belongs to whoever installed it, and a singleton reached through the scope belongs to the root container. Services the scope built are closed in dependency order, dependents before their dependencies — one instance filed under its name and its type is collapsed into one node before the ordering runs, so the close order holds however the service was resolved. An override that has nowhere else to be closed can join the teardown with [`ClosedWithScope()`](../../container/override_option.go) through `OverrideProtectedInstanceWithOptions`; a created instance such an override evicts is still the scope's, and still closes with it.

### Contracts (`container/contract`)

- [`type Container`](../../container/contract/container.go)
- [`type Resolver`](../../container/contract/resolver.go)
- [`type Registrar`](../../container/contract/registrar.go)
- [`type OverrideService`](../../container/contract/override.go)
- [`type ScopeManager`](../../container/contract/scope.go)
- [`type Scope`](../../container/contract/scope.go)
- [`type ScopedRegistrar`](../../container/contract/scoped_registrar.go)
- [`type ScopeManager`](../../container/contract/scope.go)
- [`type OverrideServiceWithOptions`](../../container/contract/override.go)
- [`type OverrideOption`](../../container/contract/override.go)
- [`type OverrideOptions`](../../container/contract/override.go)
- [`type Provider[T]`](../../container/contract/provider.go)
- [`type RegisterOption`](../../container/contract/registrar.go)
- [`type RegisterOptions`](../../container/contract/registrar.go)

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
- Override options:
    - [`ClosedWithScope()`](../../container/override_option.go)
- Typed resolution:
    - [`FromResolver[T]`](../../container/resolver.go)
    - [`MustFromResolver[T]`](../../container/resolver.go)
    - [`FromResolverByType[T]`](../../container/resolver.go)
    - [`MustFromResolverByType[T]`](../../container/resolver.go)
      Scopes are created via `Container.NewScope()` (see [`ScopeManager`](../../container/contract/scope.go)).
