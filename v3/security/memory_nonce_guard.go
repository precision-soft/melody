package security

import (
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const nonceGuardPurgeInterval = 1 * time.Second

/* MemoryNonceGuard is an in-process NonceGuard backed by a map of nonce expiries. It is suitable for single-instance deployments, tests and local development; a multi-instance deployment must use a shared guard (for example the Redis-backed guard in integrations/rueidis) so a nonce replayed against a different instance is still detected. */
func NewMemoryNonceGuard() *MemoryNonceGuard {
    return NewMemoryNonceGuardWithClock(clock.NewSystemClock())
}

/* NewMemoryNonceGuardWithClock is NewMemoryNonceGuard on an injected clock, for deterministic tests. */
func NewMemoryNonceGuardWithClock(clockInstance clockcontract.Clock) *MemoryNonceGuard {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(exception.NewError("nonce guard clock is nil", nil, nil))
    }

    return &MemoryNonceGuard{
        clock:         clockInstance,
        expiryByNonce: map[string]time.Time{},
    }
}

type MemoryNonceGuard struct {
    clock         clockcontract.Clock
    mutex         sync.Mutex
    expiryByNonce map[string]time.Time
    lastPurge     time.Time
}

func (instance *MemoryNonceGuard) Remember(
    _ runtimecontract.Runtime,
    nonce string,
    ttl time.Duration,
) (bool, error) {
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if now.Sub(instance.lastPurge) >= nonceGuardPurgeInterval {
        instance.purgeExpired(now)
        instance.lastPurge = now
    }

    if expiry, exists := instance.expiryByNonce[nonce]; true == exists && true == expiry.After(now) {
        return true, nil
    }

    if 0 >= ttl {
        return false, nil
    }

    instance.expiryByNonce[nonce] = now.Add(ttl)

    return false, nil
}

func (instance *MemoryNonceGuard) purgeExpired(now time.Time) {
    for nonce, expiry := range instance.expiryByNonce {
        if false == expiry.After(now) {
            delete(instance.expiryByNonce, nonce)
        }
    }
}

var _ securitycontract.NonceGuard = (*MemoryNonceGuard)(nil)
