package contract

import "time"

/* Storage must support concurrent Load calls and writes for different session IDs. The manager serializes Save/Delete per ID only within one process; its deletion tombstones do not prevent writes from peer processes sharing the storage. */
type Storage interface {
    Load(sessionId string) (map[string]any, bool, error)

    Save(sessionId string, data map[string]any, ttl time.Duration) error

    Delete(sessionId string) error

    Close() error
}
