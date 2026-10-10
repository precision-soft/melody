package contract

import "time"

/* Storage is where sessions are kept, and an implementation must be safe for concurrent use: the manager serialises Save and Delete per session id only, and Load runs under no manager lock. The manager's refusal to re-save a deleted session is per process, so a storage shared between instances receives a Save a peer's tombstone would have refused. */
type Storage interface {
    Load(sessionId string) (map[string]any, bool, error)

    Save(sessionId string, data map[string]any, ttl time.Duration) error

    Delete(sessionId string) error

    Close() error
}
