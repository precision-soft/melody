package amqp

import (
    "errors"
    "net"
    "os"
    "runtime"
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

type gatedConn struct {
    net.Conn

    mutex           sync.Mutex
    wedged          bool
    deadline        time.Time
    closed          chan struct{}
    closeOnce       sync.Once
    deadlineChanged chan struct{}
    blockedWrites   atomic.Int64

    heldReplies atomic.Bool
    releaseOnce sync.Once
    released    chan struct{}
}

func newGatedConn(conn net.Conn) *gatedConn {
    return &gatedConn{
        Conn:            conn,
        closed:          make(chan struct{}),
        deadlineChanged: make(chan struct{}, 1),
        released:        make(chan struct{}),
    }
}

func (instance *gatedConn) HoldReplies() {
    instance.heldReplies.Store(true)
}

func (instance *gatedConn) ReleaseReplies() {
    instance.heldReplies.Store(false)
    instance.releaseOnce.Do(func() { close(instance.released) })
}

func (instance *gatedConn) Read(buffer []byte) (int, error) {
    count, readErr := instance.Conn.Read(buffer)

    if true == instance.heldReplies.Load() {
        select {
        case <-instance.released:
        case <-instance.closed:
            return 0, net.ErrClosed
        }
    }

    return count, readErr
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

func holdPublishMutex(t *testing.T, publishMutex *sync.Mutex) func() {
    t.Helper()

    publishMutex.Lock()

    var release sync.Once
    releaseOnce := func() { release.Do(publishMutex.Unlock) }

    t.Cleanup(releaseOnce)

    return releaseOnce
}

func awaitNoPublishGoroutine(t *testing.T, frame string, within time.Duration) {
    t.Helper()

    deadline := time.Now().Add(within)
    stack := make([]byte, 1<<20)

    for {
        written := runtime.Stack(stack, true)
        for written == len(stack) {
            stack = make([]byte, 2*len(stack))
            written = runtime.Stack(stack, true)
        }

        dumped := stack[:written]

        if false == strings.Contains(string(dumped), frame) {
            return
        }

        if true == time.Now().After(deadline) {
            t.Fatalf("a publish goroutine is still alive %v after the caller gave up on it", within)
        }

        time.Sleep(time.Millisecond)
    }
}

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

func errorChainContains(err error, text string) bool {
    for nil != err {
        if true == strings.Contains(err.Error(), text) {
            return true
        }

        err = errors.Unwrap(err)
    }

    return false
}

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
