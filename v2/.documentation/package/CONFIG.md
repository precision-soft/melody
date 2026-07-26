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

### Session ttl

How long a stored session stays valid. Read through [`Http().SessionTtl()`](../../config/http.go).

| Environment key            | Parameter name             | Default     |
|----------------------------|----------------------------|-------------|
| `MELODY_HTTP_SESSION_TTL`  | `kernel.http.session_ttl`  | `24h0m0s`   |

The value is a Go duration string, so it **must carry a unit** — `30m`, `24h`, `168h`. An environment value always arrives as a string and is parsed with `time.ParseDuration`, so a unit-less number such as `1800` is not "1800 seconds" but a parse failure that **fails the boot** with `invalid environment value`. A negative duration is rejected by validation (`session ttl must be zero or positive`), and so is a positive duration below [`MinimumSessionTtl`](../../config/http.go), one second: `session ttl is positive but shorter than one second, which stores no usable session; use zero for no expiry`. Below a second the value does not describe a short session but a broken one — the store purges every lapsed entry on the very write that stores the new one, so the save reports success and persists nothing, and no sub-second lifetime survives the response reaching the client and the client coming back.

The default is [`DefaultSessionTtl`](../../config/http.go), **24 hours**, and it is bounded because of what an unbounded default costs here specifically. Melody mints a session for every request that arrives without a session cookie, so the moment an application writes to a session on a public path — a csrf token, a flash message, a locale — every cookie-less request leaves an entry behind: a crawler, a health check, a hotlinked image. The growth then comes from the framework rather than from anything the application chose, and with no expiry nothing reclaims it:

- [`InMemoryStorage`](../../session/in_memory_storage.go), the default store, only ever deletes entries that carry an expiry — its background cleanup skips entries with none — so the map grows without bound for the process's whole lifetime;
- [`FileStorage`](../../session/file_storage.go) holds every session in one map and re-serialises **the whole map** on every write, so each save gets progressively more expensive as the file grows and nothing is ever purged from it.

A day is long enough that no ordinary browsing session is cut short by it and short enough that abandoned entries leave. A deployment that wants a different lifetime still names one; `MELODY_HTTP_SESSION_TTL=0` keeps its meaning of **no expiry** and remains available as an explicit choice, for a development setup or for a deployment that reclaims sessions some other way — with the growth above as its price.

The ttl is a **lifetime since the last write, not an idle timeout.** The expiry is stamped when the session is stored, and a session is only stored when it was modified — [`Manager.SaveSession`](../../session/manager.go) returns early otherwise, and the response path calls it under the same condition — so reading a session never refreshes it. With `MELODY_HTTP_SESSION_TTL=30m` a user who logs in and then browses read-only pages is logged out 30 minutes after the login, however active they were; this is not `gc_maxlifetime`. The choice is deliberate: refreshing on read would turn every request carrying a session into a storage write, which on [`FileStorage`](../../session/file_storage.go) re-serialises the whole map. An application that wants a true idle timeout can buy one explicitly with a `kernel.request` listener that writes to the session — a last-seen timestamp, say — accepting the one storage write per request that costs.

On the default this reads: a visitor who logs in and then only navigates is logged out **24 hours after the login**, not 24 hours after their last page. Reading their session on every request refreshes nothing.

### Static file cache

Whether a static response carries cache headers, and for how long. Read through [`Http().StaticEnableCache()`](../../config/http.go) and [`Http().StaticCacheMaxAge()`](../../config/http.go).

| Environment key               | Parameter name                | Default |
|-------------------------------|-------------------------------|---------|
| `MELODY_STATIC_ENABLE_CACHE`  | `kernel.static.enable_cache`  | `true`  |
| `MELODY_STATIC_CACHE_MAX_AGE` | `kernel.static.cache_max_age` | `3600`  |

The max age is a plain count of seconds. A negative value is rejected by validation (`static cache max age must be zero or positive`), and `0` is accepted — but it does **not** mean "always revalidate". With the cache enabled, [`NewFileServer`](../../http/static/file_server.go) coerces a max age of zero to `3600`, so `MELODY_STATIC_CACHE_MAX_AGE=0` ships `Cache-Control: public, max-age=3600` — an hour of client-side freshness, the opposite of what the value reads like. No configuration emits `max-age=0`.

The way to stop handing clients that hour is `MELODY_STATIC_ENABLE_CACHE=false`, which is not the same thing: the static response then carries no `Cache-Control`, no `ETag` and no `Last-Modified` at all, so there is no validator for a conditional request to present and the file server never answers `304 Not Modified`. With no headers to go on, a shared cache falls back to its own heuristics rather than to a revalidation you asked for. See [HTTP](HTTP.md) for what the entity tag is derived from and when it fails to change.

### Static excluded paths

Which path prefixes the built-in file server declines before it looks at the disk. Read through [`Http().StaticExcludedPaths()`](../../config/http.go).

| Environment key                | Parameter name                 | Default |
|--------------------------------|--------------------------------|---------|
| `MELODY_STATIC_EXCLUDED_PATHS` | `kernel.static.excluded_paths` | `""`    |

The value is a comma-separated list whose entries are trimmed of surrounding whitespace, so `/admin, /api/internal` names two prefixes while a blank or whitespace-only value names none. An entry must begin with `/` and may not be empty; validation rejects both at boot (`static excluded path must begin with a slash`, `static excluded path may not be empty`) rather than let a stray comma — which prefix-matches every path — silently take static serving out of service. See [HTTP](HTTP.md) for what a declined request goes on to do, and for why this is the lever an application pulls to serve part of the url space behind middleware of its own.

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
    - `RegisterRuntimeSecret(name string, value any)`
    - `MarkSecret(name string) bool`
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
