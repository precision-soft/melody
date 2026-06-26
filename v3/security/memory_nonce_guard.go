package security

import (
    "sync"
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* MemoryNonceGuard is an in-process NonceGuard backed by a map of nonce expiries. It is suitable for single-instance deployments, tests and local development; a multi-instance deployment must use a shared guard (for example the Redis-backed guard in integrations/rueidis) so a nonce replayed against a different instance is still detected. */
func NewMemoryNonceGuard() *MemoryNonceGuard {
    return &MemoryNonceGuard{
        expiryByNonce: map[string]time.Time{},
    }
}

type MemoryNonceGuard struct {
    mutex         sync.Mutex
    expiryByNonce map[string]time.Time
}

func (instance *MemoryNonceGuard) Remember(
    _ runtimecontract.Runtime,
    nonce string,
    ttl time.Duration,
) (bool, error) {
    now := time.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.purgeExpired(now)

    if expiry, exists := instance.expiryByNonce[nonce]; true == exists && true == expiry.After(now) {
        return true, nil
    }

    if ttl <= 0 {
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
