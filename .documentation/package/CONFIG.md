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

### Environment variable keys

All recognised `.env` keys and their defaults live in [`config/environment.go`](../../config/environment.go) and [`config/configuration_default.go`](../../config/configuration_default.go):

| Env key                              | Parameter name                       | Default                                      |
|--------------------------------------|--------------------------------------|----------------------------------------------|
| `MELODY_ENV`                         | `kernel.environment`                 | `"dev"`                                      |
| `MELODY_DEFAULT_MODE`                | `kernel.default_mode`                | `"http"`                                     |
| `MELODY_HTTP_ADDRESS`                | `kernel.http_address`                | `":8080"`                                    |
| `MELODY_HTTP_MAX_REQUEST_BODY_BYTES` | `kernel.http.max_request_body_bytes` | `1048576`                                    |
| `MELODY_CLI_NAME`                    | `kernel.cli_name`                    | `"melody"`                                   |
| `MELODY_CLI_DESCRIPTION`             | `kernel.cli_description`             | `""`                                         |
| `MELODY_LOG_PATH`                    | `kernel.log_path`                    | `%kernel.logs_dir%/%kernel.environment%.log` |
| `MELODY_LOG_LEVEL`                   | `kernel.log_level`                   | `"debug"`                                    |
| `MELODY_DEFAULT_LOCALE`              | `kernel.default_locale`              | `"en"`                                       |
| `MELODY_PUBLIC_DIR`                  | `kernel.public_dir`                  | `"public"`                                   |
| `MELODY_STATIC_INDEX_FILE`           | `kernel.static.index_file`           | `"index.html"`                               |
| `MELODY_STATIC_ENABLE_CACHE`         | `kernel.static.enable_cache`         | `true`                                       |
| `MELODY_STATIC_CACHE_MAX_AGE`        | `kernel.static.cache_max_age`        | `3600`                                       |

Project layout defaults that are not env-overridable:

| Parameter name       | Default                            |
|----------------------|------------------------------------|
| `kernel.project_dir` | set from the application bootstrap |
| `kernel.logs_dir`    | `%kernel.project_dir%/var/log`     |
| `kernel.cache_dir`   | `%kernel.project_dir%/var/cache`   |

### Environment / mode constants

- [`EnvDevelopment` (`"dev"`) / `EnvProduction` (`"prod"`)](../../config/environment.go)
- [`ModeHttp` (`"http"`) / `ModeCli` (`"cli"`)](../../config/environment.go)

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

	"github.com/precision-soft/melody/config"
	configcontract "github.com/precision-soft/melody/config/contract"
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	"github.com/precision-soft/melody/exception"
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

### Environment variable keys (`config`)

- [`EnvKey`, `DefaultModeKey`, `HttpAddressKey`, `HttpMaxRequestBodyBytesKey`, `CliNameKey`, `CliDescriptionKey`, `LogPathKey`, `LogLevelKey`, `DefaultLocaleKey`, `PublicDirKey`, `StaticIndexFileKey`, `StaticEnableCacheKey`, `StaticCacheMaxAgeKey`](../../config/environment.go)

### Kernel parameter names (`config`)

- [`KernelDefaultMode`, `KernelEnv`, `KernelHttpAddress`, `KernelHttpMaxRequestBodyBytes`, `KernelCliName`, `KernelCliDescription`, `KernelLogPath`, `KernelLogLevel`, `KernelDefaultLocale`, `KernelPublicDir`, `KernelStaticIndexFile`, `KernelStaticEnableCache`, `KernelStaticCacheMaxAge`, `KernelProjectDir`, `KernelLogsDir`, `KernelCacheDir`](../../config/environment.go)

### Environment / mode constants (`config`)

- [`EnvDevelopment`, `EnvProduction`](../../config/environment.go)
- [`ModeHttp`, `ModeCli`](../../config/environment.go)

### Contracts (`config/contract`)

- [`type Configuration`](../../config/contract/configuration.go)
- [`type KernelConfiguration`](../../config/contract/kernel.go)
- [`type HttpConfiguration`](../../config/contract/http.go)
- [`type CliConfiguration`](../../config/contract/cli.go)
- [`type EnvironmentSource`](../../config/contract/environment_source.go)
- [`type Parameter`](../../config/contract/parameter.go)
