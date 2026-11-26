package rueidis

import (
    "context"
    "fmt"
    "sync"

    "github.com/redis/rueidis"

    melodyconfig "github.com/precision-soft/melody/v4/config"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* NewConnection wraps a client in the closer shape the container's teardown recognises: rueidis.Client.Close returns nothing, so the raw client cannot join the ordered shutdown, and the cache backend and the rate limiter decline to close a client they borrow. Register the Connection as the service that owns the client and resolve the client through it, so the container closes the one owner, once, in dependency order. The namespace is the application's, every component built over the connection deriving its default key, channel and prefix from it: a nil client or a namespace outside config.ValidateNamespace panics naming the argument. */
func NewConnection(client rueidis.Client, namespace string) *Connection {
    if true == clientIsNil(client) {
        exception.Panic(exception.NewError("redis client is nil", exceptioncontract.Context{"argument": "client"}, nil))
    }

    refuseMalformedNamespace(namespace)

    return &Connection{client: client, namespace: namespace}
}

/* refuseMalformedNamespace panics with config.ValidateNamespace's refusal, naming the argument */
func refuseMalformedNamespace(namespace string) {
    if validateErr := melodyconfig.ValidateNamespace(namespace); nil != validateErr {
        refusal := exception.FromError(validateErr)
        refusal.SetContextValue("argument", "namespace")

        exception.Panic(refusal)
    }
}

/* refuseNilConnection panics on a component handed no connection; the argument is a pointer, so a typed nil is the same nil */
func refuseNilConnection(connection *Connection) {
    if nil == connection {
        exception.Panic(exception.NewError("redis connection is nil", exceptioncontract.Context{"argument": "connection"}, nil))
    }
}

/* namespacedName is the default name of a component over the connection: the namespace, a colon and the component's suffix */
func (instance *Connection) namespacedName(suffix string) string {
    return instance.namespace + ":" + suffix
}

type Connection struct {
    client    rueidis.Client
    namespace string
    closeOnce sync.Once
    closeDone chan struct{}
    /* closeErr is written before closeDone is closed, so a reader past closeDone reads it */
    closeErr error
}

/* Client answers the owned client for commands, safe while the owner keeps the lifecycle; closing it oneself loses the ordered teardown, the idempotent close and the deadline answer. */
func (instance *Connection) Client() rueidis.Client {
    return instance.client
}

/* Namespace answers the application namespace handed to NewConnection, the stem of every default name of a component built over it. */
func (instance *Connection) Namespace() string {
    return instance.namespace
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
