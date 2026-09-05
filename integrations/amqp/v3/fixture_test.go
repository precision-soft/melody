package amqp

import (
    "errors"
    "net"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    amqp091 "github.com/rabbitmq/amqp091-go"
)

func amqpDsnOrSkip(t *testing.T) string {
    t.Helper()

    dsn := os.Getenv("AMQP_DSN")
    if "" == dsn {
        t.Skip("AMQP_DSN not set; skipping amqp integration test")
    }

    return dsn
}

/* gatedConn is a net.Conn whose Write blocks once wedged — until the conn is closed or a deadline lands on it, which is what a real socket does when its peer stops reading and the kernel buffer is full. Reads pass through untouched, so the broker's frames still arrive and the amqp client's own reader, heartbeater and shutdown run exactly as they do in production. The wedge is CONSTRUCTED rather than awaited, so a test can hold the window open for as long as its assertions need. */
type gatedConn struct {
    net.Conn

    mutex           sync.Mutex
    wedged          bool
    deadline        time.Time
    closed          chan struct{}
    closeOnce       sync.Once
    deadlineChanged chan struct{}
    blockedWrites   atomic.Int64
}

func newGatedConn(conn net.Conn) *gatedConn {
    return &gatedConn{
        Conn:            conn,
        closed:          make(chan struct{}),
        deadlineChanged: make(chan struct{}, 1),
    }
}

func (instance *gatedConn) Wedge() {
    instance.mutex.Lock()
    instance.wedged = true
    instance.mutex.Unlock()
}

func (instance *gatedConn) BlockedWrites() int64 {
    return instance.blockedWrites.Load()
}

func (instance *gatedConn) Write(buffer []byte) (int, error) {
    instance.mutex.Lock()
    wedged := instance.wedged
    instance.mutex.Unlock()

    if false == wedged {
        return instance.Conn.Write(buffer)
    }

    instance.blockedWrites.Add(1)

    for {
        instance.mutex.Lock()
        deadline := instance.deadline
        instance.mutex.Unlock()

        var timerChannel <-chan time.Time
        if false == deadline.IsZero() {
            remaining := time.Until(deadline)
            if 0 >= remaining {
                return 0, os.ErrDeadlineExceeded
            }

            timer := time.NewTimer(remaining)
            timerChannel = timer.C
            defer timer.Stop()
        }

        select {
        case <-instance.closed:
            return 0, net.ErrClosed
        case <-timerChannel:
            return 0, os.ErrDeadlineExceeded
        case <-instance.deadlineChanged:
        }
    }
}

func (instance *gatedConn) recordDeadline(t time.Time) {
    instance.mutex.Lock()
    instance.deadline = t
    instance.mutex.Unlock()

    select {
    case instance.deadlineChanged <- struct{}{}:
    default:
    }
}

func (instance *gatedConn) SetDeadline(t time.Time) error {
    instance.recordDeadline(t)

    return instance.Conn.SetDeadline(t)
}

func (instance *gatedConn) SetWriteDeadline(t time.Time) error {
    instance.recordDeadline(t)

    return instance.Conn.SetWriteDeadline(t)
}

func (instance *gatedConn) Close() error {
    instance.closeOnce.Do(func() {
        close(instance.closed)
    })

    return instance.Conn.Close()
}

/* dialGated opens a broker connection over a gatedConn. The connection is released at cleanup through CloseDeadline, the one close the amqp client can complete over a wedged socket — its plain Close is an RPC that would join the wedged write. */
func dialGated(t *testing.T, dsn string) (*amqp091.Connection, *gatedConn) {
    t.Helper()

    var gated *gatedConn
    config := amqp091.Config{
        Dial: func(network, address string) (net.Conn, error) {
            raw, dialErr := net.DialTimeout(network, address, 10*time.Second)
            if nil != dialErr {
                return nil, dialErr
            }

            gated = newGatedConn(raw)

            return gated, nil
        },
    }

    connection, dialErr := amqp091.DialConfig(dsn, config)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }

    t.Cleanup(func() {
        _ = connection.CloseDeadline(time.Now())
    })

    return connection, gated
}

/* gatedDialer counts its dials and hands the latest gated conn back, so a test can wedge the connection a transport or backplane dialed itself and then assert that the next call dialed again. */
type gatedDialer struct {
    t     *testing.T
    dsn   string
    dials atomic.Int64

    mutex  sync.Mutex
    latest *gatedConn
}

func newGatedDialer(t *testing.T, dsn string) *gatedDialer {
    return &gatedDialer{t: t, dsn: dsn}
}

func (instance *gatedDialer) Dial() (*amqp091.Connection, error) {
    instance.dials.Add(1)

    connection, gated := dialGated(instance.t, instance.dsn)

    instance.mutex.Lock()
    instance.latest = gated
    instance.mutex.Unlock()

    return connection, nil
}

func (instance *gatedDialer) Dials() int64 {
    return instance.dials.Load()
}

func (instance *gatedDialer) Latest() *gatedConn {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.latest
}

/* awaitOutcome reads one outcome within the bound or fails the test naming what did not return; every call that this package bounds is asserted through it, so a mutant that removes the bound fails on this timer instead of hanging the suite. */
func awaitOutcome(t *testing.T, label string, outcome <-chan error, within time.Duration) error {
    t.Helper()

    select {
    case err := <-outcome:
        return err
    case <-time.After(within):
        t.Fatalf("%s did not return within %v", label, within)

        return nil
    }
}

func refuseOutcome(t *testing.T, label string, outcome <-chan error, within time.Duration) {
    t.Helper()

    select {
    case err := <-outcome:
        t.Fatalf("%s returned (%v) while it was expected to still be waiting", label, err)
    case <-time.After(within):
    }
}

/* errorChainContains reads the whole cause chain: a melody error renders its own message alone, and the diagnostic a test pins is often the cause one wrap down. */
func errorChainContains(err error, text string) bool {
    for nil != err {
        if true == strings.Contains(err.Error(), text) {
            return true
        }

        err = errors.Unwrap(err)
    }

    return false
}

/* awaitBlockedWrites waits until the gated conn has caught the given number of writes, so a test can act while a write is provably inside the wedge. */
func awaitBlockedWrites(t *testing.T, gated *gatedConn, count int64) {
    t.Helper()

    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        if count == gated.BlockedWrites() {
            return
        }

        time.Sleep(5 * time.Millisecond)
    }

    t.Fatalf("expected %d blocked writes, got %d", count, gated.BlockedWrites())
}

func (instance *gatedConn) Deadline() time.Time {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.deadline
}

/* awaitArmedDeadline waits until a deadline has been set on the socket, which is the one thing a close over a wedged socket can do before it blocks — and the thing the plain close never does. */
func awaitArmedDeadline(t *testing.T, gated *gatedConn) {
    t.Helper()

    deadline := time.Now().Add(2 * time.Second)
    for time.Now().Before(deadline) {
        if false == gated.Deadline().IsZero() {
            return
        }

        time.Sleep(5 * time.Millisecond)
    }

    t.Fatalf("no deadline was armed on the wedged socket")
}
