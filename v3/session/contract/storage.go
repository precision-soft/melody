package contract

import "time"

/* Storage is where sessions are kept, and an implementation must be safe for concurrent use: the manager serialises Save and Delete through a lock striped by session id, so two writes naming the SAME session never overlap, while Load runs under no manager lock at all and writes naming different sessions usually run concurrently — a storage behind a network is exactly why they must, since one lock for the whole manager would put every session write in the process behind one round trip. Both storages melody ships take a mutex of their own; one written over redis or a database has the same obligation, per session id at the least. The manager's refusal to re-save a deleted session is a per-process guarantee, not the storage's to keep: the tombstone lives in the manager's memory, so a storage shared between instances receives the Save that a peer instance's tombstone would have refused. */
type Storage interface {
    Load(sessionId string) (map[string]any, bool, error)

    Save(sessionId string, data map[string]any, ttl time.Duration) error

    Delete(sessionId string) error

    Close() error
}
