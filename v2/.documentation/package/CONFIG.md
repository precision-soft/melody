# CONFIG

The [`config`](../../config) package provides `.env` loading and strongly typed configuration for Melody. It builds a [`configcontract.Configuration`](../../config/contract/configuration.go) from `.env` files and exposes typed sub-configurations (kernel, HTTP, CLI) used by the application, kernel, runtime, and HTTP stack.

## Scope

Configuration in Melody is file-driven (via `.env` artifacts). The `config` package does not overlay OS environment variables by design.

A `Configuration` instance is created early (typically at application bootstrap) and is then passed into the kernel and other framework components.

## Subpackages

- [`config/contract`](../../config/contract)
  Public contracts for configuration and sub-configurations (`Configuration`, `KernelConfiguration`, `HttpConfiguration`, `CliConfiguration`, `EnvironmentSource`, `Parameter`).

## Responsibilities

- Load `.env` values from a filesystem via [`EnvironmentSource`](../../config/environment_source.go)
- Construct a typed configuration via [`NewConfiguration`](../../config/configuration.go)
- Resolve templated parameters via [`(*Configuration).Resolve()`](../../config/configuration_resolve.go)
- Provide container access helpers:
    - [`ServiceConfig`](../../config/service_resolver.go)
    - [`ConfigMustFromContainer`](../../config/service_resolver.go)

## Configuration

### `.env` loading order

`EnvironmentSource` reads values from the base directory in this order:

1. `.env`
2. `.env.local`
3. `.env.<env>`
4. `.env.<env>.local`

The environment name is resolved from `MELODY_ENV` inside the loaded `.env` values (defaults to `"dev"` when missing or empty). See [`EnvKey`](../../config/environment.go) and [`EnvironmentSource`](../../config/environment_source.go).

### Empty string semantics

A present key with an empty string value is considered **present** (it is a valid value for string parameters). Typed conversions for non-string getters treat empty strings as invalid, by design.

### Template resolution

A parameter value is a template resolved left to right, positionally. Each percent opens exactly one of:

- `%%` — one literal percent;
- `%env(KEY)%` — an environment key; an undefined key fails the boot, so a credential parameter refuses to start rather than degrade to empty;
- `%env(default:<fallback>:KEY)%` — an optional environment key: `%env(default::KEY)%` falls back to the empty string, `%env(default:some.parameter:KEY)%` to another parameter. A defined key always wins over the fallback;
- `%parameter%` — another parameter, whose fully resolved value is spliced in as data (a percent inside it survives literally, it is never rescanned);
- anything else — a lone percent is data.

A self-reference — direct or through any chain of parameters and environment keys — is a circular-reference error at resolution. A value that must hold a literal percent doubles it: `pa%%ss%%word` resolves to `pa%ss%word`.

### Secret parameters

A parameter may be declared as holding a credential so that the commands rendering the configuration redact it — the value reaching the services is untouched, this governs display only. Declare one through the module registrar with [`RegisterSecretParameter`](../../application/contract/parameter_module.go), or mark one melody auto-registered from the `.env` artifacts with [`MarkParameterSecret`](../../application/contract/parameter_module.go). The marking travels with the value: a parameter whose template reads a secret (a dsn assembling a password, through `%parameter%` or `%env(KEY)%`) becomes secret itself. [`Parameter.IsSecret`](../../config/parameter.go) reports the marking; `debug:parameters` renders a marked parameter as `********`.

### Typed accessors

[`Parameter`](../../config/parameter.go) reads its value through `MustString`, `Bool`, `Int`, `Float` and `Duration`, converting from the native type or from the string an environment value always arrives as. Each fallible accessor reports an unset or non-convertible value as an error identified by environment key alone, keeping an inline credential out of the exception context.

## Container integration

The package defines the service name:

- [`ServiceConfig`](../../config/service_resolver.go) (`"service.config"`)

