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

**A present-but-empty `MELODY_ENV` selects the `.env.dev` files and then fails the boot.** The default above applies to the *file selection* — an empty value picks `dev` and `.env.dev` / `.env.dev.local` are loaded — but the parameter itself keeps the empty value it was given, and the kernel configuration refuses it: `environment may not be empty`, naming `MELODY_ENV`, `kernel.environment`, and the files the emptiness already selected. The two readers deliberately disagree, because an empty value is an accident of a rendered deployment template rather than a request to run in development, and the alternative — booting on the development files because a variable came out blank — is the failure mode worth refusing. Either remove the key or give it a value.

### Empty string semantics

A present key with an empty string value is considered **present** (it is a valid value for string parameters). Typed conversions for non-string getters treat empty strings as invalid, by design.

### Environment variable keys

All recognised `.env` keys and their defaults live in [`config/environment.go`](../../config/environment.go) and [`config/configuration_default.go`](../../config/configuration_default.go):

| Env key                                   | Parameter name                            | Default                                      |
|-------------------------------------------|-------------------------------------------|----------------------------------------------|
| `MELODY_ENV`                              | `kernel.environment`                      | `"dev"`                                      |
| `MELODY_DEFAULT_MODE`                     | `kernel.default_mode`                     | `"http"`                                     |
| `MELODY_PROCESS_ROLE`                     | `kernel.process_role`                     | `"all"`                                      |
| `MELODY_HTTP_ADDRESS`                     | `kernel.http_address`                     | `":8080"`                                    |
| `MELODY_HTTP_MAX_REQUEST_BODY_BYTES`      | `kernel.http.max_request_body_bytes`      | `1048576`                                    |
| `MELODY_HTTP_SESSION_TTL`                 | `kernel.http.session_ttl`                 | `0s`                                         |
| `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` | `kernel.http.session_tombstone_retention` | `5m`                                         |
| `MELODY_HTTP_SHUTDOWN_TIMEOUT`            | `kernel.http.shutdown_timeout`            | `5s`                                         |
| `MELODY_CLI_NAME`                         | `kernel.cli_name`                         | `"melody"`                                   |
| `MELODY_CLI_DESCRIPTION`                  | `kernel.cli_description`                  | `""`                                         |
| `MELODY_LOG_PATH`                         | `kernel.log_path`                         | `%kernel.logs_dir%/%kernel.environment%.log` |
| `MELODY_LOG_LEVEL`                        | `kernel.log_level`                        | `"debug"`                                    |
| `MELODY_DEFAULT_LOCALE`                   | `kernel.default_locale`                   | `"en"`                                       |
| `MELODY_PUBLIC_DIR`                       | `kernel.public_dir`                       | `"public"`                                   |
| `MELODY_STATIC_INDEX_FILE`                | `kernel.static.index_file`                | `"index.html"`                               |
| `MELODY_STATIC_ENABLE_CACHE`              | `kernel.static.enable_cache`              | `true`                                       |
| `MELODY_STATIC_CACHE_MAX_AGE`             | `kernel.static.cache_max_age`             | `3600`                                       |
| `MELODY_STATIC_EXCLUDED_PATHS`            | `kernel.static.excluded_paths`            | `""`                                         |

Project layout defaults that are not env-overridable:

| Parameter name       | Default                            |
|----------------------|------------------------------------|
| `kernel.project_dir` | set from the application bootstrap |
| `kernel.logs_dir`    | `%kernel.project_dir%/var/log`     |
| `kernel.cache_dir`   | `%kernel.project_dir%/var/cache`   |

### Environment / mode / role constants

- [`EnvDevelopment` (`"dev"`) / `EnvProduction` (`"prod"`)](../../config/environment.go)
- [`ModeHttp` (`"http"`) / `ModeCli` (`"cli"`)](../../config/environment.go)
- [`RoleWeb` (`"web"`) / `RoleWorker` (`"worker"`) / `RoleAll` (`"all"`)](../../config/environment.go)

### Template resolution

A parameter value is a template resolved left to right, positionally. Each percent opens exactly one of:

- `%%` — one literal percent;
- `%env(KEY)%` — an environment key; an undefined key fails the boot, so a credential parameter refuses to start rather than degrade to empty;
- `%env(default:<fallback>:KEY)%` — an optional environment key: `%env(default::KEY)%` falls back to the empty string, `%env(default:some.parameter:KEY)%` to another parameter. A defined key always wins over the fallback;
- `%parameter%` — another parameter, whose fully resolved value is spliced in as data (a percent inside it survives literally, it is never rescanned);
- anything else — a lone percent is data.

