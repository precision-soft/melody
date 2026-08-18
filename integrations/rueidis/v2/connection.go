package rueidis

import (
    "sync"

    "github.com/redis/rueidis"
)

/* NewConnection wraps a client in the closer shape the container's teardown recognizes: rueidis.Client.Close returns nothing, so the raw client can never join the ordered shutdown, and every value in this package that could — the cache backend, the rate limiter — deliberately declines to close a client it merely borrows. Register the Connection as the service that owns the client and resolve the client through it; the container then closes the one owner, once, in dependency order. */
func NewConnection(client rueidis.Client) *Connection {
    return &Connection{client: client}
}

type Connection struct {
    client    rueidis.Client
    closeOnce sync.Once
}

func (instance *Connection) Client() rueidis.Client {
    return instance.client
}

/* Close closes the wrapped client exactly once; later calls answer nil, the idempotency the container's teardown and a defer-happy caller both lean on. */
func (instance *Connection) Close() error {
    instance.closeOnce.Do(func() {
        if nil != instance.client {
            instance.client.Close()
        }
    })

    return nil
}
