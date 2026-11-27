# Defaults

Every protective default of the fourth major, every limit and every door that turns one off or widens it, one row each, for the framework and for every integration of the line. A row is a promise the code keeps: `.dev/validate/defaults.sh` reads every module with its types and refuses a default without a row, a row naming nothing, and a door no row names.

## The row

| column | holds |
|---|---|
| key | the row's identity and its anchor in the code: `<package path>.<Symbol>`, `<package path>.<Type>.<Field>`, the name a `bound.Rule` is resolved under prefixed by its package, a boot-warning code, or `<package path>.<Function>#<n>` for the n-th site of a function |
| kind | `limit` (a `bound.Rule`), `protection` (a default with behaviour), `refusal` (a refusal without a door, a row of its own), `redaction`, `constant` (a fixed figure), `mode` (a file or directory permission), `warning` (a boot-warning code), `folded` (the plain input validation of a package) |
| protects against | what goes wrong without the default |
| default | what applies when nothing is configured |
| door | each anchor with its class: `off:` turns the default off, `value:` sets the value (a large value is the door of a limit), `use:` is a sibling for one use, `env:` is a key; or `none` |
| reason | why the door is what it is; required when the door is `none` and on a limit that refuses unlimited, where a class reference is admitted unless the rule clamps at a maximum |
| documented | the page that names the door |

An `env:` door sets an operational value. It never turns a protection off: on a `protection` row it is admitted only where the reason says the key hardens the default, as `MELODY_HTTP_SESSION_COOKIE_SECURE` does; a protection is turned off in Go.

## The classes

