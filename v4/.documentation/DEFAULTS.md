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
- **class R** — a redaction: the journal, the debug output and the audit trail never carry a secret in clear; the door, where there is one, is a flag of a development command or a field of the registry.
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
| `v4/cache.InMemoryBackendConfig.CleanupInterval` | limit | a loop that spins or never runs | time.Minute min bound.TimeoutMinimum | `value:v4/cache.InMemoryBackendConfig.CleanupInterval` | — | — |
| `v4/cache.InMemoryBackendConfig.MaxItems` | limit | unbounded memory or work | 10_000 min 1 unlimited admitted | `value:v4/cache.InMemoryBackendConfig.MaxItems` | — | — |
| `v4/cache.RememberConfig.WaitTimeout` | limit | a call or a wait that never returns | 30 * time.Second min 0 unlimited admitted | `value:v4/cache.RememberConfig.WaitTimeout` | — | — |
| `v4/cache.RememberConfig.StampedeProtectionDisabled` | protection | the door: runs the callback on every miss in the caller's goroutine, under the caller's runtime, with no flight to join. | on | `off:v4/cache.RememberConfig.StampedeProtectionDisabled` | — | — |
| `v4/cache.RememberConfig.Ttl` | protection | the door: is the lifetime of the stored value unless the callback chooses its own through RememberItem.SetTtl, handed to the cache's Set | the field GoDoc | `value:v4/cache.RememberConfig.Ttl` | — | — |

## `v4/cache/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cache/contract.ttl` | limit | state held, or a decision kept, forever | min bound.TtlMinimum unlimited admitted | `value:v4/cache/contract.TtlRule` | — | — |
| `v4/cache/contract.CounterOptions.Ttl` | protection | the door: is the counter's lifetime | the field GoDoc | `value:v4/cache/contract.CounterOptions.Ttl` | — | — |

## `v4/cli`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cli.cliErrorLineCauseChainDepth` | constant | an exit line without end | 8 | none | — | — |

## `v4/cli/output`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/cli/output.Option.Limit` | limit | unbounded memory or work | default unlimited min 0 | `value:v4/cli/output.Option.Limit` | — | — |
| `v4/cli/output.Option.Offset` | limit | a negative or runaway offset | 0 min 0 | `value:v4/cli/output.Option.Offset` | — | — |
| `v4/cli/output.Option.TableMaxWidth` | limit | unbounded memory or work | defaultTableMaxWidth min 1 unlimited admitted | `value:v4/cli/output.Option.TableMaxWidth` | — | — |
| `v4/cli/output.TablePrinterConfig.MaxWidth` | limit | unbounded memory or work | defaultTableMaxWidth min 1 unlimited admitted | `value:v4/cli/output.TablePrinterConfig.MaxWidth` | — | — |
| `v4/cli/output.ListPayloadConfig.Limit` | protection | the door: Limit | the field GoDoc | `value:v4/cli/output.ListPayloadConfig.Limit` | — | — |
| `v4/cli/output.ListPayloadConfig.Offset` | protection | the door: Offset | the field GoDoc | `value:v4/cli/output.ListPayloadConfig.Offset` | — | — |

## `v4/config`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/config.ApplicationConfig.HttpKernel.UnavailableRetryAfter` | limit | a client retrying at once, or never | DefaultUnavailableRetryAfter min bound.TtlMinimum max maximumUnavailableRetryAfter | `value:v4/application.ApplicationConfig.HttpKernel.UnavailableRetryAfter`, `env:MELODY_HTTP_UNAVAILABLE_RETRY_AFTER` | — | — |
| `v4/config.ApplicationConfig.HttpServer.IdleTimeout` | limit | a call or a wait that never returns | DefaultHttpIdleTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.IdleTimeout`, `env:MELODY_HTTP_IDLE_TIMEOUT` | — | — |
| `v4/config.ApplicationConfig.HttpServer.MaxHeaderBytes` | limit | unbounded memory or work | DefaultHttpMaxHeaderBytes min 4 << 10 | `value:v4/application.ApplicationConfig.HttpServer.MaxHeaderBytes`, `env:MELODY_HTTP_MAX_HEADER_BYTES` | — | — |
| `v4/config.ApplicationConfig.HttpServer.ReadHeaderTimeout` | limit | a call or a wait that never returns | DefaultHttpReadHeaderTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.ReadHeaderTimeout`, `env:MELODY_HTTP_READ_HEADER_TIMEOUT` | — | — |
| `v4/config.ApplicationConfig.HttpServer.ReadTimeout` | limit | a call or a wait that never returns | DefaultHttpReadTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.ReadTimeout`, `env:MELODY_HTTP_READ_TIMEOUT` | — | — |
| `v4/config.ApplicationConfig.HttpServer.ShutdownTimeout` | limit | a call or a wait that never returns | DefaultHttpShutdownTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.ShutdownTimeout`, `env:MELODY_HTTP_SHUTDOWN_TIMEOUT` | — | — |
| `v4/config.ApplicationConfig.HttpServer.WriteTimeout` | limit | a call or a wait that never returns | DefaultHttpWriteTimeout min bound.TimeoutMinimum | `value:v4/application.ApplicationConfig.HttpServer.WriteTimeout`, `env:MELODY_HTTP_WRITE_TIMEOUT` | — | — |
| `v4/config.ApplicationConfig.TeardownTimeout` | limit | a call or a wait that never returns | DefaultTeardownTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/application.ApplicationConfig.TeardownTimeout`, `env:MELODY_TEARDOWN_TIMEOUT` | — | — |
| `v4/config.HttpConfiguration.MaxRequestBodyBytes` | limit | unbounded memory or work | DefaultHttpMaxRequestBodyBytes min 1 | `value:v4/config/contract.HttpConfiguration.MaxRequestBodyBytes`, `env:MELODY_HTTP_MAX_REQUEST_BODY_BYTES` | — | — |
| `v4/config.HttpConfiguration.SessionMaxSessions` | limit | unbounded memory or work | DefaultSessionMaxSessions min 1 unlimited admitted | `value:v4/config/contract.HttpConfiguration.SessionMaxSessions`, `env:MELODY_HTTP_SESSION_MAX_SESSIONS` | — | — |
| `v4/config.HttpConfiguration.SessionStorageTimeout` | limit | a call or a wait that never returns | DefaultSessionStorageTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/config/contract.HttpConfiguration.SessionStorageTimeout`, `env:MELODY_HTTP_SESSION_STORAGE_TIMEOUT` | — | — |
| `v4/config.HttpConfiguration.SessionTombstoneRetention` | limit | state held, or a decision kept, forever | DefaultSessionTombstoneRetention min bound.TtlMinimum | `value:v4/config/contract.HttpConfiguration.SessionTombstoneRetention`, `env:MELODY_HTTP_SESSION_TOMBSTONE_RETENTION` | — | — |
| `v4/config.HttpConfiguration.SessionTtl` | limit | state held, or a decision kept, forever | DefaultSessionTtl min bound.TtlMinimum unlimited admitted | `value:v4/config/contract.HttpConfiguration.SessionTtl`, `env:MELODY_HTTP_SESSION_TTL` | — | — |
| `v4/config.HttpConfiguration.StaticCacheMaxAge` | limit | unbounded memory or work | DefaultStaticCacheMaxAge min 0 | `value:v4/config/contract.HttpConfiguration.StaticCacheMaxAge`, `env:MELODY_STATIC_CACHE_MAX_AGE` | — | — |
| `v4/config.HttpSessionCookieSecureAlways` | protection | a session cookie sent in clear | from the scheme | `use:v4/http/contract.SessionCookiePolicy.Secure`, `env:MELODY_HTTP_SESSION_COOKIE_SECURE` | the key only hardens it: always is the one value it admits, and the Go field lowers the policy | — |
| `v4/config.kernelConfiguration.validateEnvironment` | refusal | an environment name outside the closed list | refused | none | — | — |

