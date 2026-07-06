package contract

import (
    "time"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* NonceGuard provides replay protection for single-use values (the nonce carried by the internal-auth envelope). Remember atomically records a nonce for the given time-to-live and reports whether it had already been recorded and not yet expired; a true result means the nonce is a replay and the request must be rejected. Implementations are expected to be safe for concurrent use and, in a multi-instance deployment, to share state (for example through Redis). */
type NonceGuard interface {
    Remember(runtimeInstance runtimecontract.Runtime, nonce string, ttl time.Duration) (bool, error)
}
