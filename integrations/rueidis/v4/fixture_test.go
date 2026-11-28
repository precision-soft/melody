package rueidis

import (
    "bytes"
    "context"
    "crypto/tls"
    "errors"
    "fmt"
    "io"
    "net"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    redisclient "github.com/redis/rueidis"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
)

type stubClient struct {
    redisclient.Client
}

func expectErrorNamingField(t *testing.T, err error, field string) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected a refusal naming %s, got none", field)
    }

    if field != exception.LogContext(err, exception.LogContextOptions{})["field"] {
        t.Fatalf("expected the refusal to name %s, got %v: %v", field, exception.LogContext(err, exception.LogContextOptions{}), err)
    }
}

func expectPanicNamingField(t *testing.T, field string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming %s, got no error panic", field)
        }

        expectErrorNamingField(t, recoveredErr, field)
    }()

    callback()
}

func exceptionContextOf(err error) map[string]any {
    return exception.LogContext(err, exception.LogContextOptions{})
}

func newTokenStoreRuntime() containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}

func newTokenStoreClient(t *testing.T) redisclient.Client {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis token store integration test")
    }

    provider := NewProvider("melody", ProviderConfig{})
    connection, openErr := provider.Open(openRuntime(t), ConnectionParameters{Address: address})
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    t.Cleanup(func() {
        connection.CloseWithContext(context.Background())
    })

    return connection.Client()
}

func openRuntime(t *testing.T) containercontract.Runtime {
    return &contextOnlyRuntime{ctx: t.Context()}
}

func connectionOver(client redisclient.Client) func(resolver containercontract.Resolver) (*Connection, error) {
    connection := NewConnection(client, "melody")

    return func(resolver containercontract.Resolver) (*Connection, error) {
        return connection, nil
    }
}

func runtimeOver(ctx context.Context) containercontract.Runtime {
    return &contextOnlyRuntime{ctx: ctx}
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
    passReplies int
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

func (instance *gate) WedgeIntegerRepliesAfter(passed int) {
    instance.mutex.Lock()
    instance.wedged = true
    instance.replyType = ':'
    instance.passReplies = passed
    instance.mutex.Unlock()
}

func (instance *gate) WedgeArrayRepliesAfter(passed int) {
    instance.mutex.Lock()
    instance.wedged = true
    instance.replyType = '*'
    instance.passReplies = passed
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

    if 0 < instance.passReplies {
        instance.passReplies--

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
        ConnWriteTimeout: clientConnectionWriteTimeoutRule.Default,
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

func pingSwallowingProxy(t *testing.T) string {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the live redis suite")
    }

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }

    t.Cleanup(func() {
        listener.Close()
    })

    go func() {
        for {
            downstream, acceptErr := listener.Accept()
            if nil != acceptErr {
                return
            }

            upstream, dialErr := net.Dial("tcp", address)
            if nil != dialErr {
                downstream.Close()
                continue
            }

            swallowing := &sync.Once{}
            swallowed := make(chan struct{})

            go func() {
                buffer := make([]byte, 4096)
                for {
                    read, readErr := downstream.Read(buffer)
                    if 0 < read {
                        if true == strings.Contains(string(buffer[:read]), "PING") {
                            swallowing.Do(func() {
                                close(swallowed)
                            })
                        }

                        if _, writeErr := upstream.Write(buffer[:read]); nil != writeErr {
                            break
                        }
                    }

                    if nil != readErr {
                        break
                    }
                }

                upstream.Close()
                downstream.Close()
            }()

            go func() {
                buffer := make([]byte, 4096)
                for {
                    read, readErr := upstream.Read(buffer)
                    if 0 < read {
                        select {
                        case <-swallowed:
                            continue
                        default:
                        }

                        if _, writeErr := downstream.Write(buffer[:read]); nil != writeErr {
                            break
                        }
                    }

                    if nil != readErr {
                        break
                    }
                }

                downstream.Close()
            }()
        }
    }()

    return listener.Addr().String()
}

/* far below the client's five-second connection timeout, so an unbounded round trip fails on the timer */
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

type capturingLogger struct {
    records []capturedRecord
}

type capturedRecord struct {
    level   loggingcontract.Level
    message string
    context loggingcontract.Context
}

func (instance *capturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.records = append(instance.records, capturedRecord{level: level, message: message, context: context})
}

func (instance *capturingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *capturingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *capturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *capturingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *capturingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *capturingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *capturingLogger) CauseChainDepth() int {
    return 8
}

type scriptCountingConn struct {
    net.Conn
    frames *atomic.Int64
}

func (instance *scriptCountingConn) Write(payload []byte) (int, error) {
    instance.frames.Add(int64(bytes.Count(bytes.ToUpper(payload), []byte("\r\nEVAL"))))

    return instance.Conn.Write(payload)
}

type contextOnlyRuntime struct {
    ctx context.Context
}

func (instance *contextOnlyRuntime) Context() context.Context {
    return instance.ctx
}

func (instance *contextOnlyRuntime) Scope() containercontract.Scope {
    return nil
}

func (instance *contextOnlyRuntime) Kind() containercontract.Kind {
    return containercontract.KindRun
}

type commandCountingConn struct {
    net.Conn
    commands *atomic.Int64
}

func (instance *commandCountingConn) Write(payload []byte) (int, error) {
    upper := bytes.ToUpper(payload)
    for _, command := range [][]byte{[]byte("\r\nEVAL"), []byte("\r\nSSCAN\r\n"), []byte("\r\nSCAN\r\n"), []byte("\r\nHMGET\r\n"), []byte("\r\nDEL\r\n")} {
        instance.commands.Add(int64(bytes.Count(upper, command)))
    }

    return instance.Conn.Write(payload)
}

func newCommandCountingClient(t *testing.T) (redisclient.Client, *atomic.Int64) {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS is not set")
    }

    commands := &atomic.Int64{}
    client, clientErr := redisclient.NewClient(redisclient.ClientOption{
        InitAddress:  []string{address},
        DisableCache: true,
        DialCtxFn: func(ctx context.Context, addr string, dialer *net.Dialer, tlsConfig *tls.Config) (net.Conn, error) {
            raw, dialErr := dialer.DialContext(ctx, "tcp", addr)
            if nil != dialErr {
                return nil, dialErr
            }

            return &commandCountingConn{Conn: raw, commands: commands}, nil
        },
    })
    if nil != clientErr {
        t.Fatalf("could not reach redis at %s: %v", address, clientErr)
    }

    t.Cleanup(client.Close)

    return client, commands
}