## `v4/container`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/container.closeFailureStackLimit` | constant | a close failure journaling a whole stack | 2048 bytes | none | — | — |
| `v4/container.teardownWalkDepthLimit` | constant | a teardown walk without end | 12 | none | — | — |
| `v4/container.teardownWalkElementLimit` | constant | a teardown walk without end | 256 | none | — | — |
| `v4/container.teardownWalkNodeBudget` | constant | a teardown walk without end | 65536 | none | — | — |
| `v4/container.ContainerConfig.CreationWaiters` | limit | a call or a wait that never returns | DefaultCreationWaiters min 1 unlimited admitted | `value:v4/container.ContainerConfig.CreationWaiters`, `value:v4/application.ApplicationConfig.ContainerCreationWaiters`, `env:MELODY_CONTAINER_CREATION_WAITERS` | — | — |
| `v4/container.ContainerConfig.ScopeTeardownTimeout` | limit | a call or a wait that never returns | DefaultScopeTeardownTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/container.ContainerConfig.ScopeTeardownTimeout`, `value:v4/application.ApplicationConfig.ScopeTeardownTimeout`, `env:MELODY_SCOPE_TEARDOWN_TIMEOUT` | — | — |

## `v4/container/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/container/contract.RegisterOptions.TeardownReflectionDisabled` | protection | the door: keeps the teardown's walk out of this service's memory, for a service whose own goroutines rewrite its pointer fields | on | `off:v4/container/contract.RegisterOptions.TeardownReflectionDisabled` | — | — |
| `v4/container/contract.RegisterOptions.TypeRegistrationDisabled` | protection | the door: keeps the service off the by-type index, so it is resolved by name alone. | on | `off:v4/container/contract.RegisterOptions.TypeRegistrationDisabled` | — | — |

## `v4/debug`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/debug.errorCauseChainDepth` | constant | a debug error without end | 9 | none | — | — |
| `v4/debug.ParameterCommand.Flags` | redaction | a secret parameter printed by debug:parameters | masked | none | — | — |

## `v4/http`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http.RequestConfig.MaxBodyBytes` | limit | unbounded memory or work | min 1 | `value:v4/http.RequestConfig.MaxBodyBytes` | — | — |
| `v4/http.RouteOptions.Protection.MaxBodyBytes` | limit | unbounded memory or work | min 1 | `value:v4/http.RouteOptions.Protection.MaxBodyBytes` | — | — |
| `v4/http.ServerSentEventHubConfig.SubscriberBuffer` | limit | unbounded memory or work | defaultServerSentEventBufferSize min 1 | `value:v4/http.ServerSentEventHubConfig.SubscriberBuffer` | — | — |
| `v4/http.ServerSentEventWriterConfig.WriteBudget` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:v4/http.ServerSentEventWriterConfig.WriteBudget` | — | — |
| `v4/http.SubscribeOptions.Buffer` | limit | unbounded memory or work | bufferSize min 1 | `value:v4/http.SubscribeOptions.Buffer` | — | — |
| `v4/http.ExceptionRenderConfig.UnavailableRetryAfter` | protection | the door: is the Retry-After of the 503 a marked infrastructure failure earns | the field GoDoc | `value:v4/http.ExceptionRenderConfig.UnavailableRetryAfter` | — | — |
| `v4/http.KernelConfig.UnavailableRetryAfter` | protection | the door: is the Retry-After of an infrastructure-failure 503 | the field GoDoc | `value:v4/http.KernelConfig.UnavailableRetryAfter` | — | — |
| `v4/http.KernelExceptionEventConfig.UnavailableRetryAfter` | protection | the door: is the Retry-After the event carries for a marked infrastructure failure | the field GoDoc | `value:v4/http.KernelExceptionEventConfig.UnavailableRetryAfter` | — | — |
| `v4/http.RegisterKernelTerminateAccessLogListener` | protection | a request leaving no access line | on, always | none | — | — |
| `v4/http.sessionCookie` | protection | a session cookie readable by a script | HttpOnly, always | none | — | — |

## `v4/http/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/contract.MethodPolicy.AutomaticOptionsDisabled` | protection | the door: AutomaticOptionsDisabled | on | `off:v4/http/contract.MethodPolicy.AutomaticOptionsDisabled` | — | — |
| `v4/http/contract.MethodPolicy.HeadFallbackDisabled` | protection | the door: HeadFallbackDisabled | on | `off:v4/http/contract.MethodPolicy.HeadFallbackDisabled` | — | — |
| `v4/http/contract.RouteProtectionConfig.CsrfDisabled` | protection | the door: exempts the route from its firewall's cross-site request forgery check, for a handler that verifies the token itself | on | `off:v4/http/contract.RouteProtectionConfig.CsrfDisabled` | — | — |
| `v4/http/contract.RouteProtectionConfig.MaxBodyBytes` | protection | the door: is this route's request body cap, larger or smaller than the application's MELODY_HTTP_MAX_REQUEST_BODY_BYTES | the field GoDoc | `value:v4/http/contract.RouteProtectionConfig.MaxBodyBytes` | — | — |

## `v4/http/cors`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/cors.Config.MaxAge` | limit | unbounded memory or work | 86400 min 0 | `value:v4/http/cors.Config.MaxAge` | — | — |
| `v4/http/cors.Config.AllowCredentials` | protection | the door: AllowCredentials | refused | `off:v4/http/cors.Config.AllowCredentials` | — | — |
| `v4/http/cors.NewService` | refusal | a scheme-less origin matching more than meant | refused when credentialed | none | — | — |

## `v4/http/middleware`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/middleware.CompressionConfig.MinSize` | limit | unbounded memory or work | 1024 min 1 | `value:v4/http/middleware.CompressionConfig.MinSize` | — | — |
| `v4/http/middleware.LimiterConfig.CleanupInterval` | limit | a loop that spins or never runs | 5 * time.Minute min bound.TimeoutMinimum | `value:v4/http/middleware.LimiterConfig.CleanupInterval` | — | — |
| `v4/http/middleware.LimiterConfig.MaxKeys` | limit | unbounded memory or work | defaultMaxRateLimitKeys min 1 | `value:v4/http/middleware.LimiterConfig.MaxKeys` | — | — |
| `v4/http/middleware.SecurityHeadersConfig.Disabled` | protection | the door: sends none of the headers. | on | `off:v4/http/middleware.SecurityHeadersConfig.Disabled` | — | — |

## `v4/http/middleware/pipeline`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/middleware/pipeline.HttpMiddlewareDefinition.AllowDuplicates` | protection | the door: admits another definition of the Name beside this one. | refused | `off:v4/http/middleware/pipeline.HttpMiddlewareDefinition.AllowDuplicates` | — | — |

## `v4/http/static`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/http/static.Config.CacheMaxAge` | limit | unbounded memory or work | 3600 min 0 | `value:v4/http/static.Config.CacheMaxAge` | — | — |
| `v4/http/static.Config.CacheDisabled` | protection | the door: sends no Cache-Control, ETag or Last-Modified, so no conditional request can be answered 304. | on | `off:v4/http/static.Config.CacheDisabled` | — | — |

