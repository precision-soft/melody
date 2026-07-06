package security

import (
    "sync"
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* nonceGuardPurgeInterval bounds how often the expired-entry sweep runs: an O(n) sweep on every Remember would be O(n²) under a high volume of distinct nonces, yet correctness never depends on the sweep (an expired entry is ignored by the per-nonce expiry check at read time regardless). Sweeping at most once per interval keeps reclamation timely while capping the amortized cost; between sweeps the map holds at most the entries added within one interval beyond what has expired. */
const nonceGuardPurgeInterval = 1 * time.Second

/* MemoryNonceGuard is an in-process NonceGuard backed by a map of nonce expiries. It is suitable for single-instance deployments, tests and local development; a multi-instance deployment must use a shared guard (for example the Redis-backed guard in integrations/rueidis) so a nonce replayed against a different instance is still detected. */
func NewMemoryNonceGuard() *MemoryNonceGuard {
    return &MemoryNonceGuard{
        expiryByNonce: map[string]time.Time{},
    }
}

type MemoryNonceGuard struct {
    mutex         sync.Mutex
    expiryByNonce map[string]time.Time
    lastPurge     time.Time
}

func (instance *MemoryNonceGuard) Remember(
    _ runtimecontract.Runtime,
    nonce string,
    ttl time.Duration,
) (bool, error) {
    now := time.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* sweep expired entries at most once per interval — correctness comes from the per-nonce expiry check below, not the sweep, so amortizing it keeps a high volume of distinct nonces from paying an O(n) sweep on every call. The zero lastPurge makes the first Remember sweep. */
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