- **class T** — a limit that is tunable and never unlimited: the large value is the door, because an unlimited value is the failure the limit exists for (a wait that never returns, a table that only grows).
- **class R** — a redaction: the journal, the debug output and the audit trail never carry a secret in clear; the door, where there is one, is a flag of a development command or a field of the registry, and the exemption of the journal is written once in [LOGGING.md](package/LOGGING.md#redaction-and-escaping).
- **class K** — a constant that bounds a diagnostic or a cycle guard: a figure no deployment tunes, written here with its reason.

## What the band reads, and what it cannot

Enumerated from the code: every `bound.Rule` and `bound.BackoffRule` literal, every boot-warning code the framework declares and every one a module declares through `DeclareBootWarning`, every `fs.FileMode` constant and every constant mode handed to `os.MkdirAll`, `os.Mkdir`, `os.OpenFile`, `os.WriteFile`, `os.Chmod` or their `os.Root` twins, every env key the framework or a module declares, and every exported `*Disabled`, `*Admitted` or `Allow*` bool and every `bound` limit field of a configuration struct. A row of another kind is written by hand; the band keeps it alive by resolving its anchors, and its completeness is owed to the review before a release: no mechanism can enumerate a behaviour that has no rule, no field and no mode, such as the `HttpOnly` of the session cookie.

A failure the tree still carries is held by a line of `.dev/validate/defaults.baseline`, with its reason; a line that holds nothing fails the band.

## `v4/application`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/application.cacheDirectoryMode` | mode | a cache directory readable by every local user | 0755 | none | — | — |
| `v4/application.Application.bootCli` | protection | debug commands outside development | development only | none | — | — |

## `v4/cache`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cache.renderedCacheKey` | constant | a cache key journaled whole | 128 | none | — | — |
| `v4/cache.InMemoryBackendConfig.CleanupInterval` | limit | a loop that spins or never runs | time.Minute min bound.TimeoutMinimum | `value:v4/cache.InMemoryBackendConfig.CleanupInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [CACHE.md](package/CACHE.md) |
| `v4/cache.InMemoryBackendConfig.MaxItems` | limit | unbounded memory or work | 10_000 min 1 unlimited admitted | `value:v4/cache.InMemoryBackendConfig.MaxItems` | — | [CACHE.md](package/CACHE.md) |
| `v4/cache.RememberConfig.WaitTimeout` | limit | a call or a wait that never returns | 30 * time.Second min 0 unlimited admitted | `value:v4/cache.RememberConfig.WaitTimeout` | — | [CACHE.md](package/CACHE.md) |
| `v4/cache.RememberConfig.StampedeProtectionDisabled` | protection | the door: runs the callback on every miss in the caller's goroutine, under the caller's runtime, with no flight to join. | on | `off:v4/cache.RememberConfig.StampedeProtectionDisabled` | — | [CACHE.md](package/CACHE.md) |

## `v4/cache/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cache/contract.ttl` | limit | state held, or a decision kept, forever | min bound.TtlMinimum unlimited admitted | `value:v4/cache/contract.TtlRule`, `value:v4/cache.RememberConfig.Ttl`, `value:v4/cache/contract.CounterOptions.Ttl` | — | [CACHE.md](package/CACHE.md) |

## `v4/cli`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cli.cliErrorLineCauseChainDepth` | constant | an exit line without end | 8 | none | — | — |

## `v4/cli/output`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cli/output.Option.Limit` | limit | unbounded memory or work | default unlimited min 0 | `value:v4/cli/output.Option.Limit`, `value:v4/cli/output.ListPayloadConfig.Limit` | — | [CLI.md](package/CLI.md) |
| `v4/cli/output.Option.Offset` | limit | a negative or runaway offset | 0 min 0 | `value:v4/cli/output.Option.Offset`, `value:v4/cli/output.ListPayloadConfig.Offset` | class T: a position, not a size: an offset has no unlimited | [CLI.md](package/CLI.md) |
| `v4/cli/output.Option.TableMaxWidth` | limit | unbounded memory or work | defaultTableMaxWidth min 1 unlimited admitted | `value:v4/cli/output.Option.TableMaxWidth` | — | [CLI.md](package/CLI.md) |
| `v4/cli/output.TablePrinterConfig.MaxWidth` | limit | unbounded memory or work | defaultTableMaxWidth min 1 unlimited admitted | `value:v4/cli/output.TablePrinterConfig.MaxWidth` | — | [CLI.md](package/CLI.md) |

## `v4/config`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/config.ApplicationConfig.HttpKernel.UnavailableRetryAfter` | limit | a client retrying at once, or never | DefaultUnavailableRetryAfter min bound.TtlMinimum max maximumUnavailableRetryAfter | `value:v4/application.ApplicationConfig.HttpKernel.UnavailableRetryAfter`, `env:MELODY_HTTP_UNAVAILABLE_RETRY_AFTER`, `value:v4/http.ExceptionRenderConfig.UnavailableRetryAfter`, `value:v4/http.KernelExceptionEventConfig.UnavailableRetryAfter`, `value:v4/security.CompiledFirewallConfig.UnavailableRetryAfter`, `value:v4/security/config.GlobalConfig.UnavailableRetryAfter`, `value:v4/security/config.FirewallOverride.UnavailableRetryAfter` | 1s to 1h in whole seconds: Retry-After is whole seconds, and a hint past an hour tells a client to stay away from an outage the instance recovers from in seconds; unlimited is a client told never to retry | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.IdleTimeout` | limit | a call or a wait that never returns | DefaultHttpIdleTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.IdleTimeout`, `env:MELODY_HTTP_IDLE_TIMEOUT` | class T: a timeout; unlimited holds every idle keep-alive connection forever | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.MaxHeaderBytes` | limit | unbounded memory or work | DefaultHttpMaxHeaderBytes min 4 << 10 | `value:v4/application.ApplicationConfig.HttpServer.MaxHeaderBytes`, `env:MELODY_HTTP_MAX_HEADER_BYTES` | class T: a size; unlimited is memory or work that only grows | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.ReadHeaderTimeout` | limit | a call or a wait that never returns | DefaultHttpReadHeaderTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.ReadHeaderTimeout`, `env:MELODY_HTTP_READ_HEADER_TIMEOUT` | class T: a timeout; unlimited is the wait that never returns | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.ReadTimeout` | limit | a call or a wait that never returns | DefaultHttpReadTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.ReadTimeout`, `env:MELODY_HTTP_READ_TIMEOUT` | class T: a timeout; unlimited is the wait that never returns | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.ShutdownTimeout` | limit | a call or a wait that never returns | DefaultHttpShutdownTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.ShutdownTimeout`, `env:MELODY_HTTP_SHUTDOWN_TIMEOUT` | class T: a timeout; an unlimited drain is a stop that never ends, one request that never finishes holding the process past every supervisor's grace | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.HttpServer.WriteTimeout` | limit | a call or a wait that never returns | DefaultHttpWriteTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.WriteTimeout`, `env:MELODY_HTTP_WRITE_TIMEOUT` | class T: a timeout; unlimited is the wait that never returns | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.ApplicationConfig.TeardownTimeout` | limit | a call or a wait that never returns | DefaultTeardownTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/application.ApplicationConfig.TeardownTimeout`, `env:MELODY_TEARDOWN_TIMEOUT` | — | [APPLICATION.md](package/APPLICATION.md) |
| `v4/config.HttpConfiguration.MaxRequestBodyBytes` | limit | unbounded memory or work | DefaultHttpMaxRequestBodyBytes min 1 | `value:v4/config/contract.HttpConfiguration.MaxRequestBodyBytes`, `env:MELODY_HTTP_MAX_REQUEST_BODY_BYTES` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/config.HttpConfiguration.SessionMaxSessions` | limit | unbounded memory or work | DefaultSessionMaxSessions min 1 unlimited admitted | `value:v4/config/contract.HttpConfiguration.SessionMaxSessions`, `env:MELODY_HTTP_SESSION_MAX_SESSIONS` | — | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.SessionStorageTimeout` | limit | a call or a wait that never returns | DefaultSessionStorageTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/config/contract.HttpConfiguration.SessionStorageTimeout`, `env:MELODY_HTTP_SESSION_STORAGE_TIMEOUT` | — | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.SessionTombstoneRetention` | limit | state held, or a decision kept, forever | DefaultSessionTombstoneRetention min bound.TtlMinimum | `value:v4/config/contract.HttpConfiguration.SessionTombstoneRetention`, `env:MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` | class T: a lifetime; unlimited keeps the state forever | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.SessionTtl` | limit | state held, or a decision kept, forever | DefaultSessionTtl min bound.TtlMinimum unlimited admitted | `value:v4/config/contract.HttpConfiguration.SessionTtl`, `env:MELODY_HTTP_SESSION_TTL` | — | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpConfiguration.StaticCacheMaxAge` | limit | a response cached for good | DefaultStaticCacheMaxAge min 0 | `value:v4/config/contract.HttpConfiguration.StaticCacheMaxAge`, `env:MELODY_STATIC_CACHE_MAX_AGE` | class T: a cache lifetime; unlimited pins a response in every cache for good | [CONFIG.md](package/CONFIG.md) |
| `v4/config.HttpSessionCookieSecureAlways` | protection | a session cookie sent in clear | from the scheme | `use:v4/http/contract.SessionCookiePolicy.Secure`, `env:MELODY_HTTP_SESSION_COOKIE_SECURE` | the key only hardens it: always is the one value it admits, and the Go field lowers the policy | [HTTP.md](package/HTTP.md) |
| `v4/config.kernelConfiguration.validateEnvironment` | refusal | an environment name outside the closed list | refused | none | — | — |

## `v4/container`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/container.closeFailureStackLimit` | constant | a close failure journaling a whole stack | 2048 bytes | none | — | — |
| `v4/container.teardownWalkDepthLimit` | constant | a teardown walk without end | 12 | none | — | — |
| `v4/container.teardownWalkElementLimit` | constant | a teardown walk without end | 256 | none | — | — |
| `v4/container.teardownWalkNodeBudget` | constant | a teardown walk without end | 65536 | none | — | — |
| `v4/container.ContainerConfig.CreationWaiters` | limit | a call or a wait that never returns | DefaultCreationWaiters min 1 unlimited admitted | `value:v4/container.ContainerConfig.CreationWaiters`, `value:v4/application.ApplicationConfig.ContainerCreationWaiters`, `env:MELODY_CONTAINER_CREATION_WAITERS` | — | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container.ContainerConfig.ScopeTeardownTimeout` | limit | a call or a wait that never returns | DefaultScopeTeardownTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/container.ContainerConfig.ScopeTeardownTimeout`, `value:v4/application.ApplicationConfig.ScopeTeardownTimeout`, `env:MELODY_SCOPE_TEARDOWN_TIMEOUT` | — | [CONTAINER.md](package/CONTAINER.md) |

## `v4/container/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/container/contract.RegisterOptions.TeardownReflectionDisabled` | protection | the door: keeps the teardown's walk out of this service's memory, for a service whose own goroutines rewrite its pointer fields | on | `off:v4/container/contract.RegisterOptions.TeardownReflectionDisabled` | — | [CONTAINER.md](package/CONTAINER.md) |
| `v4/container/contract.RegisterOptions.TypeRegistrationDisabled` | protection | the door: keeps the service off the by-type index, so it is resolved by name alone. | on | `off:v4/container/contract.RegisterOptions.TypeRegistrationDisabled` | — | [CONTAINER.md](package/CONTAINER.md) |

## `v4/debug`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/debug.errorCauseChainDepth` | constant | a debug error without end | 9 | none | — | — |
| `v4/debug.ParameterCommand.Flags` | redaction | a secret parameter printed by debug:parameters | masked | `off:v4/debug.ParameterCommandRevealFlagName` | class R: the flag reveals in the development environment only and is refused elsewhere naming the flag, so a deployed process never prints a credential | [DEBUG.md](package/DEBUG.md) |

## `v4/http`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http.RequestConfig.MaxBodyBytes` | limit | unbounded memory or work | min 1 | `value:v4/http.RequestConfig.MaxBodyBytes` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.RouteOptions.Protection.MaxBodyBytes` | limit | unbounded memory or work | min 1 | `value:v4/http.RouteOptions.Protection.MaxBodyBytes` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.ServerSentEventHubConfig.SubscriberBuffer` | limit | unbounded memory or work | defaultServerSentEventBufferSize min 1 | `value:v4/http.ServerSentEventHubConfig.SubscriberBuffer` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.ServerSentEventWriterConfig.WriteBudget` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:v4/http.ServerSentEventWriterConfig.WriteBudget` | — | [HTTP.md](package/HTTP.md) |
| `v4/http.SubscribeOptions.Buffer` | limit | unbounded memory or work | bufferSize min 1 | `value:v4/http.SubscribeOptions.Buffer` | class T: a size; unlimited is memory or work that only grows | [HTTP.md](package/HTTP.md) |
| `v4/http.RegisterKernelTerminateAccessLogListener` | protection | a request leaving no access line | on, always | none | — | — |
| `v4/http.sessionCookie` | protection | a session cookie readable by a script | HttpOnly, always | none | — | — |

## `v4/http/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/contract.MethodPolicy.AutomaticOptionsDisabled` | protection | the door: AutomaticOptionsDisabled | on | `off:v4/http/contract.MethodPolicy.AutomaticOptionsDisabled` | — | [HTTP.md](package/HTTP.md) |
| `v4/http/contract.MethodPolicy.HeadFallbackDisabled` | protection | the door: HeadFallbackDisabled | on | `off:v4/http/contract.MethodPolicy.HeadFallbackDisabled` | — | [HTTP.md](package/HTTP.md) |
| `v4/http/contract.RouteProtectionConfig.CsrfDisabled` | protection | the door: exempts the route from its firewall's cross-site request forgery check, for a handler that verifies the token itself | on | `off:v4/http/contract.RouteProtectionConfig.CsrfDisabled` | — | [HTTP.md](package/HTTP.md) |

## `v4/http/cors`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/cors.Config.MaxAge` | limit | a preflight answer cached for good | 86400 min 0 | `value:v4/http/cors.Config.MaxAge` | class T: a cache lifetime; unlimited pins a response in every cache for good | [HTTP.md](package/HTTP.md) |
| `v4/http/cors.Config.AllowCredentials` | protection | the door: AllowCredentials | refused | `off:v4/http/cors.Config.AllowCredentials` | — | [HTTP.md](package/HTTP.md) |
| `v4/http/cors.NewService` | refusal | a scheme-less origin matching more than meant | refused when credentialed | none | — | [HTTP.md](package/HTTP.md) |

## `v4/http/middleware`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/middleware.CompressionConfig.MinSize` | limit | a response compressed for no gain | 1024 min 1 | `value:v4/http/middleware.CompressionConfig.MinSize` | class T: a threshold; unlimited compresses nothing, which is leaving the middleware out | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.LimiterConfig.CleanupInterval` | limit | a loop that spins or never runs | 5 * time.Minute min bound.TimeoutMinimum | `value:v4/http/middleware.LimiterConfig.CleanupInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.LimiterConfig.MaxKeys` | limit | unbounded memory or work | defaultMaxRateLimitKeys min 1 | `value:v4/http/middleware.LimiterConfig.MaxKeys` | class T: a size; unlimited is a table a client grows by varying its key | [HTTP.md](package/HTTP.md) |
| `v4/http/middleware.SecurityHeadersConfig.Disabled` | protection | the door: sends none of the headers. | on | `off:v4/http/middleware.SecurityHeadersConfig.Disabled` | — | [HTTP.md](package/HTTP.md) |

## `v4/http/middleware/pipeline`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/middleware/pipeline.HttpMiddlewareDefinition.AllowDuplicates` | protection | the door: admits another definition of the Name beside this one. | refused | `off:v4/http/middleware/pipeline.HttpMiddlewareDefinition.AllowDuplicates` | — | [HTTP.md](package/HTTP.md) |

## `v4/http/static`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/static.Config.CacheMaxAge` | limit | a response cached for good | 3600 min 0 | `value:v4/http/static.Config.CacheMaxAge` | class T: a cache lifetime; unlimited pins a response in every cache for good | [HTTP.md](package/HTTP.md) |
| `v4/http/static.Config.CacheDisabled` | protection | the door: sends no Cache-Control, ETag or Last-Modified, so no conditional request can be answered 304. | on | `off:v4/http/static.Config.CacheDisabled` | — | [HTTP.md](package/HTTP.md) |

## `v4/httpclient`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/httpclient.HttpClientConfig.MaxRedirects` | limit | a redirect loop followed forever | 10 min 1 | `value:v4/httpclient.HttpClientConfig.MaxRedirects` | class T: a redirect count; unlimited follows a redirect loop forever | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.HttpClientConfig.Timeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:v4/httpclient.HttpClientConfig.Timeout` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.RequestOptions.MaxResponseBodyBytes` | limit | unbounded memory or work | defaultMaxResponseBodyBytes min 1 | `value:v4/httpclient.RequestOptions.MaxResponseBodyBytes` | class T: a size; unlimited is memory or work that only grows | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.RequestOptions.Timeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum unlimited admitted | `value:v4/httpclient.RequestOptions.Timeout` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.DialTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:v4/httpclient.TransportConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.ExpectContinueTimeout` | limit | a call or a wait that never returns | 1 * time.Second min 0 | `value:v4/httpclient.TransportConfig.ExpectContinueTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.IdleConnectionTimeout` | limit | a call or a wait that never returns | 90 * time.Second min time.Second unlimited admitted | `value:v4/httpclient.TransportConfig.IdleConnectionTimeout` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.KeepAlive` | limit | a dead connection never noticed | 30 * time.Second min time.Second | `value:v4/httpclient.TransportConfig.KeepAlive` | class T: a probe interval; unlimited never notices a dead peer | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.MaxIdleConnectionsPerHost` | limit | unbounded concurrency or connections | defaultMaxIdleConnections min 1 unlimited admitted | `value:v4/httpclient.TransportConfig.MaxIdleConnectionsPerHost` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | defaultMaxIdleConnections min 1 unlimited admitted | `value:v4/httpclient.TransportConfig.MaxIdleConnections` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.ResponseHeaderTimeout` | limit | a call or a wait that never returns | 15 * time.Second min bound.TimeoutMinimum | `value:v4/httpclient.TransportConfig.ResponseHeaderTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.TlsHandshakeTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:v4/httpclient.TransportConfig.TlsHandshakeTimeout` | class T: a timeout; unlimited is the wait that never returns | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.HttpClientConfig.RedirectsDisabled` | protection | the door: makes the client answer a redirect as the response it is | on | `off:v4/httpclient.HttpClientConfig.RedirectsDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.IdleConnectionsDisabled` | protection | the door: keeps no connection idle, so every request dials its own over HTTP/1 and HTTP/2 alike, and refuses MaxIdleConnections, MaxIdleConnectionsPerHost or IdleConnectionTimeout set beside it. | on | `off:v4/httpclient.TransportConfig.IdleConnectionsDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.KeepAliveDisabled` | protection | the door: KeepAliveDisabled | on | `off:v4/httpclient.TransportConfig.KeepAliveDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |
| `v4/httpclient.TransportConfig.ProxyDisabled` | protection | the door: dials every request directly, whatever the environment says, and refuses a Proxy set beside it. | on | `off:v4/httpclient.TransportConfig.ProxyDisabled` | — | [HTTPCLIENT.md](package/HTTPCLIENT.md) |

## `v4/httpclient/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|

## `v4/internal`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/internal.maxDiagnosticTextLength` | constant | a diagnostic journaled whole | 512 | none | — | — |
| `v4/internal.JournalDirectoryModeFloor` | mode | a journal directory its owner cannot traverse | 0700 | none | the floor a configured journal directory mode may not go below: below it the owner cannot list or create the journal, so the mode is refused, not doored | — |
| `v4/internal.JournalFileModeFloor` | mode | a journal file its owner cannot write | 0600 | none | the floor a configured journal file mode may not go below: below it the owner cannot write the journal and every line is lost, so the mode is refused, not doored | — |
| `v4/internal.WriteFileAtomically#1` | mode | generated source in a directory the repository does not own | 0755 | none | generated Go source is committed: its directories and a new file take the repository's modes, an existing file keeps its own | — |
| `v4/internal.BootWarningNameEnvironmentUndeclaredKey` | refusal | an undeclared MELODY_ key, booting | warning | none | — | — |
| `v4/internal.BootWarningNameSecretMarkUnmatched` | refusal | a secret mark that protects no parameter, booting | warning | none | — | — |

## `v4/lock`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/lock.defaultSessionProbeInterval` | constant | a session lock whose loss goes unnoticed | 15s | none | — | — |
| `v4/lock.ExclusiveCommandConfig.Ttl` | limit | state held, or a decision kept, forever | exclusiveTtlRule.Default min exclusiveTtlRule.Minimum unlimited admitted | `value:v4/lock.ExclusiveCommandConfig.Ttl` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.ExclusiveConfig.Ttl` | limit | state held, or a decision kept, forever | lockcontract.LockTtlRule().Default min minimumLeaseTtl unlimited admitted | `value:v4/lock.ExclusiveConfig.Ttl` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.InMemoryLockerConfig.PurgeInterval` | limit | a loop that spins or never runs | DefaultInMemoryLockerPurgeInterval min 1 unlimited admitted | `value:v4/lock.InMemoryLockerConfig.PurgeInterval` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.MaxCampaignBackoff` | limit | a campaign that spins or never comes back | 1 * time.Minute min bound.TimeoutMinimum | `value:v4/lock.LeaderGateConfig.MaxCampaignBackoff` | class T: a backoff ceiling; unlimited is a candidate that never campaigns again | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.MaxConsecutiveRefreshFailures` | limit | unbounded memory or work | 3 min 1 unlimited admitted | `value:v4/lock.LeaderGateConfig.MaxConsecutiveRefreshFailures` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.RefreshInterval` | limit | a loop that spins or never runs | derivedRefreshInterval min minimumRefreshInterval | `value:v4/lock.LeaderGateConfig.RefreshInterval` | class T: an interval; unlimited never refreshes the lease, so the leadership lapses under a leader that still runs | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.RetryInterval` | limit | a loop that spins or never runs | derivedRetryInterval min bound.TimeoutMinimum | `value:v4/lock.LeaderGateConfig.RetryInterval` | class T: an interval; unlimited never campaigns again after a lost lease | [LOCK.md](package/LOCK.md) |
| `v4/lock.LeaderGateConfig.Ttl` | limit | state held, or a decision kept, forever | lockcontract.LockTtlRule().Default min minimumLeaseTtl unlimited admitted | `value:v4/lock.LeaderGateConfig.Ttl` | — | [LOCK.md](package/LOCK.md) |
| `v4/lock.stopRenewalLimitRule#1` | limit | unbounded memory or work | defaultStopRenewalLimit min bound.TimeoutMinimum unlimited admitted | `value:v4/lock.stopRenewalLimitRule`, `value:v4/lock.ExclusiveCommandConfig.StopRenewalLimit`, `value:v4/lock.ExclusiveConfig.StopRenewalLimit`, `value:v4/lock.LeaderGateConfig.StopRenewalLimit` | — | [LOCK.md](package/LOCK.md) |

## `v4/lock/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/lock/contract.Locker.CreateLock.ttl` | limit | state held, or a decision kept, forever | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:v4/lock/contract.LockTtlRule` | — | [LOCK.md](package/LOCK.md) |

## `v4/logging`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/logging.ExitConfig.BeforeExitBudget` | limit | a call or a wait that never returns | exitStepBudget min bound.TimeoutMinimum | `value:v4/logging.ExitConfig.BeforeExitBudget` | class T: a timeout; an unlimited step before exit is a process that never exits | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.JsonLoggerConfig.ContextNormalizationDepth` | limit | a descent without end: the stack, or a payload that nests forever | normalizeJsonContextMaxDepth min 1 max normalizeJsonContextMaxDepth | `value:v4/logging.JsonLoggerConfig.ContextNormalizationDepth` | at most 10000, the nesting encoding/json decodes: each level is a frame of the goroutine stack, so a deeper walk can overflow it; unlimited is the overflow | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.ShieldedStepConfig.Budget` | limit | a call or a wait that never returns | exitStepBudget min bound.TimeoutMinimum unlimited admitted | `value:v4/logging.ShieldedStepConfig.Budget` | — | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.causeChainDepthRule#1` | limit | a descent without end: the stack, or a payload that nests forever | causeChainDefaultDepth min 1 | `value:v4/logging.DefaultLoggerConfig.CauseChainDepth`, `value:v4/logging.JsonLoggerConfig.CauseChainDepth` | class T: a depth; a chain can be cyclic, so unlimited is a walk without end | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.DefaultLogDirectoryMode` | mode | a journal directory readable by every local user | 0750 | `value:v4/logging.ReopenableFileWriterConfig.DirectoryMode`, `env:MELODY_LOG_DIRECTORY_MODE` | — | [LOGGING.md](package/LOGGING.md) |
| `v4/logging.DefaultLogFileMode` | mode | a journal readable by every local user | 0640 | `value:v4/logging.ReopenableFileWriterConfig.FileMode`, `env:MELODY_LOG_FILE_MODE` | — | [LOGGING.md](package/LOGGING.md) |
| `v4/logging` | redaction | a secret or a control character written into the journal | redacted and escaped | none | class R: the one exemption from the door rule, a raw door would make the journal the leak or the forgery it is protected against | [LOGGING.md](package/LOGGING.md#redaction-and-escaping) |

## `v4/mailer`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/mailer.SmtpConfig.DataTerminationTimeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum | `value:v4/mailer.SmtpConfig.DataTerminationTimeout` | class T: a timeout; unlimited is the wait that never returns | [MAILER.md](package/MAILER.md) |
| `v4/mailer.SmtpConfig.DialTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:v4/mailer.SmtpConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [MAILER.md](package/MAILER.md) |
| `v4/mailer.SmtpConfig.Timeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum | `value:v4/mailer.SmtpConfig.Timeout` | class T: a timeout; unlimited is the wait that never returns | [MAILER.md](package/MAILER.md) |
| `v4/mailer.SmtpConfig.AllowPlaintext` | protection | the door: lets a relay that does not offer STARTTLS receive the message in clear | refused | `off:v4/mailer.SmtpConfig.AllowPlaintext` | — | [MAILER.md](package/MAILER.md) |

## `v4/messagebus`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/messagebus.ConsumeCommandConfig.ShutdownTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:v4/messagebus.ConsumeCommandConfig.ShutdownTimeout` | class T: a timeout; an unlimited drain is a stop that never ends | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.InMemoryTransportConfig.BufferSize` | limit | unbounded memory or work | 64 min 0 | `value:v4/messagebus.InMemoryTransportConfig.BufferSize` | class T: a size; unlimited is memory or work that only grows | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.ResolveHeaderLimits#1` | limit | unbounded memory or work | DefaultHeaderMaxCount min 1 | `value:v4/messagebus.HeaderLimitsConfig.MaxCount` | class T: a size; unlimited is memory or work that only grows | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.ResolveHeaderLimits#2` | limit | unbounded memory or work | DefaultHeaderMaxBytes min 64 | `value:v4/messagebus.HeaderLimitsConfig.MaxBytes` | class T: a size; unlimited is memory or work that only grows | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.Backoff` | limit | a retry storm, or a retry that never comes back | Initialtime.Second Maxtime.Hour | `value:v4/messagebus.RetryPolicy.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.FailureRequeueDelay` | limit | a requeue that spins or never comes back | 5 * time.Second min bound.TimeoutMinimum | `value:v4/messagebus.RetryPolicy.FailureRequeueDelay` | class T: a delay; unlimited is a message never redelivered | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.MaxDeadLetterAttempts` | limit | guessing without end | 5 min 1 unlimited admitted | `value:v4/messagebus.RetryPolicy.MaxDeadLetterAttempts` | — | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.RetryPolicy.MaxRetries` | limit | a poison message redelivered forever | 3 min 0 | `value:v4/messagebus.RetryPolicy.MaxRetries` | class T: a retry count; unlimited redelivers a poison message forever | [MESSAGEBUS.md](package/MESSAGEBUS.md) |
| `v4/messagebus.HandleMiddlewareConfig.AllowMissingHandler` | protection | the door: lets a message with no registered handler pass through with a warning instead of failing the dispatch. | refused | `off:v4/messagebus.HandleMiddlewareConfig.AllowMissingHandler` | — | [MESSAGEBUS.md](package/MESSAGEBUS.md) |

## `v4/openapi`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/openapi.maximumSchemaDepth` | constant | a schema descent that overflows the stack | 64 | none | — | — |

## `v4/security`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security.HmacEnvelopeSignerConfig.Ttl` | limit | state held, or a decision kept, forever | 30 * time.Second min bound.TtlMinimum | `value:v4/security.HmacEnvelopeSignerConfig.Ttl` | class T: a lifetime; an envelope honoured forever can be replayed forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security.HmacTokenSourceConfig.Leeway` | limit | a clock difference widening what a credential admits | defaultLeeway max maximumLeeway | `value:v4/security.HmacTokenSourceConfig.Leeway` | at most 5m: a skew wider than five minutes is a misconfigured clock, not drift, and every second of leeway is a second more an expired envelope is honoured; unlimited honours it forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security.HmacTokenSourceConfig.MaxFutureExpiry` | limit | state held, or a decision kept, forever | time.Hour min bound.TtlMinimum | `value:v4/security.HmacTokenSourceConfig.MaxFutureExpiry` | class T: a lifetime; unlimited admits an envelope that never expires | [SECURITY.md](package/SECURITY.md) |
| `v4/security.InMemoryTokenStoreConfig.MaxEntries` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.MaxEntries` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.InMemoryTokenStoreConfig.RevocationEpochRetention` | limit | state held, or a decision kept, forever | 7 * 24 * time.Hour min bound.TtlMinimum unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.RevocationEpochRetention` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.InMemoryTokenStoreConfig.SweepInterval` | limit | a loop that spins or never runs | time.Minute min bound.TtlMinimum unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.SweepInterval` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.Leeway` | limit | a clock difference widening what a credential admits | defaultLeeway max maximumLeeway | `value:v4/security.JwtConfig.Leeway` | at most 5m: a skew wider than five minutes is a misconfigured clock, not drift, and every second of leeway is a second more an expired token is honoured; unlimited honours it forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.MaxCandidateKeys` | limit | unbounded memory or work | defaultJwtMaxCandidateKeys min 1 | `value:v4/security.JwtConfig.MaxCandidateKeys` | class T: a size; unlimited verifies one token against every key of the set, a cost the token's sender chooses | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.RevocationEpochSkew` | limit | a clock difference widening what a credential admits |  | `value:v4/security.JwtConfig.RevocationEpochSkew` | class T: a skew; unlimited admits every token a revocation should have ended | [SECURITY.md](package/SECURITY.md) |
| `v4/security.MemoryNonceGuardConfig.MaxEntries` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/security.MemoryNonceGuardConfig.MaxEntries` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.MemoryNonceGuardConfig.PurgeInterval` | limit | a loop that spins or never runs | time.Second min bound.TimeoutMinimum | `value:v4/security.MemoryNonceGuardConfig.PurgeInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [SECURITY.md](package/SECURITY.md) |
| `v4/security.TotpSecondFactorAuthenticatorConfig.LockoutWindow` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum | `value:v4/security.TotpSecondFactorAuthenticatorConfig.LockoutWindow` | class T: a lifetime; an unlimited window locks a factor out for good, the off door is MaxFailedAttempts | [SECURITY.md](package/SECURITY.md) |
| `v4/security.TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts` | limit | guessing without end | 5 min 1 unlimited admitted | `value:v4/security.TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.resolveMinimumSecretLength#1` | limit | a key short enough to guess | minimumHmacSha256SecretLength min minimumHmacSha256SecretLength | `value:v4/security.JwtSecretKeySetConfig.MinimumSecretLength`, `value:v4/security.HmacEnvelopeSignerConfig.MinimumSecretLength`, `value:v4/security.HmacKeyRing.MinimumSecretLength`, `value:v4/security.HmacTokenSourceConfig.MinimumSecretLength` | class T: a floor; RFC 7518 asks an HMAC key at least as long as the hash, so no floor is refused and a longer floor is the door | [SECURITY.md](package/SECURITY.md) |
| `v4/security.AccessDecisionManagerConfig.AllowIfAllAbstain` | protection | the door: grants an attribute at least one voter supported and every supporting voter abstained on | refused | `off:v4/security.AccessDecisionManagerConfig.AllowIfAllAbstain` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.AccessDecisionManagerConfig.AllowIfTied` | protection | the door: grants, under DecisionStrategyConsensus, an attribute whose granted and denied counts are equal and not zero | refused | `off:v4/security.AccessDecisionManagerConfig.AllowIfTied` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.CompiledFirewallConfig.LoginCsrfDisabled` | protection | the door: LoginCsrfDisabled | on | `off:v4/security.CompiledFirewallConfig.LoginCsrfDisabled` | — | — |
| `v4/security.CompiledFirewallConfig.LogoutCsrfDisabled` | protection | the door: LogoutCsrfDisabled | on | `off:v4/security.CompiledFirewallConfig.LogoutCsrfDisabled` | — | — |
| `v4/security.CsrfModeDisabled` | protection | a cross-site request forging an unsafe method on a session firewall | origin check on | `off:v4/security.CsrfModeDisabled`, `off:v4/http/contract.RouteProtectionConfig.CsrfDisabled`, `off:v4/security/config.LoginConfig.CsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.AllowFutureIssuedAt` | protection | the door: accepts a token whose iat is ahead of the clock by more than the leeway | refused | `off:v4/security.JwtConfig.AllowFutureIssuedAt` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtConfig.AllowWithoutExpiry` | protection | the door: AllowWithoutExpiry | refused | `off:v4/security.JwtConfig.AllowWithoutExpiry` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security.JwtAlgorithmHs256` | refusal | an algorithm confusion: a token signed with another algorithm | HS256 only | none | — | [SECURITY.md](package/SECURITY.md) |

## `v4/security/accesscontrol`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/accesscontrol.Control.MatchRuleIndex` | refusal | a path no access rule covers, admitted by omission | admitted | none | — | — |

## `v4/security/config`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/config.FirewallOverride.GlobalAccessControlDisabled` | protection | the door: cuts this firewall off the global access control | on | `off:v4/security/config.FirewallOverride.GlobalAccessControlDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.LoginConfig.CsrfDisabled` | protection | the door: turns the door's forgery check off, the stated other origin refused without a session cookie included | on | `off:v4/security/config.LoginConfig.CsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.LoginConfig.Limiter` | protection | credential guessing at the login door | none metered | none | — | [SECURITY.md](package/SECURITY.md) |
| `v4/security/config.LogoutConfig.CsrfDisabled` | protection | the door: CsrfDisabled | on | `off:v4/security/config.LogoutConfig.CsrfDisabled` | — | [SECURITY.md](package/SECURITY.md) |

## `v4/security/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/contract.FailedAttemptStore.Increment.window` | limit | state held, or a decision kept, forever | min bound.TtlMinimum | `value:v4/security/contract.FailedAttemptWindowRule` | class T: a lifetime; unlimited keeps the state forever | [SECURITY.md](package/SECURITY.md) |
| `v4/security/contract.NonceGuard.Remember.ttl` | limit | state held, or a decision kept, forever | min bound.TimeoutMinimum | `value:v4/security/contract.NonceTtlRule` | class T: a lifetime; a nonce remembered forever is a table that only grows | [SECURITY.md](package/SECURITY.md) |
| `v4/security/contract.RevocableTokenStore.Put.ttl` | limit | state held, or a decision kept, forever | min bound.TtlMinimum unlimited admitted | `value:v4/security/contract.TokenTtlRule` | — | [SECURITY.md](package/SECURITY.md) |

## `v4/security/totp`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/totp.totp.Config.Digits` | limit | a code short enough to guess | 6 min 6 max 8 | `value:v4/security/totp.Config.Digits` | 6 to 8: RFC 4226 asks at least six digits and authenticator apps show at most eight; a code outside the range is one no app generates | [SECURITY.md](package/SECURITY.md) |
| `v4/security/totp.totp.Config.Period` | limit | a loop that spins or never runs | 30 * time.Second min bound.TtlMinimum max 24 * time.Hour | `value:v4/security/totp.Config.Period` | 1s to 24h in whole seconds: a step longer than a day makes one code a day-long credential; unlimited is a code that never changes | [SECURITY.md](package/SECURITY.md) |
| `v4/security/totp.totp.Config.Skew` | limit | a clock difference widening what a credential admits | 1 min 0 max 10 | `value:v4/security/totp.Config.Skew` | at most 10: each step of skew widens by two periods the window a guessed code is accepted in, ten already five minutes either side at 30s; unlimited accepts every code ever issued | [SECURITY.md](package/SECURITY.md) |

## `v4/serializer`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/serializer.PlainTextSerializerConfig.MaxBytes` | limit | unbounded memory or work | plaintext.DefaultMaxBytes min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxBytes` | — | [SERIALIZER.md](package/SERIALIZER.md) |
| `v4/serializer.PlainTextSerializerConfig.MaxDepth` | limit | a descent without end: the stack, or a payload that nests forever | plaintext.DefaultMaxDepth min 1 max maxPlainTextDepth | `value:v4/serializer.PlainTextSerializerConfig.MaxDepth` | at most 1024: the walk recurses once per level on the goroutine stack and no reader follows a text body past a thousand levels; a value may hold itself, so unlimited never ends | [SERIALIZER.md](package/SERIALIZER.md) |
| `v4/serializer.PlainTextSerializerConfig.MaxLines` | limit | unbounded memory or work | plaintext.DefaultMaxLines min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxLines` | — | [SERIALIZER.md](package/SERIALIZER.md) |
| `v4/serializer.PlainTextSerializerConfig.MaxMembers` | limit | unbounded memory or work | plaintext.DefaultMaxMembers min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxMembers` | — | [SERIALIZER.md](package/SERIALIZER.md) |

## `v4/session`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/session.FileStorageConfig.JournalMaxBytes` | limit | unbounded memory or work | DefaultJournalMaxBytes min 64 << 10 | `value:v4/session.FileStorageConfig.JournalMaxBytes` | class T: a size; unlimited is memory or work that only grows | [SESSION.md](package/SESSION.md) |
| `v4/session.FileStorageConfig.MaxSessions` | limit | unbounded memory or work | DefaultMaxSessions min 1 unlimited admitted | `value:v4/session.FileStorageConfig.MaxSessions` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.InMemoryStorageConfig.CleanupInterval` | limit | a loop that spins or never runs | time.Minute min bound.TimeoutMinimum | `value:v4/session.InMemoryStorageConfig.CleanupInterval` | class T: an interval; unlimited is a sweep that never runs, so the table only grows | [SESSION.md](package/SESSION.md) |
| `v4/session.InMemoryStorageConfig.MaxSessions` | limit | unbounded memory or work | DefaultMaxSessions min 1 unlimited admitted | `value:v4/session.InMemoryStorageConfig.MaxSessions` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.ManagerConfig.StorageTimeout` | limit | a call or a wait that never returns | DefaultStorageTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/session.ManagerConfig.StorageTimeout` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.ManagerConfig.TombstoneRetention` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum | `value:v4/session.ManagerConfig.TombstoneRetention` | class T: a lifetime; unlimited keeps the state forever | [SESSION.md](package/SESSION.md) |
| `v4/session.ManagerConfig.Ttl` | limit | state held, or a decision kept, forever | 24 * time.Hour min bound.TtlMinimum unlimited admitted | `value:v4/session.ManagerConfig.Ttl` | — | [SESSION.md](package/SESSION.md) |
| `v4/session.newFileStorageFromPath#1` | mode | a session directory readable by every local user | 0755 | none | — | — |
| `v4/session.openSessionJournal#1` | mode | a session journal readable beyond its owner | 0600 | none | — | — |
| `v4/session.writeSessionBytesAtomically#1` | mode | a session directory readable by every local user | 0755 | none | — | — |
| `v4/session.FileStorageConfig.JournalDisabled` | protection | the door: writes the whole snapshot on every Save and Delete over a path | on | `off:v4/session.FileStorageConfig.JournalDisabled` | — | [SESSION.md](package/SESSION.md) |

## `v4/session/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/session/contract.ttl` | limit | state held, or a decision kept, forever | min bound.TtlMinimum unlimited admitted | `value:v4/session/contract.TtlRule` | — | [SESSION.md](package/SESSION.md) |

## `v4/storage`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/storage.LocalStorageConfig.SweepBatchSize` | limit | unbounded memory or work | storageSweepBatchSize min 1 | `value:v4/storage.LocalStorageConfig.SweepBatchSize` | class T: a batch; unlimited is one call over the whole set | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.LocalStorageConfig.TemporaryStaleAge` | limit | unbounded memory or work | storageTemporaryStaleAge min bound.TtlMinimum unlimited admitted | `value:v4/storage.LocalStorageConfig.TemporaryStaleAge` | — | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.LocalStorage.Get#1` | mode | — | 0 (read only) | none | opened read only: the mode argument creates nothing | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.LocalStorage.Put#1` | mode | a storage directory readable beyond its owner | 0750 | none | — | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.LocalStorage.Put#2` | mode | a storage directory readable beyond its owner | 0750 | none | — | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.NewLocalStorage#1` | mode | a storage directory readable beyond its owner | 0750 | none | — | [STORAGE.md](package/STORAGE.md) |
| `v4/storage.createStorageTemporaryFile#1` | mode | a stored object readable by every local user | 0640 | none | — | — |
| `v4/storage.readStorageContentType#1` | mode | — | 0 (read only) | none | opened read only: the mode argument creates nothing | — |

## `v4/storage/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/storage/contract.PresignConfig.Expiry` | limit | state held, or a decision kept, forever | 15 * time.Minute min bound.TtlMinimum max 7 * 24 * time.Hour | `value:v4/storage/contract.PresignConfig.Expiry` | at most 7 days, the longest a SigV4 presigned url is signed for: the url is a bearer credential to the object, so unlimited is a credential that never expires | [STORAGE.md](package/STORAGE.md) |

## `v4/translation`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/translation.maxInterpolationDepth` | constant | a message that interpolates itself forever | 32 | none | — | — |

## `v4/validation`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/validation.ValidatorConfig.MaxAnswerBytes` | limit | unbounded memory or work | maxValidationAnswerBytes min 1 | `value:v4/validation.ValidatorConfig.MaxAnswerBytes` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxErrors` | limit | unbounded memory or work | maxValidationErrors min 1 | `value:v4/validation.ValidatorConfig.MaxErrors` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxNestingDepth` | limit | a descent without end: the stack, or a payload that nests forever | maxNestedValidationDepth min 1 max maxNestingDepthCeiling | `value:v4/validation.ValidatorConfig.MaxNestingDepth` | at most 10000, the nesting encoding/json decodes: each level is a frame of the goroutine stack; unlimited is a payload that nests until the stack overflows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxRenderedMapKeyLength` | limit | unbounded memory or work | maxRenderedMapKeyLength min 1 | `value:v4/validation.ValidatorConfig.MaxRenderedMapKeyLength` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |
| `v4/validation.ValidatorConfig.MaxRenderedPathLength` | limit | unbounded memory or work | maxRenderedPathLength min 1 | `value:v4/validation.ValidatorConfig.MaxRenderedPathLength` | class T: a size; unlimited is memory or work that only grows | [VALIDATION.md](package/VALIDATION.md) |

## `integrations/amqp/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/amqp/v4.maxDelayBuckets` | constant | a delay exchange per distinct delay | 8 | none | — | — |
| `integrations/amqp/v4.ConnectionConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.ConnectionConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ProviderConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.ProviderConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ProviderConfig.DialTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:integrations/amqp/v4.ProviderConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ProviderConfig.Heartbeat` | limit | a dead connection never noticed | 10 * time.Second min time.Second | `value:integrations/amqp/v4.ProviderConfig.Heartbeat` | class T: a heartbeat interval; unlimited never notices a dead peer | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ReconnectConfig.Backoff` | limit | a retry storm, or a retry that never comes back | Initialparent.Initial Maxparent.Max | `value:integrations/amqp/v4.ReconnectConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ServerSentEventBackplaneConfig.CallTimeout` | limit | a call or a wait that never returns | time.Second min bound.TimeoutMinimum | `value:integrations/amqp/v4.ServerSentEventBackplaneConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.ServerSentEventBackplaneConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.ServerSentEventBackplaneConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.CallTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:integrations/amqp/v4.TransportConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.TransportConfig.CloseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.Prefetch` | limit | unbounded concurrency or connections | defaultPrefetch min 1 max maxPrefetch | `value:integrations/amqp/v4.TransportConfig.Prefetch` | at most 65535: the prefetch count of basic.qos is a 16-bit field; unlimited would let the broker push the whole queue into one consumer's memory | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.PublishReturnBuffer` | limit | unbounded memory or work | 16 min 1 | `value:integrations/amqp/v4.TransportConfig.PublishReturnBuffer` | class T: a size; unlimited is memory or work that only grows | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig.DeadLetterDisabled` | protection | the door: declares the queue without its <queue>.dlx exchange and <queue>.dlq queue, so a poison message is dropped | on | `off:integrations/amqp/v4.TransportConfig.DeadLetterDisabled` | — | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |
| `integrations/amqp/v4.TransportConfig` | protection | a dead-letter queue that grows without end | no cap | none | — | [integrations/amqp/v4/README.md](../../integrations/amqp/v4/README.md) |

## `integrations/aws/s3/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/aws/s3/v4.StorageConfig.MultipartAbortTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:integrations/aws/s3/v4.StorageConfig.MultipartAbortTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/aws/s3/v4/README.md](../../integrations/aws/s3/v4/README.md) |
| `integrations/aws/s3/v4.StorageConfig.PutSpoolLimit` | limit | unbounded memory or work | 16 * 1024 * 1024 min 1 | `value:integrations/aws/s3/v4.StorageConfig.PutSpoolLimit` | class T: a size; unlimited is memory or work that only grows | [integrations/aws/s3/v4/README.md](../../integrations/aws/s3/v4/README.md) |

## `integrations/bunorm/migrate/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/migrate/v4.Options.UnlockTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/migrate/v4.Options.UnlockTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/migrate/v4/README.md](../../integrations/bunorm/migrate/v4/README.md) |
| `integrations/bunorm/migrate/v4.migrationFileMode` | mode | a generated migration readable beyond the repository's own files | 0644 | none | a migration file is source the repository commits: its mode is the repository's, like any file the developer writes | — |
| `integrations/bunorm/migrate/v4.printAppliedGroup` | refusal | a migration that ran but was not recorded, run again | warning | none | — | — |

## `integrations/bunorm/mysql/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/mysql/v4.LockerConfig.ReleaseTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/mysql/v4.LockerConfig.ReleaseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxIdleTime` | limit | state held, or a decision kept, forever | 1 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxIdleTime` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxLifetime` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxLifetime` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | 5 min 0 unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.MaxIdleConnections` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.PoolConfig.MaxOpenConnections` | limit | unbounded concurrency or connections | 25 min 1 unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.MaxOpenConnections` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.RetryConfig.Backoff` | limit | a retry storm, or a retry that never comes back | Initial500 * time.Millisecond Max5 * time.Second | `value:integrations/bunorm/mysql/v4.RetryConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.RetryConfig.MaxAttempts` | limit | a failure retried forever | 3 min 1 | `value:integrations/bunorm/mysql/v4.RetryConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.TimeoutConfig.ConnectTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/mysql/v4.TimeoutConfig.ConnectTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.TimeoutConfig.ReadTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.TimeoutConfig.ReadTimeout` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |
| `integrations/bunorm/mysql/v4.TimeoutConfig.WriteTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.TimeoutConfig.WriteTimeout` | — | [integrations/bunorm/mysql/v4/README.md](../../integrations/bunorm/mysql/v4/README.md) |

## `integrations/bunorm/pgsql/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/pgsql/v4.LockerConfig.ReleaseTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/pgsql/v4.LockerConfig.ReleaseTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxIdleTime` | limit | state held, or a decision kept, forever | 1 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxIdleTime` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxLifetime` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxLifetime` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | 25 min 0 unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.MaxIdleConnections` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.PoolConfig.MaxOpenConnections` | limit | unbounded concurrency or connections | 50 min 1 unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.MaxOpenConnections` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.RetryConfig.Backoff` | limit | a retry storm, or a retry that never comes back | Initial500 * time.Millisecond Max5 * time.Second | `value:integrations/bunorm/pgsql/v4.RetryConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.RetryConfig.MaxAttempts` | limit | a failure retried forever | 3 min 1 | `value:integrations/bunorm/pgsql/v4.RetryConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.ConnectTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.ConnectTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.ReadTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.ReadTimeout` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.WriteTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.WriteTimeout` | — | [integrations/bunorm/pgsql/v4/README.md](../../integrations/bunorm/pgsql/v4/README.md) |

## `integrations/bunorm/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4.SplitterConfig.ReplicaRetryInterval` | limit | a loop that spins or never runs | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/v4.SplitterConfig.ReplicaRetryInterval` | class T: an interval; unlimited never tries again a replica that came back | [integrations/bunorm/v4/README.md](../../integrations/bunorm/v4/README.md) |
| `integrations/bunorm/v4.SplitterConfig` | protection | reads failing while the primary still answers | fall back to the primary | none | — | [integrations/bunorm/v4/README.md](../../integrations/bunorm/v4/README.md) |

## `integrations/bunorm/v4/audit`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4/audit.AsyncStorageConfig.BufferSize` | limit | unbounded memory or work | 1024 min 1 | `value:integrations/bunorm/v4/audit.AsyncStorageConfig.BufferSize` | class T: a size; unlimited is memory or work that only grows | [integrations/bunorm/v4/audit/README.md](../../integrations/bunorm/v4/audit/README.md) |
| `integrations/bunorm/v4/audit.FileStorage.Save#1` | mode | an audit trail readable by every local user | 0640 | none | — | [integrations/bunorm/v4/audit/README.md](../../integrations/bunorm/v4/audit/README.md) |
| `integrations/bunorm/v4/audit.RegistryConfig` | redaction | an encrypted column's plaintext in the audit trail | masked | `off:integrations/bunorm/v4/audit.RegistryConfig.EncryptedColumnsUnmasked` | class R: per registry rather than per entity, so the door is not set once and forgotten on the entity that holds the card numbers; a field tagged redact stays masked | [integrations/bunorm/v4/audit/README.md](../../integrations/bunorm/v4/audit/README.md) |

## `integrations/bunorm/v4/encrypt`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4/encrypt.TableSpecification.BatchSize` | limit | unbounded memory or work | defaultMigrateBatchSize min 1 | `value:integrations/bunorm/v4/encrypt.TableSpecification.BatchSize` | class T: a batch; unlimited is one call over the whole set | [integrations/bunorm/v4/encrypt/README.md](../../integrations/bunorm/v4/encrypt/README.md) |
| `integrations/bunorm/v4/encrypt.ColumnSealBinding.UnboundAdmitted` | protection | the door: UnboundAdmitted | refused | `off:integrations/bunorm/v4/encrypt.ColumnSealBinding.UnboundAdmitted` | — | [integrations/bunorm/v4/encrypt/README.md](../../integrations/bunorm/v4/encrypt/README.md) |
| `integrations/bunorm/v4/encrypt.SealBindingConfig.UnboundAdmitted` | protection | the door: UnboundAdmitted | refused | `off:integrations/bunorm/v4/encrypt.SealBindingConfig.UnboundAdmitted` | — | [integrations/bunorm/v4/encrypt/README.md](../../integrations/bunorm/v4/encrypt/README.md) |

## `integrations/cron/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/cron/v4.EntryConfig.Instances` | limit | unbounded concurrency or connections | 1 min 1 | `value:integrations/cron/v4.EntryConfig.Instances` | class T: a concurrency; unlimited lets the runs of a slow entry overlap without end | [integrations/cron/README.md](../../integrations/cron/README.md) |
| `integrations/cron/v4.EntryConfig.ShutdownTimeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum | `value:integrations/cron/v4.EntryConfig.ShutdownTimeout` | class T: a timeout; an unlimited drain is a stop that never ends | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.EntryConfig.TickDedup.Window` | limit | state held, or a decision kept, forever | min time.Second | `value:integrations/cron/v4.EntryConfig.TickDedup.Window` | class T: a lifetime; an unlimited window deduplicates every later tick of the entry too | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.EntryConfig.Timeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum unlimited admitted | `value:integrations/cron/v4.EntryConfig.Timeout` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.ClockJumpResetThreshold` | limit | a clock jump replaying or skipping the schedule | 3 * time.Hour min 2 * time.Minute | `value:integrations/cron/v4.RunnerConfig.ClockJumpResetThreshold` | class T: a threshold; unlimited never treats a jump of the clock as one, so a jump replays or skips the schedule it crosses | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.OutputCaptureLimit` | limit | unbounded memory or work | 64 * 1024 min 1 | `value:integrations/cron/v4.RunnerConfig.OutputCaptureLimit` | class T: a size; unlimited is memory or work that only grows | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.ShutdownTimeout` | limit | a call or a wait that never returns | 5 * time.Minute min bound.TimeoutMinimum | `value:integrations/cron/v4.RunnerConfig.ShutdownTimeout` | class T: a timeout; an unlimited drain is a stop that never ends | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.TickDedup.CallTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/cron/v4.RunnerConfig.TickDedup.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.TickDedup.Window` | limit | state held, or a decision kept, forever | 5 * time.Minute min time.Second | `value:integrations/cron/v4.RunnerConfig.TickDedup.Window` | class T: a lifetime; an unlimited window deduplicates every later tick of the entry too | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerConfig.Timeout` | limit | a call or a wait that never returns | time.Hour min bound.TimeoutMinimum unlimited admitted | `value:integrations/cron/v4.RunnerConfig.Timeout` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.GenerateCommand.writeDestinations#1` | mode | a crontab directory writable beyond its owner | 0755 | none | — | — |
| `integrations/cron/v4.createTheLogsRoot#1` | mode | a logs root writable beyond its owner | 0755 | none | — | — |
| `integrations/cron/v4.resolveEntryLogPath#1` | mode | a log directory writable beyond its owner | 0755 | none | — | — |
| `integrations/cron/v4.EntryConfig.LogDisabled` | protection | the door: LogDisabled | on | `off:integrations/cron/v4.EntryConfig.LogDisabled` | — | [integrations/cron/README.md](../../integrations/cron/README.md) |
| `integrations/cron/v4.EntryTickDedupConfig.Disabled` | protection | the door: runs this entry on every replica that evaluates its tick, whatever RunnerConfig.TickDedup says | on | `off:integrations/cron/v4.EntryTickDedupConfig.Disabled` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.TickDedupConfig.Disabled` | protection | the door: turns the dedup off for every entry | on | `off:integrations/cron/v4.TickDedupConfig.Disabled` | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.TickDedupConfig` | protection | a tick run twice over a locker without a lease | deduplicated | none | — | [integrations/cron/v4/README.md](../../integrations/cron/v4/README.md) |
| `integrations/cron/v4.RunnerCommand.Run` | refusal | a cron User the in-process runner cannot honour | warning | none | — | [integrations/cron/README.md](../../integrations/cron/README.md) |

## `integrations/opentelemetry/v4/otlp`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/opentelemetry/v4/otlp.Config.BatchTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/opentelemetry/v4/otlp.Config.BatchTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/opentelemetry/v4/README.md](../../integrations/opentelemetry/v4/README.md) |
| `integrations/opentelemetry/v4/otlp.Config.Insecure` | protection | spans in clear to a non-loopback collector | transport security on | `off:integrations/opentelemetry/v4/otlp.Config.Insecure` | — | [integrations/opentelemetry/v4/README.md](../../integrations/opentelemetry/v4/README.md) |

## `integrations/outbox/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/outbox/v4.RelayCommandConfig.ErrorBackoff` | limit | a retry storm, or a retry that never comes back | Initialbound.TimeoutMinimum Maxtime.Minute | `value:integrations/outbox/v4.RelayCommandConfig.ErrorBackoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.Backoff` | limit | a retry storm, or a retry that never comes back | Initial15 * time.Second Max10 * time.Minute | `value:integrations/outbox/v4.RelayConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.BatchSize` | limit | unbounded memory or work | 100 min 1 max maximumBatchSize | `value:integrations/outbox/v4.RelayConfig.BatchSize` | at most 100000: the batch sizes a slice allocated before the claim; unlimited would claim the whole table into one slice and one transaction | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.LockTtl` | limit | state held, or a decision kept, forever | 30 * time.Second min bound.TtlMinimum | `value:integrations/outbox/v4.RelayConfig.LockTtl` | class T: a lifetime; a lock that outlives a crashed holder forever stops every relay | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.MaxAttempts` | limit | a failure retried forever | 12 min 1 | `value:integrations/outbox/v4.RelayConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.MaxDeliveryAttempts` | limit | a poison row redelivered forever | min 1 | `value:integrations/outbox/v4.RelayConfig.MaxDeliveryAttempts` | class T: a retry count; unlimited redelivers a poison row forever | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.ResolutionTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/outbox/v4.RelayConfig.ResolutionTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.UnavailableBackoff` | limit | a retry storm, or a retry that never comes back | Initial15 * time.Second Maxtime.Minute | `value:integrations/outbox/v4.RelayConfig.UnavailableBackoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |
| `integrations/outbox/v4.RelayConfig.VisibilityTimeout` | limit | a call or a wait that never returns | 5 * time.Minute min bound.TtlMinimum | `value:integrations/outbox/v4.RelayConfig.VisibilityTimeout` | class T: a lifetime; a row hidden forever is never recovered from a claimer that crashed | [integrations/outbox/v4/README.md](../../integrations/outbox/v4/README.md) |

## `integrations/rueidis/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/rueidis/v4.ClientConfig.BootPingTimeout` | limit | a call or a wait that never returns | 3 * time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4.ClientConfig.BootPingTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.ClientSideCacheBytes` | limit | unbounded memory or work | rueidis.DefaultCacheBytes min 1 | `value:integrations/rueidis/v4.ClientConfig.ClientSideCacheBytes` | class T: a size; unlimited is memory or work that only grows | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.ConnectionWriteTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4.ClientConfig.ConnectionWriteTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.DialTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4.ClientConfig.DialTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ReconnectConfig.Backoff` | limit | a retry storm, or a retry that never comes back | Initialtime.Second Max30 * time.Second | `value:integrations/rueidis/v4.ReconnectConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.RetryConfig.Backoff` | limit | a retry storm, or a retry that never comes back | Initial500 * time.Millisecond Max5 * time.Second | `value:integrations/rueidis/v4.RetryConfig.Backoff` | class T: a backoff; an unbounded ceiling is a retry that never comes back | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.RetryConfig.MaxAttempts` | limit | a failure retried forever | 3 min 1 | `value:integrations/rueidis/v4.RetryConfig.MaxAttempts` | class T: a retry count; unlimited retries a failure forever | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.TokenStoreConfig.MaximumClockSkew` | limit | a clock difference widening what a credential admits | 0 min 0 | `value:integrations/rueidis/v4.TokenStoreConfig.MaximumClockSkew` | class T: a skew; unlimited admits a credential whatever its time | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.TokenStoreConfig.RevocationEpochRetention` | limit | state held, or a decision kept, forever | defaultRevocationEpochRetention min bound.TtlMinimum unlimited admitted | `value:integrations/rueidis/v4.TokenStoreConfig.RevocationEpochRetention` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.TokenStoreConfig.ScanCount` | limit | unbounded memory or work | defaultTokenStoreScanCount min 1 | `value:integrations/rueidis/v4.TokenStoreConfig.ScanCount` | class T: a batch; unlimited is one call over the whole set | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.callTimeoutRule#1` | limit | a call or a wait that never returns | fallback min bound.TimeoutMinimum | `value:integrations/rueidis/v4.callTimeoutRule`, `value:integrations/rueidis/v4.LockerConfig.CallTimeout`, `value:integrations/rueidis/v4.NonceGuardConfig.CallTimeout`, `value:integrations/rueidis/v4.RateLimiterConfig.CallTimeout`, `value:integrations/rueidis/v4.ServerSentEventBackplaneConfig.CallTimeout`, `value:integrations/rueidis/v4.TokenStoreConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.ClientConfig.BootPingDisabled` | protection | the door: skips the ping that verifies the store answers before Open returns | on | `off:integrations/rueidis/v4.ClientConfig.BootPingDisabled` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4.LockerConfig.KeyPrefixDisabled` | protection | the door: keys a lock by its bare name | on | `off:integrations/rueidis/v4.LockerConfig.KeyPrefixDisabled` | — | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |

## `integrations/rueidis/v4/cache`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/rueidis/v4/cache.BackendConfig.CallTimeout` | limit | a call or a wait that never returns | time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4/cache.BackendConfig.CallTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4/cache.BackendConfig.DeleteBatch` | limit | unbounded memory or work | 500 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.DeleteBatch` | class T: a batch; unlimited is one call over the whole set | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4/cache.BackendConfig.MaxKeyLength` | limit | unbounded memory or work | 1024 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.MaxKeyLength` | class T: a size; unlimited is memory or work that only grows | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |
| `integrations/rueidis/v4/cache.BackendConfig.ScanCount` | limit | unbounded memory or work | 1000 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.ScanCount` | class T: a batch; unlimited is one call over the whole set | [integrations/rueidis/v4/README.md](../../integrations/rueidis/v4/README.md) |

## `integrations/websocket/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/websocket/v4.StreamHandlerConfig.CloseHandshakeGrace` | limit | a call or a wait that never returns | time.Second min bound.TimeoutMinimum | `value:integrations/websocket/v4.StreamHandlerConfig.CloseHandshakeGrace` | class T: a timeout; unlimited is the wait that never returns | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.StreamHandlerConfig.IdleTimeout` | limit | a call or a wait that never returns | 30 * time.Second min time.Second | `value:integrations/websocket/v4.StreamHandlerConfig.IdleTimeout` | class T: a timeout; unlimited holds an idle socket and its goroutines forever | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.StreamHandlerConfig.ReadLimit` | limit | unbounded memory or work | 32768 min 1 unlimited admitted | `value:integrations/websocket/v4.StreamHandlerConfig.ReadLimit` | — | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |
| `integrations/websocket/v4.StreamHandlerConfig.WriteTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:integrations/websocket/v4.StreamHandlerConfig.WriteTimeout` | class T: a timeout; unlimited is the wait that never returns | [integrations/websocket/v4/README.md](../../integrations/websocket/v4/README.md) |

## Boot warnings

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `application.environment.processVariableIgnored` | warning | a process variable the operator believes is read | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `application.environment.undeclaredKey` | warning | a MELODY_ key nothing reads, a typo the operator believes is set | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `application.secretMark.unmatched` | warning | a secret mark that protects no parameter | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `application.service.builtAtBootFailed` | warning | a service that failed its boot build going unnoticed | written in http | none | — | — |
| `aws.s3.plaintextEndpoint` | warning | object traffic in clear to a non-loopback endpoint | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `http.session.unboundedTtlOnBoundedDefaultStorage` | warning | sessions without end on a storage that evicts | written in http | none | — | — |
| `http.static.cacheMaxAgeIgnored` | warning | a cache max age the operator believes is applied | written in http | none | — | — |
| `messagebus.retryPolicyServiceIgnored` | warning | a retry policy service the operator believes is used | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
| `security.csrfDisabledOnLoginFirewall` | warning | a login firewall without its forgery check | written in http | none | — | — |
| `security.firewallDoorReachUnknown` | warning | a firewall door whose reach the compiler cannot prove | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | [APPLICATION.md](package/APPLICATION.md) |
