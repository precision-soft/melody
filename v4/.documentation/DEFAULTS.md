# Defaults

Every protective default of the fourth major, every limit and every door that turns one off or widens it, one row each, for the framework and for every integration of the line. A row is a promise the code keeps: `.dev/validate/defaults.sh` reads every module with its types and refuses a default without a row, a row naming nothing, and a door no row names.

## The row

| column | holds |
|---|---|
| key | the row's identity and its anchor in the code: `<package path>.<Symbol>`, `<package path>.<Type>.<Field>`, the name a `bound.Rule` is resolved under prefixed by its package, a boot-warning code, or `<package path>.<Function>#<n>` for the n-th site of a function |
| kind | `limit` (a `bound.Rule`), `protection` (a default that guards against a fault or an attack), `switch` (an operational default: a feature or a behaviour whose door loosens nothing, such as keep-alive, a proxy, a cache header), `refusal` (a refusal without a door, a row of its own), `redaction`, `constant` (a fixed figure), `mode` (a file or directory permission), `warning` (a boot-warning code) |
| protects against | what goes wrong without the default |
| default | what applies when nothing is configured |
| door | each anchor with its class: `off:` turns the default off, `value:` sets the value (a large value is the door of a limit), `use:` is a sibling for one use, `env:` is a key; or `none` |
| reason | why the door is what it is; required when the door is `none` and on a limit that refuses unlimited, where a class reference is admitted unless the rule clamps at a maximum |
| documented | the page that names the door |

An `env:` door sets an operational value, and turns a `switch` on or off. It never turns a protection off: on a `protection` row it is admitted only where the reason says the key hardens the default, as `MELODY_HTTP_SESSION_COOKIE_SECURE` does; a protection is turned off in Go.

## The classes