## `v4/httpclient`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/httpclient.HttpClientConfig.MaxRedirects` | limit | unbounded memory or work | 10 min 1 | `value:v4/httpclient.HttpClientConfig.MaxRedirects` | — | — |
| `v4/httpclient.HttpClientConfig.Timeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:v4/httpclient.HttpClientConfig.Timeout` | — | — |
| `v4/httpclient.RequestOptions.MaxResponseBodyBytes` | limit | unbounded memory or work | defaultMaxResponseBodyBytes min 1 | `value:v4/httpclient.RequestOptions.MaxResponseBodyBytes` | — | — |
| `v4/httpclient.RequestOptions.Timeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum unlimited admitted | `value:v4/httpclient.RequestOptions.Timeout` | — | — |
| `v4/httpclient.TransportConfig.DialTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:v4/httpclient.TransportConfig.DialTimeout` | — | — |
| `v4/httpclient.TransportConfig.ExpectContinueTimeout` | limit | a call or a wait that never returns | 1 * time.Second min 0 | `value:v4/httpclient.TransportConfig.ExpectContinueTimeout` | — | — |
| `v4/httpclient.TransportConfig.IdleConnectionTimeout` | limit | a call or a wait that never returns | 90 * time.Second min time.Second unlimited admitted | `value:v4/httpclient.TransportConfig.IdleConnectionTimeout` | — | — |
| `v4/httpclient.TransportConfig.KeepAlive` | limit | unbounded memory or work | 30 * time.Second min time.Second | `value:v4/httpclient.TransportConfig.KeepAlive` | — | — |
| `v4/httpclient.TransportConfig.MaxIdleConnectionsPerHost` | limit | unbounded concurrency or connections | defaultMaxIdleConnections min 1 unlimited admitted | `value:v4/httpclient.TransportConfig.MaxIdleConnectionsPerHost` | — | — |
| `v4/httpclient.TransportConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | defaultMaxIdleConnections min 1 unlimited admitted | `value:v4/httpclient.TransportConfig.MaxIdleConnections` | — | — |
| `v4/httpclient.TransportConfig.ResponseHeaderTimeout` | limit | a call or a wait that never returns | 15 * time.Second min bound.TimeoutMinimum | `value:v4/httpclient.TransportConfig.ResponseHeaderTimeout` | — | — |
| `v4/httpclient.TransportConfig.TlsHandshakeTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:v4/httpclient.TransportConfig.TlsHandshakeTimeout` | — | — |
| `v4/httpclient.HttpClientConfig.RedirectsDisabled` | protection | the door: makes the client answer a redirect as the response it is | on | `off:v4/httpclient.HttpClientConfig.RedirectsDisabled` | — | — |
| `v4/httpclient.TransportConfig.IdleConnectionsDisabled` | protection | the door: keeps no connection idle, so every request dials its own over HTTP/1 and HTTP/2 alike, and refuses MaxIdleConnections, MaxIdleConnectionsPerHost or IdleConnectionTimeout set beside it. | on | `off:v4/httpclient.TransportConfig.IdleConnectionsDisabled` | — | — |
| `v4/httpclient.TransportConfig.KeepAliveDisabled` | protection | the door: KeepAliveDisabled | on | `off:v4/httpclient.TransportConfig.KeepAliveDisabled` | — | — |
| `v4/httpclient.TransportConfig.ProxyDisabled` | protection | the door: dials every request directly, whatever the environment says, and refuses a Proxy set beside it. | on | `off:v4/httpclient.TransportConfig.ProxyDisabled` | — | — |

## `v4/httpclient/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/httpclient/contract.RequestOptions.MaxResponseBodyBytes` | protection | the door: caps the response body | the field GoDoc | `value:v4/httpclient/contract.RequestOptions.MaxResponseBodyBytes` | — | — |
| `v4/httpclient/contract.RequestOptions.Timeout` | protection | the door: bounds the whole exchange | the field GoDoc | `value:v4/httpclient/contract.RequestOptions.Timeout` | — | — |

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
| `v4/lock.ExclusiveCommandConfig.Ttl` | limit | state held, or a decision kept, forever | exclusiveTtlRule.Default min exclusiveTtlRule.Minimum unlimited admitted | `value:v4/lock.ExclusiveCommandConfig.Ttl` | — | — |
| `v4/lock.ExclusiveConfig.Ttl` | limit | state held, or a decision kept, forever | lockcontract.LockTtlRule().Default min minimumLeaseTtl unlimited admitted | `value:v4/lock.ExclusiveConfig.Ttl` | — | — |
| `v4/lock.InMemoryLockerConfig.PurgeInterval` | limit | a loop that spins or never runs | DefaultInMemoryLockerPurgeInterval min 1 unlimited admitted | `value:v4/lock.InMemoryLockerConfig.PurgeInterval` | — | — |
| `v4/lock.LeaderGateConfig.MaxCampaignBackoff` | limit | unbounded memory or work | 1 * time.Minute min bound.TimeoutMinimum | `value:v4/lock.LeaderGateConfig.MaxCampaignBackoff` | — | — |
| `v4/lock.LeaderGateConfig.MaxConsecutiveRefreshFailures` | limit | unbounded memory or work | 3 min 1 unlimited admitted | `value:v4/lock.LeaderGateConfig.MaxConsecutiveRefreshFailures` | — | — |
| `v4/lock.LeaderGateConfig.RefreshInterval` | limit | a loop that spins or never runs | derivedRefreshInterval min minimumRefreshInterval | `value:v4/lock.LeaderGateConfig.RefreshInterval` | — | — |
| `v4/lock.LeaderGateConfig.RetryInterval` | limit | a loop that spins or never runs | derivedRetryInterval min bound.TimeoutMinimum | `value:v4/lock.LeaderGateConfig.RetryInterval` | — | — |
| `v4/lock.LeaderGateConfig.Ttl` | limit | state held, or a decision kept, forever | lockcontract.LockTtlRule().Default min minimumLeaseTtl unlimited admitted | `value:v4/lock.LeaderGateConfig.Ttl` | — | — |
| `v4/lock.stopRenewalLimitRule#1` | limit | unbounded memory or work | defaultStopRenewalLimit min bound.TimeoutMinimum unlimited admitted | `value:v4/lock.stopRenewalLimitRule`, `value:v4/lock.ExclusiveCommandConfig.StopRenewalLimit`, `value:v4/lock.ExclusiveConfig.StopRenewalLimit`, `value:v4/lock.LeaderGateConfig.StopRenewalLimit` | — | — |

## `v4/lock/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/lock/contract.Locker.CreateLock.ttl` | limit | state held, or a decision kept, forever | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:v4/lock/contract.LockTtlRule` | — | — |

## `v4/logging`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/logging.ExitConfig.BeforeExitBudget` | limit | a call or a wait that never returns | exitStepBudget min bound.TimeoutMinimum | `value:v4/logging.ExitConfig.BeforeExitBudget` | — | — |
| `v4/logging.JsonLoggerConfig.ContextNormalizationDepth` | limit | a descent without end: the stack, or a payload that nests forever | normalizeJsonContextMaxDepth min 1 max normalizeJsonContextMaxDepth | `value:v4/logging.JsonLoggerConfig.ContextNormalizationDepth` | — | — |
| `v4/logging.ShieldedStepConfig.Budget` | limit | a call or a wait that never returns | exitStepBudget min bound.TimeoutMinimum unlimited admitted | `value:v4/logging.ShieldedStepConfig.Budget` | — | — |
| `v4/logging.causeChainDepthRule#1` | limit | a descent without end: the stack, or a payload that nests forever | causeChainDefaultDepth min 1 | `value:v4/logging.DefaultLoggerConfig.CauseChainDepth`, `value:v4/logging.JsonLoggerConfig.CauseChainDepth` | — | — |
| `v4/logging.DefaultLogDirectoryMode` | mode | a journal directory readable by every local user | 0750 | `value:v4/logging.ReopenableFileWriterConfig.DirectoryMode`, `env:MELODY_LOG_DIRECTORY_MODE` | — | — |
| `v4/logging.DefaultLogFileMode` | mode | a journal readable by every local user | 0640 | `value:v4/logging.ReopenableFileWriterConfig.FileMode`, `env:MELODY_LOG_FILE_MODE` | — | — |
| `v4/logging` | redaction | a secret or a control character written into the journal | redacted and escaped | none | — | — |

