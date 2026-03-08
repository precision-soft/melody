package contract

import "time"

type Storage interface {
    Load(sessionId string) (map[string]any, bool, error)

    Save(sessionId string, data map[string]any, ttl time.Duration) error

    Delete(sessionId string) error

    Close() error
}
