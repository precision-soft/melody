package contract

import "time"

/* Storage is where sessions are kept, and an implementation must be safe for concurrent use: the manager serialises Save and Delete per session id through a striped lock, while Load runs under no manager lock and writes naming different sessions run concurrently. The manager's refusal to re-save a deleted session is per process: the tombstone lives in the manager's memory, so a storage shared between instances receives the Save a peer instance's tombstone would have refused. */
type Storage interface {
    Load(sessionId string) (map[string]any, bool, error)

    Save(sessionId string, data map[string]any, ttl time.Duration) error

    Delete(sessionId string) error

    Close() error
}