## `v4/mailer`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/mailer.SmtpConfig.DataTerminationTimeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum | `value:v4/mailer.SmtpConfig.DataTerminationTimeout` | — | — |
| `v4/mailer.SmtpConfig.DialTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:v4/mailer.SmtpConfig.DialTimeout` | — | — |
| `v4/mailer.SmtpConfig.Timeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum | `value:v4/mailer.SmtpConfig.Timeout` | — | — |
| `v4/mailer.SmtpConfig.AllowPlaintext` | protection | the door: lets a relay that does not offer STARTTLS receive the message in clear | refused | `off:v4/mailer.SmtpConfig.AllowPlaintext` | — | — |

## `v4/messagebus`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/messagebus.ConsumeCommandConfig.ShutdownTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:v4/messagebus.ConsumeCommandConfig.ShutdownTimeout` | — | — |
| `v4/messagebus.InMemoryTransportConfig.BufferSize` | limit | unbounded memory or work | 64 min 0 | `value:v4/messagebus.InMemoryTransportConfig.BufferSize` | — | — |
| `v4/messagebus.ResolveHeaderLimits#1` | limit | unbounded memory or work | DefaultHeaderMaxCount min 1 | `value:v4/messagebus.HeaderLimitsConfig.MaxCount` | — | — |
| `v4/messagebus.ResolveHeaderLimits#2` | limit | unbounded memory or work | DefaultHeaderMaxBytes min 64 | `value:v4/messagebus.HeaderLimitsConfig.MaxBytes` | — | — |
| `v4/messagebus.RetryPolicy.Backoff` | limit | unbounded memory or work | Initialtime.Second Maxtime.Hour | `value:v4/messagebus.RetryPolicy.Backoff` | — | — |
| `v4/messagebus.RetryPolicy.FailureRequeueDelay` | limit | unbounded memory or work | 5 * time.Second min bound.TimeoutMinimum | `value:v4/messagebus.RetryPolicy.FailureRequeueDelay` | — | — |
| `v4/messagebus.RetryPolicy.MaxDeadLetterAttempts` | limit | guessing without end | 5 min 1 unlimited admitted | `value:v4/messagebus.RetryPolicy.MaxDeadLetterAttempts` | — | — |
| `v4/messagebus.RetryPolicy.MaxRetries` | limit | unbounded memory or work | 3 min 0 | `value:v4/messagebus.RetryPolicy.MaxRetries` | — | — |
| `v4/messagebus.HandleMiddlewareConfig.AllowMissingHandler` | protection | the door: lets a message with no registered handler pass through with a warning instead of failing the dispatch. | refused | `off:v4/messagebus.HandleMiddlewareConfig.AllowMissingHandler` | — | — |

## `v4/openapi`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/openapi.maximumSchemaDepth` | constant | a schema descent that overflows the stack | 64 | none | — | — |

## `v4/security`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security.HmacEnvelopeSignerConfig.Ttl` | limit | state held, or a decision kept, forever | 30 * time.Second min bound.TtlMinimum | `value:v4/security.HmacEnvelopeSignerConfig.Ttl` | — | — |
| `v4/security.HmacTokenSourceConfig.Leeway` | limit | a clock difference widening what a credential admits | defaultLeeway max maximumLeeway | `value:v4/security.HmacTokenSourceConfig.Leeway` | — | — |
| `v4/security.HmacTokenSourceConfig.MaxFutureExpiry` | limit | state held, or a decision kept, forever | time.Hour min bound.TtlMinimum | `value:v4/security.HmacTokenSourceConfig.MaxFutureExpiry` | — | — |
| `v4/security.InMemoryTokenStoreConfig.MaxEntries` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.MaxEntries` | — | — |
| `v4/security.InMemoryTokenStoreConfig.RevocationEpochRetention` | limit | state held, or a decision kept, forever | 7 * 24 * time.Hour min bound.TtlMinimum unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.RevocationEpochRetention` | — | — |
| `v4/security.InMemoryTokenStoreConfig.SweepInterval` | limit | a loop that spins or never runs | time.Minute min bound.TtlMinimum unlimited admitted | `value:v4/security.InMemoryTokenStoreConfig.SweepInterval` | — | — |
| `v4/security.JwtConfig.Leeway` | limit | a clock difference widening what a credential admits | defaultLeeway max maximumLeeway | `value:v4/security.JwtConfig.Leeway` | — | — |
| `v4/security.JwtConfig.MaxCandidateKeys` | limit | unbounded memory or work | defaultJwtMaxCandidateKeys min 1 | `value:v4/security.JwtConfig.MaxCandidateKeys` | — | — |
| `v4/security.JwtConfig.RevocationEpochSkew` | limit | a clock difference widening what a credential admits |  | `value:v4/security.JwtConfig.RevocationEpochSkew` | — | — |
| `v4/security.MemoryNonceGuardConfig.MaxEntries` | limit | unbounded memory or work | 100_000 min 1 unlimited admitted | `value:v4/security.MemoryNonceGuardConfig.MaxEntries` | — | — |
| `v4/security.MemoryNonceGuardConfig.PurgeInterval` | limit | a loop that spins or never runs | time.Second min bound.TimeoutMinimum | `value:v4/security.MemoryNonceGuardConfig.PurgeInterval` | — | — |
| `v4/security.TotpSecondFactorAuthenticatorConfig.LockoutWindow` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum | `value:v4/security.TotpSecondFactorAuthenticatorConfig.LockoutWindow` | — | — |
| `v4/security.TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts` | limit | guessing without end | 5 min 1 unlimited admitted | `value:v4/security.TotpSecondFactorAuthenticatorConfig.MaxFailedAttempts` | — | — |
| `v4/security.resolveMinimumSecretLength#1` | limit | a key short enough to guess | minimumHmacSha256SecretLength min minimumHmacSha256SecretLength | `value:v4/security.JwtSecretKeySetConfig.MinimumSecretLength`, `value:v4/security.HmacEnvelopeSignerConfig.MinimumSecretLength`, `value:v4/security.HmacKeyRing.MinimumSecretLength`, `value:v4/security.HmacTokenSourceConfig.MinimumSecretLength` | — | — |
| `v4/security.AccessDecisionManagerConfig.AllowIfAllAbstain` | protection | the door: grants an attribute at least one voter supported and every supporting voter abstained on | refused | `off:v4/security.AccessDecisionManagerConfig.AllowIfAllAbstain` | — | — |
| `v4/security.AccessDecisionManagerConfig.AllowIfTied` | protection | the door: grants, under DecisionStrategyConsensus, an attribute whose granted and denied counts are equal and not zero | refused | `off:v4/security.AccessDecisionManagerConfig.AllowIfTied` | — | — |
| `v4/security.CompiledFirewallConfig.LoginCsrfDisabled` | protection | the door: LoginCsrfDisabled | on | `off:v4/security.CompiledFirewallConfig.LoginCsrfDisabled` | — | — |
| `v4/security.CompiledFirewallConfig.LogoutCsrfDisabled` | protection | the door: LogoutCsrfDisabled | on | `off:v4/security.CompiledFirewallConfig.LogoutCsrfDisabled` | — | — |
| `v4/security.CompiledFirewallConfig.UnavailableRetryAfter` | protection | the door: is the Retry-After of the 503 an unavailable token earns on a protected route | the field GoDoc | `value:v4/security.CompiledFirewallConfig.UnavailableRetryAfter` | — | — |
| `v4/security.CsrfModeDisabled` | protection | a cross-site request forging an unsafe method on a session firewall | origin check on | `off:v4/security.CsrfModeDisabled`, `off:v4/http/contract.RouteProtectionConfig.CsrfDisabled`, `off:v4/security/config.LoginConfig.CsrfDisabled` | — | — |
| `v4/security.JwtConfig.AllowFutureIssuedAt` | protection | the door: accepts a token whose iat is ahead of the clock by more than the leeway | refused | `off:v4/security.JwtConfig.AllowFutureIssuedAt` | — | — |
| `v4/security.JwtConfig.AllowWithoutExpiry` | protection | the door: AllowWithoutExpiry | refused | `off:v4/security.JwtConfig.AllowWithoutExpiry` | — | — |
| `v4/security.JwtAlgorithmHs256` | refusal | an algorithm confusion: a token signed with another algorithm | HS256 only | none | — | — |

