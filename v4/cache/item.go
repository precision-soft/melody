package cache

import (
    "sync/atomic"
    "time"
)

func NewItem(
    key string,
    payload []byte,
    createdAt time.Time,
    expiresAt *time.Time,
) *Item {
    var expiresAtCopy *time.Time
    if nil != expiresAt {
        value := *expiresAt
        expiresAtCopy = &value
    }

    var payloadCopy []byte
    if nil != payload {
        payloadCopy = append([]byte{}, payload...)
    }

    item := &Item{
        key:       key,
        payload:   payloadCopy,
        createdAt: createdAt,
        expiresAt: expiresAtCopy,
    }

    item.lastAccessedAtNano.Store(createdAt.UnixNano())

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
