package websocket

import (
    "context"
    "errors"
    "fmt"
    "io"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    goruntime "runtime"
    "strings"
    "testing"
    "time"

    coderwebsocket "github.com/coder/websocket"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestStreamHandler_BroadcastReachesClient(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    30 * time.Second,
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    wsUrl := "ws" + strings.TrimPrefix(server.URL, "http")

    connection, _, dialErr := coderwebsocket.Dial(ctx, wsUrl, nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    delivered := hub.Broadcast("demo", melodyhttp.ServerSentEvent{Event: "notification", Data: "hello-ws"})
    if 1 != delivered {
        t.Fatalf("expected the broadcast to reach 1 subscriber, got %d", delivered)
    }

    messageType, payload, readErr := connection.Read(ctx)
    if nil != readErr {
        t.Fatalf("read: %v", readErr)
    }

    if coderwebsocket.MessageText != messageType || "hello-ws" != string(payload) {
        t.Fatalf("unexpected message: %v %q", messageType, payload)
    }

    connection.Close(coderwebsocket.StatusNormalClosure, "")
}

func TestStreamHandler_IdleTimeoutKeepsHealthyClientConnected(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    100 * time.Millisecond,
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    wsUrl := "ws" + strings.TrimPrefix(server.URL, "http")

    connection, _, dialErr := coderwebsocket.Dial(ctx, wsUrl, nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    go func() {
        time.Sleep(350 * time.Millisecond)
        hub.Broadcast("demo", melodyhttp.ServerSentEvent{Event: "notification", Data: "still-here"})
    }()

    messageType, payload, readErr := connection.Read(ctx)
    if nil != readErr {
        t.Fatalf("a healthy idle client should survive the keepalive ping loop, got read error: %v", readErr)
    }

    if coderwebsocket.MessageText != messageType || "still-here" != string(payload) {
        t.Fatalf("unexpected message after keepalive intervals: %v %q", messageType, payload)
    }

    connection.Close(coderwebsocket.StatusNormalClosure, "")
}

func TestStreamHandler_IdleTimeoutDisconnectsUnresponsiveClient(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    100 * time.Millisecond,
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    wsUrl := "ws" + strings.TrimPrefix(server.URL, "http")

    connection, _, dialErr := coderwebsocket.Dial(ctx, wsUrl, nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    disconnectDeadline := time.Now().Add(3 * time.Second)
    for hub.SubscriberCount("demo") > 0 {
        if true == time.Now().After(disconnectDeadline) {
            t.Fatalf("expected the keepalive loop to disconnect an unresponsive client and drop its subscription")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

func TestPingLoop_ReceivedPongRefreshesTheActivityMark(t *testing.T) {
    serverConnections := make(chan *coderwebsocket.Conn, 1)
    handlerRelease := make(chan struct{})
    defer close(handlerRelease)

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        connection, acceptErr := coderwebsocket.Accept(writer, request, &coderwebsocket.AcceptOptions{
            OriginPatterns: []string{"*"},
        })
        if nil != acceptErr {
            return
        }
        defer connection.CloseNow()

        serverConnections <- connection

        <-handlerRelease
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    clientConnection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer clientConnection.CloseNow()

    go func() {
        _, _, _ = clientConnection.Read(ctx)
    }()

    serverConnection := <-serverConnections

    liveness := newConnectionLiveness()
    liveness.recordActivity()

    interval := 50 * time.Millisecond

    loopContext, loopCancel := context.WithCancel(ctx)
    defer loopCancel()

    serviceContainer := container.NewContainer()
    serverRuntime := runtime.New(loopContext, serviceContainer.NewScope(), serviceContainer)

    go readLoop(loopContext, loopCancel, serverConnection, serverRuntime, Options{}, liveness)
    go pingLoop(loopContext, loopCancel, serverConnection, interval, time.Second, liveness)

    deadline := time.Now().Add(3 * time.Second)
    for int64(interval) > liveness.lastActivityOffset.Load() {
        if true == time.Now().After(deadline) {
            t.Fatal("a received pong never refreshed the activity mark: the liveness window is dead for a receive-only client")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

type smallSendBufferListener struct {
    net.Listener
}

func (instance *smallSendBufferListener) Accept() (net.Conn, error) {
    connection, acceptErr := instance.Listener.Accept()
    if nil != acceptErr {
        return nil, acceptErr
    }

    if tcpConnection, isTcp := connection.(*net.TCPConn); true == isTcp {
        _ = tcpConnection.SetWriteBuffer(4096)
    }

    return connection, nil
}

func TestStreamHandler_SlowClientDrainingOneFrameIsNotDisconnected(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    100 * time.Millisecond,
        WriteTimeout:   10 * time.Second,
    })

    server := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))

    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }

    server.Listener = &smallSendBufferListener{Listener: listener}
    server.Start()
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    clientConnection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer clientConnection.CloseNow()

    clientConnection.SetReadLimit(4 << 20)

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    payload := strings.Repeat("p", 512*1024)

    drained := make(chan error, 1)
    go func() {
        _, reader, readerErr := clientConnection.Reader(ctx)
        if nil != readerErr {
            drained <- readerErr

            return
        }

        chunk := make([]byte, 8192)
        total := 0
        for {
            read, readErr := reader.Read(chunk)
            total += read

            if io.EOF == readErr {
                break
            }

            if nil != readErr {
                drained <- readErr

                return
            }

            time.Sleep(10 * time.Millisecond)
        }

        if len(payload) != total {
            drained <- io.ErrUnexpectedEOF

            return
        }

        drained <- nil
    }()

    if 1 != hub.Broadcast("demo", melodyhttp.ServerSentEvent{Event: "notification", Data: payload}) {
        t.Fatalf("expected the broadcast to reach the subscriber")
    }

    select {
    case drainErr := <-drained:
        if nil != drainErr {
            t.Fatalf("a healthy client that was slow to drain one frame was disconnected mid-write: %v", drainErr)
        }
    case <-time.After(25 * time.Second):
        t.Fatal("the slow client never finished draining the frame")
    }

    clientConnection.Close(coderwebsocket.StatusNormalClosure, "")
}

func TestStreamHandler_UnansweredPingsDisconnectAPeerThatKeepsAcceptingWrites(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    100 * time.Millisecond,
        WriteTimeout:   500 * time.Millisecond,
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    stopBroadcast := make(chan struct{})
    defer close(stopBroadcast)

    go func() {
        payload := strings.Repeat("e", 200)

        for {
            select {
            case <-stopBroadcast:
                return
            case <-time.After(30 * time.Millisecond):
                hub.Broadcast("demo", melodyhttp.ServerSentEvent{Event: "notification", Data: payload})
            }
        }
    }()

    disconnectDeadline := time.Now().Add(3 * time.Second)
    for 0 < hub.SubscriberCount("demo") {
        if true == time.Now().After(disconnectDeadline) {
            t.Fatalf("a peer that never answered a ping was held for 3s because writes to it kept succeeding: the keepalive loop no longer detects a half-open connection")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

func TestDispatchOnMessage_RecoversPanicFromCallback(t *testing.T) {
    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    options := Options{
        OnMessage: func(_ runtimecontract.Runtime, _ coderwebsocket.MessageType, _ []byte) {
            panic("boom from user callback")
        },
    }

    panicked := dispatchOnMessage(runtimeInstance, options, coderwebsocket.MessageText, []byte("payload"))

    if false == panicked {
        t.Fatalf("expected dispatchOnMessage to recover the callback panic and report it, so the read goroutine does not crash the process")
    }
}

func TestStreamHandler_SlowOnMessageDoesNotDisconnectHealthyClient(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    callbackEntered := make(chan struct{})

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    100 * time.Millisecond,
        OnMessage: func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte) {
            close(callbackEntered)

            time.Sleep(400 * time.Millisecond)
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("work")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    <-callbackEntered

    go func() {
        time.Sleep(500 * time.Millisecond)
        hub.Broadcast("demo", melodyhttp.ServerSentEvent{Event: "notification", Data: "still-here"})
    }()

    _, payload, readErr := connection.Read(ctx)
    if nil != readErr {
        t.Fatalf("a healthy client was disconnected because its OnMessage callback outlived the ping interval: %v", readErr)
    }

    if "still-here" != string(payload) {
        t.Fatalf("unexpected payload %q", payload)
    }

    connection.Close(coderwebsocket.StatusNormalClosure, "")
}

func TestStreamHandler_StuckOnMessageStopsHoldingTheConnection(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    releaseCallback := make(chan struct{})
    defer close(releaseCallback)

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    50 * time.Millisecond,
        OnMessage: func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte) {
            <-releaseCallback
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("wedge")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    reapDeadline := time.Now().Add(3 * time.Second)
    for 0 < hub.SubscriberCount("demo") {
        if true == time.Now().After(reapDeadline) {
            t.Fatalf("a connection wedged in OnMessage was never reaped: the hub subscription is still registered")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

func TestStreamHandler_InFlightCallbackDoesNotRaceScopeTeardown(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    callbackEntered := make(chan struct{})
    resolveOutcome := make(chan string, 1)

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    30 * time.Second,
        OnMessage: func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte) {
            close(callbackEntered)

            time.Sleep(200 * time.Millisecond)

            _, getErr := runtimeInstance.Scope().Get("service")
            if true == errors.Is(getErr, container.ErrScopeClosed) {
                resolveOutcome <- "scope-closed"

                return
            }

            resolveOutcome <- "alive"
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        scope := serviceContainer.NewScope()
        runtimeInstance := runtime.New(request.Context(), scope, serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
        _ = scope.Close()
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    go func() {
        _, _, _ = connection.Read(ctx)
    }()

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("work")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    <-callbackEntered

    hub.Shutdown()

    outcome := <-resolveOutcome
    if "alive" != outcome {
        t.Fatalf("an in-flight OnMessage observed a closed scope (%s): the handler returned to the kernel without waiting for the read loop", outcome)
    }
}

func TestStreamHandler_WedgedCallbackReleasesConnectionAtGraceNotFiveSeconds(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    releaseCallback := make(chan struct{})
    defer close(releaseCallback)

    callbackEntered := make(chan struct{})
    handlerReturned := make(chan struct{})

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    30 * time.Second,
        OnMessage: func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte) {
            close(callbackEntered)
            <-releaseCallback
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
        close(handlerReturned)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("wedge")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    <-callbackEntered

    hub.Shutdown()

    start := time.Now()
    select {
    case <-handlerReturned:
    case <-time.After(4 * time.Second):
        t.Fatalf("the handler still held the wedged connection 4s after teardown: the deferred CloseNow is blocked on the abandoned graceful close")
    }

    if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
        t.Fatalf("the handler held the wedged connection for %v after teardown; expected release at the close grace", elapsed)
    }
}

func TestConnectionLiveness_LeaveCallbackRefreshesActivityBeforeClearingCallback(t *testing.T) {
    previousProcs := goruntime.GOMAXPROCS(0)
    if previousProcs < 2 {
        goruntime.GOMAXPROCS(2)
        defer goruntime.GOMAXPROCS(previousProcs)
    }

    liveness := newConnectionLiveness()

    window := 50 * time.Millisecond

    stop := make(chan struct{})
    reaped := make(chan struct{}, 1)

    go func() {
        for {
            select {
            case <-stop:
                return
            default:
            }

            if 0 != liveness.callbacksRunning.Load() {
                continue
            }

            mark := liveness.lastActivityOffset.Load()
            if window > liveness.elapsed()-time.Duration(mark) {
                continue
            }

            if 0 == liveness.callbacksRunning.Load() && mark == liveness.lastActivityOffset.Load() {
                select {
                case reaped <- struct{}{}:
                default:
                }

                return
            }
        }
    }()

    for iteration := 0; iteration < 1000000; iteration++ {
        liveness.recordActivity()
        liveness.enterCallback()
        liveness.lastActivityOffset.Store(int64(liveness.elapsed()) - int64(2*window))
        liveness.leaveCallback()

        select {
        case <-reaped:
            close(stop)
            t.Fatalf("cannotAnswer observed a stale activity mark at callbacks==0: the ping loop can reap a healthy connection when a callback returns")
        default:
        }
    }

    close(stop)
}

func TestConnectionLiveness_GraceUsesMonotonicBase(t *testing.T) {
    liveness := newConnectionLiveness()

    if liveness.base == liveness.base.Round(0) {
        t.Fatalf("connectionLiveness base carries no monotonic reading; the grace windows would follow the wall clock")
    }

    window := 40 * time.Millisecond
    grace := 200 * time.Millisecond

    liveness.enterCallback()

    if false == liveness.cannotAnswer(false, window, grace, grace) {
        t.Fatalf("a callback within the grace must be excused")
    }

    liveness.callbackStartedOffset.Store(int64(liveness.elapsed()) - int64(2*grace))
    if true == liveness.cannotAnswer(false, window, grace, grace) {
        t.Fatalf("a callback that outran the grace must no longer be excused")
    }
}

func TestConnectionLiveness_InFlightWriteExcusesOnlyThePingItQueuedBehindItself(t *testing.T) {
    liveness := newConnectionLiveness()

    window := 40 * time.Millisecond
    callbackGrace := 200 * time.Millisecond
    graceForWrite := 500 * time.Millisecond

    liveness.lastActivityOffset.Store(int64(liveness.elapsed()) - int64(4*window))

    if true == liveness.cannotAnswer(false, window, callbackGrace, graceForWrite) {
        t.Fatalf("a silent peer with no write in flight must not be excused")
    }

    if false == liveness.cannotAnswer(true, window, callbackGrace, graceForWrite) {
        t.Fatalf("a ping that could not be written past the frame in flight when it was issued must be excused")
    }

    liveness.enterWrite()

    if false == liveness.cannotAnswer(false, window, callbackGrace, graceForWrite) {
        t.Fatalf("a frame that started flushing after the ping was issued must still be excused while it is in flight")
    }

    liveness.writeStartedOffset.Store(int64(liveness.elapsed()) - int64(2*graceForWrite))
    if true == liveness.cannotAnswer(false, window, callbackGrace, graceForWrite) {
        t.Fatalf("a write that outran its grace must no longer excuse anything")
    }

    liveness.leaveWrite()

    liveness.writeStartedOffset.Store(int64(liveness.elapsed()))

    if true == liveness.cannotAnswer(false, window, callbackGrace, graceForWrite) {
        t.Fatalf("a write that completed before the ping was issued must not excuse the ping's timeout")
    }
}

func TestNewStreamHandler_RefusesAZeroIdleTimeout(t *testing.T) {
    for _, idleTimeout := range []time.Duration{0, -time.Second} {
        assertStreamHandlerRefusesIdleTimeout(t, idleTimeout)
    }
}

func assertStreamHandlerRefusesIdleTimeout(t *testing.T, idleTimeout time.Duration) {
    t.Helper()

    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected a panic for an IdleTimeout of %v", idleTimeout)
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        message := recoveredErr.Error()
        if false == strings.Contains(message, "IdleTimeout") {
            t.Fatalf("expected the diagnostic to name the option, got %q", message)
        }

        if false == strings.Contains(message, "ping") {
            t.Fatalf("expected the diagnostic to explain that the keepalive ping is the only reaper, got %q", message)
        }
    }()

    NewStreamHandler(melodyhttp.NewServerSentEventHub(), Options{IdleTimeout: idleTimeout})
}

func TestNewStreamHandler_AcceptsAPositiveIdleTimeout(t *testing.T) {
    if nil == NewStreamHandler(melodyhttp.NewServerSentEventHub(), Options{IdleTimeout: time.Second}) {
        t.Fatal("expected a handler for a positive IdleTimeout")
    }
}

func TestNewStreamHandler_RefusesANilHub(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a nil hub to be refused at construction, not dereferenced on the first request")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError || false == strings.Contains(recoveredErr.Error(), "hub is nil") {
            t.Fatalf("expected the refusal to name the nil hub, got %v", recovered)
        }
    }()

    NewStreamHandler(nil, Options{IdleTimeout: time.Second})
}

func TestStreamHandler_RefusesAnEmptyResolvedTopicBeforeTheUpgrade(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    handler := NewStreamHandler(hub, Options{
        IdleTimeout:   time.Second,
        TopicResolver: func(request httpcontract.Request) string { return "" },
    })

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, nil)
    recorder := httptest.NewRecorder()

    _, handlerErr := handler(runtimeInstance, recorder, request)

    if nil == handlerErr {
        t.Fatal("expected the empty resolved topic to refuse the connection")
    }

    if 0 != hub.SubscriberCount("") {
        t.Fatalf("expected no subscription on the shared degenerate topic, got %d", hub.SubscriberCount(""))
    }

    if nethttp.StatusSwitchingProtocols == recorder.Code {
        t.Fatal("expected the refusal to land before the upgrade, not after a 101")
    }
}

type capturingLogger struct {
    contexts []loggingcontract.Context
}

func (instance *capturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.contexts = append(instance.contexts, context)
}
func (instance *capturingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *capturingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *capturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *capturingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *capturingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func TestDispatchOnMessage_PreservesThePanickedErrorsCauseChain(t *testing.T) {
    logger := &capturingLogger{}

    serviceContainer := container.NewContainer()
    if registerErr := serviceContainer.Register(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    rootCause := errors.New("db down")
    options := Options{
        OnMessage: func(_ runtimecontract.Runtime, _ coderwebsocket.MessageType, _ []byte) {
            panic(exception.NewError("user failure", nil, rootCause))
        },
    }

    panicked := dispatchOnMessage(runtimeInstance, options, coderwebsocket.MessageText, []byte("payload"))

    if false == panicked {
        t.Fatal("expected the panic to be recovered and reported")
    }

    if 0 == len(logger.contexts) {
        t.Fatal("expected the panic to be logged")
    }

    rendered := fmt.Sprintf("%v", logger.contexts[len(logger.contexts)-1])
    if false == strings.Contains(rendered, "db down") {
        t.Fatalf("expected the panicked error's cause chain to survive into the record - the old %%v flatten kept only the message; got %q", rendered)
    }
}

func TestStreamHandler_ANegativeReadLimitDisablesTheDefaultCap(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    defer hub.Shutdown()

    received := make(chan int, 1)

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "large" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    30 * time.Second,
        ReadLimit:      -1,
        OnMessage: func(_ runtimecontract.Runtime, _ coderwebsocket.MessageType, payload []byte) {
            select {
            case received <- len(payload):
            default:
            }
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer()
        runtimeInstance := runtime.New(request.Context(), serviceContainer.NewScope(), serviceContainer)
        melodyRequest := melodyhttp.NewRequest(request, nil, runtimeInstance, nil)
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    oversized := make([]byte, 40*1024)
    if writeErr := connection.Write(ctx, coderwebsocket.MessageBinary, oversized); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    select {
    case size := <-received:
        if len(oversized) != size {
            t.Fatalf("expected the whole %d-byte frame, got %d", len(oversized), size)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the oversized frame to reach OnMessage with the read limit disabled")
    }
}

func TestStreamHandler_RefusesAShutDownHubBeforeTheUpgrade(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()
    hub.Shutdown()

    handler := NewStreamHandler(hub, Options{
        IdleTimeout:   time.Second,
        TopicResolver: func(request httpcontract.Request) string { return "demo" },
    })

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, nil)
    recorder := httptest.NewRecorder()

    _, handlerErr := handler(runtimeInstance, recorder, request)

    if nil == handlerErr {
        t.Fatal("expected a connection against a shut-down hub to be refused, not upgraded to an instantly-closed stream")
    }

    if nethttp.StatusSwitchingProtocols == recorder.Code {
        t.Fatal("expected the refusal to land before the upgrade, not after a 101")
    }

    if 0 != hub.SubscriberCount("demo") {
        t.Fatalf("expected no subscription on a shut-down hub, got %d", hub.SubscriberCount("demo"))
    }
}