func newScriptCountingClient(t *testing.T) (redisclient.Client, *atomic.Int64) {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS is not set")
    }

    frames := &atomic.Int64{}
    client, clientErr := redisclient.NewClient(redisclient.ClientOption{
        InitAddress:  []string{address},
        DisableCache: true,
        DialCtxFn: func(ctx context.Context, addr string, dialer *net.Dialer, tlsConfig *tls.Config) (net.Conn, error) {
            raw, dialErr := dialer.DialContext(ctx, "tcp", addr)
            if nil != dialErr {
                return nil, dialErr
            }

            return &scriptCountingConn{Conn: raw, frames: frames}, nil
        },
    })
    if nil != clientErr {
        t.Fatalf("could not reach redis at %s: %v", address, clientErr)
    }

    t.Cleanup(client.Close)

    return client, frames
}

func mustCreateLock(lock lockcontract.Lock, createErr error) lockcontract.Lock {
    if nil != createErr {
        panic(createErr)
    }

    return lock
}

func newTestConnection(client redisclient.Client) *Connection {
    return NewConnection(client, "melody")
}

func expectPanicNamingTheArgument(t *testing.T, argument string, callback func()) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a panic naming the argument %s", argument)
        }

        recoveredErr, isError := recovered.(error)
        if false == isError || argument != exceptionContextOf(recoveredErr)["argument"] {
            t.Fatalf("expected a panic naming the argument %s, got %v", argument, recovered)
        }
    }()

    callback()
}

func subscribeTestTopic(t *testing.T, hub *melodyhttp.ServerSentEventHub, topic string) *melodyhttp.ServerSentEventSubscriber {
    t.Helper()

    subscriber, subscribeErr := hub.Subscribe(topic, melodyhttp.SubscribeOptions{})
    if nil != subscribeErr {
        t.Fatalf("unexpected subscribe error: %v", subscribeErr)
    }

    return subscriber
}

func captureStandardError(t *testing.T, action func()) string {
    t.Helper()

    readEnd, writeEnd, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("pipe: %v", pipeErr)
    }

    savedStandardError := os.Stderr
    logging.CloseEmergencyLogger()
    os.Stderr = writeEnd
    _ = logging.EmergencyLogger()
    os.Stderr = savedStandardError

    action()

    logging.CloseEmergencyLogger()
    _ = writeEnd.Close()

    written, readErr := io.ReadAll(readEnd)
    _ = readEnd.Close()
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    return string(written)
}
