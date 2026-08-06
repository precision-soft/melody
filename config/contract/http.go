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

    StaticExcludedPaths() []string

    SessionTtl() time.Duration

    ShutdownTimeout() time.Duration
}
