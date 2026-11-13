package rueidis

import (
    "context"
    "fmt"
    "sync"

    "github.com/redis/rueidis"

    "github.com/precision-soft/melody/v4/exception"
)

/* NewConnection wraps a client in the closer shape the container's teardown recognises: rueidis.Client.Close returns nothing, so the raw client cannot join the ordered shutdown, and the cache backend and the rate limiter decline to close a client they borrow. Register the Connection as the service that owns the client and resolve the client through it, so the container closes the one owner, once, in dependency order. */
func NewConnection(client rueidis.Client) *Connection {
    return &Connection{client: client}
}

type Connection struct {
    client    rueidis.Client
    closeOnce sync.Once
    closeDone chan struct{}
    /* closeErr is written before closeDone is closed, so a reader past closeDone reads it */
    closeErr error
}

func (instance *Connection) Client() rueidis.Client {
    return instance.client
}

/* CloseWithContext closes the wrapped client exactly once. The client's close takes no context, so it runs on a goroutine of its own and the wait for it ends at the deadline, answering the deadline's error while the close finishes on its own; every later call waits on that same close, the idempotency the container's teardown and a defer-happy caller both lean on. */
func (instance *Connection) CloseWithContext(closeContext context.Context) error {
    instance.closeOnce.Do(func() {
        instance.closeDone = make(chan struct{})

        go func() {
            defer close(instance.closeDone)

            /* the close runs past the teardown's own containment, so a panicking client is recovered here and answered as the close's error */
            defer func() {
                if recoveredValue := recover(); nil != recoveredValue {
                    instance.closeErr = exception.NewError(
                        "redis client close panicked",
                        map[string]any{"panicValue": fmt.Sprintf("%v", recoveredValue)},
                        exception.PanicCause(recoveredValue),
                    )
                }
            }()

            if nil != instance.client {
                instance.client.Close()
            }
        }()
    })

    select {
    case <-instance.closeDone:
        return instance.closeErr
    case <-closeContext.Done():
    }

    /* a close that ended in the same instant as the deadline is answered as itself, since a select between two ready channels picks at random */
    select {
    case <-instance.closeDone:
        return instance.closeErr
    default:
    }

    return exception.NewError("redis client did not close before the close deadline; its close goes on in the background", nil, closeContext.Err())
}
