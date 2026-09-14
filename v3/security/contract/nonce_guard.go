package contract

import (
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* NonceGuard atomically records a nonce for its TTL. Remember returns true for an unexpired replay, which callers must reject. Implementations must support concurrent use and share state across deployment instances. */
type NonceGuard interface {
    Remember(runtimeInstance runtimecontract.Runtime, nonce string, ttl time.Duration) (bool, error)
}
