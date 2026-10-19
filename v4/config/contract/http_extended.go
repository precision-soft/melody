package contract

import "time"

/* ExtendedHttpConfiguration carries the http settings this major added after v3.13.0. The framework's own configuration implements it, and an implementation of HttpConfiguration that does not is read with the default named on each method. */
type ExtendedHttpConfiguration interface {
    /* StaticExcludedPaths names the path prefixes the built-in file server declines; absent, no path is excluded. */
    StaticExcludedPaths() []string

    /* SessionTtl is how long a stored session stays valid; absent, config.DefaultSessionTtl, no expiry. */
    SessionTtl() time.Duration

    /* SessionTombstoneRetention is how long a deleted session id keeps refusing a write-back; absent, config.DefaultSessionTombstoneRetention. */
    SessionTombstoneRetention() time.Duration

    /* ShutdownTimeout is how long a stopping http server waits for the requests it has admitted; absent, config.DefaultHttpShutdownTimeout. */
    ShutdownTimeout() time.Duration
}