## `v4/security/accesscontrol`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/accesscontrol.Control.MatchRuleIndex` | refusal | a path no access rule covers, admitted by omission | admitted | none | — | — |

## `v4/security/config`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/config.FirewallOverride.GlobalAccessControlDisabled` | protection | the door: cuts this firewall off the global access control | on | `off:v4/security/config.FirewallOverride.GlobalAccessControlDisabled` | — | — |
| `v4/security/config.FirewallOverride.UnavailableRetryAfter` | protection | the door: overrides GlobalConfig.UnavailableRetryAfter for this firewall | the field GoDoc | `value:v4/security/config.FirewallOverride.UnavailableRetryAfter` | — | — |
| `v4/security/config.GlobalConfig.UnavailableRetryAfter` | protection | the door: is the Retry-After of the 503 an unavailable token earns on a protected route | the field GoDoc | `value:v4/security/config.GlobalConfig.UnavailableRetryAfter` | — | — |
| `v4/security/config.LoginConfig.CsrfDisabled` | protection | the door: turns the door's forgery check off, the stated other origin refused without a session cookie included | on | `off:v4/security/config.LoginConfig.CsrfDisabled` | — | — |
| `v4/security/config.LoginConfig.Limiter` | protection | credential guessing at the login door | none metered | none | — | — |
| `v4/security/config.LogoutConfig.CsrfDisabled` | protection | the door: CsrfDisabled | on | `off:v4/security/config.LogoutConfig.CsrfDisabled` | — | — |

## `v4/security/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/contract.FailedAttemptStore.Increment.window` | limit | state held, or a decision kept, forever | min bound.TtlMinimum | `value:v4/security/contract.FailedAttemptWindowRule` | — | — |
| `v4/security/contract.NonceGuard.Remember.ttl` | limit | state held, or a decision kept, forever | min bound.TimeoutMinimum | `value:v4/security/contract.NonceTtlRule` | — | — |
| `v4/security/contract.RevocableTokenStore.Put.ttl` | limit | state held, or a decision kept, forever | min bound.TtlMinimum unlimited admitted | `value:v4/security/contract.TokenTtlRule` | — | — |

## `v4/security/totp`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/security/totp.totp.Config.Digits` | limit | a code short enough to guess | 6 min 6 max 8 | `value:v4/security/totp.Config.Digits` | — | — |
| `v4/security/totp.totp.Config.Period` | limit | a loop that spins or never runs | 30 * time.Second min bound.TtlMinimum max 24 * time.Hour | `value:v4/security/totp.Config.Period` | — | — |
| `v4/security/totp.totp.Config.Skew` | limit | a clock difference widening what a credential admits | 1 min 0 max 10 | `value:v4/security/totp.Config.Skew` | — | — |

## `v4/serializer`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/serializer.PlainTextSerializerConfig.MaxBytes` | limit | unbounded memory or work | plaintext.DefaultMaxBytes min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxBytes` | — | — |
| `v4/serializer.PlainTextSerializerConfig.MaxDepth` | limit | a descent without end: the stack, or a payload that nests forever | plaintext.DefaultMaxDepth min 1 max maxPlainTextDepth | `value:v4/serializer.PlainTextSerializerConfig.MaxDepth` | — | — |
| `v4/serializer.PlainTextSerializerConfig.MaxLines` | limit | unbounded memory or work | plaintext.DefaultMaxLines min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxLines` | — | — |
| `v4/serializer.PlainTextSerializerConfig.MaxMembers` | limit | unbounded memory or work | plaintext.DefaultMaxMembers min 1 unlimited admitted | `value:v4/serializer.PlainTextSerializerConfig.MaxMembers` | — | — |

## `v4/session`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/session.FileStorageConfig.JournalMaxBytes` | limit | unbounded memory or work | DefaultJournalMaxBytes min 64 << 10 | `value:v4/session.FileStorageConfig.JournalMaxBytes` | — | — |
| `v4/session.FileStorageConfig.MaxSessions` | limit | unbounded memory or work | DefaultMaxSessions min 1 unlimited admitted | `value:v4/session.FileStorageConfig.MaxSessions` | — | — |
| `v4/session.InMemoryStorageConfig.CleanupInterval` | limit | a loop that spins or never runs | time.Minute min bound.TimeoutMinimum | `value:v4/session.InMemoryStorageConfig.CleanupInterval` | — | — |
| `v4/session.InMemoryStorageConfig.MaxSessions` | limit | unbounded memory or work | DefaultMaxSessions min 1 unlimited admitted | `value:v4/session.InMemoryStorageConfig.MaxSessions` | — | — |
| `v4/session.ManagerConfig.StorageTimeout` | limit | a call or a wait that never returns | DefaultStorageTimeout min bound.TimeoutMinimum unlimited admitted | `value:v4/session.ManagerConfig.StorageTimeout` | — | — |
| `v4/session.ManagerConfig.TombstoneRetention` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum | `value:v4/session.ManagerConfig.TombstoneRetention` | — | — |
| `v4/session.ManagerConfig.Ttl` | limit | state held, or a decision kept, forever | 24 * time.Hour min bound.TtlMinimum unlimited admitted | `value:v4/session.ManagerConfig.Ttl` | — | — |
| `v4/session.newFileStorageFromPath#1` | mode | a session directory readable by every local user | 0755 | none | — | — |
| `v4/session.openSessionJournal#1` | mode | a session journal readable beyond its owner | 0600 | none | — | — |
| `v4/session.writeSessionBytesAtomically#1` | mode | a session directory readable by every local user | 0755 | none | — | — |
| `v4/session.FileStorageConfig.JournalDisabled` | protection | the door: writes the whole snapshot on every Save and Delete over a path | on | `off:v4/session.FileStorageConfig.JournalDisabled` | — | — |

## `v4/session/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/session/contract.ttl` | limit | state held, or a decision kept, forever | min bound.TtlMinimum unlimited admitted | `value:v4/session/contract.TtlRule` | — | — |

## `v4/storage`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/storage.LocalStorageConfig.SweepBatchSize` | limit | unbounded memory or work | storageSweepBatchSize min 1 | `value:v4/storage.LocalStorageConfig.SweepBatchSize` | — | — |
| `v4/storage.LocalStorageConfig.TemporaryStaleAge` | limit | unbounded memory or work | storageTemporaryStaleAge min bound.TtlMinimum unlimited admitted | `value:v4/storage.LocalStorageConfig.TemporaryStaleAge` | — | — |
| `v4/storage.LocalStorage.Get#1` | mode | — | 0 (read only) | none | opened read only: the mode argument creates nothing | — |
| `v4/storage.LocalStorage.Put#1` | mode | a storage directory readable beyond its owner | 0750 | none | — | — |
| `v4/storage.LocalStorage.Put#2` | mode | a storage directory readable beyond its owner | 0750 | none | — | — |
| `v4/storage.NewLocalStorage#1` | mode | a storage directory readable beyond its owner | 0750 | none | — | — |
| `v4/storage.createStorageTemporaryFile#1` | mode | a stored object readable by every local user | 0640 | none | — | — |
| `v4/storage.readStorageContentType#1` | mode | — | 0 (read only) | none | opened read only: the mode argument creates nothing | — |

## `v4/storage/contract`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/storage/contract.PresignConfig.Expiry` | limit | state held, or a decision kept, forever | 15 * time.Minute min bound.TtlMinimum max 7 * 24 * time.Hour | `value:v4/storage/contract.PresignConfig.Expiry` | — | — |