A self-reference — direct or through any chain of parameters and environment keys — is a circular-reference error at resolution. A value that must hold a literal percent doubles it: `pa%%ss%%word` resolves to `pa%ss%word`.

The grammar fails closed on what would otherwise survive as literal text: an `%env(...)%` whose closing `)%` is malformed or missing (`%env(A))%`, a dsn whose final percent was forgotten), and a `%name` reference a percent opened and nothing closed (`%app-name%`), are boot errors naming the parameter — content is redacted where it may hold a credential. A percent in front of a character no name may start with stays data, so `50% overall` needs no escaping. In `.env` values, a braced `${...}` reference whose name is outside the key grammar (`${DB-PASS}`) is refused the same way, while the bare dollar stays data (`pa$sword`, `$1.50`); a literal dollar is written `\$`.

### Secret parameters

A parameter may be declared as holding a credential so that the commands rendering the configuration redact it — the value reaching the services is untouched, this governs display only. Declare one through the module registrar with [`RegisterSecretParameter`](../../application/contract/parameter_module.go), or mark one melody auto-registered from the `.env` artifacts with [`MarkParameterSecret`](../../application/contract/parameter_module.go). The marking travels with the value: a parameter whose template reads a secret (a dsn assembling a password, through `%parameter%` or `%env(KEY)%`) becomes secret itself — and a marking that arrives after the boot resolution travels retroactively to a fixpoint — the readers of each freshly marked name are scanned in turn — so a late `MarkSecret` covers the whole derivation chain, however many hops it takes from the credential. A secret parameter additionally withholds the value-quoting cause from its conversion errors, so a credential that fails an `Int()` it was never meant for does not reach the log through the failure. [`Parameter.IsSecret`](../../config/parameter.go) reports the marking; `debug:parameters` renders a marked parameter as `********`.

### Session ttl

How long a stored session stays valid. Read through [`Http().SessionTtl()`](../../config/http.go).

| Environment key            | Parameter name             | Default     |
|----------------------------|----------------------------|-------------|
| `MELODY_HTTP_SESSION_TTL`  | `kernel.http.session_ttl`  | `0s`        |

The value is a Go duration string, so it **must carry a unit** — `30m`, `24h`, `168h`. An environment value always arrives as a string and is parsed with `time.ParseDuration`, so a unit-less number such as `1800` is not "1800 seconds" but a parse failure that **fails the boot** with `invalid environment value`. A negative duration is rejected by validation (`session ttl must be zero or positive`), and so is a positive duration below [`MinimumSessionTtl`](../../config/http.go), one second: `session ttl is positive but shorter than one second, which stores no usable session; use zero for no expiry`. Below a second the value does not describe a short session but a broken one — the write succeeds, but the entry lapses before the response reaches the client and the client comes back with the cookie, so every request that follows loads nothing.

The default is [`DefaultSessionTtl`](../../config/http.go), **zero — no expiry**, which is what every deployment that predates this setting already had; upgrading does not begin ending sessions at a lifetime nobody chose. It is worth knowing what zero costs here specifically, because the cost does not come from anything the application wrote. Melody mints a session for every request that arrives without a session cookie, so the moment an application writes to a session on a public path — a csrf token, a flash message, a locale — every cookie-less request leaves an entry behind: a crawler, a health check, a hotlinked image. With no expiry nothing reclaims it:

- [`InMemoryStorage`](../../session/in_memory_storage.go), the default store, only ever deletes entries that carry an expiry — its background cleanup skips entries with none — so the map grows without bound for the process's whole lifetime;
- [`FileStorage`](../../session/file_storage.go) holds every session in one map and re-serialises **the whole map** on every write, so each save gets progressively more expensive as the file grows. Its flush does purge the entries that have lapsed, but with no expiry there are none to purge, so nothing ever leaves.

Which is why the application says so at boot rather than picking a lifetime on the deployment's behalf: an http application that is still on the framework-supplied [`InMemoryStorage`](../../session/in_memory_storage.go) **and** on a ttl of zero logs a warning naming both halves. Either half alone is silent — a shared storage is the operator's to prune, and a lifetime the deployment set reclaims on its own. Set `MELODY_HTTP_SESSION_TTL` to what the deployment actually wants; a value such as `24h` is long enough that no ordinary browsing session is cut short and short enough that abandoned entries leave.

