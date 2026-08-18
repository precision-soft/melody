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

Services are always registered by name (`serviceName string`). Optionally, a registration may also register the service under a concrete type, enabling typed resolution by type.

Registration options (see [`RegisterOptions`](../../container/contract/registrar.go)):

- [`WithTypeRegistration(isStrict bool)`](../../container/register_option.go)  
  Enables type registration. When `isStrict` is true, registering a different service under the same type fails.
- [`WithoutTypeRegistration()`](../../container/register_option.go)  
  Explicitly disables type registration for that registration call.

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
- enabling type registration (strict),
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
		container.WithTypeRegistration(true),
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
- Providers must be functions compatible with [`Provider[T]`](../../container/contract/provider.go). A provider is called at most once per container instance (per service), and the result is cached.
- Typed resolution by type is delegated to the underlying name registration when the type maps to a single service name, ensuring that resolving by name and by type returns the same instance (see [`container/resolver_context.go`](../../container/resolver_context.go)).
- Circular dependency detection is scoped to a single resolver context (see [`Resolver`](../../container/contract/resolver.go) and the resolver context stack logic in [`container/container_resolver.go`](../../container/container_resolver.go)).
- Closing is deterministic and dependency-aware: dependents are closed before dependencies (see [`container/container_close.go`](../../container/container_close.go)).
- After `Close()`, already-created instances can still be looked up, but resolving a service that has not been created yet fails with a `container is closed` error instead of creating an instance that would never be closed; a creation that races `Close()` is closed best-effort and the resolution fails the same way (see [`container/container_resolver.go`](../../container/container_resolver.go)). A container (or scope) should still not be used after it is closed.
- `OverrideInstance` rejects service names with the `service.` prefix (protected services). If you must override a protected service in userland tests, use `OverrideProtectedInstance` (see [`OverrideService`](../../container/contract/override.go) and its implementations in [`container/container.go`](../../container/container.go) and [`container/scope.go`](../../container/scope.go)).

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

A name — or a registered type — claimed at both lifetimes is refused where it is made, in either order, unless the scoped registration declares [`Replacing()`](../../container/register_option.go). It admits substitution, not decoration: a scoped provider that resolves the name it replaces re-enters itself and is reported as a circular dependency.

The declaration lives on [`ScopeManager`](../../container/contract/scope.go) — the interface the container satisfies by being the thing that makes scopes — rather than beside the container's own registrations. A scope does not exist until a request arrives, so what a scope will own has to be declared at boot by whatever will be creating them.

`Scope.RegisterScoped` adds a service to one live scope, layered over the plan the container was booted with. It is the rare case; a scoped service is normally declared at boot so every scope gets it.

### Scope teardown

`Close` closes what the scope built and nothing else: an override belongs to whoever installed it, and a singleton reached through the scope belongs to the root container. Services the scope built are closed in dependency order, dependents before their dependencies. An override that has nowhere else to be closed can join the teardown with [`ClosedWithScope()`](../../container/override_option.go) through `OverrideProtectedInstanceWithOptions`.

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
- [`type TypeLister`](../../container/contract/type_lister.go)
- [`type ServiceReference`](../../container/contract/type_lister.go)

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
