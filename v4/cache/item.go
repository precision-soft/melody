package cache

import (
    "sync/atomic"
    "time"
)

/* ItemConfig is what an item carries beside its key and payload; no ExpiresAt is an item that does not expire. */
type ItemConfig struct {
    CreatedAt time.Time
    ExpiresAt *time.Time
}

func NewItem(key string, payload []byte, config ItemConfig) *Item {
    var expiresAtCopy *time.Time
    if nil != config.ExpiresAt {
        value := *config.ExpiresAt
        expiresAtCopy = &value
    }

    var payloadCopy []byte
    if nil != payload {
        payloadCopy = append([]byte{}, payload...)
    }

    item := &Item{
        key:       key,
        payload:   payloadCopy,
        createdAt: config.CreatedAt,
        expiresAt: expiresAtCopy,
    }

    item.lastAccessedAtNano.Store(config.CreatedAt.UnixNano())

    return item
}

type Item struct {
    key                string
    payload            []byte
    createdAt          time.Time
    expiresAt          *time.Time
    lastAccessedAtNano atomic.Int64
    hitCount           atomic.Uint64
}

func (instance *Item) Key() string {
    return instance.key
}

func (instance *Item) Payload() []byte {
    if nil == instance.payload {
        return nil
    }

    return append([]byte{}, instance.payload...)
}

func (instance *Item) CreatedAt() time.Time {
    return instance.createdAt
}

func (instance *Item) ExpiresAt() *time.Time {
    if nil == instance.expiresAt {
        return nil
    }

    value := *instance.expiresAt

    return &value
}

func (instance *Item) Touch(accessTime time.Time) {
    instance.lastAccessedAtNano.Store(accessTime.UnixNano())
    instance.hitCount.Add(1)
}

func (instance *Item) LastAccessedAt() time.Time {
    return time.Unix(0, instance.lastAccessedAtNano.Load())
}

func (instance *Item) HitCount() uint64 {
    return instance.hitCount.Load()
}
