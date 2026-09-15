package rueidis

import (
    "context"
    "strconv"
    "strings"
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


func (instance *gate) WedgeIntegerReplies() {
    instance.mutex.Lock()
    instance.wedged = true
    instance.replyType = ':'
    instance.mutex.Unlock()
}

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

const boundProbeBudget = 2 * time.Second

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

func requireDeadlineExceeded(t *testing.T, err error) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected the bounded call to be refused, got nil")
    }

    if false == errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("expected context.DeadlineExceeded in the chain, got %v", err)
    }
}

func commandCallCount(t *testing.T, client redisclient.Client, prefixes ...string) int64 {
    t.Helper()

    info, infoErr := client.Do(context.Background(), client.B().Info().Section("commandstats").Build()).ToString()
    if nil != infoErr {
        t.Fatalf("info commandstats: %v", infoErr)
    }

    total := int64(0)

    for _, line := range strings.Split(info, "\n") {
        matches := false
        for _, prefix := range prefixes {
            if true == strings.HasPrefix(line, prefix) {
                matches = true

                break
            }
        }

        if false == matches {
            continue
        }

        for _, field := range strings.Split(strings.TrimSpace(line), ",") {
            if false == strings.Contains(field, "calls=") {
                continue
            }

            calls, parseErr := strconv.ParseInt(strings.TrimPrefix(field[strings.Index(field, "calls="):], "calls="), 10, 64)
            if nil != parseErr {
                t.Fatalf("parse %q: %v", field, parseErr)
            }

            total += calls
        }
    }

    return total
}
