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

## Usage

The example below demonstrates:

- registering a service by name,
- enabling type registration (strict),
- resolving a dependency inside a provider,
- creating a scope and overriding a service instance.

```go
package example

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

- Providers must be functions compatible with [`Provider[T]`](../../container/contract/provider.go). A provider is called at most once per container instance (per service), and the result is cached.
- Typed resolution by type is delegated to the underlying name registration when the type maps to a single service name, ensuring that resolving by name and by type returns the same instance (see [`container/resolver_context.go`](../../container/resolver_context.go)).
- Circular dependency detection is scoped to a single resolver context (see [`Resolver`](../../container/contract/resolver.go) and the resolver context stack logic in [`container/container_resolver.go`](../../container/container_resolver.go)).
- Closing is deterministic and dependency-aware: dependents are closed before dependencies (see [`container/container_close.go`](../../container/container_close.go)).
- `Close()` does not prevent subsequent resolutions; a container (or scope) must not be used after it is closed. Do not call `Close()` concurrently with service resolution (see [`container/container_close.go`](../../container/container_close.go) and the locking behavior in [`container/resolver_context.go`](../../container/resolver_context.go)).
- `OverrideInstance` rejects service names with the `service.` prefix (protected services). If you must override a protected service in userland tests, use `OverrideProtectedInstance` (see [`OverrideService`](../../container/contract/override.go) and its implementations in [`container/container.go`](../../container/container.go) and [`container/scope.go`](../../container/scope.go)).

## Userland API

### Contracts (`container/contract`)

- [`type Container`](../../container/contract/container.go)
- [`type Resolver`](../../container/contract/resolver.go)
- [`type Registrar`](../../container/contract/registrar.go)
- [`type OverrideService`](../../container/contract/override.go)
- [`type ScopeManager`](../../container/contract/scope.go)
- [`type Scope`](../../container/contract/scope.go)
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
- Registration options:
    - [`WithTypeRegistration(isStrict bool)`](../../container/register_option.go)
    - [`WithoutTypeRegistration()`](../../container/register_option.go)
- Typed resolution:
    - [`FromResolver[T]`](../../container/resolver.go)
    - [`MustFromResolver[T]`](../../container/resolver.go)
    - [`FromResolverByType[T]`](../../container/resolver.go)
    - [`MustFromResolverByType[T]`](../../container/resolver.go)
      Scopes are created via `Container.NewScope()` (see [`ScopeManager`](../../container/contract/scope.go)).
