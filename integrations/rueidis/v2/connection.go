package rueidis

import (
    "sync"

    "github.com/redis/rueidis"
)

/* NewConnection wraps a client in the closer shape the container's teardown recognises: rueidis.Client.Close returns nothing, so the raw client cannot join the ordered shutdown, and the cache backend and the rate limiter decline to close a client they borrow. Register the Connection as the service that owns the client and resolve the client through it, so the container closes the one owner, once, in dependency order. */
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
