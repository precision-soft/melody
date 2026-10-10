package cache

import (
    "context"
    "crypto/tls"
    "net"
    "os"
    "sync"
    "testing"
    "time"

    "github.com/redis/rueidis"
)

/* liveBackend answers a backend over the redis the validation lane starts, under a prefix unique to the calling test so parallel suites never read one another's keys. The suite skips where no address is exported, the way the rate limiter suite of the parent package does. */
func liveBackend(t *testing.T) (*Backend, rueidis.Client) {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the live redis suite")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{
        InitAddress:  []string{address},
        DisableCache: true,
    })
    if nil != clientErr {
        t.Fatalf("could not reach redis at %s: %v", address, clientErr)
    }

    t.Cleanup(client.Close)

    prefix := "melody:test:" + t.Name() + ":"

    backend, backendErr := NewBackend(client, context.Background(), prefix, 0, 0)
    if nil != backendErr {
        t.Fatalf("could not build the backend: %v", backendErr)
    }

    t.Cleanup(func() {
        /* the cleanup clears through its own backend: a test that closed the one under test would otherwise leave its keys behind, since a closed backend refuses Clear like every other operation */
        cleaner, cleanerErr := NewBackend(client, context.Background(), prefix, 0, 0)
        if nil != cleanerErr {
            t.Logf("could not build the cleanup backend: %v", cleanerErr)

            return
        }

        if clearErr := cleaner.Clear(); nil != clearErr {
            t.Logf("could not clear the test prefix: %v", clearErr)
        }
    })

    return backend, client
}

/* rawTtl reads the expiry redis actually holds, which is the only way to tell a key written without expiry from one written with a long one. */
func rawTtl(t *testing.T, client rueidis.Client, fullKey string) int64 {
    t.Helper()

    response := client.Do(context.Background(), client.B().Pttl().Key(fullKey).Build())
    remaining, remainingErr := response.AsInt64()
    if nil != remainingErr {
        t.Fatalf("could not read the expiry of %s: %v", fullKey, remainingErr)
    }

    return remaining
}

/* gatedConn is a net.Conn whose Read stops delivering once wedged, the shape of a store that accepts connections but stops answering; the gate is shared by every conn the client dials, so the retry the client makes on a fresh connection meets the same silence. */
type gatedConn struct {
    net.Conn

    gate            *gate
    mutex           sync.Mutex
    deadline        time.Time
    deadlineChanged chan struct{}
    closed          chan struct{}
    closeOnce       sync.Once
}

type gate struct {
    mutex  sync.Mutex
    wedged bool
}

func (instance *gate) Wedge() {
    instance.mutex.Lock()
    instance.wedged = true
    instance.mutex.Unlock()
}

func (instance *gate) IsWedged() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.wedged
}

func (instance *gatedConn) Read(buffer []byte) (int, error) {
    n, err := instance.Conn.Read(buffer)
    if false == instance.gate.IsWedged() {
        return n, err
    }

    if nil != err {
        return 0, err
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

func dialGated(t *testing.T) (rueidis.Client, *gate) {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the live redis suite")
    }

    shared := &gate{}
    client, clientErr := rueidis.NewClient(rueidis.ClientOption{
        InitAddress:      []string{address},
        DisableCache:     true,
        ConnWriteTimeout: 5 * time.Second,
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

/* awaitOutcome runs a call expected to return on its own within the budget and fails the test by name when it does not, so a mutant that removes a bound fails here instead of hanging the suite. */
func awaitOutcome(t *testing.T, budget time.Duration, call func() error) error {
    t.Helper()

    outcome := make(chan error, 1)

    go func() {
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
