package rueidis

import (
    "context"
    "crypto/tls"
    "errors"
    "fmt"
    "net"
    "os"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    redisclient "github.com/redis/rueidis"
)

func newTokenStoreRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func newTokenStoreClient(t *testing.T) redisclient.Client {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis token store integration test")
    }

    provider := NewProvider()
    client, openErr := provider.Open(NewConnectionParameters(address, "", ""))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    t.Cleanup(func() {
        provider.Close(client)
    })

    return client
}

/* gatedConn is a net.Conn whose Read stops delivering once wedged: every byte the store sends after that is swallowed and the read blocks until the conn is closed or a deadline set on it lands, which is what a store that accepts connections but stops answering looks like from the client. Writes pass through untouched, so the command reaches the store and the client's own reader, pinger and reconnect run exactly as they do in production. The wedge is CONSTRUCTED rather than awaited, so a test holds the window open for as long as its assertions need. */
type gatedConn struct {
    net.Conn

    gate            *gate
    mutex           sync.Mutex
    deadline        time.Time
    deadlineChanged chan struct{}
    closed          chan struct{}
    closeOnce       sync.Once
}

/* gate is shared by every conn a client dials, including the ones its retry policy dials AFTER the first one failed: a wedge that reached only the first conn would let the retry succeed on the second, which is not what an unanswering store does. A wedge may be narrowed to the replies of one RESP type, read off the first byte, so a door that makes two round trips in one call — a cursor step answered with an array, then a Lua batch answered with an integer — can have its SECOND round trip wedged while the first passes; nothing outside the call could interpose between them otherwise. */
type gate struct {
    mutex      sync.Mutex
    wedged     bool
    replyType  byte
    passArrays int
}

func (instance *gate) Wedge() {
    instance.mutex.Lock()
    instance.wedged = true
    instance.replyType = 0
    instance.mutex.Unlock()
}

/* WedgeIntegerReplies swallows only the replies that open with ':', the shape of a Lua script answering a count, and lets every array — a SCAN or SSCAN step — through. */
func (instance *gate) WedgeIntegerReplies() {
    instance.mutex.Lock()
    instance.wedged = true
    instance.replyType = ':'
    instance.mutex.Unlock()
}

/* WedgeArrayRepliesAfter lets the next `passed` array replies through and swallows the arrays after them, so the second cursor walk of a call is wedged while the first step of the outer walk passes; the client's own PONGs are not arrays and are never counted. */
func (instance *gate) WedgeArrayRepliesAfter(passed int) {
    instance.mutex.Lock()
    instance.wedged = true
    instance.replyType = '*'
    instance.passArrays = passed
    instance.mutex.Unlock()
}

func (instance *gate) swallows(first byte) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if false == instance.wedged {
        return false
    }

    if 0 == instance.replyType {
        return true
    }

    if first != instance.replyType {
        return false
    }

    if 0 < instance.passArrays {
        instance.passArrays--

        return false
    }

    return true
}

func (instance *gatedConn) Read(buffer []byte) (int, error) {
    n, err := instance.Conn.Read(buffer)
    if nil != err {
        return n, err
    }

    if 0 == n || false == instance.gate.swallows(buffer[0]) {
        return n, nil
    }

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

func (instance *gatedConn) SetReadDeadline(t time.Time) error {
    instance.recordDeadline(t)

    return instance.Conn.SetReadDeadline(t)
}

func (instance *gatedConn) Close() error {
    instance.closeOnce.Do(func() {
        close(instance.closed)
    })

    return instance.Conn.Close()
}

/* dialGated opens a client over gated conns against the redis the validation lane starts, with the provider's default connection timeout so the client's own ceiling is the one production runs under. */
func dialGated(t *testing.T) (redisclient.Client, *gate) {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the live redis suite")
    }

    shared := &gate{}
    client, clientErr := redisclient.NewClient(redisclient.ClientOption{
        InitAddress:      []string{address},
        DisableCache:     true,
        ConnWriteTimeout: DefaultClientConfig().ConnWriteTimeout,
        DialCtxFn: func(ctx context.Context, addr string, dialer *net.Dialer, tlsConfig *tls.Config) (net.Conn, error) {
            raw, dialErr := dialer.DialContext(ctx, "tcp", addr)
            if nil != dialErr {
                return nil, dialErr
            }

            return &gatedConn{Conn: raw, gate: shared, deadlineChanged: make(chan struct{}, 1), closed: make(chan struct{})}, nil
        },
    })
    if nil != clientErr {
        t.Fatalf("could not reach redis at %s: %v", address, clientErr)
    }

    t.Cleanup(client.Close)

    return client, shared
}

/* awaitOutcome runs a call that is expected to return on its own within the budget and fails the test by name when it does not, so a mutant that removes a bound fails here instead of hanging the suite for the client's ceiling times the number of tests. A panic inside the call is handed back as its value. */
func awaitOutcome(t *testing.T, budget time.Duration, call func() error) error {
    t.Helper()

    outcome := make(chan error, 1)

    go func() {
        defer func() {
            if recovered := recover(); nil != recovered {
                if recoveredErr, ok := recovered.(error); ok {
                    outcome <- recoveredErr

                    return
                }

                outcome <- fmt.Errorf("panic: %v", recovered)
            }
        }()

        outcome <- call()
    }()

    select {
    case result := <-outcome:
        return result
    case <-time.After(budget):
        t.Fatalf("the call did not return within %s", budget)

        return nil
    }
}

/* requireDeadlineExceeded asserts that a refusal carries the call timeout's own deadline in its chain, which is what separates a bounded call from one the client's ceiling or retry policy ended. */
func requireDeadlineExceeded(t *testing.T, err error) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected the bounded call to be refused, got nil")
    }

    if false == errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("expected context.DeadlineExceeded in the chain, got %v", err)
    }
}