The ttl is a **lifetime since the last write, not an idle timeout.** The expiry is stamped when the session is stored, and a session is only stored when it was modified — [`Manager.SaveSession`](../../session/manager.go) returns early otherwise, and the response path calls it under the same condition — so reading a session never refreshes it. With `MELODY_HTTP_SESSION_TTL=30m` a user who logs in and then browses read-only pages is logged out 30 minutes after the login, however active they were; this is not `gc_maxlifetime`. The choice is deliberate: refreshing on read would turn every request carrying a session into a storage write, which on [`FileStorage`](../../session/file_storage.go) re-serialises the whole map. An application that wants a true idle timeout can buy one explicitly with a `kernel.request` listener that writes to the session — a last-seen timestamp, say — accepting the one storage write per request that costs.

On the default this reads: nothing expires at all, so the question does not arise until a lifetime is set — at which point it is measured from the login, not from the last page.

### Session tombstone retention

How long a deleted session id is remembered, so that a slow in-flight request still holding a snapshot loaded before the delete cannot write the deleted session back. Read through [`Http().SessionTombstoneRetention()`](../../config/http.go).

| Environment key                           | Parameter name                            | Default |
|-------------------------------------------|-------------------------------------------|---------|
| `MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` | `kernel.http.session_tombstone_retention` | `5m`    |

The value is a Go duration string and must be positive: zero and negative values fail the boot, because a window that refuses nothing disarms the logout defence. The default is [`DefaultSessionTombstoneRetention`](../../config/http.go), five minutes.

### Http shutdown timeout

How long a stopping http server waits for the requests it has already admitted before cutting them. Read through [`Http().ShutdownTimeout()`](../../config/http.go).

| Environment key                | Parameter name                 | Default |
|--------------------------------|--------------------------------|---------|
| `MELODY_HTTP_SHUTDOWN_TIMEOUT` | `kernel.http.shutdown_timeout` | `5s`    |

The value is a Go duration string and must be positive: zero and negative values fail the boot. The default is [`DefaultHttpShutdownTimeout`](../../config/http.go), five seconds.

### Static file cache

Whether a static response carries cache headers, and for how long. Read through [`Http().StaticEnableCache()`](../../config/http.go) and [`Http().StaticCacheMaxAge()`](../../config/http.go).

| Environment key               | Parameter name                | Default |
|-------------------------------|-------------------------------|---------|
| `MELODY_STATIC_ENABLE_CACHE`  | `kernel.static.enable_cache`  | `true`  |
| `MELODY_STATIC_CACHE_MAX_AGE` | `kernel.static.cache_max_age` | `3600`  |

The max age is a plain count of seconds. A negative value is rejected by validation (`static cache max age must be zero or positive`), and `0` means what it reads like: with the cache enabled, `MELODY_STATIC_CACHE_MAX_AGE=0` ships `Cache-Control: public, max-age=0` — always revalidate, with the `ETag`/`Last-Modified` machinery intact. Only a negative value passed to [`NewFileServer`](../../http/static/file_server.go) directly reads as unset and takes the `3600` default; the configuration path never produces one.

The way to stop handing clients that hour is `MELODY_STATIC_ENABLE_CACHE=false`, which is not the same thing: the static response then carries no `Cache-Control`, no `ETag` and no `Last-Modified` at all, so there is no validator for a conditional request to present and the file server never answers `304 Not Modified`. With no headers to go on, a shared cache falls back to its own heuristics rather than to a revalidation you asked for. See [HTTP](HTTP.md) for what the entity tag is derived from and when it fails to change.

### Static excluded paths

Which path prefixes the built-in file server declines before it looks at the disk. Read through [`Http().StaticExcludedPaths()`](../../config/http.go).

| Environment key                | Parameter name                 | Default |
|--------------------------------|--------------------------------|---------|
| `MELODY_STATIC_EXCLUDED_PATHS` | `kernel.static.excluded_paths` | `""`    |

The value is a comma-separated list whose entries are trimmed of surrounding whitespace, so `/admin, /api/internal` names two prefixes while a blank or whitespace-only value names none. An entry must begin with `/` and may not be empty; validation rejects both at boot (`static excluded path must begin with a slash`, `static excluded path may not be empty`) rather than let a stray comma — which prefix-matches every path — silently take static serving out of service. See [HTTP](HTTP.md) for what a declined request goes on to do, and for why this is the lever an application pulls to serve part of the url space behind middleware of its own.

### Typed accessors

