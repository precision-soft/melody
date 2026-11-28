package bound_test

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/container"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    "github.com/precision-soft/melody/v4/session"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

/* expectRule fails when a rule's name, default or bounds are not the documented ones */
func expectRule[T bound.Unit](t *testing.T, ruleName string, actual bound.Rule[T], expected bound.Rule[T]) {
    t.Helper()

    if expected != actual {
        t.Fatalf("%s: expected %+v, got %+v", ruleName, expected, actual)
    }
}

func TestRule_EveryFrameworkRuleCarriesItsDocumentedValues(t *testing.T) {
    expectRule(t, "cachecontract.TtlRule", cachecontract.TtlRule(), bound.Rule[time.Duration]{
        Name: "ttl",
        Minimum: time.Second,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "config.HttpIdleTimeoutRule", config.HttpIdleTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ApplicationConfig.HttpServer.IdleTimeout",
        Default: time.Minute,
        Minimum: time.Millisecond,
    })
    expectRule(t, "config.HttpMaxHeaderBytesRule", config.HttpMaxHeaderBytesRule(), bound.Rule[int]{
        Name: "ApplicationConfig.HttpServer.MaxHeaderBytes",
        Default: 1048576,
        Minimum: 4096,
    })
    expectRule(t, "config.HttpReadHeaderTimeoutRule", config.HttpReadHeaderTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ApplicationConfig.HttpServer.ReadHeaderTimeout",
        Default: 5 * time.Second,
        Minimum: time.Millisecond,
    })
    expectRule(t, "config.HttpReadTimeoutRule", config.HttpReadTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ApplicationConfig.HttpServer.ReadTimeout",
        Default: 15 * time.Second,
        Minimum: time.Millisecond,
    })
    expectRule(t, "config.HttpWriteTimeoutRule", config.HttpWriteTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ApplicationConfig.HttpServer.WriteTimeout",
        Default: 30 * time.Second,
        Minimum: time.Millisecond,
    })
    expectRule(t, "config.MaxRequestBodyBytesRule", config.MaxRequestBodyBytesRule(), bound.Rule[int]{
        Name: "HttpConfiguration.MaxRequestBodyBytes",
        Default: 1048576,
        Minimum: 1,
    })
    expectRule(t, "config.SessionMaxSessionsRule", config.SessionMaxSessionsRule(), bound.Rule[int]{
        Name: "HttpConfiguration.SessionMaxSessions",
        Default: 100000,
        Minimum: 1,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "config.SessionStorageTimeoutRule", config.SessionStorageTimeoutRule(), bound.Rule[time.Duration]{
        Name: "HttpConfiguration.SessionStorageTimeout",
        Default: 5 * time.Second,
        Minimum: time.Millisecond,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "config.SessionTombstoneRetentionRule", config.SessionTombstoneRetentionRule(), bound.Rule[time.Duration]{
        Name: "HttpConfiguration.SessionTombstoneRetention",
        Default: 5 * time.Minute,
        Minimum: time.Second,
    })
    expectRule(t, "config.SessionTtlRule", config.SessionTtlRule(), bound.Rule[time.Duration]{
        Name: "HttpConfiguration.SessionTtl",
        Default: 24 * time.Hour,
        Minimum: time.Second,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "config.ShutdownTimeoutRule", config.ShutdownTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ApplicationConfig.HttpServer.ShutdownTimeout",
        Default: 5 * time.Second,
        Minimum: time.Millisecond,
    })
    expectRule(t, "config.StaticCacheMaxAgeRule", config.StaticCacheMaxAgeRule(), bound.Rule[int]{
        Name: "HttpConfiguration.StaticCacheMaxAge",
        Default: 3600,
    })
    expectRule(t, "config.TeardownTimeoutRule", config.TeardownTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ApplicationConfig.TeardownTimeout",
        Default: 10 * time.Second,
        Minimum: time.Millisecond,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "config.UnavailableRetryAfterRule", config.UnavailableRetryAfterRule(), bound.Rule[time.Duration]{
        Name: "ApplicationConfig.HttpKernel.UnavailableRetryAfter",
        Default: 5 * time.Second,
        Minimum: time.Second,
        Maximum: time.Hour,
    })
    expectRule(t, "container.CreationWaitersRule", container.CreationWaitersRule(), bound.Rule[int]{
        Name: "ContainerConfig.CreationWaiters",
        Default: 1024,
        Minimum: 1,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "container.ScopeTeardownTimeoutRule", container.ScopeTeardownTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ContainerConfig.ScopeTeardownTimeout",
        Default: 3 * time.Second,
        Minimum: time.Millisecond,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "lockcontract.LockTtlRule", lockcontract.LockTtlRule(), bound.Rule[time.Duration]{
        Name: "CreateLockOptions.Ttl",
        Default: 30 * time.Second,
        Minimum: time.Millisecond,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "output.OptionsLimitRule", output.OptionsLimitRule(), bound.Rule[int]{
        Name: "Options.Limit",
        DefaultUnlimited: true,
    })
    expectRule(t, "output.OptionsOffsetRule", output.OptionsOffsetRule(), bound.Rule[int]{
        Name: "Options.Offset",
    })
    expectRule(t, "output.OptionsTableMaxWidthRule", output.OptionsTableMaxWidthRule(), bound.Rule[int]{
        Name: "Options.TableMaxWidth",
        Default: 120,
        Minimum: 1,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "securitycontract.NonceTtlRule", securitycontract.NonceTtlRule(), bound.Rule[time.Duration]{
        Name: "NonceGuard.Remember.ttl",
        Minimum: time.Millisecond,
    })
    expectRule(t, "securitycontract.TokenTtlRule", securitycontract.TokenTtlRule(), bound.Rule[time.Duration]{
        Name: "RevocableTokenStore.Put.ttl",
        Minimum: time.Second,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "session.StorageTimeoutRule", session.StorageTimeoutRule(), bound.Rule[time.Duration]{
        Name: "ManagerConfig.StorageTimeout",
        Default: 5 * time.Second,
        Minimum: time.Millisecond,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "sessioncontract.TtlRule", sessioncontract.TtlRule(), bound.Rule[time.Duration]{
        Name: "ttl",
        Minimum: time.Second,
        UnlimitedAdmitted: true,
    })
    expectRule(t, "storagecontract.PresignExpiryRule", storagecontract.PresignExpiryRule(), bound.Rule[time.Duration]{
        Name: "PresignOptions.Expiry",
        Default: 15 * time.Minute,
        Minimum: time.Second,
        Maximum: 168 * time.Hour,
    })
}