If you want other services to resolve a configuration from the container, register `ServiceConfig` as a `configcontract.Configuration` provider and use `ConfigMustFromContainer`.

## Usage

The example below demonstrates loading an environment from `.env` files, creating a configuration, resolving it, and registering it into the container.

```go
package main

import (
	"context"
	"os"

	"github.com/precision-soft/melody/v2/config"
	configcontract "github.com/precision-soft/melody/v2/config/contract"
	"github.com/precision-soft/melody/v2/container"
	containercontract "github.com/precision-soft/melody/v2/container/contract"
	"github.com/precision-soft/melody/v2/exception"
)

func registerConfiguration(
	serviceContainer containercontract.Container,
	projectDirectory string,
) configcontract.Configuration {
	projectFileSystem := os.DirFS(
		projectDirectory,
	)

	environmentSource := config.NewEnvironmentSource(
		projectFileSystem,
		".",
	)

	environment, environmentErr := config.NewEnvironment(
		environmentSource,
	)
	if nil != environmentErr {
		exception.Panic(
			exception.NewError("failed to create environment", nil, environmentErr),
		)
	}

	configuration, configurationErr := config.NewConfiguration(
		environment,
		projectDirectory,
	)
	if nil != configurationErr {
		exception.Panic(
			exception.NewError("failed to create configuration", nil, configurationErr),
		)
	}

	configuration.RegisterRuntime(
		"runtime.context",
		context.Background(),
	)

	resolveErr := configuration.Resolve()
	if nil != resolveErr {
		exception.Panic(
			exception.NewError("failed to resolve configuration", nil, resolveErr),
		)
	}

	serviceContainer.MustRegister(
		config.ServiceConfig,
		func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
			return configuration, nil
		},
	)

	return configuration
}

func example() configcontract.Configuration {
	serviceContainer := container.NewContainer()

	return registerConfiguration(
		serviceContainer,
		"/path/to/project",
	)
}
```

## Footguns & caveats

- `ConfigMustFromContainer` is a fail-fast helper and will panic if `ServiceConfig` is missing or has an invalid type.
- `Application.Boot()` calls `Resolve()` after all runtime parameters are registered via `Application.RegisterParameter`.
- Runtime parameters are preserved during `Resolve()` because they store non-string values.
- Templates (e.g., `%kernel.project_dir%`, `%env(MELODY_ENV)%`) resolve only string environment-backed parameters; do not reference runtime parameters inside templates.

## Userland API

### Core types (`config`)

- [`type Configuration`](../../config/configuration.go)
    - [`NewConfiguration(*Environment, string) (*Configuration, error)`](../../config/configuration.go)
    - `Get(name string) configcontract.Parameter`
    - `MustGet(name string) configcontract.Parameter`
    - `RegisterRuntime(name string, value any)`
    - `Resolve() error`
    - `Names() []string`
    - `Kernel() configcontract.KernelConfiguration`
    - `Http() configcontract.HttpConfiguration`
    - `Cli() configcontract.CliConfiguration`
- [`type Environment`](../../config/environment.go)
    - [`NewEnvironment(configcontract.EnvironmentSource) (*Environment, error)`](../../config/environment.go)
- [`type EnvironmentSource`](../../config/environment_source.go)
    - [`NewEnvironmentSource(fs.FS, string) *EnvironmentSource`](../../config/environment_source.go)

### Container helpers (`config`)

- [`const ServiceConfig`](../../config/service_resolver.go)
- [`ConfigMustFromContainer(containercontract.Container) configcontract.Configuration`](../../config/service_resolver.go)

### Contracts (`config/contract`)

- [`type Configuration`](../../config/contract/configuration.go)
- [`type KernelConfiguration`](../../config/contract/kernel.go)
- [`type HttpConfiguration`](../../config/contract/http.go)
- [`type CliConfiguration`](../../config/contract/cli.go)
- [`type EnvironmentSource`](../../config/contract/environment_source.go)
- [`type Parameter`](../../config/contract/parameter.go)
