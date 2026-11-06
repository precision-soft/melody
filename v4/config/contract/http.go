package contract

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

type HttpConfiguration interface {
    Address() string

    DefaultLocale() string

    PublicDir() string

    StaticIndexFile() string

    MaxRequestBodyBytes() bound.Resolved[int]

    StaticEnableCache() bool

    StaticCacheMaxAge() bound.Resolved[int]

    /* StaticExcludedPaths names the path prefixes the built-in file server declines without touching the disk; the framework's configuration excludes none by default. */
    StaticExcludedPaths() []string

    /* SessionTtl is how long a stored session stays valid; the framework's configuration defaults it to config.DefaultSessionTtl, 24h, and reads unlimited as no expiry. */
    SessionTtl() bound.Resolved[time.Duration]

    /* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back; the framework's configuration defaults it to config.DefaultSessionTombstoneRetention. */
    SessionTombstoneRetention() bound.Resolved[time.Duration]

    /* ShutdownTimeout is how long a stopping http server waits for the requests it has admitted; the framework's configuration defaults it to config.DefaultHttpShutdownTimeout. */
    ShutdownTimeout() bound.Resolved[time.Duration]

    /* ReadTimeout, ReadHeaderTimeout, WriteTimeout, IdleTimeout and MaxHeaderBytes are the server limit keys as the deployment set them, each absent when its key says nothing: the application decides them against its own config, the key winning. */
    ReadTimeout() bound.Duration

    ReadHeaderTimeout() bound.Duration

    WriteTimeout() bound.Duration

    IdleTimeout() bound.Duration

    MaxHeaderBytes() bound.Count
}