## `v4/translation`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/translation.maxInterpolationDepth` | constant | a message that interpolates itself forever | 32 | none | — | — |

## `v4/validation`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `v4/validation.ValidatorConfig.MaxAnswerBytes` | limit | unbounded memory or work | maxValidationAnswerBytes min 1 | `value:v4/validation.ValidatorConfig.MaxAnswerBytes` | — | — |
| `v4/validation.ValidatorConfig.MaxErrors` | limit | unbounded memory or work | maxValidationErrors min 1 | `value:v4/validation.ValidatorConfig.MaxErrors` | — | — |
| `v4/validation.ValidatorConfig.MaxNestingDepth` | limit | a descent without end: the stack, or a payload that nests forever | maxNestedValidationDepth min 1 max maxNestingDepthCeiling | `value:v4/validation.ValidatorConfig.MaxNestingDepth` | — | — |
| `v4/validation.ValidatorConfig.MaxRenderedMapKeyLength` | limit | unbounded memory or work | maxRenderedMapKeyLength min 1 | `value:v4/validation.ValidatorConfig.MaxRenderedMapKeyLength` | — | — |
| `v4/validation.ValidatorConfig.MaxRenderedPathLength` | limit | unbounded memory or work | maxRenderedPathLength min 1 | `value:v4/validation.ValidatorConfig.MaxRenderedPathLength` | — | — |

## `integrations/amqp/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/amqp/v4.maxDelayBuckets` | constant | a delay exchange per distinct delay | 8 | none | — | — |
| `integrations/amqp/v4.ConnectionConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.ConnectionConfig.CloseTimeout` | — | — |
| `integrations/amqp/v4.ProviderConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.ProviderConfig.CloseTimeout` | — | — |
| `integrations/amqp/v4.ProviderConfig.DialTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:integrations/amqp/v4.ProviderConfig.DialTimeout` | — | — |
| `integrations/amqp/v4.ProviderConfig.Heartbeat` | limit | unbounded memory or work | 10 * time.Second min time.Second | `value:integrations/amqp/v4.ProviderConfig.Heartbeat` | — | — |
| `integrations/amqp/v4.ReconnectConfig.Backoff` | limit | unbounded memory or work | Initialparent.Initial Maxparent.Max | `value:integrations/amqp/v4.ReconnectConfig.Backoff` | — | — |
| `integrations/amqp/v4.ServerSentEventBackplaneConfig.CallTimeout` | limit | a call or a wait that never returns | time.Second min bound.TimeoutMinimum | `value:integrations/amqp/v4.ServerSentEventBackplaneConfig.CallTimeout` | — | — |
| `integrations/amqp/v4.ServerSentEventBackplaneConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.ServerSentEventBackplaneConfig.CloseTimeout` | — | — |
| `integrations/amqp/v4.TransportConfig.CallTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum | `value:integrations/amqp/v4.TransportConfig.CallTimeout` | — | — |
| `integrations/amqp/v4.TransportConfig.CloseTimeout` | limit | a call or a wait that never returns | closeJoinTimeout min bound.TimeoutMinimum | `value:integrations/amqp/v4.TransportConfig.CloseTimeout` | — | — |
| `integrations/amqp/v4.TransportConfig.Prefetch` | limit | unbounded concurrency or connections | defaultPrefetch min 1 max maxPrefetch | `value:integrations/amqp/v4.TransportConfig.Prefetch` | — | — |
| `integrations/amqp/v4.TransportConfig.PublishReturnBuffer` | limit | unbounded memory or work | 16 min 1 | `value:integrations/amqp/v4.TransportConfig.PublishReturnBuffer` | — | — |
| `integrations/amqp/v4.TransportConfig.DeadLetterDisabled` | protection | the door: declares the queue without its <queue>.dlx exchange and <queue>.dlq queue, so a poison message is dropped | on | `off:integrations/amqp/v4.TransportConfig.DeadLetterDisabled` | — | — |
| `integrations/amqp/v4.TransportConfig` | protection | a dead-letter queue that grows without end | no cap | none | — | — |

## `integrations/aws/s3/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/aws/s3/v4.StorageConfig.MultipartAbortTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:integrations/aws/s3/v4.StorageConfig.MultipartAbortTimeout` | — | — |
| `integrations/aws/s3/v4.StorageConfig.PutSpoolLimit` | limit | unbounded memory or work | 16 * 1024 * 1024 min 1 | `value:integrations/aws/s3/v4.StorageConfig.PutSpoolLimit` | — | — |

## `integrations/bunorm/migrate/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/migrate/v4.Options.UnlockTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/migrate/v4.Options.UnlockTimeout` | — | — |
| `integrations/bunorm/migrate/v4.migrationFileMode` | mode | a generated migration readable beyond the repository's own files | 0644 | none | a migration file is source the repository commits: its mode is the repository's, like any file the developer writes | — |
| `integrations/bunorm/migrate/v4.printAppliedGroup` | refusal | a migration that ran but was not recorded, run again | warning | none | — | — |

## `integrations/bunorm/mysql/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/mysql/v4.LockerConfig.ReleaseTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/mysql/v4.LockerConfig.ReleaseTimeout` | — | — |
| `integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxIdleTime` | limit | state held, or a decision kept, forever | 1 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxIdleTime` | — | — |
| `integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxLifetime` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.ConnectionMaxLifetime` | — | — |
| `integrations/bunorm/mysql/v4.PoolConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | 5 min 0 unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.MaxIdleConnections` | — | — |
| `integrations/bunorm/mysql/v4.PoolConfig.MaxOpenConnections` | limit | unbounded concurrency or connections | 25 min 1 unlimited admitted | `value:integrations/bunorm/mysql/v4.PoolConfig.MaxOpenConnections` | — | — |
| `integrations/bunorm/mysql/v4.RetryConfig.Backoff` | limit | unbounded memory or work | Initial500 * time.Millisecond Max5 * time.Second | `value:integrations/bunorm/mysql/v4.RetryConfig.Backoff` | — | — |
| `integrations/bunorm/mysql/v4.RetryConfig.MaxAttempts` | limit | guessing without end | 3 min 1 | `value:integrations/bunorm/mysql/v4.RetryConfig.MaxAttempts` | — | — |
| `integrations/bunorm/mysql/v4.TimeoutConfig.ConnectTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/mysql/v4.TimeoutConfig.ConnectTimeout` | — | — |
| `integrations/bunorm/mysql/v4.TimeoutConfig.ReadTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.TimeoutConfig.ReadTimeout` | — | — |
| `integrations/bunorm/mysql/v4.TimeoutConfig.WriteTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/mysql/v4.TimeoutConfig.WriteTimeout` | — | — |

## `integrations/bunorm/pgsql/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/pgsql/v4.LockerConfig.ReleaseTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/pgsql/v4.LockerConfig.ReleaseTimeout` | — | — |
| `integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxIdleTime` | limit | state held, or a decision kept, forever | 1 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxIdleTime` | — | — |
| `integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxLifetime` | limit | state held, or a decision kept, forever | 5 * time.Minute min bound.TtlMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.ConnectionMaxLifetime` | — | — |
| `integrations/bunorm/pgsql/v4.PoolConfig.MaxIdleConnections` | limit | unbounded concurrency or connections | 25 min 0 unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.MaxIdleConnections` | — | — |
| `integrations/bunorm/pgsql/v4.PoolConfig.MaxOpenConnections` | limit | unbounded concurrency or connections | 50 min 1 unlimited admitted | `value:integrations/bunorm/pgsql/v4.PoolConfig.MaxOpenConnections` | — | — |
| `integrations/bunorm/pgsql/v4.RetryConfig.Backoff` | limit | unbounded memory or work | Initial500 * time.Millisecond Max5 * time.Second | `value:integrations/bunorm/pgsql/v4.RetryConfig.Backoff` | — | — |
| `integrations/bunorm/pgsql/v4.RetryConfig.MaxAttempts` | limit | guessing without end | 3 min 1 | `value:integrations/bunorm/pgsql/v4.RetryConfig.MaxAttempts` | — | — |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.ConnectTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.ConnectTimeout` | — | — |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.ReadTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.ReadTimeout` | — | — |
| `integrations/bunorm/pgsql/v4.TimeoutConfig.WriteTimeout` | limit | a call or a wait that never returns | 30 * time.Second min bound.TimeoutMinimum unlimited admitted | `value:integrations/bunorm/pgsql/v4.TimeoutConfig.WriteTimeout` | — | — |

