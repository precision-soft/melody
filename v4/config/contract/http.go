package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

type HttpConfiguration interface {
    Address() string

    /* PublicUrl is the public base GenerateUrl answers behind, MELODY_HTTP_PUBLIC_URL and ApplicationConfig.HttpPublicUrl when the key is unset, an http or https url whose path carries no trailing slash, empty when neither names one. */
    PublicUrl() string

    DefaultLocale() string

    PublicDirectory() string

    StaticIndexFile() string

    MaxRequestBodyBytes() bound.Resolved[int]

    /* StaticCacheDisabled switches off the built-in file server's cache headers and validators; false by default. */
    StaticCacheDisabled() bool

    StaticCacheMaxAge() bound.Resolved[int]

    /* StaticExcludedPaths names the path prefixes the built-in file server declines without touching the disk. */
    StaticExcludedPaths() []string

    /* SessionTtl is how long a stored session stays valid; unlimited is no expiry. */
    SessionTtl() bound.Resolved[time.Duration]

    /* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back; the framework's configuration defaults it to config.DefaultSessionTombstoneRetention. */
    SessionTombstoneRetention() bound.Resolved[time.Duration]

    /* SessionStorageTimeout bounds every call of the session manager to its storage: the load, the mint, the rotation, the save and the delete. */
    SessionStorageTimeout() bound.Resolved[time.Duration]

    /* SessionMaxSessions is how many sessions the framework-wired in-memory storage holds at once. */
    SessionMaxSessions() bound.Resolved[int]

    /* ShutdownTimeout is MELODY_HTTP_SHUTDOWN_TIMEOUT as the deployment set it, absent when unset: how long a stopping http server waits for the requests it has admitted. */
    ShutdownTimeout() bound.Duration

    /* ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout and MaxHeaderBytes are the server limit keys as the deployment set them, absent when unset. */
    ReadTimeout() bound.Duration

    ReadHeaderTimeout() bound.Duration

    WriteTimeout() bound.Duration

    IdleTimeout() bound.Duration

    MaxHeaderBytes() bound.Count

    /* UnavailableRetryAfter is MELODY_HTTP_UNAVAILABLE_RETRY_AFTER as the deployment set it, absent when unset. */
    UnavailableRetryAfter() bound.Duration

    /* SessionEmptyOnOutage is MELODY_HTTP_SESSION_EMPTY_ON_OUTAGE as the deployment set it, off or unset when the key says nothing: the key only hardens, true is refused naming the key. */
    SessionEmptyOnOutage() Switch

    /* SessionCookieSecure is MELODY_HTTP_SESSION_COOKIE_SECURE as the deployment set it, always or empty when the key says nothing. */
    SessionCookieSecure() string
}
