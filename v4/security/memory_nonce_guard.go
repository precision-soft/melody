package security

import (
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* MemoryNonceGuardConfig is what the in-process guard declares at construction; the zero value is the default. */
type MemoryNonceGuardConfig struct {
    /* MaxEntries bounds the nonces remembered at once: at the ceiling, after the lapsed ones are swept, Remember refuses a new nonce as the error, which its callers treat as the guard failing and refuse the request, so a flood never evicts a live nonce into replayability. 100,000 by default, at least 1, unlimited for a guard bounded by its callers' horizons alone. */
    MaxEntries bound.Count

    /* PurgeInterval is how often the lapsed entries are swept, so distinct nonces at volume do not pay an O(n) sweep on every Remember: 1s by default, at least 1ms, unlimited refused. Correctness never depends on the sweep, since every read checks the entry's expiry. */
    PurgeInterval bound.Duration

    /* Clock is where the expiries are stamped and read; nil is the system clock. */
    Clock clockcontract.Clock
}

var memoryNonceGuardMaxEntriesRule = bound.Rule[int]{
    Name:              "MemoryNonceGuardConfig.MaxEntries",
    Default:           100_000,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var memoryNonceGuardPurgeIntervalRule = bound.Rule[time.Duration]{
    Name:    "MemoryNonceGuardConfig.PurgeInterval",
    Default: time.Second,
    Minimum: bound.TimeoutMinimum,
}

/* NewMemoryNonceGuard resolves the configuration, panicking on a field below its minimum by name, and builds the guard. */
func NewMemoryNonceGuard(config MemoryNonceGuardConfig) *MemoryNonceGuard {
    maxEntries, maxEntriesErr := bound.Resolve(config.MaxEntries, memoryNonceGuardMaxEntriesRule)
    if nil != maxEntriesErr {
        exception.Panic(exception.FromError(maxEntriesErr))
    }

    purgeInterval, purgeIntervalErr := bound.Resolve(config.PurgeInterval, memoryNonceGuardPurgeIntervalRule)
    if nil != purgeIntervalErr {
        exception.Panic(exception.FromError(purgeIntervalErr))
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    /* 0 is the unlimited ceiling */
    return &MemoryNonceGuard{
        clock:         clockInstance,
        maxEntries:    maxEntries.Value,
        purgeInterval: purgeInterval.Value,
        expiryByNonce: map[string]time.Time{},
    }
}

/* MemoryNonceGuard is an in-process NonceGuard backed by a map of nonce expiries. It is suitable for single-instance deployments, tests and local development; a multi-instance deployment must use a shared guard (for example the Redis-backed guard in integrations/rueidis) so a nonce replayed against a different instance is still detected. */
type MemoryNonceGuard struct {
    clock         clockcontract.Clock
    maxEntries    int
    purgeInterval time.Duration
    mutex         sync.Mutex
    expiryByNonce map[string]time.Time
    lastPurge     time.Time

    /* earliestExpiry is the soonest expiry among the remembered nonces, so a guard at its ceiling sweeps only once an entry can have lapsed rather than walking the whole map on every refused call */
    earliestExpiry time.Time
}

func (instance *MemoryNonceGuard) Remember(
    _ runtimecontract.Runtime,
    nonce string,
    ttl bound.Duration,
) (bool, error) {
    resolvedTtl, err := securitycontract.ResolveNonceTtl(ttl)
    if nil != err {
        return false, err
    }

    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if now.Sub(instance.lastPurge) >= instance.purgeInterval {
        instance.purgeExpired(now)
        instance.lastPurge = now
    }

    if expiry, exists := instance.expiryByNonce[nonce]; true == exists && true == expiry.After(now) {
        return true, nil
    }

    if 0 < instance.maxEntries && len(instance.expiryByNonce) >= instance.maxEntries {
        if false == now.Before(instance.earliestExpiry) {
            instance.purgeExpired(now)
            instance.lastPurge = now
        }

        if len(instance.expiryByNonce) >= instance.maxEntries {
            return false, exception.NewError(
                "the nonce guard is full",
                exceptioncontract.Context{
                    "field":      memoryNonceGuardMaxEntriesRule.Name,
                    "maxEntries": instance.maxEntries,
                },
                nil,
            )
        }
    }

    expiry := now.Add(resolvedTtl)
    instance.expiryByNonce[nonce] = expiry

    if true == instance.earliestExpiry.IsZero() || true == expiry.Before(instance.earliestExpiry) {
        instance.earliestExpiry = expiry
    }

    return false, nil
}

func (instance *MemoryNonceGuard) purgeExpired(now time.Time) {
    earliestExpiry := time.Time{}
    for nonce, expiry := range instance.expiryByNonce {
        if false == expiry.After(now) {
            delete(instance.expiryByNonce, nonce)

            continue
        }

        if true == earliestExpiry.IsZero() || true == expiry.Before(earliestExpiry) {
            earliestExpiry = expiry
        }
    }

    instance.earliestExpiry = earliestExpiry
}

var _ securitycontract.NonceGuard = (*MemoryNonceGuard)(nil)
