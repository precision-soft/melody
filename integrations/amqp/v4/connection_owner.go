package amqp

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* Connection owns one broker connection and the close timeout of what built it: the provider's for a connection it opened, the ConnectionConfig's for one wrapped by NewConnection. */
type Connection struct {
    raw          *amqp091.Connection
    closeTimeout time.Duration
    closeOnce    sync.Once
    closeErr     error
}

/* ConnectionConfig is what a connection dialled outside a Provider declares. */
type ConnectionConfig struct {
    /* CloseTimeout bounds each stretch of the close that cannot observe its signal: 30s when left out, at least 1ms, unlimited refused. */
    CloseTimeout bound.Duration
}

var connectionCloseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ConnectionConfig.CloseTimeout",
    Default: closeJoinTimeout,
    Minimum: bound.TimeoutMinimum,
}

/* NewConnection wraps a connection dialled outside a Provider; a nil connection panics naming the argument. */
func NewConnection(raw *amqp091.Connection, config ConnectionConfig) *Connection {
    if nil == raw {
        exception.Panic(exception.NewError("amqp connection is nil", exceptioncontract.Context{"argument": "raw"}, nil))
    }

    closeTimeout, closeTimeoutErr := bound.Resolve(config.CloseTimeout, connectionCloseTimeoutRule)
    if nil != closeTimeoutErr {
        exception.Panic(exception.FromError(closeTimeoutErr))
    }

    return newOwnedConnection(raw, closeTimeout.Value)
}

func newOwnedConnection(raw *amqp091.Connection, closeTimeout time.Duration) *Connection {
    return &Connection{raw: raw, closeTimeout: closeTimeout}
}

/* Raw answers the client's connection, safe while the owner keeps the lifecycle; closing it oneself loses the bounded, idempotent close. */
func (instance *Connection) Raw() *amqp091.Connection {
    if nil == instance {
        return nil
    }

    return instance.raw
}

/* CloseWithContext closes the connection once, under a deadline on the socket, the close timeout within what is left of the caller's deadline, and returns within that bound plus a short grace; every later call answers the first one's error, and a connection the client already closed, a broker that dropped it among them, closes as nil. The client's plain Close is an RPC sharing the send locks with every publish, so on a connection whose peer stopped reading an unbounded close would never return. */
func (instance *Connection) CloseWithContext(closeContext context.Context) error {
    if nil == instance || nil == instance.raw {
        return nil
    }

    instance.closeOnce.Do(func() {
        closeErr, _ := closeConnectionWithin(closeContext, teardownStretchWithin(closeContext, instance.closeTimeout), instance.closeTimeout, instance.raw)
        instance.closeErr = ignoringAlreadyClosed(closeErr)
    })

    return instance.closeErr
}

/* Dialer dials one connection under the context handed; a component cancels it when it stops waiting, and owns the connection it answers. */
type Dialer func(dialContext context.Context) (*Connection, error)

/* ConnectionSource is where a transport or a backplane takes its connection from, built by FromConnection or FromDialer; its zero value is refused. */
type ConnectionSource struct {
    connection *Connection
    dialer     Dialer
}

/* FromConnection hands a component a connection it borrows: a lost one is terminal, and the component never closes it. */
func FromConnection(connection *Connection) ConnectionSource {
    if nil == connection || nil == connection.raw {
        exception.Panic(exception.NewError("amqp connection source is handed a nil connection", exceptioncontract.Context{"door": "amqp.FromConnection"}, nil))
    }

    return ConnectionSource{connection: connection}
}

/* FromDialer hands a component a dialer it runs on first use and after every loss; it owns and closes each connection the dialer answers. */
func FromDialer(dialer Dialer) ConnectionSource {
    if nil == dialer {
        exception.Panic(exception.NewError("amqp connection source is handed a nil dialer", exceptioncontract.Context{"door": "amqp.FromDialer"}, nil))
    }

    return ConnectionSource{dialer: dialer}
}

func (instance ConnectionSource) empty() bool {
    return nil == instance.connection && nil == instance.dialer
}

func refuseEmptySource(source ConnectionSource, message string) {
    if true == source.empty() {
        exception.Panic(exception.NewError(message, exceptioncontract.Context{"argument": "source", "doors": "amqp.FromConnection, amqp.FromDialer"}, nil))
    }
}

/* runDialer runs the application's dialer, reading a panic as the dial's failure, since it has no handler on the dialling goroutine, and refusing an answer that carries neither a connection nor an error. */
func runDialer(dialContext context.Context, dialer Dialer, panicMessage string, componentContext map[string]any) (outcome dialOutcome) {
    defer func() {
        if recovered := recover(); nil != recovered {
            panicContext := map[string]any{"panic": fmt.Sprint(recovered)}
            for key, value := range componentContext {
                panicContext[key] = value
            }

            outcome = dialOutcome{err: exception.NewError(panicMessage, panicContext, exception.PanicCause(recovered))}
        }
    }()

    connection, dialErr := dialer(dialContext)
    if nil != dialErr {
        if raw := connection.Raw(); nil != raw {
            _ = raw.CloseDeadline(time.Now())
        }

        return dialOutcome{err: dialErr}
    }

    if nil == connection.Raw() {
        return dialOutcome{err: exception.NewError("amqp dialer answered no connection and no error", componentContext, nil)}
    }

    return dialOutcome{connection: connection.Raw()}
}