## `integrations/bunorm/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4.SplitterConfig.ReplicaRetryInterval` | limit | a loop that spins or never runs | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/bunorm/v4.SplitterConfig.ReplicaRetryInterval` | — | — |
| `integrations/bunorm/v4.SplitterConfig` | protection | reads failing while the primary still answers | fall back to the primary | none | — | — |

## `integrations/bunorm/v4/audit`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4/audit.AsyncStorageConfig.BufferSize` | limit | unbounded memory or work | 1024 min 1 | `value:integrations/bunorm/v4/audit.AsyncStorageConfig.BufferSize` | — | — |
| `integrations/bunorm/v4/audit.FileStorage.Save#1` | mode | an audit trail readable by every local user | 0640 | none | — | — |
| `integrations/bunorm/v4/audit.RegistryConfig` | redaction | an encrypted column's plaintext in the audit trail | masked | none | — | — |

## `integrations/bunorm/v4/encrypt`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/bunorm/v4/encrypt.TableSpecification.BatchSize` | limit | unbounded memory or work | defaultMigrateBatchSize min 1 | `value:integrations/bunorm/v4/encrypt.TableSpecification.BatchSize` | — | — |
| `integrations/bunorm/v4/encrypt.ColumnSealBinding.UnboundAdmitted` | protection | the door: UnboundAdmitted | refused | `off:integrations/bunorm/v4/encrypt.ColumnSealBinding.UnboundAdmitted` | — | — |
| `integrations/bunorm/v4/encrypt.SealBindingConfig.UnboundAdmitted` | protection | the door: UnboundAdmitted | refused | `off:integrations/bunorm/v4/encrypt.SealBindingConfig.UnboundAdmitted` | — | — |

## `integrations/cron/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/cron/v4.EntryConfig.Instances` | limit | unbounded memory or work | 1 min 1 | `value:integrations/cron/v4.EntryConfig.Instances` | — | — |
| `integrations/cron/v4.EntryConfig.ShutdownTimeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum | `value:integrations/cron/v4.EntryConfig.ShutdownTimeout` | — | — |
| `integrations/cron/v4.EntryConfig.TickDedup.Window` | limit | state held, or a decision kept, forever | min time.Second | `value:integrations/cron/v4.EntryConfig.TickDedup.Window` | — | — |
| `integrations/cron/v4.EntryConfig.Timeout` | limit | a call or a wait that never returns | min bound.TimeoutMinimum unlimited admitted | `value:integrations/cron/v4.EntryConfig.Timeout` | — | — |
| `integrations/cron/v4.RunnerConfig.ClockJumpResetThreshold` | limit | unbounded memory or work | 3 * time.Hour min 2 * time.Minute | `value:integrations/cron/v4.RunnerConfig.ClockJumpResetThreshold` | — | — |
| `integrations/cron/v4.RunnerConfig.OutputCaptureLimit` | limit | unbounded memory or work | 64 * 1024 min 1 | `value:integrations/cron/v4.RunnerConfig.OutputCaptureLimit` | — | — |
| `integrations/cron/v4.RunnerConfig.ShutdownTimeout` | limit | a call or a wait that never returns | 5 * time.Minute min bound.TimeoutMinimum | `value:integrations/cron/v4.RunnerConfig.ShutdownTimeout` | — | — |
| `integrations/cron/v4.RunnerConfig.TickDedup.CallTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/cron/v4.RunnerConfig.TickDedup.CallTimeout` | — | — |
| `integrations/cron/v4.RunnerConfig.TickDedup.Window` | limit | state held, or a decision kept, forever | 5 * time.Minute min time.Second | `value:integrations/cron/v4.RunnerConfig.TickDedup.Window` | — | — |
| `integrations/cron/v4.RunnerConfig.Timeout` | limit | a call or a wait that never returns | time.Hour min bound.TimeoutMinimum unlimited admitted | `value:integrations/cron/v4.RunnerConfig.Timeout` | — | — |
| `integrations/cron/v4.GenerateCommand.writeDestinations#1` | mode | a crontab directory writable beyond its owner | 0755 | none | — | — |
| `integrations/cron/v4.createTheLogsRoot#1` | mode | a logs root writable beyond its owner | 0755 | none | — | — |
| `integrations/cron/v4.resolveEntryLogPath#1` | mode | a log directory writable beyond its owner | 0755 | none | — | — |
| `integrations/cron/v4.EntryConfig.LogDisabled` | protection | the door: LogDisabled | on | `off:integrations/cron/v4.EntryConfig.LogDisabled` | — | — |
| `integrations/cron/v4.EntryTickDedupConfig.Disabled` | protection | the door: runs this entry on every replica that evaluates its tick, whatever RunnerConfig.TickDedup says | on | `off:integrations/cron/v4.EntryTickDedupConfig.Disabled` | — | — |
| `integrations/cron/v4.EntryTickDedupConfig.Window` | protection | the door: replaces RunnerConfig.TickDedup.Window for this entry | the field GoDoc | `value:integrations/cron/v4.EntryTickDedupConfig.Window` | — | — |
| `integrations/cron/v4.TickDedupConfig.CallTimeout` | protection | the door: bounds each acquire and each release the runner issues | the field GoDoc | `value:integrations/cron/v4.TickDedupConfig.CallTimeout` | — | — |
| `integrations/cron/v4.TickDedupConfig.Disabled` | protection | the door: turns the dedup off for every entry | on | `off:integrations/cron/v4.TickDedupConfig.Disabled` | — | — |
| `integrations/cron/v4.TickDedupConfig.Window` | protection | the door: is how far apart two replicas may evaluate one tick and still be deduplicated, the clock skew and the wake latency between them | the field GoDoc | `value:integrations/cron/v4.TickDedupConfig.Window` | — | — |
| `integrations/cron/v4.TickDedupConfig` | protection | a tick run twice over a locker without a lease | deduplicated | none | — | — |
| `integrations/cron/v4.RunnerCommand.Run` | refusal | a cron User the in-process runner cannot honour | warning | none | — | — |

## `integrations/opentelemetry/v4/otlp`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/opentelemetry/v4/otlp.Config.BatchTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/opentelemetry/v4/otlp.Config.BatchTimeout` | — | — |
| `integrations/opentelemetry/v4/otlp.Config.Insecure` | protection | spans in clear to a non-loopback collector | transport security on | `off:integrations/opentelemetry/v4/otlp.Config.Insecure` | — | — |