- **class T** — a limit that is tunable and never unlimited: the large value is the door, because an unlimited value is the failure the limit exists for (a wait that never returns, a table that only grows).
- **class R** — a redaction: the journal, the debug output and the audit trail never carry a secret in clear; the door, where there is one, is a flag of a development command or a field of the registry, and the exemption of the journal is written once in [LOGGING.md](package/LOGGING.md#redaction-and-escaping).
- **class K** — a constant that bounds a diagnostic or a cycle guard: a figure no deployment tunes, written here with its reason. The cuts of a diagnostic (an error's cause chain, a stored error's length, a sample of skipped names) and the guards of a recursion (a copy, an impersonation chain, an Unwrap chain, a module nesting) that have no field and refuse nothing a caller wrote are out of the inventory; a figure that refuses an input or shapes a behaviour has its row.

## What the band reads, and what it cannot

Enumerated from the code: every `bound.Rule` and `bound.BackoffRule` literal, every boot-warning code the framework declares and every one a module declares through `DeclareBootWarning`, every `fs.FileMode` constant and every constant mode handed to `os.MkdirAll`, `os.Mkdir`, `os.OpenFile`, `os.WriteFile`, `os.Chmod` or their `os.Root` twins, every env key the framework or a module declares, and every `bound` limit field of a configuration struct. Every bool, every field typed by an enumeration of the line's own and every `*Disabled` field of a configuration struct is read as a door: a row names it, or a line of `.dev/validate/defaults.not-doors` says, with its reason, why it turns nothing off. A row of another kind is written by hand; the band keeps it alive by resolving its anchors, and its completeness is owed to the review before a release: no mechanism can enumerate a behaviour that has no rule, no field and no mode, such as the `HttpOnly` of the session cookie.

A failure the tree still carries is held by a line of `.dev/validate/defaults.baseline`, with its reason; a line that holds nothing fails the band.

## `v4/application`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/application.Application.bootCli` | protection | debug commands outside development | development only | `off:v4/application.ApplicationConfig.DebugCommandsEnabled` | — | [DEBUG.md](package/DEBUG.md) |
| `v4/application.Application.refuseUnappliedSecretMarks` | refusal | a misspelled secret mark leaving the credential in clear | the boot refused | `use:v4/application.Application.MarkParameterSecretOptional` | the optional mark is the door for a parameter some environments do not define; a mandatory mark that matches nothing is a typo | [CONFIG.md](package/CONFIG.md) |
| `v4/application.Application.refuseUndeclaredEnvironmentKeys` | refusal | a misspelt MELODY_ key the operator believes is set | the boot refused | `use:v4/application/contract.EnvironmentKeyModule` | the namespace is the framework's and the modules': a key nothing declares is read by nothing, so the door is declaring it in the module that reads it, never a switch | [CONFIG.md](package/CONFIG.md) |
| `v4/application.maxModuleProviderDepth` | constant | a module provider that provides itself, nesting without end | 100 levels, a deeper nesting refused | none | class K: a cycle guard; no application nests its modules a hundred deep | [APPLICATION.md](package/APPLICATION.md) |
| `v4/application.builtAtBootCauseChainDepth` | constant | a boot warning without end | 8 causes | none | class K: a diagnostic cut, the default depth of the exit line; a boot warning is journaled before any command's RootConfig is read, so it cannot take ErrorCauseChainDepth | [APPLICATION.md](package/APPLICATION.md) |
| `v4/application.signalContextForceExitDebounce` | constant | one shutdown forwarded twice, a supervisor and a terminal each sending a signal, read as an escalation to the forced exit | a second signal within 500ms absorbed | none | a duplicate delivery arrives within milliseconds and an operator's second press does not; the window is no deployment's tuning | [APPLICATION.md](package/APPLICATION.md) |

## `v4/cache`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cache.renderedCacheKey` | constant | a cache key journaled whole | 128 | none | class K: a diagnostic cut; the refusal names the key, not its payload, and carries the whole length beside it | [CACHE.md](package/CACHE.md) |
| `v4/cache.InMemoryBackendConfig.CleanupInterval` | limit | a loop that spins or never runs | 1m min 1ms | `value:v4/cache.InMemoryBackendConfig.CleanupInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [CACHE.md](package/CACHE.md) |
| `v4/cache.InMemoryBackendConfig.MaxKeyLength` | limit | unbounded memory or work | 1024 min 1 | `value:v4/cache.InMemoryBackendConfig.MaxKeyLength` | class T: a size; unlimited is a key of any length kept in the table, as the rueidis backend refuses it | [CACHE.md](package/CACHE.md) |
| `v4/cache.InMemoryBackendConfig.MaxItems` | limit | unbounded memory or work | 10_000 min 1 unlimited admitted | `value:v4/cache.InMemoryBackendConfig.MaxItems` | — | [CACHE.md](package/CACHE.md) |
| `v4/cache.RememberConfig.WaitTimeout` | limit | a call or a wait that never returns | 30s min 0s unlimited admitted | `value:v4/cache.RememberConfig.WaitTimeout` | — | [CACHE.md](package/CACHE.md) |
| `v4/cache.RememberConfig.StampedeProtectionDisabled` | protection | one cache miss computed by every concurrent caller at once (a stampede on the backing store) | one flight per key, the callers after the first join it | `off:v4/cache.RememberConfig.StampedeProtectionDisabled` | — | [CACHE.md](package/CACHE.md) |
| `v4/cache.RememberConfig.Detached` | protection | a callback that never returns pinning its key for the life of the process | the last waiter leaving cancels the flight, the next caller leads a fresh one | `off:v4/cache.RememberConfig.Detached` | — | [CACHE.md](package/CACHE.md) |

## `v4/cache/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cache/contract.ttl` | limit | state held, or a decision kept, forever | no default: an absent ttl refused, min 1s unlimited admitted | `value:v4/cache/contract.Cache.Set`, `value:v4/cache/contract.Cache.SetMultiple`, `value:v4/cache.RememberConfig.Ttl`, `value:v4/cache/contract.CounterOptions.Ttl` | — | [CACHE.md](package/CACHE.md) |

## `v4/cli`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cli.RootConfig.ErrorCauseChainDepth` | limit | an exit line without end | 8 min 0 | `value:v4/cli.RootConfig.ErrorCauseChainDepth`, `value:v4/application.ApplicationConfig.CliErrorCauseChainDepth` | class T: a count; unlimited is an error line an endless Unwrap of the application's own never ends | [CLI.md](package/CLI.md) |

## `v4/cli/output`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cli/output.Option.Limit` | limit | unbounded memory or work | unlimited min 0 | `value:v4/cli/output.Option.Limit`, `value:v4/cli/output.ListPayloadConfig.Limit` | — | [CLI.md](package/CLI.md) |
| `v4/cli/output.Option.Offset` | limit | a negative or runaway offset | 0 min 0 | `value:v4/cli/output.Option.Offset`, `value:v4/cli/output.ListPayloadConfig.Offset` | class T: a position, not a size: an offset has no unlimited | [CLI.md](package/CLI.md) |
| `v4/cli/output.Option.TableMaxWidth` | limit | unbounded memory or work | 120 min 1 unlimited admitted | `value:v4/cli/output.Option.TableMaxWidth` | — | [CLI.md](package/CLI.md) |
| `v4/cli/output.TablePrinterConfig.MaxWidth` | limit | unbounded memory or work | 120 min 1 unlimited admitted | `value:v4/cli/output.TablePrinterConfig.MaxWidth` | — | [CLI.md](package/CLI.md) |

## `v4/config`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/config.ApplicationConfig.HttpKernel.UnavailableRetryAfter` | limit | a client retrying at once, or never | 5s min 1s max 1h | `value:v4/application.ApplicationConfig.HttpKernel.UnavailableRetryAfter`, `env:MELODY_HTTP_UNAVAILABLE_RETRY_AFTER`, `value:v4/http.ExceptionRenderConfig.UnavailableRetryAfter`, `value:v4/http.KernelExceptionEventConfig.UnavailableRetryAfter`, `value:v4/security.CompiledFirewallConfig.UnavailableRetryAfter`, `value:v4/security/config.GlobalConfig.UnavailableRetryAfter`, `value:v4/security/config.FirewallOverride.UnavailableRetryAfter` | 1s to 1h in whole seconds: Retry-After is whole seconds, and a hint past an hour tells a client to stay away from an outage the instance recovers from in seconds; unlimited is a client told never to retry | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.IdleTimeout` | limit | a call or a wait that never returns | 1m min 1ms | `value:v4/application.ApplicationConfig.HttpServer.IdleTimeout`, `env:MELODY_HTTP_IDLE_TIMEOUT` | class T: a timeout; unlimited holds every idle keep-alive connection forever | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.MaxHeaderBytes` | limit | unbounded memory or work | 1 MiB min 4 << 10 | `value:v4/application.ApplicationConfig.HttpServer.MaxHeaderBytes`, `env:MELODY_HTTP_MAX_HEADER_BYTES` | class T: a size; unlimited is memory or work that only grows | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.ReadHeaderTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:v4/application.ApplicationConfig.HttpServer.ReadHeaderTimeout`, `env:MELODY_HTTP_READ_HEADER_TIMEOUT` | class T: a timeout; unlimited is the wait that never returns | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.ReadTimeout` | limit | a call or a wait that never returns | 15s min 1ms | `value:v4/application.ApplicationConfig.HttpServer.ReadTimeout`, `env:MELODY_HTTP_READ_TIMEOUT` | class T: a timeout; unlimited is the wait that never returns | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.ShutdownTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:v4/application.ApplicationConfig.HttpServer.ShutdownTimeout`, `env:MELODY_HTTP_SHUTDOWN_TIMEOUT` | class T: a timeout; an unlimited drain is a stop that never ends, one request that never finishes holding the process past every supervisor's grace | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.WriteTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:v4/application.ApplicationConfig.HttpServer.WriteTimeout`, `env:MELODY_HTTP_WRITE_TIMEOUT` | class T: a timeout; unlimited is the wait that never returns | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.TeardownTimeout` | limit | a call or a wait that never returns | 10s min 1ms unlimited admitted | `value:v4/application.ApplicationConfig.TeardownTimeout`, `env:MELODY_TEARDOWN_TIMEOUT` | — | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.HttpConfiguration.MaxRequestBodyBytes` | limit | unbounded memory or work | 1 MiB min 1 | `value:v4/application.ApplicationConfig.ParameterOverrides`, `use:v4/http.RequestConfig.MaxBodyBytes`, `use:v4/http/contract.RouteProtectionConfig.MaxBodyBytes`, `env:MELODY_HTTP_MAX_REQUEST_BODY_BYTES` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/config.HttpConfiguration.SessionMaxSessions` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/application.ApplicationConfig.ParameterOverrides`, `use:v4/session.InMemoryStorageConfig.MaxSessions`, `use:v4/session.FileStorageConfig.MaxSessions`, `env:MELODY_HTTP_SESSION_MAX_SESSIONS` | — | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.SessionStorageTimeout` | limit | a call or a wait that never returns | 5s min 1ms unlimited admitted | `value:v4/application.ApplicationConfig.ParameterOverrides`, `use:v4/session.ManagerConfig.StorageTimeout`, `env:MELODY_HTTP_SESSION_STORAGE_TIMEOUT` | — | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.SessionTombstoneRetention` | limit | state held, or a decision kept, forever | 5m min 1s | `value:v4/application.ApplicationConfig.ParameterOverrides`, `use:v4/session.ManagerConfig.TombstoneRetention`, `env:MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` | class T: a lifetime; unlimited keeps the state forever | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.SessionTtl` | limit | state held, or a decision kept, forever | 24h min 1s unlimited admitted | `value:v4/application.ApplicationConfig.ParameterOverrides`, `use:v4/session.ManagerConfig.Ttl`, `env:MELODY_HTTP_SESSION_TTL` | — | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.StaticCacheMaxAge` | limit | a response cached for good | 3600 (seconds) min 0 | `value:v4/application.ApplicationConfig.ParameterOverrides`, `use:v4/http/static.Config.CacheMaxAge`, `env:MELODY_STATIC_CACHE_MAX_AGE` | class T: a cache lifetime; unlimited pins a response in every cache for good | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpSessionCookieSecureAlways` | protection | a session cookie sent in clear | from the scheme | `use:v4/http/contract.SessionCookiePolicy.Secure`, `env:MELODY_HTTP_SESSION_COOKIE_SECURE` | the key only hardens it: always is the one value it admits, and the Go field lowers the policy | [HTTP.md](package/HTTP.md) |
| `v4/config.DefaultCacheDirectoryMode` | mode | a cache directory readable by every local user | 0750 | `env:MELODY_CACHE_DIRECTORY_MODE` | — | [CONFIG.md](package/CONFIG.md) |
| `v4/config.kernelConfiguration.validateEnvironment` | refusal | an environment name outside the closed list | refused | none | the framework has two behaviours and branches on the name: a third name would fall to one of them by the order of a comparison; a staging deployment runs as prod and names itself in its own keys | [CONFIG.md](package/CONFIG.md) |
| `v4/config.namespaceMaximumLength` | constant | a namespace of any length prefixed to every key the application writes | 32 bytes, a longer namespace refused | none | class K: an identifier, not a payload; every store keys on it | [CONFIG.md](package/CONFIG.md) |

## `v4/container`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/container.ContainerConfig.CloseFailureStackBytes` | limit | a close failure journaling a whole stack | 2048 min 4 unlimited admitted | `value:v4/container.ContainerConfig.CloseFailureStackBytes`, `value:v4/application.ApplicationConfig.ContainerCloseFailureStackBytes` | — | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container.ContainerConfig.TeardownWalk.Depth` | limit | a teardown walk without end | 12 min 1 | `value:v4/container.TeardownWalkConfig.Depth`, `value:v4/application.ApplicationConfig.ContainerTeardownWalk` | class T: a stack backstop; the walk descends a service's value by reflection at its creation, so unlimited is a creation that costs what the deepest value costs | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container.ContainerConfig.TeardownWalk.Elements` | limit | a teardown walk without end | 256 min 1 | `value:v4/container.TeardownWalkConfig.Elements`, `value:v4/application.ApplicationConfig.ContainerTeardownWalk` | class T: a size; unlimited enters every element of the largest array a service holds | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container.ContainerConfig.TeardownWalk.Nodes` | limit | a teardown walk without end | 65_536 min 1 | `value:v4/container.TeardownWalkConfig.Nodes`, `value:v4/application.ApplicationConfig.ContainerTeardownWalk` | class T: a size; unlimited is a walk queue that grows with the value; a walk the budget ends is named in the warning the application writes at close (`v4/application.TeardownWalkBudgetEndedMessage`) | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container.ContainerConfig.CreationWaiters` | limit | a call or a wait that never returns | 1024 min 1 unlimited admitted | `value:v4/container.ContainerConfig.CreationWaiters`, `value:v4/application.ApplicationConfig.ContainerCreationWaiters`, `env:MELODY_CONTAINER_CREATION_WAITERS` | — | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container.ContainerConfig.ScopeTeardownTimeout` | limit | a call or a wait that never returns | 3s min 1ms unlimited admitted | `value:v4/container.ContainerConfig.ScopeTeardownTimeout`, `value:v4/application.ApplicationConfig.ScopeTeardownTimeout`, `env:MELODY_SCOPE_TEARDOWN_TIMEOUT` | — | [CONTAINER.md](package/CONTAINER.md) |

## `v4/container/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/container/contract.RegisterOptions.TeardownReflectionDisabled` | switch | a service whose own goroutines rewrite its pointer fields read by the teardown walk while they write | the teardown walks the service's memory for its dependencies | `off:v4/container/contract.RegisterOptions.TeardownReflectionDisabled` | — | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container/contract.RegisterOptions.TypeRegistrationDisabled` | switch | a service a by-type resolution cannot find | the service is indexed by its type and by its name | `off:v4/container/contract.RegisterOptions.TypeRegistrationDisabled` | — | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container/contract.RegisterOptions.TypeRegistrationRelaxed` | protection | a second service under one type silently answering a by-type resolution | a second registration under a type refused | `off:v4/container/contract.RegisterOptions.TypeRegistrationRelaxed` | — | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container/contract.RegisterOptions.ReplacesContainerService` | protection | a scoped registration shadowing a container service unseen | a scoped registration whose name or type the container claims refused | `off:v4/container/contract.RegisterOptions.ReplacesContainerService` | — | [CONTAINER.md](package/CONTAINER.md) |

## `v4/debug`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/debug.errorCauseChainDepth` | constant | a debug error without end | 9 | none | class K: a cycle guard of a development command; the report names the first causes, an endless Unwrap of the application's own ends at the ninth | [DEBUG.md](package/DEBUG.md) |
| `v4/debug.ParameterCommand.Flags` | redaction | a secret parameter printed by debug:parameters | masked | `off:v4/debug.ParameterCommandRevealFlagName` | class R: the flag reveals in the development environment only and is refused elsewhere naming the flag, so a deployed process never prints a credential | [DEBUG.md](package/DEBUG.md) |

## `v4/event`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/event/contract.ListenerFailureConfig.Required` | protection | a stopped dispatch skipping the access-control listener | a dispatch that ends before a required listener ran refused, the http kernel failing closed | `off:v4/event/contract.ListenerFailureConfig.MaySkipRequiredListeners`, `value:v4/event/contract.ListenerFailureConfig.Required` | — | [EVENT.md](package/EVENT.md) |

## `v4/http`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http.RequestConfig.MaxBodyBytes` | limit | unbounded memory or work | absent: the application's MELODY_HTTP_MAX_REQUEST_BODY_BYTES, 1 MiB; min 1 | `value:v4/http.RequestConfig.MaxBodyBytes` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.RouteOptions.Protection.MaxBodyBytes` | limit | unbounded memory or work | absent: the application's MELODY_HTTP_MAX_REQUEST_BODY_BYTES, 1 MiB; min 1 | `value:v4/http.RouteOptions.Protection.MaxBodyBytes` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.ServerSentEventHubConfig.SubscriberBuffer` | limit | unbounded memory or work | 16 min 1 | `value:v4/http.ServerSentEventHubConfig.SubscriberBuffer` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.ServerSentEventWriterConfig.WriteBudget` | limit | a call or a wait that never returns | 30s min 1ms unlimited admitted | `value:v4/http.ServerSentEventWriterConfig.WriteBudget` | — | [HTTP.md](package/HTTP.md) |
| `v4/http.SubscribeOptions.Buffer` | limit | unbounded memory or work | absent: the hub's SubscriberBuffer, 16; min 1 | `value:v4/http.SubscribeOptions.Buffer` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.RegisterKernelTerminateAccessLogListener` | protection | a request leaving no access line | on | `off:v4/http.AccessLogConfig.Disabled`, `off:v4/application.ApplicationConfig.AccessLog` | — | [HTTP.md](package/HTTP.md) |
| `v4/http.AccessLogConfig.MaxFieldBytes` | limit | an access line of any length | 512 min 1 unlimited admitted | `value:v4/http.AccessLogConfig.MaxFieldBytes`, `value:v4/application.ApplicationConfig.AccessLog` | — | [HTTP.md](package/HTTP.md) |
| `v4/http.sessionCookie` | protection | a session cookie readable by a script | HttpOnly, always | none | the cookie is the session's credential and nothing on the page has a use for it: a script reading it is a cross-site scripting flaw taking the session | [HTTP.md](package/HTTP.md) |
| `v4/http.markResponsePrivateForSessionCookie` | protection | a shared cache replaying one client's session cookie to the next | Cache-Control private on a response carrying the session cookie | none | a shared cache holding the session's Set-Cookie hands one client's session to another; a route meant for a shared cache does not touch the session | [HTTP.md](package/HTTP.md) |
| `v4/http.requestPathIsCanonical` | refusal | a folded path spelling reaching a resource a rule written on the other spelling guards | 400 before the middleware chain | none | admitting the fold is the disagreement between the router and the rules it prevents, and a redirect teaches the spelling that sidesteps the rule | [HTTP.md](package/HTTP.md) |
| `v4/http.detectSchemeWithForwardedHeadersPolicy` | protection | a client forging X-Forwarded-Proto or X-Forwarded-For to pass as https or as another address | forwarded headers ignored, the direct peer is the client | `off:v4/http/contract.ForwardedHeadersPolicy.TrustForwardedHeaders`, `value:v4/http/contract.ForwardedHeadersPolicy.TrustedProxyList` | both are needed: the flag alone trusts nobody, an unparsable list entry is refused at NewKernel | [HTTP.md](package/HTTP.md) |
| `v4/http.resolveSessionCookieSameSite` | protection | the session cookie riding a cross-site request | SameSite=Lax; SameSite=None forces Secure | `value:v4/http/contract.SessionCookiePolicy.SameSite` | — | [HTTP.md](package/HTTP.md) |
| `v4/http.NewKernel` | refusal | a session cookie every browser drops | SameSite=None beside SessionCookieSecureNever refused at construction, naming SessionCookiePolicy.Secure | none | the pair works in no browser; either half alone is the door | [SECURITY.md](package/SECURITY.md) |
| `v4/http.RedirectResponse` | refusal | an open redirect minted from client input | an absolute, scheme-relative or backslash-carrying location refused by panic | `use:v4/http.RedirectExternalResponse` | the sibling's name is the caller's assertion that the target is trusted | [HTTP.md](package/HTTP.md) |
| `v4/http.exceptionListenerDebugMode` | redaction | an error's context and cause, or a plain error's text, served to a client | shown in the dev environment only | `use:v4/http.ExceptionRenderConfig.DebugMode` | class R: the environment name is the one switch, the status is the same in every environment and only the debug payload differs; DebugMode is for a caller rendering by hand | [EXCEPTION.md](package/EXCEPTION.md) |
| `v4/http.requestSession.load` | protection | a session storage outage read as nobody signed in | the read fails, answered 503 with Retry-After | `off:v4/http/contract.SessionPolicy.EmptyOnOutage`, `env:MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE` | the Go field is the door; the key only hardens it: false is the one value it admits, forcing the 503 over a field that serves the empty session, and true refuses the boot naming the key | [SESSION.md](package/SESSION.md) |
| `v4/http.afterFailureDoor.admitAfterFailureWrite` | protection | a listener running after a failure overwriting the failure's answer | the write refused with a warning, the standing response served | `off:v4/event/contract.ListenerFailureConfig.ReplacesResponseAfterFailure` | — | [HTTP.md](package/HTTP.md) |

## `v4/http/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/contract.MethodPolicy.AutomaticOptionsDisabled` | switch | an OPTIONS request to a known path answered 404 or 405 instead of its Allow list | OPTIONS answered automatically with the path's methods | `off:v4/http/contract.MethodPolicy.AutomaticOptionsDisabled` | — | [HTTP.md](package/HTTP.md) |
| `v4/http/contract.MethodPolicy.HeadFallbackDisabled` | switch | a HEAD request to a GET route answered 405 | HEAD served by the GET route, the body dropped | `off:v4/http/contract.MethodPolicy.HeadFallbackDisabled` | — | [HTTP.md](package/HTTP.md) |
| `v4/http/contract.RouteProtectionConfig.CsrfDisabled` | protection | a cross-site request riding the session cookie into this route's unsafe method | checked by the firewall's forgery check | `off:v4/http/contract.RouteProtectionConfig.CsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |

## `v4/http/cors`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/cors.Config.MaxAge` | limit | a preflight answer cached for good | 86400 min 0 | `value:v4/http/cors.Config.MaxAge` | class T: a cache lifetime; unlimited pins a response in every cache for good | [HTTP.md](package/HTTP.md) |
| `v4/http/cors.Config.AllowCredentials` | protection | a cross-origin page reading an answer to a request that carried the visitor's cookies | credentials not allowed: no Access-Control-Allow-Credentials sent | `off:v4/http/cors.Config.AllowCredentials` | Access-Control-Allow-Credentials beside `*` or with no origin is refused at construction, with no door: the Fetch standard forbids the pair and a browser drops it | [HTTP.md](package/HTTP.md) |
| `v4/http/cors.Config.AllowOrigins` | protection | a page of any origin reading a public, credential-less answer | nil or empty grants no origin | `value:v4/http/cors.Config.AllowOrigins`, `use:v4/http/cors.AllowOriginAll` | every origin is granted by `[]string{cors.AllowOriginAll}` written out, which refuses credentials beside it | [HTTP.md](package/HTTP.md) |
| `v4/http/cors.NewService` | protection | a scheme-less origin admitting the host under any scheme, http included | refused | `off:v4/http/cors.Config.SchemelessOriginsAdmitted` | the door admits it only without credentials: a credentialed service refuses one all the same, since a credential would travel over http | [HTTP.md](package/HTTP.md) |

## `v4/http/middleware`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/middleware.CompressionConfig.MinSize` | limit | a response compressed for no gain | 1024 min 1 | `value:v4/http/middleware.CompressionConfig.MinSize` | class T: a threshold; unlimited compresses nothing, which is leaving the middleware out | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.LimiterConfig.CleanupInterval` | limit | a loop that spins or never runs | 5m min 1ms | `value:v4/http/middleware.LimiterConfig.CleanupInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.LimiterConfig.MaxKeys` | limit | unbounded memory or work | 1_000_000 min 1 | `value:v4/http/middleware.LimiterConfig.MaxKeys` | class T: a size; unlimited is a table a client grows by varying its key | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.SecurityHeadersConfig.Disabled` | protection | content sniffing, framing, a referrer leaking the path, a downgrade to http | X-Content-Type-Options nosniff, X-Frame-Options DENY, Referrer-Policy strict-origin-when-cross-origin, Strict-Transport-Security max-age=31536000 on https | `off:v4/http/middleware.SecurityHeadersConfig.Disabled`, `off:v4/http/middleware.SecurityHeaderDisabled`, `off:v4/http/contract.RouteProtectionConfig.SecurityHeadersDisabled` | Disabled sends none, the value SecurityHeaderDisabled in a header field drops that header application-wide, the route's list drops the named headers on its responses | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.requestCarriesCredentials` | protection | the length of a compressed body leaking a per-user secret (BREACH) | a request with the session cookie or an Authorization header answered uncompressed | `off:v4/http/middleware.CompressionConfig.CompressAuthenticatedResponses`, `use:v4/http/middleware.CompressionConfig.ExcludedPaths` | a token source reading another cookie or header is not seen: its paths go in ExcludedPaths | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.limiterNameMaximumLength` | constant | a limiter name of any length keying every bucket | 64 bytes, a longer name refused | none | class K: an identifier the deployment writes, not a payload | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.sniffLength` | constant | a body's content type guessed from more, or fewer, bytes than net/http reads | 512 bytes | none | the figure is net/http's own sniffing window, a wire grammar constant | [HTTP.md](package/HTTP.md) |

## `v4/http/middleware/pipeline`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/middleware/pipeline.HttpMiddlewareDefinition.AllowDuplicates` | protection | one middleware registered twice by mistake and run twice on every request | a second definition of a Name refused | `off:v4/http/middleware/pipeline.HttpMiddlewareDefinition.AllowDuplicates` | — | [HTTP.md](package/HTTP.md) |

## `v4/http/static`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/static.Config.CacheMaxAge` | limit | a response cached for good | 3600 min 0 | `value:v4/http/static.Config.CacheMaxAge` | class T: a cache lifetime; unlimited pins a response in every cache for good | [HTTP.md](package/HTTP.md) |
| `v4/http/static.Config.CacheDisabled` | switch | every static file downloaded whole on every visit | Cache-Control with the configured max-age, ETag and Last-Modified sent, a conditional request answered 304 | `off:v4/http/static.Config.CacheDisabled`, `env:MELODY_STATIC_CACHE_DISABLED` | the key sets the field for the application's own file server | [HTTP.md](package/HTTP.md) |
| `v4/http/static.hasDotPrefixedPathElement` | protection | serving .env, .git or any dot-prefixed file kept beside the served ones | refused but a .well-known first element | `off:v4/http/static.AllowedDotPrefixAll`, `use:v4/http/static.Config.AllowedDotPrefixList` | — | [HTTP.md](package/HTTP.md) |
| `v4/http/static.dirFileSystem.Open` | refusal | a symlink publishing a file outside the served directory | refused | none | a link out of the served directory publishes whatever it names; another tree is served by a file server rooted there, and a symlinked base is resolved and served | [HTTP.md](package/HTTP.md) |

## `v4/httpclient`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/httpclient.HttpClientConfig.MaxResponseBodyBytes` | limit | unbounded memory or work | 10 MiB min 1 | `value:v4/httpclient.HttpClientConfig.MaxResponseBodyBytes` | class T: a size; the client's cap is the default of every buffered request, whose body is held whole in memory | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.HttpClientConfig.MaxRedirects` | limit | a redirect loop followed forever | 10 min 1 | `value:v4/httpclient.HttpClientConfig.MaxRedirects` | class T: a redirect count; unlimited follows a redirect loop forever | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.HttpClientConfig.Timeout` | limit | a call or a wait that never returns | 30s min 1ms unlimited admitted | `value:v4/httpclient.HttpClientConfig.Timeout` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.RequestOptions.MaxResponseBodyBytes` | limit | unbounded memory or work | 10 MiB min 1 | `value:v4/httpclient.RequestOptions.MaxResponseBodyBytes`, `value:v4/httpclient.HttpClientConfig.MaxResponseBodyBytes` | class T: a size; unlimited is a body held whole in memory that only grows, so it is admitted on RequestStream alone, where the caller reads the body as it arrives | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.HttpClient.credentialStrippingRedirectPolicy` | protection | a credential the caller meant for one origin handed to the origin a redirect names | stripped on every redirect leaving the origin | none | the Location is the server's choice, the party a credential must not follow on its say-so; a caller follows by hand under RedirectsDisabled and attaches the credential it chose | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.RequestOptions.Timeout` | limit | a call or a wait that never returns | absent: the client's Timeout, 30s; min 1ms unlimited admitted | `value:v4/httpclient.RequestOptions.Timeout` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.DialTimeout` | limit | a call or a wait that never returns | 10s min 1ms | `value:v4/httpclient.TransportConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.ExpectContinueTimeout` | limit | a call or a wait that never returns | 1s min 0s | `value:v4/httpclient.TransportConfig.ExpectContinueTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.IdleConnectionTimeout` | limit | a call or a wait that never returns | 1m30s min 1s unlimited admitted | `value:v4/httpclient.TransportConfig.IdleConnectionTimeout` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.KeepAlive` | limit | a dead connection never noticed | 30s min 1s | `value:v4/httpclient.TransportConfig.KeepAlive` | class T: a probe interval; unlimited never notices a dead peer | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.MaxIdleConnectionsPerHost` | limit | unbounded concurrency or connections | 100 min 1 unlimited admitted | `value:v4/httpclient.TransportConfig.MaxIdleConnectionsPerHost` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | 100 min 1 unlimited admitted | `value:v4/httpclient.TransportConfig.MaxIdleConnections` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.ResponseHeaderTimeout` | limit | a call or a wait that never returns | 15s min 1ms | `value:v4/httpclient.TransportConfig.ResponseHeaderTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.TlsHandshakeTimeout` | limit | a call or a wait that never returns | 10s min 1ms | `value:v4/httpclient.TransportConfig.TlsHandshakeTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.HttpClientConfig.RedirectsDisabled` | switch | a caller that must see the redirect itself handed its target's answer | redirects followed, credentials stripped off-origin | `off:v4/httpclient.HttpClientConfig.RedirectsDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.IdleConnectionsDisabled` | switch | a dial and a handshake on every request | idle connections kept for reuse within MaxIdleConnections | `off:v4/httpclient.TransportConfig.IdleConnectionsDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.KeepAliveDisabled` | switch | a dead peer behind an idle connection noticed only on the next write | tcp keep-alive probes every KeepAlive | `off:v4/httpclient.TransportConfig.KeepAliveDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.ProxyDisabled` | switch | a request that must reach its host directly sent through the environment's proxy, or the reverse | the proxy the environment names (HTTP_PROXY, HTTPS_PROXY, NO_PROXY) | `off:v4/httpclient.TransportConfig.ProxyDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.sanitizeUrlForDiagnostics` | redaction | a url's userinfo or query values written into an error's context | userinfo dropped and every query value redacted, the names kept | none | class R: a url routinely carries a credential in either place | [HTTPCLIENT.md](package/HTTPCLIENT.md) |

## `v4/httpclient/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|

## `v4/internal`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/internal.DiagnosticTextMaxLength` | constant | a diagnostic journaled whole | 512 | `value:v4/http.AccessLogConfig.MaxFieldBytes` | class K: a diagnostic cut; a kernel, security or exception record carries the request-supplied field it names, cut at 512 bytes with the dropped bytes counted, and the access log, the one record written per request, takes its own cut through AccessLogConfig.MaxFieldBytes | [HTTP.md](package/HTTP.md) |
| `v4/internal.JournalDirectoryModeFloor` | mode | a directory its owner cannot traverse or write: of the journal, the session file storage, the local storage, the cache | 0700 | none | the floor every configured directory mode may not go below, ReopenableFileWriterConfig, session.FileStorageConfig, storage.LocalStorageConfig and MELODY_CACHE_DIRECTORY_MODE: below it the owner cannot create what the directory holds, so the mode is refused, not doored; only a floor is checked | [LOGGING.md](package/LOGGING.md) |
| `v4/internal.JournalFileModeFloor` | mode | a file its owner cannot read and write: a journal, a session file, a stored object | 0600 | none | the floor every configured file mode may not go below, ReopenableFileWriterConfig, session.FileStorageConfig and storage.LocalStorageConfig: below it the owner loses what it writes, so the mode is refused, not doored; only a floor is checked | [LOGGING.md](package/LOGGING.md) |
| `v4/internal.WriteFileAtomically#1` | mode | generated source in a directory the repository does not own | 0755 directories, 0644 a new file, an existing file its own | none | generated Go source is committed: its directories and a new file take the repository's modes, an existing file keeps its own | — |
| `v4/internal.RedactQueryValuesForDiagnostics` | redaction | a credential in a query string or a Referer written to the access log on every request | every value redacted, the names kept | none | class R: a query string routinely carries a credential; a deployment that needs the values logs them in its own kernel.terminate listener | [HTTP.md](package/HTTP.md) |

## `v4/lock`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/lock.ExclusiveCommandConfig.Ttl` | limit | state held, or a decision kept, forever | 30s min 2ms unlimited admitted | `value:v4/lock.ExclusiveCommandConfig.Ttl` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.ExclusiveConfig.Ttl` | limit | state held, or a decision kept, forever | 30s min 2ms unlimited admitted | `value:v4/lock.ExclusiveConfig.Ttl` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.InMemoryLockerConfig.PurgeInterval` | limit | a loop that spins or never runs | 512 min 1 unlimited admitted | `value:v4/lock.InMemoryLockerConfig.PurgeInterval` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.MaxCampaignBackoff` | limit | a campaign that spins or never comes back | 1m min 1ms | `value:v4/lock.LeaderGateConfig.MaxCampaignBackoff` | class T: a backoff ceiling; unlimited is a candidate that never campaigns again | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.MaxConsecutiveRefreshFailures` | limit | unbounded memory or work | 3 min 1 unlimited admitted | `value:v4/lock.LeaderGateConfig.MaxConsecutiveRefreshFailures` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.RefreshInterval` | limit | a loop that spins or never runs | derived: max(Ttl/2, 1ms), 15s under an unlimited Ttl; min 1ms | `value:v4/lock.LeaderGateConfig.RefreshInterval` | class T: an interval; unlimited never refreshes the lease, so the leadership lapses under a leader that still runs | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.RetryInterval` | limit | a loop that spins or never runs | derived: max(Ttl/2, 1s), 1s under an unlimited Ttl; min 1ms | `value:v4/lock.LeaderGateConfig.RetryInterval` | class T: an interval; unlimited never campaigns again after a lost lease | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.Ttl` | limit | state held, or a decision kept, forever | 30s min 2ms unlimited admitted | `value:v4/lock.LeaderGateConfig.Ttl` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.sessionProbeIntervalRule#1` | limit | a session lock whose loss goes unnoticed | 15s min 1ms | `value:v4/lock.sessionProbeIntervalRule`, `value:v4/lock.ExclusiveConfig.ProbeInterval`, `value:v4/lock.ExclusiveCommandConfig.ProbeInterval` | class T: an interval; unlimited never probes the session, so a lock lost with its connection goes unnoticed | [LOCK.md](package/LOCK.md) |
| `v4/lock.stopRenewalLimitRule#1` | limit | unbounded memory or work | 30s min 1ms unlimited admitted | `value:v4/lock.stopRenewalLimitRule`, `value:v4/lock.ExclusiveCommandConfig.StopRenewalLimit`, `value:v4/lock.ExclusiveConfig.StopRenewalLimit`, `value:v4/lock.LeaderGateConfig.StopRenewalLimit` | — | [LOCK.md](package/LOCK.md) |

## `v4/lock/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/lock/contract.Locker.CreateLock.ttl` | limit | state held, or a decision kept, forever | 30s min 1ms unlimited admitted | `value:v4/lock/contract.Locker.CreateLock` | — | [LOCK.md](package/LOCK.md) |

## `v4/logging`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/logging.ExitConfig.BeforeExitBudget` | limit | a call or a wait that never returns | 10s min 1ms | `value:v4/logging.ExitConfig.BeforeExitBudget` | class T: a timeout; an unlimited step before exit is a process that never exits | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.JsonLoggerConfig.ContextNormalizationDepth` | limit | a descent without end: the stack, or a payload that nests forever | 10_000 min 1 max 10_000 | `value:v4/logging.JsonLoggerConfig.ContextNormalizationDepth` | at most 10000, the nesting encoding/json decodes: each level is a frame of the goroutine stack, so a deeper walk can overflow it; unlimited is the overflow | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.ShieldedStepConfig.Budget` | limit | a call or a wait that never returns | 10s min 1ms unlimited admitted | `value:v4/logging.ShieldedStepConfig.Budget` | — | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.causeChainDepthRule#1` | limit | a descent without end: the stack, or a payload that nests forever | 8 min 1 | `value:v4/logging.DefaultLoggerConfig.CauseChainDepth`, `value:v4/logging.JsonLoggerConfig.CauseChainDepth` | class T: a depth; a chain can be cyclic, so unlimited is a walk without end | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.DefaultLogDirectoryMode` | mode | a journal directory readable by every local user | 0750 | `value:v4/logging.ReopenableFileWriterConfig.DirectoryMode`, `env:MELODY_LOG_DIRECTORY_MODE` | — | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.DefaultLogFileMode` | mode | a journal readable by every local user | 0640 | `value:v4/logging.ReopenableFileWriterConfig.FileMode`, `env:MELODY_LOG_FILE_MODE` | — | [LOGGING.md](package/LOGGING.md) |
| `v4/logging` | redaction | a secret or a control character written into the journal | redacted and escaped | none | class R: the one exemption from the door rule, a raw door would make the journal the leak or the forgery it is protected against | [LOGGING.md](package/LOGGING.md#redaction-and-escaping) |

## `v4/mailer`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/mailer.SmtpConfig.DataTerminationTimeout` | limit | a call or a wait that never returns | derived: 4 x the resolved Timeout, at least 2m; min 1ms | `value:v4/mailer.SmtpConfig.DataTerminationTimeout` | class T: a timeout; unlimited is the wait that never returns | [MAILER.md](package/MAILER.md) |
| `v4/mailer.SmtpConfig.DialTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:v4/mailer.SmtpConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [MAILER.md](package/MAILER.md) |
| `v4/mailer.SmtpConfig.Timeout` | limit | a call or a wait that never returns | derived: the resolved DialTimeout, 30s; min 1ms | `value:v4/mailer.SmtpConfig.Timeout` | class T: a timeout; unlimited is the wait that never returns | [MAILER.md](package/MAILER.md) |
| `v4/mailer.SmtpConfig.AllowPlaintext` | protection | a message and the relay credentials sent in clear to a relay that does not offer STARTTLS | such a relay refuses the send | `off:v4/mailer.SmtpConfig.AllowPlaintext` | — | [MAILER.md](package/MAILER.md) |
| `v4/mailer.SmtpTransport.deliver` | protection | a message sent anonymously while the transport reports success, the configured credentials never offered | configured credentials fail closed when the relay does not advertise AUTH | none | skipping the authentication would deliver under another identity than the one configured; the common trigger is a relay advertising AUTH only after STARTTLS | [MAILER.md](package/MAILER.md) |

## `v4/messagebus`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/messagebus.ConsumeCommandConfig.ShutdownTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:v4/messagebus.ConsumeCommandConfig.ShutdownTimeout` | class T: a timeout; an unlimited drain is a stop that never ends | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.InMemoryTransportConfig.BufferSize` | limit | unbounded memory or work | 64 min 0 | `value:v4/messagebus.InMemoryTransportConfig.BufferSize` | class T: a size; unlimited is memory or work that only grows | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.ResolveHeaderLimits#1` | limit | unbounded memory or work | 32 min 1 | `value:v4/messagebus.HeaderLimitsConfig.MaxCount` | class T: a size; unlimited is memory or work that only grows | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.ResolveHeaderLimits#2` | limit | unbounded memory or work | 8 KiB min 64 | `value:v4/messagebus.HeaderLimitsConfig.MaxBytes` | class T: a size; unlimited is memory or work that only grows | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.ResolveHeaderLimits#3` | limit | a header name of any length carried on every envelope | 64 min 1 max 255 | `value:v4/messagebus.HeaderLimitsConfig.MaxNameLength` | at most 255, HeaderNameMaximumLength: an amqp field-table key is a short string, and a received name is read up to it whatever the reader's own limit | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.Backoff` | limit | a retry storm, or a retry that never comes back | 1s doubling to 1h | `value:v4/messagebus.RetryPolicy.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.FailureRequeueDelay` | limit | a requeue that spins or never comes back | 5s min 1ms | `value:v4/messagebus.RetryPolicy.FailureRequeueDelay` | class T: a delay; unlimited is a message never redelivered | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.MaxDeadLetterAttempts` | limit | guessing without end | 5 min 1 unlimited admitted | `value:v4/messagebus.RetryPolicy.MaxDeadLetterAttempts` | — | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.MaxRetries` | limit | a poison message redelivered forever | 3 min 0 | `value:v4/messagebus.RetryPolicy.MaxRetries` | class T: a retry count; unlimited redelivers a poison message forever | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.HandleMiddlewareConfig.AllowMissingHandler` | protection | a message no handler is registered for acknowledged and lost | the dispatch fails | `off:v4/messagebus.HandleMiddlewareConfig.AllowMissingHandler` | — | [MESSAGEBUS.md](package/MESSAGEBUS.md) |

## `v4/openapi`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/openapi.maximumSchemaDepth` | constant | a schema descent that overflows the stack | 64 | none | class K: a stack backstop; the descent runs inside the spec handler, where a stack overflow is fatal, and the bound degrades a position to an undescribed one, never fails the document | [OPENAPI.md](package/OPENAPI.md) |

## `v4/security`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security.HmacEnvelopeSignerConfig.Ttl` | limit | state held, or a decision kept, forever | 30s min 1s | `value:v4/security.HmacEnvelopeSignerConfig.Ttl` | class T: a lifetime; an envelope honoured forever can be replayed forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security.HmacTokenSourceConfig.Leeway` | limit | a clock difference widening what a credential admits | 5s max 5m | `value:v4/security.HmacTokenSourceConfig.Leeway` | at most 5m: a skew wider than five minutes is a misconfigured clock, not drift, and every second of leeway is a second more an expired envelope is honoured; unlimited honours it forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security.HmacTokenSourceConfig.MaxFutureExpiry` | limit | state held, or a decision kept, forever | 1h min 1s | `value:v4/security.HmacTokenSourceConfig.MaxFutureExpiry` | class T: a lifetime; unlimited admits an envelope that never expires | [SECURITY.md](package/SECURITY.md) |
| `v4/security.InMemoryTokenStoreConfig.MaxEntries` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.MaxEntries` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.InMemoryTokenStoreConfig.RevocationEpochRetention` | limit | state held, or a decision kept, forever | 168h min 1s unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.RevocationEpochRetention` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.InMemoryTokenStoreConfig.SweepInterval` | limit | a loop that spins or never runs | 1m min 1s unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.SweepInterval` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.Leeway` | limit | a clock difference widening what a credential admits | 5s max 5m | `value:v4/security.JwtConfig.Leeway` | at most 5m: a skew wider than five minutes is a misconfigured clock, not drift, and every second of leeway is a second more an expired token is honoured; unlimited honours it forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.MaxCandidateKeys` | limit | unbounded memory or work | 8 min 1 | `value:v4/security.JwtConfig.MaxCandidateKeys` | class T: a size; unlimited verifies one token against every key of the set, a cost the token's sender chooses | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.RevocationEpochSkew` | limit | a clock difference widening what a credential admits | 0 min 0 | `value:v4/security.JwtConfig.RevocationEpochSkew` | class T: a skew; unlimited admits every token a revocation should have ended | [SECURITY.md](package/SECURITY.md) |
| `v4/security.MemoryNonceGuardConfig.MaxEntries` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/security.MemoryNonceGuardConfig.MaxEntries` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.MemoryNonceGuardConfig.PurgeInterval` | limit | a loop that spins or never runs | 1s min 1ms | `value:v4/security.MemoryNonceGuardConfig.PurgeInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [SECURITY.md](package/SECURITY.md) |
| `v4/security.TotpSecondFactorAuthenticatorConfig.LockoutWindow` | limit | state held, or a decision kept, forever | 5m min 1s | `value:v4/security.TotpSecondFactorAuthenticatorConfig.LockoutWindow` | class T: a lifetime; an unlimited window locks a factor out for good, the off door is MaxFailedAttempts | [SECURITY.md](package/SECURITY.md) |
| `v4/security.TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts` | limit | guessing without end | 5 min 1 unlimited admitted | `value:v4/security.TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.resolveMinimumSecretLength#1` | limit | a key short enough to guess | 32 min 32 | `value:v4/security.JwtSecretKeySetConfig.MinimumSecretLength`, `value:v4/security.HmacEnvelopeSignerConfig.MinimumSecretLength`, `value:v4/security.HmacKeyRing.MinimumSecretLength`, `value:v4/security.HmacTokenSourceConfig.MinimumSecretLength` | class T: a floor; RFC 7518 asks an HMAC key at least as long as the hash, so no floor is refused and a longer floor is the door | [SECURITY.md](package/SECURITY.md) |
| `v4/security.AccessDecisionManagerConfig.AllowIfAllAbstain` | protection | an attribute granted because every voter that supports it abstained | denied | `off:v4/security.AccessDecisionManagerConfig.AllowIfAllAbstain` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.AccessDecisionManagerConfig.AllowIfTied` | protection | an attribute granted on a tie under DecisionStrategyConsensus | denied | `off:v4/security.AccessDecisionManagerConfig.AllowIfTied` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.CompiledFirewallConfig.LoginCsrfDisabled` | protection | a cross-site sign-in fixing the visitor into the attacker's account (login forgery) | checked by the firewall's forgery check | `off:v4/security.CompiledFirewallConfig.LoginCsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.CompiledFirewallConfig.LogoutCsrfDisabled` | protection | a cross-site request signing the visitor out | checked by the firewall's forgery check | `off:v4/security.CompiledFirewallConfig.LogoutCsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.CsrfModeDisabled` | protection | a cross-site request forging an unsafe method on a session firewall | origin check on | `off:v4/security.CsrfModeDisabled`, `value:v4/security.CsrfConfig.Mode`, `value:v4/security/config.FirewallCsrfConfig.Mode`, `off:v4/http/contract.RouteProtectionConfig.CsrfDisabled`, `off:v4/security/config.LoginConfig.CsrfDisabled`, `off:v4/security.ResolverTokenSourceConfig.Stateless`, `off:v4/security.AuthenticatorTokenSourceConfig.Stateless` | a token source declared stateless reads no credential a browser attaches, so its firewall checks none of its routes; declared over a resolver that reads the session it turns the check off | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.AllowFutureIssuedAt` | protection | a token minted with a clock ahead, or forged ahead, admitted before its time | an iat ahead of the clock by more than the leeway refused | `off:v4/security.JwtConfig.AllowFutureIssuedAt` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.AllowWithoutExpiry` | protection | a token that never expires honoured forever | a token without exp refused | `off:v4/security.JwtConfig.AllowWithoutExpiry` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtAlgorithmHs256` | refusal | an algorithm confusion: a token signed with another algorithm | HS256 only | none | a token choosing among algorithms is the confusion alg none came from; another algorithm is another TokenValidator behind NewBearerTokenSource | [SECURITY.md](package/SECURITY.md) |
| `v4/security.HmacTokenSourceConfig.NonceGuard` | protection | an internal-auth envelope replayed | the in-process guard | `use:v4/security.HmacTokenSourceConfig.NonceGuard` | the framework ships no guard that remembers nothing; the field takes the shared guard of several instances | [SECURITY.md](package/SECURITY.md) |
| `v4/security.TotpSecondFactorAuthenticatorConfig.ReplayGuard` | protection | a TOTP code replayed inside its window | the in-process guard | `use:v4/security.TotpSecondFactorAuthenticatorConfig.ReplayGuard` | the framework ships no guard that remembers nothing; the field takes the shared guard of several instances | [SECURITY.md](package/SECURITY.md) |
| `v4/security.serveFirewallDoor` | protection | an id the client held before signing in carrying the identity (session fixation) | the session id rotated at every sign-in through the login door, a failed rotation refusing the request | none | an id fixed before the sign-in is the attack itself; a handler signing in outside the door rotates through http.RegenerateRequestSession | [SECURITY.md](package/SECURITY.md) |
| `v4/security.HmacTokenSourceConfig.VerifyBodyBeforeNonce` | protection | a captured envelope forcing a body buffering on every replay | the nonce consumed before the body is read | `off:v4/security.HmacTokenSourceConfig.VerifyBodyBeforeNonce`, `use:v4/http/contract.RouteProtectionConfig.HmacCheckOrder` | the other order spends a body read on every replay and keeps an on-path party from burning a legitimate nonce: the trade is the deployment's | [SECURITY.md](package/SECURITY.md) |
| `v4/security.ImpersonationTokenSourceConfig.RoleMode` | protection | an impersonating administrator acting with administrator rights while seen as the target | the target's roles authorize | `value:v4/security.ImpersonationTokenSourceConfig.RoleMode` | — | [SECURITY.md](package/SECURITY.md) |

## `v4/security/config`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/config.FirewallOverride.GlobalAccessControlDisabled` | protection | a firewall's routes escaping the global access rules | the global rules apply to every firewall | `off:v4/security/config.FirewallOverride.GlobalAccessControlDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.LoginConfig.CsrfDisabled` | protection | a cross-site sign-in fixing the visitor into the attacker's account (login forgery) | checked by the firewall's forgery check | `off:v4/security/config.LoginConfig.CsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.LoginConfig.Limiter` | protection | credential guessing at the login door | a door without a limiter refused | `off:v4/security/config.LoginConfig.LimiterDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.validateFirewallDoor` | refusal | a sign-in or sign-out forged by a link | a door on a safe or empty method refused | none | a state change on a safe method is forgeable by an image tag, a sign-out included; a legacy link is served by a GET route rendering a form that posts to the door | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.refuseAccessControlWithoutFallback` | refusal | a path no access rule covers, reaching its handler undecided | refused at compile | `use:v4/security/accesscontrol.NewSegmentPrefixRule` | the door is the explicit fallback, a rule on "/" (PUBLIC_ACCESS keeps the rest open on purpose), never a switch: the control then states what an unnamed path gets | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.LogoutConfig.CsrfDisabled` | protection | a cross-site request signing the visitor out | checked by the firewall's forgery check | `off:v4/security/config.LogoutConfig.CsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |

## `v4/security/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/contract.FailedAttemptStore.Increment.window` | limit | state held, or a decision kept, forever | no default: an absent window refused, min 1s | `value:v4/security/contract.FailedAttemptStore.Increment` | class T: a lifetime; unlimited keeps the state forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security/contract.NonceGuard.Remember.ttl` | limit | state held, or a decision kept, forever | no default: an absent ttl refused, min 1ms | `value:v4/security/contract.NonceGuard.Remember` | class T: a lifetime; a nonce remembered forever is a table that only grows | [SECURITY.md](package/SECURITY.md) |
| `v4/security/contract.RevocableTokenStore.Put.ttl` | limit | state held, or a decision kept, forever | no default: an absent ttl refused, min 1s unlimited admitted | `value:v4/security/contract.RevocableTokenStore.Put` | — | [SECURITY.md](package/SECURITY.md) |

## `v4/security/totp`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/totp.totp.Config.Digits` | limit | a code short enough to guess | 6 min 6 max 8 | `value:v4/security/totp.Config.Digits` | 6 to 8: RFC 4226 asks at least six digits and authenticator apps show at most eight; a code outside the range is one no app generates | [SECURITY.md](package/SECURITY.md) |
| `v4/security/totp.totp.Config.Period` | limit | a loop that spins or never runs | 30s min 1s max 24h | `value:v4/security/totp.Config.Period` | 1s to 24h in whole seconds: a step longer than a day makes one code a day-long credential; unlimited is a code that never changes | [SECURITY.md](package/SECURITY.md) |
| `v4/security/totp.totp.Config.Skew` | limit | a clock difference widening what a credential admits | 1 min 0 max 10 | `value:v4/security/totp.Config.Skew` | at most 10: each step of skew widens by two periods the window a guessed code is accepted in, ten already five minutes either side at 30s; unlimited accepts every code ever issued | [SECURITY.md](package/SECURITY.md) |

## `v4/serializer`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/serializer.SerializerManagerConfig.MaxAcceptMembers` | limit | unbounded memory or work | 64 min 1 | `value:v4/serializer.SerializerManagerConfig.MaxAcceptMembers` | class T: a size; the Accept header is the client's, every member weighed against every serializer, so unlimited is negotiation work a request names | [SERIALIZER.md](package/SERIALIZER.md) |
| `v4/serializer.PlainTextSerializerConfig.MaxBytes` | limit | unbounded memory or work | 256 KiB min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxBytes` | — | [SERIALIZER.md](package/SERIALIZER.md) |
| `v4/serializer.PlainTextSerializerConfig.MaxDepth` | limit | a descent without end: the stack, or a payload that nests forever | 8 min 1 max 1024 | `value:v4/serializer.PlainTextSerializerConfig.MaxDepth` | at most 1024: the walk recurses once per level on the goroutine stack and no reader follows a text body past a thousand levels; a value may hold itself, so unlimited never ends | [SERIALIZER.md](package/SERIALIZER.md) |
| `v4/serializer.PlainTextSerializerConfig.MaxLines` | limit | unbounded memory or work | 1024 min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxLines` | — | [SERIALIZER.md](package/SERIALIZER.md) |
| `v4/serializer.PlainTextSerializerConfig.MaxMembers` | limit | unbounded memory or work | 16_384 min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxMembers` | — | [SERIALIZER.md](package/SERIALIZER.md) |

## `v4/session`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/session.FileStorageConfig.JournalMaxBytes` | limit | unbounded memory or work | 8 MiB min 64 << 10 | `value:v4/session.FileStorageConfig.JournalMaxBytes` | class T: a size; unlimited is memory or work that only grows | [SESSION.md](package/SESSION.md) |
| `v4/session.FileStorageConfig.MaxSessions` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/session.FileStorageConfig.MaxSessions` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.InMemoryStorageConfig.CleanupInterval` | limit | a loop that spins or never runs | 1m min 1ms | `value:v4/session.InMemoryStorageConfig.CleanupInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [SESSION.md](package/SESSION.md) |
| `v4/session.InMemoryStorageConfig.MaxSessions` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/session.InMemoryStorageConfig.MaxSessions` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.ManagerConfig.StorageTimeout` | limit | a call or a wait that never returns | 5s min 1ms unlimited admitted | `value:v4/session.ManagerConfig.StorageTimeout` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.ManagerConfig.TombstoneRetention` | limit | state held, or a decision kept, forever | 5m min 1s | `value:v4/session.ManagerConfig.TombstoneRetention` | class T: a lifetime; unlimited keeps the state forever | [SESSION.md](package/SESSION.md) |
| `v4/session.ManagerConfig.Ttl` | limit | state held, or a decision kept, forever | 24h min 1s unlimited admitted | `value:v4/session.ManagerConfig.Ttl` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.DefaultFileStorageDirectoryMode` | mode | a session directory readable by every local user | 0700 | `value:v4/session.FileStorageConfig.DirectoryMode` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.DefaultFileStorageFileMode` | mode | a session file or journal readable beyond its owner | 0600 | `value:v4/session.FileStorageConfig.FileMode` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.sessionFileModeOtherBits` | refusal | a session file, journal or directory readable, writable or searchable by every local user | a FileMode or DirectoryMode granting others any bit refused at construction, naming the field and the octal value | none | the file holds every session's data, a credential each; the group's bits stay the operator's, and the other modes of the major keep their floor only | [SESSION.md](package/SESSION.md) |
| `v4/session.FileStorageConfig.JournalDisabled` | switch | the whole snapshot rewritten on every Save and Delete | an append-only journal compacted at JournalMaxBytes | `off:v4/session.FileStorageConfig.JournalDisabled` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.generateSessionId` | constant | a guessable session id | 16 bytes of crypto/rand, 32 hex characters | none | class K: 128 bits; the storages key on the width | [SESSION.md](package/SESSION.md) |

## `v4/session/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/session/contract.ttl` | limit | state held, or a decision kept, forever | no default: an absent ttl refused, min 1s unlimited admitted | `value:v4/session/contract.Storage.Save` | — | [SESSION.md](package/SESSION.md) |

## `v4/storage`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/storage.LocalStorageConfig.SweepBatchSize` | limit | unbounded memory or work | 256 min 1 | `value:v4/storage.LocalStorageConfig.SweepBatchSize` | class T: a batch; unlimited is one call over the whole set | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.LocalStorageConfig.TemporaryStaleAge` | limit | unbounded memory or work | 1h min 1s unlimited admitted | `value:v4/storage.LocalStorageConfig.TemporaryStaleAge` | — | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.LocalStorage.Get#1` | mode | — | 0 (read only) | none | opened read only: the mode argument creates nothing | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.DefaultLocalStorageDirectoryMode` | mode | a storage directory readable beyond its owner | 0750 | `value:v4/storage.LocalStorageConfig.DirectoryMode` | — | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.DefaultLocalStorageFileMode` | mode | a stored object readable by every local user | 0640 | `value:v4/storage.LocalStorageConfig.FileMode` | — | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.readStorageContentType#1` | mode | — | 0 (read only) | none | opened read only: the mode argument creates nothing | — |
| `v4/storage.storageRelativeKey` | refusal | a key escaping its prefix or the base directory through a ".." segment | refused by name, the root confined through os.OpenRoot | none | another tree is another LocalStorage rooted there; a fold would cross an application that isolates its callers by key prefix | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.storageMetadataMaximumBytes` | constant | an object's metadata of any size written beside it | 4096 bytes, a longer content type refused naming PutOptions.ContentType | none | class K: the metadata is a content type and its framing, not a payload | [STORAGE.md](package/STORAGE.md) |

## `v4/storage/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/storage/contract.PresignConfig.Expiry` | limit | state held, or a decision kept, forever | 15m min 1s max 168h | `value:v4/storage/contract.PresignConfig.Expiry` | at most 7 days, the longest a SigV4 presigned url is signed for: the url is a bearer credential to the object, so unlimited is a credential that never expires | [STORAGE.md](package/STORAGE.md) |

## `v4/translation`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/translation.maxInterpolationDepth` | constant | a message that interpolates itself forever | 32 | none | class K: a cycle guard; every plural or select level is a recursion on the stack and no translated sentence nests a tenth as deep | [TRANSLATION.md](package/TRANSLATION.md) |

## `v4/validation`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/validation.ValidatorConfig.MaxAnswerBytes` | limit | unbounded memory or work | 512 KiB min 1 | `value:v4/validation.ValidatorConfig.MaxAnswerBytes` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxErrors` | limit | unbounded memory or work | 1000 min 1 | `value:v4/validation.ValidatorConfig.MaxErrors` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxNestingDepth` | limit | a descent without end: the stack, or a payload that nests forever | 64 min 1 max 10_000 | `value:v4/validation.ValidatorConfig.MaxNestingDepth` | at most 10000, the nesting encoding/json decodes: each level is a frame of the goroutine stack; unlimited is a payload that nests until the stack overflows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxRenderedMapKeyLength` | limit | unbounded memory or work | 128 min 1 | `value:v4/validation.ValidatorConfig.MaxRenderedMapKeyLength` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxRenderedPathLength` | limit | unbounded memory or work | 256 min 1 | `value:v4/validation.ValidatorConfig.MaxRenderedPathLength` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |

## `integrations/amqp/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/amqp/v4.maxDelayBuckets` | constant | a delay exchange per distinct delay | 8 | none | each tier is a queue the transport declares on the broker, so the tier list is topology a deployment migrates; eight tiers ten times apart span a millisecond to about three hours, and the default four span 5s to 1h | [README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ConnectionConfig.CloseTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:integrations/amqp/v4.ConnectionConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ProviderConfig.CloseTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:integrations/amqp/v4.ProviderConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ProviderConfig.DialTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:integrations/amqp/v4.ProviderConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ProviderConfig.Heartbeat` | limit | a dead connection never noticed | 10s min 1s | `value:integrations/amqp/v4.ProviderConfig.Heartbeat` | class T: a heartbeat interval; unlimited never notices a dead peer | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ReconnectConfig.Backoff` | limit | a retry storm, or a retry that never comes back | inherited: the provider's, 1s doubling to 30s | `value:integrations/amqp/v4.ReconnectConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ServerSentEventBackplaneConfig.CallTimeout` | limit | a call or a wait that never returns | 1s min 1ms | `value:integrations/amqp/v4.ServerSentEventBackplaneConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ServerSentEventBackplaneConfig.CloseTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:integrations/amqp/v4.ServerSentEventBackplaneConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.CallTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:integrations/amqp/v4.TransportConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.CloseTimeout` | limit | a call or a wait that never returns | 30s min 1ms | `value:integrations/amqp/v4.TransportConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.Prefetch` | limit | unbounded concurrency or connections | 10 min 1 max 65_535 | `value:integrations/amqp/v4.TransportConfig.Prefetch` | at most 65535: the prefetch count of basic.qos is a 16-bit field; unlimited would let the broker push the whole queue into one consumer's memory | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.PublishReturnBuffer` | limit | unbounded memory or work | 16 min 1 | `value:integrations/amqp/v4.TransportConfig.PublishReturnBuffer` | class T: a size; unlimited is memory or work that only grows | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.DeadLetterDisabled` | protection | a poison message dropped by the broker once its attempts are spent | a <queue>.dlx exchange and a <queue>.dlq queue declared with the queue | `off:integrations/amqp/v4.TransportConfig.DeadLetterDisabled` | — | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.DeadLetterMaxLength` | limit | a dead-letter queue that grows without end | unlimited min 1 | `value:integrations/amqp/v4.TransportConfig.DeadLetterMaxLength` | — | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.DeadLetterTtl` | limit | a dead letter kept forever | unlimited min 1ms max 4294967295ms (about 49.7 days) | `value:integrations/amqp/v4.TransportConfig.DeadLetterTtl` | — | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.Transport.publish` | refusal | a message lost in silence: unroutable or nacked by the broker | confirmed and mandatory, a return or a nack is the send's error | none | a publish that may vanish is the loss the transport exists to prevent: an application that accepts it publishes through its own channel, not through the transport | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ProviderConfig.TlsConfig` | protection | messages and credentials crossing the network in clear | none: plaintext unless the dsn is amqps | `value:integrations/amqp/v4.ProviderConfig.TlsConfig` | the dsn's scheme selects TLS; an amqp:// dsn off this machine dials plaintext and is named once per host by the warning amqp.plaintextDsn | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.republishAttemptCount` | constant | a transient failure of a re-publish losing the advanced retry counters | 3 attempts | none | only the re-publish carries the advanced counters, so a transient failure is worth a few attempts; each retry waits the reconnect backoff, and the last failure is answered to the caller | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |

## `integrations/aws/s3/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/aws/s3/v4.StorageConfig.MultipartAbortTimeout` | limit | a call or a wait that never returns | 10s min 1ms | `value:integrations/aws/s3/v4.StorageConfig.MultipartAbortTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/aws/s3/v4/README.md](../../integrations/aws/s3/v4/README.md) |
| `integrations/aws/s3/v4.StorageConfig.PutSpoolLimit` | limit | unbounded memory or work | 16 * 1024 * 1024 min 1 | `value:integrations/aws/s3/v4.StorageConfig.PutSpoolLimit` | class T: a size; unlimited is memory or work that only grows | [integrations/aws/s3/v4/README.md](../../integrations/aws/s3/v4/README.md) |
| `integrations/aws/s3/v4.Config.Insecure` | protection | credentials, objects and presigned urls crossing the network in clear | https | `off:integrations/aws/s3/v4.Config.Insecure` | a plaintext endpoint off this machine is named once per process by the warning aws.s3.plaintextEndpoint | [integrations/aws/s3/v4/README.md](../../integrations/aws/s3/v4/README.md) |

## `integrations/bunorm/migrate/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/migrate/v4.Options.UnlockTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/bunorm/migrate/v4.Options.UnlockTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/migrate/v4/README.md](../../integrations/bunorm/migrate/v4/README.md) |
| `integrations/bunorm/migrate/v4.migrationFileMode` | mode | a generated migration readable beyond the repository's own files | 0644 | none | a migration file is source the repository commits: its mode is the repository's, like any file the developer writes | — |
| `integrations/bunorm/migrate/v4.clearRanNotRecordedMarkers` | protection | a migration that ran but was not recorded, run again | the next db:migrate refused while its marker stands | `off:integrations/bunorm/migrate/v4.MigrateForceFlagName` | — | [integrations/bunorm/migrate/v4/README.md](../../integrations/bunorm/migrate/v4/README.md) |

## `integrations/bunorm/mysql/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/mysql/v4.LockerConfig.ReleaseTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/bunorm/mysql/v4.LockerConfig.ReleaseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxIdleTime` | limit | state held, or a decision kept, forever | 1m min 1s unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxIdleTime` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxLifetime` | limit | state held, or a decision kept, forever | 5m min 1s unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxLifetime` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | 5 min 0 unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.MaxIdleConnections` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.MaxOpenConnections` | limit | unbounded concurrency or connections | 25 min 1 unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.MaxOpenConnections` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.RetryConfig.Backoff` | limit | a retry storm, or a retry that never comes back | 500ms doubling to 5s | `value:integrations/bunorm/mysql/v4.RetryConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.RetryConfig.MaxAttempts` | limit | a failure retried forever | 3 min 1 | `value:integrations/bunorm/mysql/v4.RetryConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.TimeoutConfig.ConnectTimeout` | limit | a call or a wait that never returns | 10s min 1ms | `value:integrations/bunorm/mysql/v4.TimeoutConfig.ConnectTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.TimeoutConfig.ReadTimeout` | limit | a call or a wait that never returns | 30s min 1ms unlimited admitted | `value:integrations/bunorm/mysql/v4.TimeoutConfig.ReadTimeout` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.TimeoutConfig.WriteTimeout` | limit | a call or a wait that never returns | 30s min 1ms unlimited admitted | `value:integrations/bunorm/mysql/v4.TimeoutConfig.WriteTimeout` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.ProviderConfig.Insecure` | protection | credentials and rows crossing the network in clear, or an unverified server | a verifying TLS session | `off:integrations/bunorm/mysql/v4.ProviderConfig.Insecure`, `value:integrations/bunorm/mysql/v4.ProviderConfig.TlsConfig` | the two refused together | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |

## `integrations/bunorm/pgsql/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/pgsql/v4.LockerConfig.ReleaseTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/bunorm/pgsql/v4.LockerConfig.ReleaseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxIdleTime` | limit | state held, or a decision kept, forever | 1m min 1s unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxIdleTime` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxLifetime` | limit | state held, or a decision kept, forever | 5m min 1s unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxLifetime` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | 25 min 0 unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.MaxIdleConnections` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.MaxOpenConnections` | limit | unbounded concurrency or connections | 50 min 1 unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.MaxOpenConnections` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.RetryConfig.Backoff` | limit | a retry storm, or a retry that never comes back | 500ms doubling to 5s | `value:integrations/bunorm/pgsql/v4.RetryConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.RetryConfig.MaxAttempts` | limit | a failure retried forever | 3 min 1 | `value:integrations/bunorm/pgsql/v4.RetryConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.ConnectTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.ConnectTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.ReadTimeout` | limit | a call or a wait that never returns | 30s min 1ms unlimited admitted | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.ReadTimeout` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.WriteTimeout` | limit | a call or a wait that never returns | 30s min 1ms unlimited admitted | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.WriteTimeout` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.ProviderConfig.Insecure` | protection | credentials and rows crossing the network in clear, or an unverified server | a verifying TLS session | `off:integrations/bunorm/pgsql/v4.ProviderConfig.Insecure`, `value:integrations/bunorm/pgsql/v4.ProviderConfig.TlsConfig` | the two refused together | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |

## `integrations/bunorm/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4.SplitterConfig.ReplicaRetryInterval` | limit | a loop that spins or never runs | 5s min 1ms | `value:integrations/bunorm/v4.SplitterConfig.ReplicaRetryInterval` | class T: an interval; unlimited never tries again a replica that came back | [integrations/bunorm/v4/README.md](../../integrations/bunorm/v4/README.md) |
| `integrations/bunorm/v4.SplitterConfig` | protection | reads failing while the primary still answers | fall back to the primary | `off:integrations/bunorm/v4.SplitterConfig.PrimaryFallbackDisabled` | — | [integrations/bunorm/v4/README.md](../../integrations/bunorm/v4/README.md) |

## `integrations/bunorm/v4/audit`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4/audit.AsyncStorageConfig.BufferSize` | limit | unbounded memory or work | 1024 min 1 | `value:integrations/bunorm/v4/audit.AsyncStorageConfig.BufferSize` | class T: a size; unlimited is memory or work that only grows | [integrations/bunorm/v4/audit/README.md](../../integrations/bunorm/v4/audit/README.md) |
| `integrations/bunorm/v4/audit.DefaultFileStorageFileMode` | mode | an audit trail readable by every local user | 0640 | `value:integrations/bunorm/v4/audit.FileStorageConfig.FileMode` | — | [integrations/bunorm/v4/audit/README.md](../../integrations/bunorm/v4/audit/README.md) |
| `integrations/bunorm/v4/audit.RegistryConfig` | redaction | an encrypted column's plaintext in the audit trail | masked | `off:integrations/bunorm/v4/audit.RegistryConfig.EncryptedColumnsUnmasked` | class R: per registry rather than per entity, so the door is not set once and forgotten on the entity that holds the card numbers; a field tagged redact stays masked | [integrations/bunorm/v4/audit/README.md](../../integrations/bunorm/v4/audit/README.md) |

## `integrations/bunorm/v4/encrypt`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4/encrypt.TableSpecification.BatchSize` | limit | unbounded memory or work | 500 min 1 | `value:integrations/bunorm/v4/encrypt.TableSpecification.BatchSize` | class T: a batch; unlimited is one call over the whole set | [integrations/bunorm/v4/encrypt/README.md](../../integrations/bunorm/v4/encrypt/README.md) |
| `integrations/bunorm/v4/encrypt.ColumnSealBinding.UnboundAdmitted` | protection | a sealed value copied from one row or column into another and decrypted there | a seal bound to its row and column, an unbound one refused | `off:integrations/bunorm/v4/encrypt.ColumnSealBinding.UnboundAdmitted` | — | [integrations/bunorm/v4/encrypt/README.md](../../integrations/bunorm/v4/encrypt/README.md) |
| `integrations/bunorm/v4/encrypt.SealBindingConfig.UnboundAdmitted` | protection | a sealed value copied from one row or column into another and decrypted there | a seal bound to its row and column, an unbound one refused | `off:integrations/bunorm/v4/encrypt.SealBindingConfig.UnboundAdmitted` | — | [integrations/bunorm/v4/encrypt/README.md](../../integrations/bunorm/v4/encrypt/README.md) |
| `integrations/bunorm/v4/encrypt.NewFakeCipher` | protection | an encrypted column written in clear | a write without a cipher refused | `off:integrations/bunorm/v4/encrypt.NewFakeCipher` | an identity cipher for tests and local development: the module refuses it at registration outside MELODY_ENV=dev, naming the field | [integrations/bunorm/v4/README.md](../../integrations/bunorm/v4/README.md) |
| `integrations/bunorm/v4/encrypt.EncryptDatabaseModeFlagName` | protection | the encrypted columns of a table rewritten in clear | encrypt | `off:integrations/bunorm/v4/encrypt.EncryptDatabaseModeDecrypt` | the decrypt mode is the way out of an encrypted column, named on the command line of each run | [integrations/bunorm/v4/README.md](../../integrations/bunorm/v4/README.md) |

## `integrations/cron/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/cron/v4.EntryConfig.Instances` | limit | unbounded concurrency or connections | 1 min 1 | `value:integrations/cron/v4.EntryConfig.Instances` | class T: a count of manifest lines; unlimited is a manifest without end | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.EntryConfig.ShutdownTimeout` | limit | a call or a wait that never returns | inherited: RunnerConfig.ShutdownTimeout, 5m; min 1ms | `value:integrations/cron/v4.EntryConfig.ShutdownTimeout` | class T: a timeout; an unlimited drain is a stop that never ends | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.EntryConfig.TickDedup.Window` | limit | state held, or a decision kept, forever | inherited: RunnerConfig.TickDedup.Window, 5m; min 1s | `value:integrations/cron/v4.EntryConfig.TickDedup.Window` | class T: a lifetime; an unlimited window deduplicates every later tick of the entry too | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.EntryConfig.Timeout` | limit | a call or a wait that never returns | inherited: RunnerConfig.Timeout, 1h; min 1ms unlimited admitted | `value:integrations/cron/v4.EntryConfig.Timeout` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.ClockJumpResetThreshold` | limit | a clock jump replaying or skipping the schedule | 3h min 2m | `value:integrations/cron/v4.RunnerConfig.ClockJumpResetThreshold` | class T: a threshold; unlimited never treats a jump of the clock as one, so a jump replays or skips the schedule it crosses | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.OutputCaptureLimit` | limit | unbounded memory or work | 64 * 1024 min 1 | `value:integrations/cron/v4.RunnerConfig.OutputCaptureLimit` | class T: a size; unlimited is memory or work that only grows | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.ShutdownTimeout` | limit | a call or a wait that never returns | 5m min 1ms | `value:integrations/cron/v4.RunnerConfig.ShutdownTimeout` | class T: a timeout; an unlimited drain is a stop that never ends | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.TickDedup.CallTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/cron/v4.RunnerConfig.TickDedup.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.TickDedup.Window` | limit | state held, or a decision kept, forever | 5m min 1s | `value:integrations/cron/v4.RunnerConfig.TickDedup.Window` | class T: a lifetime; an unlimited window deduplicates every later tick of the entry too | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.Timeout` | limit | a call or a wait that never returns | 1h min 1ms unlimited admitted | `value:integrations/cron/v4.RunnerConfig.Timeout` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.DefaultGeneratorDirectoryMode` | mode | a crontab or logs directory writable beyond its owner | 0755 | `value:integrations/cron/v4.GeneratorConfig.DirectoryMode` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.judgeTheLogsRoot` | protection | a root-run generator leaving a logs directory the entries' users cannot write, so the job never runs | the run refused before anything is written | `off:integrations/cron/v4.GeneratorConfig.LogsDirectoryCheckDisabled` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.entryLogPath` | refusal | an entry's log file written outside the logs directory | a LogFileName escaping it refused | none | the logs directory is the one root the generator creates and hands to the entries' users; a log elsewhere is a path the operator owns, named by --logs-directory or by an entry's command | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.ErrBusyboxDivergentDaySchedule` | refusal | a day-field pair busybox crond runs on another schedule | refused under the user-less dialect | `use:integrations/cron/v4.TemplateNameCrontab` | the user-less dialect is defined as the one busybox runs as written; a host running vixie crond with user crontabs renders the pair through the crontab dialect | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.EntryConfig.LogDisabled` | switch | an entry's output lost, or a run that needs no log refused for want of a logs directory | the output redirected to its log file under the logs directory | `off:integrations/cron/v4.EntryConfig.LogDisabled` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.EntryTickDedupConfig.Disabled` | protection | one tick run on every replica that evaluates it | one replica runs the tick when a locker is configured or registered | `off:integrations/cron/v4.EntryTickDedupConfig.Disabled` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.TickDedupConfig.Disabled` | protection | one tick run on every replica that evaluates it | one replica runs the tick when a locker is configured or registered; with none nothing is deduplicated and Run warns once | `off:integrations/cron/v4.TickDedupConfig.Disabled` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.TickDedupConfig` | protection | a tick run twice over a locker without a lease | deduplicated when a locker is configured or registered; with none nothing is deduplicated and Run warns once | none | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.NewRunnerCommand` | protection | a cron User the in-process runner cannot honour | an entry naming one refused | `off:integrations/cron/v4.RunnerConfig.EntryUserIgnored` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.TickDedupConfig.FailureMode` | protection | one tick run on every replica during a locker outage | skip: the tick not run, reported as the run's failure | `off:integrations/cron/v4.TickDedupFailureRun`, `value:integrations/cron/v4.TickDedupConfig.FailureMode`, `value:integrations/cron/v4.EntryTickDedupConfig.FailureMode` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.atomicWriteFile` | constant | a crontab cron refuses, or one others can write | 0644, an existing file keeps its own | none | cron ignores a group- or world-writable crontab, and the generated file holds no secret | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.pruneStaleDestinations` | protection | a file the generator did not write emptied | nothing pruned; under the prune flag only a file carrying the generator's ownership line in its leading lines | `off:integrations/cron/v4.PruneFlagName` | emptying is irreversible, so it is opt-in by the flag, and the ownership check has no door | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |

## `integrations/opentelemetry/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/opentelemetry/v4.routeLabel` | protection | a metric label per raw path or per invented method, growing the series without bound | the route pattern and a normalised method | none | a label set a client can widen is an unbounded series count on the collector | [integrations/opentelemetry/v4/README.md](../../integrations/opentelemetry/v4/README.md) |

## `integrations/opentelemetry/v4/otlp`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/opentelemetry/v4/otlp.Config.BatchTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/opentelemetry/v4/otlp.Config.BatchTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/opentelemetry/v4/README.md](../../integrations/opentelemetry/v4/README.md) |
| `integrations/opentelemetry/v4/otlp.Config.Insecure` | protection | spans in clear to a non-loopback collector | transport security on | `off:integrations/opentelemetry/v4/otlp.Config.Insecure` | — | [integrations/opentelemetry/v4/README.md](../../integrations/opentelemetry/v4/README.md) |

## `integrations/outbox/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/outbox/v4.RelayCommandConfig.ErrorBackoff` | limit | a retry storm, or a retry that never comes back | 1ms doubling to 1m | `value:integrations/outbox/v4.RelayCommandConfig.ErrorBackoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.Backoff` | limit | a retry storm, or a retry that never comes back | 15s doubling to 10m | `value:integrations/outbox/v4.RelayConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.BatchSize` | limit | unbounded memory or work | 100 min 1 max 100_000 | `value:integrations/outbox/v4.RelayConfig.BatchSize` | at most 100000: the batch sizes a slice allocated before the claim; unlimited would claim the whole table into one slice and one transaction | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.LockTtl` | limit | state held, or a decision kept, forever | 30s min 1s | `value:integrations/outbox/v4.RelayConfig.LockTtl` | class T: a lifetime; a lock that outlives a crashed holder forever stops every relay | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.MaxAttempts` | limit | a failure retried forever | 12 min 1 | `value:integrations/outbox/v4.RelayConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.MaxDeliveryAttempts` | limit | a poison row redelivered forever | derived: 2 x the resolved MaxAttempts, 24; min 1, at or below MaxAttempts refused | `value:integrations/outbox/v4.RelayConfig.MaxDeliveryAttempts` | class T: a retry count; unlimited redelivers a poison row forever | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.ResolutionTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/outbox/v4.RelayConfig.ResolutionTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.UnavailableBackoff` | limit | a retry storm, or a retry that never comes back | 15s doubling to 1m | `value:integrations/outbox/v4.RelayConfig.UnavailableBackoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.VisibilityTimeout` | limit | a call or a wait that never returns | 5m min 1s | `value:integrations/outbox/v4.RelayConfig.VisibilityTimeout` | class T: a lifetime; a row hidden forever is never recovered from a claimer that crashed | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.Relay.holdUnreached` | refusal | a row delivered ahead of an older one that failed | the first failed send ends the batch, the rows behind it held | none | ordering is the contract within one relay's batch: the relay delivers each row of its batch after the rows written before it, so a row past a failure would overtake it; across relay instances only under a Locker, since two relays without one claim disjoint batches | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |

## `integrations/rueidis/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/rueidis/v4.ClientConfig.BootPingTimeout` | limit | a call or a wait that never returns | 3s min 1ms | `value:integrations/rueidis/v4.ClientConfig.BootPingTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.ClientSideCacheBytes` | limit | unbounded memory or work | 128 MiB min 1 | `value:integrations/rueidis/v4.ClientConfig.ClientSideCacheBytes` | class T: a size; unlimited is memory or work that only grows | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.ConnectionWriteTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/rueidis/v4.ClientConfig.ConnectionWriteTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.DialTimeout` | limit | a call or a wait that never returns | 5s min 1ms | `value:integrations/rueidis/v4.ClientConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ReconnectConfig.Backoff` | limit | a retry storm, or a retry that never comes back | 1s doubling to 30s | `value:integrations/rueidis/v4.ReconnectConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.RetryConfig.Backoff` | limit | a retry storm, or a retry that never comes back | 500ms doubling to 5s | `value:integrations/rueidis/v4.RetryConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.RetryConfig.MaxAttempts` | limit | a failure retried forever | 3 min 1 | `value:integrations/rueidis/v4.RetryConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.TokenStoreConfig.MaximumClockSkew` | limit | a clock difference widening what a credential admits | 0s min 0s | `value:integrations/rueidis/v4.TokenStoreConfig.MaximumClockSkew` | class T: a skew; unlimited admits a credential whatever its time | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.TokenStoreConfig.RevocationEpochRetention` | limit | state held, or a decision kept, forever | 168h min 1s unlimited admitted | `value:integrations/rueidis/v4.TokenStoreConfig.RevocationEpochRetention` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.TokenStoreConfig.ScanCount` | limit | unbounded memory or work | 256 min 1 | `value:integrations/rueidis/v4.TokenStoreConfig.ScanCount` | class T: a batch; unlimited is one call over the whole set | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.callTimeoutRule#1` | limit | a call or a wait that never returns | 1s (LockerConfig, NonceGuardConfig, ServerSentEventBackplaneConfig, TokenStoreConfig), 250ms (RateLimiterConfig); min 1ms | `value:integrations/rueidis/v4.callTimeoutRule`, `value:integrations/rueidis/v4.LockerConfig.CallTimeout`, `value:integrations/rueidis/v4.NonceGuardConfig.CallTimeout`, `value:integrations/rueidis/v4.RateLimiterConfig.CallTimeout`, `value:integrations/rueidis/v4.ServerSentEventBackplaneConfig.CallTimeout`, `value:integrations/rueidis/v4.TokenStoreConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.BootPingDisabled` | switch | an unreachable store discovered at the first request instead of at Open | Open pings the store and fails when it does not answer | `off:integrations/rueidis/v4.ClientConfig.BootPingDisabled` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.LockerConfig.KeyPrefixDisabled` | protection | a lock name colliding with another key of the store | every lock key prefixed | `off:integrations/rueidis/v4.LockerConfig.KeyPrefixDisabled` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.RateLimiterConfig.FailureMode` | protection | a store outage lifting the limit on sign-in and one-time-code routes | closed: denied | `off:integrations/rueidis/v4.FailureModeOpen`, `value:integrations/rueidis/v4.RateLimiterConfig.FailureMode` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.TlsConfig` | protection | commands and values crossing the network in clear | none: plaintext unless a TlsConfig is given | `value:integrations/rueidis/v4.ClientConfig.TlsConfig` | the store is most often reached on a private network, so no transport security is assumed; an address off this machine dialled without it is named once per address by the warning rueidis.plaintextAddress | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |

## `integrations/rueidis/v4/cache`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/rueidis/v4/cache.BackendConfig.CallTimeout` | limit | a call or a wait that never returns | 1s min 1ms | `value:integrations/rueidis/v4/cache.BackendConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4/cache.BackendConfig.DeleteBatch` | limit | unbounded memory or work | 500 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.DeleteBatch` | class T: a batch; unlimited is one call over the whole set | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4/cache.BackendConfig.MaxKeyLength` | limit | unbounded memory or work | 1024 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.MaxKeyLength` | class T: a size; unlimited is memory or work that only grows | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4/cache.BackendConfig.ScanCount` | limit | unbounded memory or work | 1000 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.ScanCount` | class T: a batch; unlimited is one call over the whole set | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |

## `integrations/websocket/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/websocket/v4.StreamHandlerConfig.CloseHandshakeGrace` | limit | a call or a wait that never returns | 1s min 1ms | `value:integrations/websocket/v4.StreamHandlerConfig.CloseHandshakeGrace` | class T: a timeout; unlimited is the wait that never returns | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.StreamHandlerConfig.IdleTimeout` | limit | a call or a wait that never returns | 30s min 1s | `value:integrations/websocket/v4.StreamHandlerConfig.IdleTimeout` | class T: a timeout; unlimited holds an idle socket and its goroutines forever | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.StreamHandlerConfig.ReadLimit` | limit | unbounded memory or work | 32768 min 1 unlimited admitted | `value:integrations/websocket/v4.StreamHandlerConfig.ReadLimit` | — | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.StreamHandlerConfig.WriteTimeout` | limit | a call or a wait that never returns | 10s min 1ms | `value:integrations/websocket/v4.StreamHandlerConfig.WriteTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.StreamHandlerConfig.OriginPatterns` | protection | a page of another origin opening a socket that rides the visitor's session cookie | same origin only | `value:integrations/websocket/v4.StreamHandlerConfig.OriginPatterns` | a pattern list; `*` admits every origin | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.maximumCallbackGraceFactor` | constant | a callback that never returns excusing every ping and leaking the connection, its subscription and three goroutines | 10 ping intervals, then the connection closed | none | a callback running past ten ping intervals is stuck, not slow; IdleTimeout, the ping interval, is the tunable figure | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |

## Boot warnings

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `application.environment.processVariableIgnored` | warning | a process variable the operator believes is read | written | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `application.service.builtAtBootFailed` | warning | a service that failed its boot build going unnoticed | written in http | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `aws.s3.plaintextEndpoint` | warning | object traffic in clear to a non-loopback endpoint | written | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `amqp.plaintextDsn` | warning | messages and credentials in clear to a non-loopback broker | written | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `rueidis.plaintextAddress` | warning | commands, values and the password in clear to a non-loopback store | written | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `http.session.unboundedTtlOnBoundedDefaultStorage` | warning | sessions without end on a storage that evicts | written in http | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `http.static.cacheMaxAgeIgnored` | warning | a cache max age the operator believes is applied | written in http | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `messagebus.retryPolicyServiceIgnored` | warning | a retry policy service the operator believes is used | written | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `opentelemetry.otlp.insecureCollector` | warning | spans and the exporter headers in clear to a non-loopback collector | written | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [integrations/opentelemetry/v4/README.md](../../integrations/opentelemetry/v4/README.md) |
| `security.csrfDisabledOnLoginFirewall` | warning | a login firewall without its forgery check | written in http | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `security.firewallDoorReachUnknown` | warning | a firewall door whose reach the compiler cannot prove | written | `env:MELODY_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
