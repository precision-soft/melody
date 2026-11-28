package amqp

import (
    "context"
    "encoding/binary"
    "errors"
    "fmt"
    "io"
    "net"
    "os"
    "reflect"
    "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    "github.com/precision-soft/melody/v4/logging"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
    melodyserializer "github.com/precision-soft/melody/v4/serializer"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func refusingDialer(dialContext context.Context) (*Connection, error) {
    return nil, errors.New("refused by the test dialer")
}

func refusingSource() ConnectionSource {
    return FromDialer(refusingDialer)
}

func ownerOf(raw *amqp091.Connection) *Connection {
    if nil == raw {
        return nil
    }

    return NewConnection(raw, ConnectionConfig{})
}

func ownedDial(dsn string) (*Connection, error) {
    raw, dialErr := amqp091.Dial(dsn)
    if nil != dialErr {
        return nil, dialErr
    }

    return ownerOf(raw), nil
}

func borrowedSource(raw *amqp091.Connection) ConnectionSource {
    return FromConnection(ownerOf(raw))
}

func connectionThenDialerSource(raw *amqp091.Connection, dialer Dialer) ConnectionSource {
    var handedOut atomic.Bool

    return FromDialer(func(dialContext context.Context) (*Connection, error) {
        if false == handedOut.Swap(true) && nil != raw {
            return ownerOf(raw), nil
        }

        return dialer(dialContext)
    })
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

func expectPanicNamingTheArgument(t *testing.T, argument string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError {
            t.Fatalf("expected a refusal naming the %s argument, got no error panic", argument)
        }

        if context := exception.LogContext(recoveredErr, exception.LogContextOptions{}); argument != context["argument"] {
            t.Fatalf("expected a refusal naming the %s argument, got %v: %v", argument, context, recoveredErr)
        }
    }()

    callback()
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

func (instance *gatedDialer) Dial(dialContext context.Context) (*Connection, error) {
    instance.dials.Add(1)

    connection, gated := dialGated(instance.t, instance.dsn)

    instance.mutex.Lock()
    instance.latest = gated
    instance.mutex.Unlock()

    return ownerOf(connection), nil
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
        /* a dump that fills the buffer is cut short and could hide the frame, so the buffer grows until the dump fits */
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

func writeFakeBrokerMethod(conn net.Conn, channelId uint16, classId uint16, methodId uint16, arguments []byte) error {
    payload := binary.BigEndian.AppendUint16(nil, classId)
    payload = binary.BigEndian.AppendUint16(payload, methodId)
    payload = append(payload, arguments...)

    frame := []byte{1}
    frame = binary.BigEndian.AppendUint16(frame, channelId)
    frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
    frame = append(frame, payload...)
    frame = append(frame, 0xCE)

    _, writeErr := conn.Write(frame)

    return writeErr
}

func readFakeBrokerMethod(conn net.Conn, classId uint16, methodId uint16) error {
    header := make([]byte, 7)
    if _, readErr := io.ReadFull(conn, header); nil != readErr {
        return readErr
    }

    body := make([]byte, binary.BigEndian.Uint32(header[3:7])+1)
    if _, readErr := io.ReadFull(conn, body); nil != readErr {
        return readErr
    }

    if 1 != header[0] || 4 > len(body) || classId != binary.BigEndian.Uint16(body[0:2]) || methodId != binary.BigEndian.Uint16(body[2:4]) {
        return fmt.Errorf("the fake broker expected method %d.%d, got frame type %d payload %v", classId, methodId, header[0], body)
    }

    return nil
}

func serveFakeBrokerHandshake(conn net.Conn) error {
    protocolHeader := make([]byte, 8)
    if _, readErr := io.ReadFull(conn, protocolHeader); nil != readErr {
        return readErr
    }

    start := []byte{0, 9}
    start = binary.BigEndian.AppendUint32(start, 0)
    start = binary.BigEndian.AppendUint32(start, uint32(len("PLAIN")))
    start = append(start, "PLAIN"...)
    start = binary.BigEndian.AppendUint32(start, uint32(len("en_US")))
    start = append(start, "en_US"...)

    tune := binary.BigEndian.AppendUint16(nil, 2047)
    tune = binary.BigEndian.AppendUint32(tune, 131072)
    tune = binary.BigEndian.AppendUint16(tune, 0)

    steps := []func() error{
        func() error { return writeFakeBrokerMethod(conn, 0, 10, 10, start) },
        func() error { return readFakeBrokerMethod(conn, 10, 11) },
        func() error { return writeFakeBrokerMethod(conn, 0, 10, 30, tune) },
        func() error { return readFakeBrokerMethod(conn, 10, 31) },
        func() error { return readFakeBrokerMethod(conn, 10, 40) },
        func() error { return writeFakeBrokerMethod(conn, 0, 10, 41, []byte{0}) },
        func() error { return readFakeBrokerMethod(conn, 20, 10) },
        func() error { return writeFakeBrokerMethod(conn, 1, 20, 11, []byte{0, 0, 0, 0}) },
    }

    for _, step := range steps {
        if stepErr := step(); nil != stepErr {
            return stepErr
        }
    }

    return nil
}

type fakeBrokerWedge struct {
    connection *amqp091.Connection
    channel    *amqp091.Channel
    gated      *gatedConn
    brokerSide net.Conn
    written    chan struct{}
}

type fakeBrokerConnection struct {
    connection *amqp091.Connection
    channel    *amqp091.Channel
    gated      *gatedConn
    brokerSide net.Conn
}

func dialFakeBroker(t *testing.T) *fakeBrokerConnection {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    t.Cleanup(func() { _ = listener.Close() })

    brokerSides := make(chan net.Conn, 1)
    handshakeErrs := make(chan error, 1)
    go func() {
        brokerSide, acceptErr := listener.Accept()
        if nil != acceptErr {
            handshakeErrs <- acceptErr
            return
        }

        brokerSides <- brokerSide
        handshakeErrs <- serveFakeBrokerHandshake(brokerSide)
    }()

    var gated *gatedConn
    connection, dialErr := amqp091.DialConfig("amqp://guest:guest@"+listener.Addr().String()+"/?heartbeat=0", amqp091.Config{
        Dial: func(network string, address string) (net.Conn, error) {
            raw, rawErr := net.DialTimeout(network, address, 2*time.Second)
            if nil != rawErr {
                return nil, rawErr
            }

            gated = newGatedConn(raw)

            return gated, nil
        },
    })
    if nil != dialErr {
        t.Fatalf("dial the fake broker: %v", dialErr)
    }

    brokerSide := <-brokerSides
    t.Cleanup(func() { _ = brokerSide.Close() })

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        t.Fatalf("open a channel on the fake broker: %v", channelErr)
    }

    if handshakeErr := <-handshakeErrs; nil != handshakeErr {
        t.Fatalf("the fake broker handshake failed: %v", handshakeErr)
    }

    t.Cleanup(func() { _ = gated.Close() })

    return &fakeBrokerConnection{connection: connection, channel: channel, gated: gated, brokerSide: brokerSide}
}

func wedgeAPublishOnAFakeBroker(t *testing.T) *fakeBrokerWedge {
    t.Helper()

    fake := dialFakeBroker(t)
    connection := fake.connection
    channel := fake.channel
    gated := fake.gated
    brokerSide := fake.brokerSide

    gated.Wedge()

    wedge := &fakeBrokerWedge{connection: connection, channel: channel, gated: gated, brokerSide: brokerSide, written: make(chan struct{})}

    go func() {
        _ = channel.Publish("", "melody.amqp.v4.test.wedged", false, false, amqp091.Publishing{Body: []byte("wedged")})

        close(wedge.written)
    }()

    t.Cleanup(func() {
        _ = gated.Close()

        select {
        case <-wedge.written:
        case <-time.After(5 * time.Second):
            t.Errorf("the wedged publish did not return after its socket was closed")
        }
    })

    deadline := time.Now().Add(2 * time.Second)
    for 0 == gated.BlockedWrites() {
        if true == time.Now().After(deadline) {
            t.Fatal("the publish never reached the socket; there is no wedged write to measure")
        }

        time.Sleep(5 * time.Millisecond)
    }

    return wedge
}

func (instance *fakeBrokerWedge) beginUnfreedCut(t *testing.T, freedAfter time.Duration) {
    t.Helper()

    cutDeadline := time.Now().Add(freedAfter)
    go func() {
        _ = instance.connection.CloseDeadline(cutDeadline)
    }()

    time.Sleep(100 * time.Millisecond)

    if true == writeReturned(instance.written) {
        t.Fatal("the wedged write returned before the close was measured; there is no unfreed cut")
    }
}

func (instance *fakeBrokerWedge) beginClientShutdown(t *testing.T) {
    t.Helper()

    _ = instance.brokerSide.Close()

    deadline := time.Now().Add(2 * time.Second)
    for false == instance.connection.IsClosed() {
        if true == time.Now().After(deadline) {
            t.Fatal("the client never began its shutdown after the broker side dropped")
        }

        time.Sleep(5 * time.Millisecond)
    }

    time.Sleep(50 * time.Millisecond)
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

func listenFakeBroker(t *testing.T) string {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    t.Cleanup(func() { _ = listener.Close() })

    go func() {
        for {
            brokerSide, acceptErr := listener.Accept()
            if nil != acceptErr {
                return
            }
            t.Cleanup(func() { _ = brokerSide.Close() })

            go func() { _ = serveFakeBrokerHandshake(brokerSide) }()
        }
    }()

    return listener.Addr().String()
}

func listenSilently(t *testing.T) (string, *atomic.Int64) {
    t.Helper()

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    t.Cleanup(func() { _ = listener.Close() })

    accepts := &atomic.Int64{}

    go func() {
        for {
            brokerSide, acceptErr := listener.Accept()
            if nil != acceptErr {
                return
            }

            accepts.Add(1)
            t.Cleanup(func() { _ = brokerSide.Close() })
        }
    }()

    return listener.Addr().String(), accepts
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

func expectARawDeliveryRequeuesCounted(t *testing.T, queueName string, rawHeaders amqp091.Table, expected map[string]string, added map[string]string) *recordingLogger {
    t.Helper()

    connection := openHeadersTestConnection(t)

    typeName := "amqp.test." + queueName
    registry := NewMessageRegistry()
    RegisterMessage[testMessage](registry, typeName)

    deleteHeadersTestQueues(t, connection, queueName)
    transport := NewTransport(queueName, registry, borrowedSource(connection), TransportConfig{Prefetch: bound.Of(1), DeadLetterDisabled: true})
    defer transport.CloseWithContext(context.Background())

    runtimeInstance, logger := newRecordingLoggerRuntime(t.Context())

    subscriptionInstance, receiveErr := transport.Receive(runtimeInstance)
    if nil != receiveErr {
        t.Fatalf("receive: %v", receiveErr)
    }
    queue := subscriptionInstance.Envelopes()

    body, serializeErr := melodyserializer.NewJsonSerializer().Serialize(testMessage{Id: 1, Name: "raw"})
    if nil != serializeErr {
        t.Fatalf("serialize: %v", serializeErr)
    }

    headers := amqp091.Table{headerMessageType: typeName}
    for key, value := range rawHeaders {
        headers[key] = value
    }

    channel, channelErr := connection.Channel()
    if nil != channelErr {
        t.Fatalf("open publish channel: %v", channelErr)
    }
    defer channel.Close()

    publishErr := channel.PublishWithContext(t.Context(), "", queueName, false, false, amqp091.Publishing{ContentType: "application/json", Headers: headers, Body: body})
    if nil != publishErr {
        t.Fatalf("raw publish: %v", publishErr)
    }

    first := receiveWithin(t, queue, 10*time.Second)
    if received := melodymessagebus.Headers(first); false == reflect.DeepEqual(expected, received) {
        t.Fatalf("expected the received headers %v, got %v", expected, received)
    }

    retried := first.WithStamps([]messagebuscontract.Stamp{melodymessagebus.RedeliveryStamp{Count: 1}, melodymessagebus.HeadersStamp{Headers: added}})
    if nackErr := transport.Nack(runtimeInstance, retried, messagebuscontract.NackOptions{Requeue: true}); nil != nackErr {
        t.Fatalf("nack requeue: %v", nackErr)
    }

    second := receiveWithin(t, queue, 10*time.Second)
    if 1 != melodymessagebus.RedeliveryCount(second) {
        t.Fatalf("expected the requeue to carry its redelivery count 1, got %d", melodymessagebus.RedeliveryCount(second))
    }

    if received := melodymessagebus.Headers(second); false == reflect.DeepEqual(expected, received) {
        t.Fatalf("expected the requeue to keep the headers %v, got %v", expected, received)
    }

    if ackErr := transport.Ack(runtimeInstance, second); nil != ackErr {
        t.Fatalf("ack: %v", ackErr)
    }

    return logger
}