[`Parameter`](../../config/parameter.go) reads its value through `MustString`, `Bool`, `Int`, `Float` and `Duration`, converting from the native type or from the string an environment value always arrives as. `Bool` accepts a native bool, and for a string one grammar, owned by the shared parser: `true`/`false`, `1`/`0`, `yes`/`no`, `y`/`n`, `on`/`off`, case-insensitive, with surrounding whitespace trimmed — the empty string is not in it, so a key registered but set to nothing is a refusal, not a `false`. `Int`, `Float` and `Duration` read through the shared parser, so a whole number registered at runtime as an `int64` — or as an integral `float64` — converts through `Int` and `Float`; `Duration` refuses the bare integer, because it carries no unit, and wants a `time.Duration` or a string such as `"30s"`. The **string** grammars of `Int` and `Float` differ, though: `Int` reads strict base-10, while `Float` reads Go's `strconv.ParseFloat` in full — underscore spellings, hexadecimal floats (`0x1p10` is `1024`) and exponents all parse — so a value refused as an int can be silently accepted as a float under a spelling nobody meant to support; only `NaN` and the infinities are refused. `Int` additionally refuses by name a value outside the `int` range rather than truncating it on a 32-bit build. Each fallible accessor reports an unset or non-convertible value as an error naming the parameter and its environment key; the value-quoting cause is carried for an ordinary parameter, whose mistyped pool size deserves the diagnostic, and withheld for one marked secret.

A parameter melody auto-registered from a `MELODY_*` key and the reserved `kernel.*` name that aliases it are one parameter under two names. When the constructor's tolerant pass defers the non-reserved name over an undefined reference, the failure that remains names the alias — and it then carries the environment key beside it, the name the operator actually wrote, under `environmentKey` in the error context. A failure reported under the environment-key name itself carries no extra key, the two names being the same word there.

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
- The boot `Resolve()` passes through any parameter whose value is not a string — which is what a runtime parameter registered with a native value is; a runtime parameter registered with a string template is resolved like any other, eagerly at registration once the boot resolution has run.
- A template reference (e.g., `%kernel.project_dir%`, `%env(MELODY_ENV)%`) resolves only against a parameter whose value is a string; a parameter registered after construction is deferred by the constructor's tolerant pass and settled by the boot `Resolve()`, so the composition root may reference what it registers before boot.

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

### Environment variable keys (`config`)

- [`EnvKey`, `DefaultModeKey`, `ProcessRoleKey`, `HttpAddressKey`, `HttpMaxRequestBodyBytesKey`, `HttpSessionTtlKey`, `HttpSessionTombstoneRetentionKey`, `HttpShutdownTimeoutKey`, `CliNameKey`, `CliDescriptionKey`, `LogPathKey`, `LogLevelKey`, `DefaultLocaleKey`, `PublicDirKey`, `StaticIndexFileKey`, `StaticEnableCacheKey`, `StaticCacheMaxAgeKey`, `StaticExcludedPathsKey`](../../config/environment.go)

### Kernel parameter names (`config`)

- [`KernelDefaultMode`, `KernelProcessRole`, `KernelEnv`, `KernelHttpAddress`, `KernelHttpMaxRequestBodyBytes`, `KernelHttpSessionTtl`, `KernelHttpSessionTombstoneRetention`, `KernelHttpShutdownTimeout`, `KernelCliName`, `KernelCliDescription`, `KernelLogPath`, `KernelLogLevel`, `KernelDefaultLocale`, `KernelPublicDir`, `KernelStaticIndexFile`, `KernelStaticEnableCache`, `KernelStaticCacheMaxAge`, `KernelStaticExcludedPaths`, `KernelProjectDir`, `KernelLogsDir`, `KernelCacheDir`](../../config/environment.go)

### Environment / mode / role constants (`config`)

- [`EnvDevelopment`, `EnvProduction`](../../config/environment.go)
- [`ModeHttp`, `ModeCli`](../../config/environment.go)
- [`RoleWeb`, `RoleWorker`, `RoleAll`](../../config/environment.go)

### Contracts (`config/contract`)

- [`type Configuration`](../../config/contract/configuration.go)
- [`type KernelConfiguration`](../../config/contract/kernel.go)
- [`type HttpConfiguration`](../../config/contract/http.go)
- [`type CliConfiguration`](../../config/contract/cli.go)
- [`type EnvironmentSource`](../../config/contract/environment_source.go)
- [`type Parameter`](../../config/contract/parameter.go)