## `integrations/outbox/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/outbox/v4.RelayCommandConfig.ErrorBackoff` | limit | unbounded memory or work | Initialbound.TimeoutMinimum Maxtime.Minute | `value:integrations/outbox/v4.RelayCommandConfig.ErrorBackoff` | — | — |
| `integrations/outbox/v4.RelayConfig.Backoff` | limit | unbounded memory or work | Initial15 * time.Second Max10 * time.Minute | `value:integrations/outbox/v4.RelayConfig.Backoff` | — | — |
| `integrations/outbox/v4.RelayConfig.BatchSize` | limit | unbounded memory or work | 100 min 1 max maximumBatchSize | `value:integrations/outbox/v4.RelayConfig.BatchSize` | — | — |
| `integrations/outbox/v4.RelayConfig.LockTtl` | limit | state held, or a decision kept, forever | 30 * time.Second min bound.TtlMinimum | `value:integrations/outbox/v4.RelayConfig.LockTtl` | — | — |
| `integrations/outbox/v4.RelayConfig.MaxAttempts` | limit | guessing without end | 12 min 1 | `value:integrations/outbox/v4.RelayConfig.MaxAttempts` | — | — |
| `integrations/outbox/v4.RelayConfig.MaxDeliveryAttempts` | limit | guessing without end | min 1 | `value:integrations/outbox/v4.RelayConfig.MaxDeliveryAttempts` | — | — |
| `integrations/outbox/v4.RelayConfig.ResolutionTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/outbox/v4.RelayConfig.ResolutionTimeout` | — | — |
| `integrations/outbox/v4.RelayConfig.UnavailableBackoff` | limit | unbounded memory or work | Initial15 * time.Second Maxtime.Minute | `value:integrations/outbox/v4.RelayConfig.UnavailableBackoff` | — | — |
| `integrations/outbox/v4.RelayConfig.VisibilityTimeout` | limit | a call or a wait that never returns | 5 * time.Minute min bound.TtlMinimum | `value:integrations/outbox/v4.RelayConfig.VisibilityTimeout` | — | — |

## `integrations/rueidis/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/rueidis/v4.ClientConfig.BootPingTimeout` | limit | a call or a wait that never returns | 3 * time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4.ClientConfig.BootPingTimeout` | — | — |
| `integrations/rueidis/v4.ClientConfig.ClientSideCacheBytes` | limit | unbounded memory or work | rueidis.DefaultCacheBytes min 1 | `value:integrations/rueidis/v4.ClientConfig.ClientSideCacheBytes` | — | — |
| `integrations/rueidis/v4.ClientConfig.ConnectionWriteTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4.ClientConfig.ConnectionWriteTimeout` | — | — |
| `integrations/rueidis/v4.ClientConfig.DialTimeout` | limit | a call or a wait that never returns | 5 * time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4.ClientConfig.DialTimeout` | — | — |
| `integrations/rueidis/v4.ReconnectConfig.Backoff` | limit | unbounded memory or work | Initialtime.Second Max30 * time.Second | `value:integrations/rueidis/v4.ReconnectConfig.Backoff` | — | — |
| `integrations/rueidis/v4.RetryConfig.Backoff` | limit | unbounded memory or work | Initial500 * time.Millisecond Max5 * time.Second | `value:integrations/rueidis/v4.RetryConfig.Backoff` | — | — |
| `integrations/rueidis/v4.RetryConfig.MaxAttempts` | limit | guessing without end | 3 min 1 | `value:integrations/rueidis/v4.RetryConfig.MaxAttempts` | — | — |
| `integrations/rueidis/v4.TokenStoreConfig.MaximumClockSkew` | limit | a clock difference widening what a credential admits | 0 min 0 | `value:integrations/rueidis/v4.TokenStoreConfig.MaximumClockSkew` | — | — |
| `integrations/rueidis/v4.TokenStoreConfig.RevocationEpochRetention` | limit | state held, or a decision kept, forever | defaultRevocationEpochRetention min bound.TtlMinimum unlimited admitted | `value:integrations/rueidis/v4.TokenStoreConfig.RevocationEpochRetention` | — | — |
| `integrations/rueidis/v4.TokenStoreConfig.ScanCount` | limit | unbounded memory or work | defaultTokenStoreScanCount min 1 | `value:integrations/rueidis/v4.TokenStoreConfig.ScanCount` | — | — |
| `integrations/rueidis/v4.callTimeoutRule#1` | limit | a call or a wait that never returns | fallback min bound.TimeoutMinimum | `value:integrations/rueidis/v4.callTimeoutRule`, `value:integrations/rueidis/v4.LockerConfig.CallTimeout`, `value:integrations/rueidis/v4.NonceGuardConfig.CallTimeout`, `value:integrations/rueidis/v4.RateLimiterConfig.CallTimeout`, `value:integrations/rueidis/v4.ServerSentEventBackplaneConfig.CallTimeout`, `value:integrations/rueidis/v4.TokenStoreConfig.CallTimeout` | — | — |
| `integrations/rueidis/v4.ClientConfig.BootPingDisabled` | protection | the door: skips the ping that verifies the store answers before Open returns | on | `off:integrations/rueidis/v4.ClientConfig.BootPingDisabled` | — | — |
| `integrations/rueidis/v4.LockerConfig.KeyPrefixDisabled` | protection | the door: keys a lock by its bare name | on | `off:integrations/rueidis/v4.LockerConfig.KeyPrefixDisabled` | — | — |

## `integrations/rueidis/v4/cache`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/rueidis/v4/cache.BackendConfig.CallTimeout` | limit | a call or a wait that never returns | time.Second min bound.TimeoutMinimum | `value:integrations/rueidis/v4/cache.BackendConfig.CallTimeout` | — | — |
| `integrations/rueidis/v4/cache.BackendConfig.DeleteBatch` | limit | unbounded memory or work | 500 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.DeleteBatch` | — | — |
| `integrations/rueidis/v4/cache.BackendConfig.MaxKeyLength` | limit | unbounded memory or work | 1024 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.MaxKeyLength` | — | — |
| `integrations/rueidis/v4/cache.BackendConfig.ScanCount` | limit | unbounded memory or work | 1000 min 1 | `value:integrations/rueidis/v4/cache.BackendConfig.ScanCount` | — | — |

## `integrations/websocket/v4`

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `integrations/websocket/v4.StreamHandlerConfig.CloseHandshakeGrace` | limit | unbounded memory or work | time.Second min bound.TimeoutMinimum | `value:integrations/websocket/v4.StreamHandlerConfig.CloseHandshakeGrace` | — | — |
| `integrations/websocket/v4.StreamHandlerConfig.IdleTimeout` | limit | a call or a wait that never returns | 30 * time.Second min time.Second | `value:integrations/websocket/v4.StreamHandlerConfig.IdleTimeout` | — | — |
| `integrations/websocket/v4.StreamHandlerConfig.ReadLimit` | limit | unbounded memory or work | 32768 min 1 unlimited admitted | `value:integrations/websocket/v4.StreamHandlerConfig.ReadLimit` | — | — |
| `integrations/websocket/v4.StreamHandlerConfig.WriteTimeout` | limit | a call or a wait that never returns | 10 * time.Second min bound.TimeoutMinimum | `value:integrations/websocket/v4.StreamHandlerConfig.WriteTimeout` | — | — |

## Boot warnings

| key | kind | protects against | default | door | reason | documented |
|---|---|---|---|---|---|---|
| `application.environment.processVariableIgnored` | warning | a process variable the operator believes is read | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | — |
| `application.environment.undeclaredKey` | warning | a MELODY_ key nothing reads, a typo the operator believes is set | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | — |
| `application.secretMark.unmatched` | warning | a secret mark that protects no parameter | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | — |
| `application.service.builtAtBootFailed` | warning | a service that failed its boot build going unnoticed | written in http | none | — | — |
| `aws.s3.plaintextEndpoint` | warning | object traffic in clear to a non-loopback endpoint | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | — |
| `http.session.unboundedTtlOnBoundedDefaultStorage` | warning | sessions without end on a storage that evicts | written in http | none | — | — |
| `http.static.cacheMaxAgeIgnored` | warning | a cache max age the operator believes is applied | written in http | none | — | — |
| `messagebus.retryPolicyServiceIgnored` | warning | a retry policy service the operator believes is used | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | — |
| `security.csrfDisabledOnLoginFirewall` | warning | a login firewall without its forgery check | written in http | none | — | — |
| `security.firewallDoorReachUnknown` | warning | a firewall door whose reach the compiler cannot prove | written | `env:MELODY_CLI_BOOT_WARNINGS_IGNORED` | — | — |
