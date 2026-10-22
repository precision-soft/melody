package contract

import "time"

type HttpConfiguration interface {
    Address() string

    DefaultLocale() string

    PublicDir() string

    StaticIndexFile() string

    MaxRequestBodyBytes() int

    StaticEnableCache() bool

    StaticCacheMaxAge() int

    /* StaticExcludedPaths names the path prefixes the built-in file server declines without touching the disk; the framework's configuration excludes none by default. */
    StaticExcludedPaths() []string

    /* SessionTtl is how long a stored session stays valid; the framework's configuration defaults it to config.DefaultSessionTtl, no expiry. */
    SessionTtl() time.Duration

    /* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back; the framework's configuration defaults it to config.DefaultSessionTombstoneRetention. */
    SessionTombstoneRetention() time.Duration

    /* ShutdownTimeout is how long a stopping http server waits for the requests it has admitted; the framework's configuration defaults it to config.DefaultHttpShutdownTimeout. */
    ShutdownTimeout() time.Duration
}
