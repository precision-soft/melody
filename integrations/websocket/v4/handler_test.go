package websocket

import (
    "context"
    "errors"
    "fmt"
    "io"
    "net"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    goruntime "runtime"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    coderwebsocket "github.com/coder/websocket"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func TestStreamHandler_BroadcastReachesClient(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    bound.Of(30 * time.Second),
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    handler := newStreamHandlerPastTheDoor(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
    }, 100*time.Millisecond)

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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

    /* the client's blocking Read below answers the server pings in the meantime */
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
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    handler := newStreamHandlerPastTheDoor(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
    }, 100*time.Millisecond)

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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

    /* the client never reads, so it never answers a ping: coder/websocket only replies while the application reads */
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

    /* the client's blocking read is what answers the server pings with pongs */
    go func() {
        _, _, _ = clientConnection.Read(ctx)
    }()

    serverConnection := <-serverConnections

    liveness := newConnectionLiveness()
    liveness.recordActivity()

    interval := 50 * time.Millisecond

    loopContext, loopCancel := context.WithCancel(ctx)
    defer loopCancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serverRuntime := runtime.NewRequest(loopContext, serviceContainer.NewScope())

    go readLoop(loopContext, loopCancel, newStreamConnection(serverConnection, loopContext, time.Second, liveness), serverRuntime, StreamHandlerConfig{}, liveness, newConnectionLogger(serverRuntime))
    go pingLoop(loopContext, loopCancel, serverConnection, interval, time.Second, liveness)

    /* the accept-time mark is younger than one interval, so only a pong observed at or after the first tick can push it past that */
    deadline := time.Now().Add(3 * time.Second)
    for int64(interval) > liveness.lastActivityOffset.Load() {
        if true == time.Now().After(deadline) {
            t.Fatal("a received pong never refreshed the activity mark: the liveness window is dead for a receive-only client")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

/* a small kernel send buffer parks the server in connection.Write without pushing megabytes */
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

func TestStreamHandler_TheWriteTimeoutBoundsAWriteTheClientNeverDrains(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    defer hub.CloseWithContext(context.Background())

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        WriteTimeout:   bound.Of(100 * time.Millisecond),
    })

    handlerReturned := make(chan struct{})

    server := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
        handler(runtimeInstance, writer, melodyRequest)
        close(handlerReturned)
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

    subscribeDeadline := time.Now().Add(2 * time.Second)
    for hub.SubscriberCount("demo") < 1 {
        if true == time.Now().After(subscribeDeadline) {
            t.Fatalf("the websocket handler did not subscribe to the hub in time")
        }
        time.Sleep(time.Millisecond)
    }

    /* far more than the loopback buffers hold, and the client never reads, so the write parks until its timeout */
    if 1 != hub.Broadcast("demo", melodyhttp.ServerSentEvent{Event: "notification", Data: strings.Repeat("p", 16<<20)}) {
        t.Fatalf("expected the broadcast to reach the subscriber")
    }

    select {
    case <-handlerReturned:
    case <-time.After(4 * time.Second):
        t.Fatal("expected the undrained write to fail at the 100ms write timeout and the handler to return")
    }
}

func TestStreamHandler_SlowClientDrainingOneFrameIsNotDisconnected(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    handler := newStreamHandlerPastTheDoor(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        WriteTimeout:   bound.Of(10 * time.Second),
    }, 100*time.Millisecond)

    server := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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

            /* drain slower than the server writes, so the handler stays parked inside connection.Write for several ping intervals */
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
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    handler := newStreamHandlerPastTheDoor(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        WriteTimeout:   bound.Of(500 * time.Millisecond),
    }, 100*time.Millisecond)

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
        handler(runtimeInstance, writer, melodyRequest)
    }))
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    /* the client never reads, so it never answers a ping: coder/websocket only replies while the application reads */
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

    /* every third of an interval: each write completes into the send buffer while the peer answers nothing */
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
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())

    config := StreamHandlerConfig{
        OnMessage: func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            panic("boom from user callback")
        },
    }

    panicked := dispatchOnMessage(runtimeInstance, config, nil, InboundMessage{Payload: []byte("payload")}, newConnectionLogger(runtimeInstance))

    if false == panicked {
        t.Fatalf("expected dispatchOnMessage to recover the callback panic and report it, so the read goroutine does not crash the process")
    }
}

func TestStreamHandler_SlowOnMessageDoesNotDisconnectHealthyClient(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    callbackEntered := make(chan struct{})

    handler := newStreamHandlerPastTheDoor(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        OnMessage: func(runtimeInstance containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            close(callbackEntered)

            /* outlive several ping intervals while the read loop cannot answer pongs */
            time.Sleep(400 * time.Millisecond)

            return nil
        },
    }, 100*time.Millisecond)

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    releaseCallback := make(chan struct{})
    defer close(releaseCallback)

    handler := newStreamHandlerPastTheDoor(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        OnMessage: func(runtimeInstance containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            <-releaseCallback

            return nil
        },
    }, 50*time.Millisecond)

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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

    /* the grace is ten intervals; give it that plus slack, then the subscription must be gone */
    reapDeadline := time.Now().Add(3 * time.Second)
    for 0 < hub.SubscriberCount("demo") {
        if true == time.Now().After(reapDeadline) {
            t.Fatalf("a connection wedged in OnMessage was never reaped: the hub subscription is still registered")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

func TestStreamHandler_InFlightCallbackDoesNotRaceScopeTeardown(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    callbackEntered := make(chan struct{})
    resolveOutcome := make(chan string, 1)

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    bound.Of(30 * time.Second),
        OnMessage: func(runtimeInstance containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            close(callbackEntered)

            time.Sleep(200 * time.Millisecond)

            _, getErr := runtimeInstance.Scope().Get(runtimeInstance.Context(), "service")
            if true == errors.Is(getErr, container.ErrScopeClosed) {
                resolveOutcome <- "scope-closed"

                return nil
            }

            resolveOutcome <- "alive"

            return nil
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        scope := serviceContainer.NewScope()
        runtimeInstance := runtime.NewRequest(request.Context(), scope)
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
        handler(runtimeInstance, writer, melodyRequest)
        /* mimic the kernel's deferred scope.Close() the instant the handler returns */
        _ = scope.CloseWithContext(context.Background())
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

    /* keep answering control frames, so a close that did not wait for the read loop would return early */
    go func() {
        _, _, _ = connection.Read(ctx)
    }()

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("work")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    <-callbackEntered

    _ = hub.CloseWithContext(context.Background())

    outcome := <-resolveOutcome
    if "alive" != outcome {
        t.Fatalf("an in-flight OnMessage observed a closed scope (%s): the handler returned to the kernel without waiting for the read loop", outcome)
    }
}

func TestStreamHandler_WedgedCallbackReleasesConnectionAtGraceNotFiveSeconds(t *testing.T) {
    if elapsed := wedgedCallbackReleaseAfterTeardown(t, bound.Duration{}); elapsed > 2500*time.Millisecond {
        t.Fatalf("the handler held the wedged connection for %v after teardown; expected release at the close grace", elapsed)
    }
}

func TestStreamHandler_TheCloseHandshakeGraceBoundsTheTeardownWait(t *testing.T) {
    if elapsed := wedgedCallbackReleaseAfterTeardown(t, bound.Of(50*time.Millisecond)); elapsed > 600*time.Millisecond {
        t.Fatalf("the handler held the wedged connection for %v after teardown; expected release at the 50ms close grace", elapsed)
    }
}

func wedgedCallbackReleaseAfterTeardown(t *testing.T, closeHandshakeGrace bound.Duration) time.Duration {
    t.Helper()

    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    releaseCallback := make(chan struct{})
    defer close(releaseCallback)

    callbackEntered := make(chan struct{})
    handlerReturned := make(chan struct{})

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        TopicResolver:       func(request httpcontract.Request) string { return "demo" },
        OriginPatterns:      []string{"*"},
        IdleTimeout:         bound.Of(30 * time.Second),
        CloseHandshakeGrace: closeHandshakeGrace,
        OnMessage: func(runtimeInstance containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            close(callbackEntered)
            <-releaseCallback

            return nil
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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

    /* the client never reads again, so an unfixed graceful close ends only at coder/websocket's five-second timeout */
    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("wedge")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    <-callbackEntered

    _ = hub.CloseWithContext(context.Background())

    start := time.Now()
    select {
    case <-handlerReturned:
    case <-time.After(4 * time.Second):
        t.Fatalf("the handler still held the wedged connection 4s after teardown: the deferred CloseNow is blocked on the abandoned graceful close")
    }

    return time.Since(start)
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
        /* age the activity mark past the window, as a long-running callback would */
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

    /* a monotonic reading differs from its wall-clock rounding; one stripped of it does not */
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

    /* age the activity mark out of its window so only the write branches can excuse */
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

func TestResolveStreamHandlerConfig_AnEmptyConfigTakesEveryDefault(t *testing.T) {
    resolved, resolveErr := resolveStreamHandlerConfig(StreamHandlerConfig{})
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if 10*time.Second != resolved.writeTimeout || 32768 != resolved.readLimit || 30*time.Second != resolved.idleTimeout || time.Second != resolved.closeHandshakeGrace {
        t.Fatalf("expected 10s, 32 KiB, 30s and 1s, got %+v", resolved)
    }
}

func TestResolveStreamHandlerConfig_GivenValuesReachTheResolvedConfig(t *testing.T) {
    resolved, resolveErr := resolveStreamHandlerConfig(StreamHandlerConfig{
        WriteTimeout:        bound.Of(time.Millisecond),
        ReadLimit:           bound.Of(1),
        IdleTimeout:         bound.Of(time.Second),
        CloseHandshakeGrace: bound.Of(time.Millisecond),
    })
    if nil != resolveErr {
        t.Fatalf("unexpected error: %v", resolveErr)
    }

    if time.Millisecond != resolved.writeTimeout || 1 != resolved.readLimit || time.Second != resolved.idleTimeout || time.Millisecond != resolved.closeHandshakeGrace {
        t.Fatalf("expected the given values, got %+v", resolved)
    }

    unlimited, unlimitedErr := resolveStreamHandlerConfig(StreamHandlerConfig{ReadLimit: bound.UnlimitedCount()})
    if nil != unlimitedErr {
        t.Fatalf("unexpected error: %v", unlimitedErr)
    }

    if -1 != unlimited.readLimit {
        t.Fatalf("expected an unlimited read limit to resolve to the library's no-limit value, got %d", unlimited.readLimit)
    }
}

func TestNewStreamHandler_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    testCases := []struct {
        name    string
        config  StreamHandlerConfig
        field   string
    }{
        {"sub-second idle timeout", StreamHandlerConfig{IdleTimeout: bound.Of(500 * time.Millisecond)}, "StreamHandlerConfig.IdleTimeout"},
        {"negative idle timeout", StreamHandlerConfig{IdleTimeout: bound.Of(-time.Second)}, "StreamHandlerConfig.IdleTimeout"},
        {"unlimited idle timeout", StreamHandlerConfig{IdleTimeout: bound.UnlimitedDuration()}, "StreamHandlerConfig.IdleTimeout"},
        {"zero write timeout", StreamHandlerConfig{WriteTimeout: bound.Of(time.Duration(0))}, "StreamHandlerConfig.WriteTimeout"},
        {"unlimited write timeout", StreamHandlerConfig{WriteTimeout: bound.UnlimitedDuration()}, "StreamHandlerConfig.WriteTimeout"},
        {"zero read limit", StreamHandlerConfig{ReadLimit: bound.Of(0)}, "StreamHandlerConfig.ReadLimit"},
        {"negative read limit", StreamHandlerConfig{ReadLimit: bound.Of(-1)}, "StreamHandlerConfig.ReadLimit"},
        {"zero close handshake grace", StreamHandlerConfig{CloseHandshakeGrace: bound.Of(time.Duration(0))}, "StreamHandlerConfig.CloseHandshakeGrace"},
        {"unlimited close handshake grace", StreamHandlerConfig{CloseHandshakeGrace: bound.UnlimitedDuration()}, "StreamHandlerConfig.CloseHandshakeGrace"},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            var refusal *exception.Error

            func() {
                defer func() {
                    recovered := recover()
                    recoveredErr, isError := recovered.(error)
                    if false == isError || false == errors.As(recoveredErr, &refusal) {
                        t.Fatalf("expected an exception error, got %T: %v", recovered, recovered)
                    }
                }()

                NewStreamHandler(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), testCase.config)
            }()

            if testCase.field != refusal.Context()["field"] {
                t.Fatalf("expected the refusal to name %s, got %v", testCase.field, refusal.Context())
            }
        })
    }
}

func TestNewStreamHandler_AcceptsAnEmptyConfig(t *testing.T) {
    if nil == NewStreamHandler(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), StreamHandlerConfig{}) {
        t.Fatal("expected a handler for an empty config")
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

    NewStreamHandler(nil, StreamHandlerConfig{IdleTimeout: bound.Of(time.Second)})
}

func TestStreamHandler_RefusesAnEmptyResolvedTopicBeforeTheUpgrade(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        IdleTimeout:   bound.Of(time.Second),
        TopicResolver: func(request httpcontract.Request) string { return "" },
    })

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{})
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

func (instance *capturingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *capturingLogger) CauseChainDepth() int {
    return 8
}

func TestDispatchOnMessage_PreservesThePanickedErrorsCauseChain(t *testing.T) {
    logger := &capturingLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    if registerErr := serviceContainer.Register(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{}); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }
    runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())

    rootCause := errors.New("db down")
    config := StreamHandlerConfig{
        OnMessage: func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            panic(exception.NewError("user failure", nil, rootCause))
        },
    }

    panicked := dispatchOnMessage(runtimeInstance, config, nil, InboundMessage{Payload: []byte("payload")}, newConnectionLogger(runtimeInstance))

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

type unwrapPanickingCallbackError struct{}

func (unwrapPanickingCallbackError) Error() string {
    return "a callback failure whose Unwrap panics"
}

func (unwrapPanickingCallbackError) Unwrap() error {
    panic("Unwrap() panics")
}

func TestDispatchOnMessage_SurvivesAPanicWhoseUnwrapPanics(t *testing.T) {
    logger := &capturingLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    if registerErr := serviceContainer.Register(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{}); nil != registerErr {
        t.Fatalf("register: %v", registerErr)
    }
    runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())

    config := StreamHandlerConfig{
        OnMessage: func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            panic(unwrapPanickingCallbackError{})
        },
    }

    if false == dispatchOnMessage(runtimeInstance, config, nil, InboundMessage{Payload: []byte("payload")}, newConnectionLogger(runtimeInstance)) {
        t.Fatal("expected the panic to be recovered and reported")
    }

    if 0 == len(logger.contexts) {
        t.Fatal("expected the panic to be logged")
    }

    rendered := fmt.Sprintf("%v", logger.contexts[len(logger.contexts)-1])
    if false == strings.Contains(rendered, "a callback failure whose Unwrap panics") {
        t.Fatalf("expected the panic value in the record, got %q", rendered)
    }
}

func TestStreamHandler_AnUnlimitedReadLimitDisablesTheDefaultCap(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    defer hub.CloseWithContext(context.Background())

    received := make(chan int, 1)

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "large" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    bound.Of(30 * time.Second),
        ReadLimit:      bound.UnlimitedCount(),
        OnMessage: func(_ containercontract.Runtime, _ StreamConnection, message InboundMessage) error {
            select {
            case received <- len(message.Payload):
            default:
            }

            return nil
        },
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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

    /* larger than coder/websocket's 32 KiB default read limit */
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
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    _ = hub.CloseWithContext(context.Background())

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        IdleTimeout:   bound.Of(time.Second),
        TopicResolver: func(request httpcontract.Request) string { return "demo" },
    })

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/stream", nil)
    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{})
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

func TestStreamHandler_BinaryWritesDeliverTheBroadcastAsABinaryFrame(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    handler := NewStreamHandler(hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    bound.Of(30 * time.Second),
        BinaryWrites:   true,
    })

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(request.Context(), serviceContainer.NewScope())
        melodyRequest := melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{})
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

    if coderwebsocket.MessageBinary != messageType || "hello-ws" != string(payload) {
        t.Fatalf("unexpected message: %v %q", messageType, payload)
    }

    connection.Close(coderwebsocket.StatusNormalClosure, "")
}

type messageCapturingLogger struct {
    mutex    sync.Mutex
    messages []string
    contexts []loggingcontract.Context
}

func (instance *messageCapturingLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.messages = append(instance.messages, message)
    instance.contexts = append(instance.contexts, context)
}
func (instance *messageCapturingLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *messageCapturingLogger) Info(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *messageCapturingLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *messageCapturingLogger) Error(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}
func (instance *messageCapturingLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log("", message, context)
}

func (instance *messageCapturingLogger) Enabled(level loggingcontract.Level) bool {
    return true
}

func (instance *messageCapturingLogger) CauseChainDepth() int {
    return 8
}

func (instance *messageCapturingLogger) hasMessage(message string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, recorded := range instance.messages {
        if message == recorded {
            return true
        }
    }

    return false
}

func runtimeWithLogger(ctx context.Context, logger loggingcontract.Logger) containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{})

    return runtime.NewRequest(ctx, serviceContainer.NewScope())
}

func TestDispatchOnMessage_APanicRecordCarriesTheStack(t *testing.T) {
    logger := &messageCapturingLogger{}
    runtimeInstance := runtimeWithLogger(context.Background(), logger)

    config := StreamHandlerConfig{
        OnMessage: func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            panic("callback boom")
        },
    }

    if false == dispatchOnMessage(runtimeInstance, config, nil, InboundMessage{Payload: []byte("payload")}, newConnectionLogger(runtimeInstance)) {
        t.Fatal("expected the panic recovered")
    }

    panicStack, _ := logger.contexts[len(logger.contexts)-1]["panicStack"].(string)
    if false == strings.Contains(panicStack, "TestDispatchOnMessage_APanicRecordCarriesTheStack") {
        t.Fatalf("expected the record to carry the stack the callback panicked on, got %q", panicStack)
    }
}

func TestDispatchOnMessage_APanicAfterTheScopeClosedStillLeavesARecordThroughTheLoggerCapturedAtUpgrade(t *testing.T) {
    logger := &messageCapturingLogger{}

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(logging.ServiceLogger, func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
        return logger, nil
    }, container.RegisterOptions{})
    scope := serviceContainer.NewScope()
    runtimeInstance := runtime.NewRequest(context.Background(), scope)

    captured := newConnectionLogger(runtimeInstance)

    if closeErr := scope.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("close the scope: %v", closeErr)
    }

    config := StreamHandlerConfig{
        OnMessage: func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
            panic("callback boom after the scope closed")
        },
    }

    if false == dispatchOnMessage(runtimeInstance, config, nil, InboundMessage{Payload: []byte("payload")}, captured) {
        t.Fatal("expected the panic recovered")
    }

    if false == logger.hasMessage("websocket OnMessage panicked") {
        t.Fatal("expected the panic recorded through the logger captured while the scope was alive")
    }
}

func TestStreamHandler_ADrainingHubAnswers503AndAnEmptyTopic400(t *testing.T) {
    for name, setUp := range map[string]struct {
        hub      func() *melodyhttp.ServerSentEventHub
        resolver func(request httpcontract.Request) string
        status   int
    }{
        "draining hub": {
            hub: func() *melodyhttp.ServerSentEventHub {
                hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
                _ = hub.CloseWithContext(context.Background())

                return hub
            },
            resolver: func(request httpcontract.Request) string { return "demo" },
            status:   nethttp.StatusServiceUnavailable,
        },
        "empty topic": {
            hub: func() *melodyhttp.ServerSentEventHub {
                return melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
            },
            resolver: func(request httpcontract.Request) string { return "" },
            status:   nethttp.StatusBadRequest,
        },
    } {
        handler := NewStreamHandler(setUp.hub(), StreamHandlerConfig{IdleTimeout: bound.Of(time.Second), TopicResolver: setUp.resolver})

        serviceContainer := container.NewContainer(container.ContainerConfig{})
        runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())
        request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/stream", nil), runtimeInstance, melodyhttp.RequestConfig{})

        _, handlerErr := handler(runtimeInstance, httptest.NewRecorder(), request)

        httpException := exception.AsHttpException(handlerErr)
        if nil == httpException || setUp.status != httpException.StatusCode() {
            t.Fatalf("%s: expected an http exception at %d, got %v", name, setUp.status, handlerErr)
        }
    }
}

func serveStreamHandlerWithLogger(t *testing.T, hub *melodyhttp.ServerSentEventHub, config StreamHandlerConfig, logger loggingcontract.Logger) string {
    t.Helper()

    handler := NewStreamHandler(hub, config)

    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        runtimeInstance := runtimeWithLogger(request.Context(), logger)
        handler(runtimeInstance, writer, melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{}))
    }))
    t.Cleanup(server.Close)

    return "ws" + strings.TrimPrefix(server.URL, "http")
}

func awaitLoggedMessage(t *testing.T, logger *messageCapturingLogger, message string) {
    t.Helper()

    deadline := time.Now().Add(3 * time.Second)
    for false == logger.hasMessage(message) {
        if true == time.Now().After(deadline) {
            t.Fatalf("expected the record %q, got %v", message, logger.messages)
        }

        time.Sleep(5 * time.Millisecond)
    }
}

func TestStreamHandler_AnOversizedFrameLeavesAReadLoopEndedRecord(t *testing.T) {
    logger := &messageCapturingLogger{}
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    wsUrl := serveStreamHandlerWithLogger(t, hub, StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    bound.Of(30 * time.Second),
        ReadLimit:      bound.Of(16),
    }, logger)

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, wsUrl, nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    defer connection.CloseNow()

    _ = connection.Write(ctx, coderwebsocket.MessageText, []byte(strings.Repeat("x", 64)))

    awaitLoggedMessage(t, logger, "websocket read loop ended")
}

func TestStreamHandler_AHubClosedBetweenTheCheckAndTheSubscribeLeavesARecord(t *testing.T) {
    logger := &messageCapturingLogger{}
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    wsUrl := serveStreamHandlerWithLogger(t, hub, StreamHandlerConfig{
        TopicResolver: func(request httpcontract.Request) string {
            _ = hub.CloseWithContext(context.Background())

            return "demo"
        },
        OriginPatterns: []string{"*"},
        IdleTimeout:    bound.Of(30 * time.Second),
    }, logger)

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    connection, _, dialErr := coderwebsocket.Dial(ctx, wsUrl, nil)
    if nil == dialErr {
        defer connection.CloseNow()
    }

    awaitLoggedMessage(t, logger, "websocket hub shut down during connect, closing the stream")
}

func (instance *messageCapturingLogger) countMessage(message string) int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    count := 0
    for _, logged := range instance.messages {
        if message == logged {
            count++
        }
    }

    return count
}

func dialStream(t *testing.T, wsUrl string) (*coderwebsocket.Conn, context.Context) {
    t.Helper()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    t.Cleanup(cancel)

    connection, _, dialErr := coderwebsocket.Dial(ctx, wsUrl, nil)
    if nil != dialErr {
        t.Fatalf("dial: %v", dialErr)
    }
    t.Cleanup(func() {
        _ = connection.CloseNow()
    })

    return connection, ctx
}

func awaitSubscriberCount(t *testing.T, hub *melodyhttp.ServerSentEventHub, expected int) {
    t.Helper()

    deadline := time.Now().Add(3 * time.Second)
    for expected != hub.SubscriberCount("demo") {
        if true == time.Now().After(deadline) {
            t.Fatalf("expected %d subscribers on the topic, got %d", expected, hub.SubscriberCount("demo"))
        }

        time.Sleep(time.Millisecond)
    }
}

func demoStreamHandlerConfig(onMessage func(runtimeInstance containercontract.Runtime, connection StreamConnection, message InboundMessage) error) StreamHandlerConfig {
    return StreamHandlerConfig{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    bound.Of(30 * time.Second),
        OnMessage:      onMessage,
    }
}

func TestStreamHandler_AFrameReachesOnMessageAsAnInboundMessageOfItsKind(t *testing.T) {
    received := make(chan InboundMessage, 2)
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    wsUrl := serveStreamHandlerWithLogger(t, hub, demoStreamHandlerConfig(func(_ containercontract.Runtime, _ StreamConnection, message InboundMessage) error {
        received <- message

        return nil
    }), &messageCapturingLogger{})

    connection, ctx := dialStream(t, wsUrl)

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("text-frame")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }
    if writeErr := connection.Write(ctx, coderwebsocket.MessageBinary, []byte("binary-frame")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    textMessage := <-received
    if false != textMessage.Binary || "text-frame" != string(textMessage.Payload) {
        t.Fatalf("expected a text InboundMessage, got %+v", textMessage)
    }

    binaryMessage := <-received
    if true != binaryMessage.Binary || "binary-frame" != string(binaryMessage.Payload) {
        t.Fatalf("expected a binary InboundMessage, got %+v", binaryMessage)
    }
}

func TestStreamHandler_ANilErrorKeepsTheConnection(t *testing.T) {
    received := make(chan string, 2)
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    wsUrl := serveStreamHandlerWithLogger(t, hub, demoStreamHandlerConfig(func(_ containercontract.Runtime, _ StreamConnection, message InboundMessage) error {
        received <- string(message.Payload)

        return nil
    }), &messageCapturingLogger{})

    connection, ctx := dialStream(t, wsUrl)
    awaitSubscriberCount(t, hub, 1)

    for _, payload := range []string{"first", "second"} {
        if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte(payload)); nil != writeErr {
            t.Fatalf("write: %v", writeErr)
        }

        if payload != <-received {
            t.Fatalf("expected %q delivered", payload)
        }
    }

    if 1 != hub.Broadcast("demo", melodyhttp.ServerSentEvent{Data: "after-the-callbacks"}) {
        t.Fatal("expected the connection still subscribed after two callbacks answered nil")
    }

    _, payload, readErr := connection.Read(ctx)
    if nil != readErr || "after-the-callbacks" != string(payload) {
        t.Fatalf("expected the broadcast after two nil errors, got %q %v", payload, readErr)
    }
}

func TestStreamHandler_AFailingOrPanickingCallbackClosesTheConnectionWithStatus1011(t *testing.T) {
    testCases := map[string]struct {
        onMessage func(runtimeInstance containercontract.Runtime, connection StreamConnection, message InboundMessage) error
        record    string
    }{
        "error": {
            onMessage: func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
                return errors.New("callback refused the frame")
            },
            record: "websocket OnMessage failed",
        },
        "panic": {
            onMessage: func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
                panic("callback boom")
            },
            record: "websocket OnMessage panicked",
        },
    }

    for name, testCase := range testCases {
        t.Run(name, func(t *testing.T) {
            logger := &messageCapturingLogger{}
            hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

            wsUrl := serveStreamHandlerWithLogger(t, hub, demoStreamHandlerConfig(testCase.onMessage), logger)

            connection, ctx := dialStream(t, wsUrl)
            awaitSubscriberCount(t, hub, 1)

            if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("frame")); nil != writeErr {
                t.Fatalf("write: %v", writeErr)
            }

            _, _, readErr := connection.Read(ctx)
            if coderwebsocket.StatusInternalError != coderwebsocket.CloseStatus(readErr) {
                t.Fatalf("expected the peer to read close status 1011, got %v", readErr)
            }

            awaitSubscriberCount(t, hub, 0)

            if 1 != logger.countMessage(testCase.record) {
                t.Fatalf("expected the record %q once, got %v", testCase.record, logger.messages)
            }
        })
    }
}

func TestStreamHandler_OnMessageRepliesThroughTheConnection(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    wsUrl := serveStreamHandlerWithLogger(t, hub, demoStreamHandlerConfig(func(runtimeInstance containercontract.Runtime, connection StreamConnection, message InboundMessage) error {
        return connection.Send(runtimeInstance, OutboundMessage{Binary: true, Payload: append([]byte("echo:"), message.Payload...)})
    }), &messageCapturingLogger{})

    connection, ctx := dialStream(t, wsUrl)

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("ping")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    messageType, payload, readErr := connection.Read(ctx)
    if nil != readErr || coderwebsocket.MessageBinary != messageType || "echo:ping" != string(payload) {
        t.Fatalf("expected the binary reply echo:ping, got %v %q %v", messageType, payload, readErr)
    }
}

func TestStreamConnection_ConcurrentSendsEachArriveWhole(t *testing.T) {
    kept := make(chan StreamConnection, 1)
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    wsUrl := serveStreamHandlerWithLogger(t, hub, demoStreamHandlerConfig(func(_ containercontract.Runtime, connection StreamConnection, _ InboundMessage) error {
        kept <- connection

        return nil
    }), &messageCapturingLogger{})

    connection, ctx := dialStream(t, wsUrl)
    awaitSubscriberCount(t, hub, 1)

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("hello")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    streamConnection := <-kept
    senderRuntime := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())

    const senderCount = 16
    sendErrors := make(chan error, senderCount+1)
    var waitGroup sync.WaitGroup
    for index := 0; index < senderCount; index++ {
        waitGroup.Add(1)
        go func(index int) {
            defer waitGroup.Done()

            sendErrors <- streamConnection.Send(senderRuntime, OutboundMessage{Payload: []byte(fmt.Sprintf("frame-%d", index))})
        }(index)
    }
    hub.Broadcast("demo", melodyhttp.ServerSentEvent{Data: "broadcast"})

    expected := map[string]bool{"broadcast": true}
    for index := 0; index < senderCount; index++ {
        expected[fmt.Sprintf("frame-%d", index)] = true
    }

    for 0 < len(expected) {
        _, payload, readErr := connection.Read(ctx)
        if nil != readErr {
            t.Fatalf("read: %v, still expecting %v", readErr, expected)
        }

        if false == expected[string(payload)] {
            t.Fatalf("unexpected or repeated frame %q", payload)
        }
        delete(expected, string(payload))
    }

    waitGroup.Wait()
    close(sendErrors)
    for sendErr := range sendErrors {
        if nil != sendErr {
            t.Fatalf("expected every concurrent send to succeed, got %v", sendErr)
        }
    }
}

func TestStreamConnection_ASendAfterTheConnectionClosedIsRefused(t *testing.T) {
    kept := make(chan StreamConnection, 1)
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

    wsUrl := serveStreamHandlerWithLogger(t, hub, demoStreamHandlerConfig(func(_ containercontract.Runtime, connection StreamConnection, _ InboundMessage) error {
        kept <- connection

        return nil
    }), &messageCapturingLogger{})

    connection, ctx := dialStream(t, wsUrl)
    awaitSubscriberCount(t, hub, 1)

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("hello")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    streamConnection := <-kept

    _ = connection.Close(coderwebsocket.StatusNormalClosure, "")
    awaitSubscriberCount(t, hub, 0)

    senderRuntime := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
    sendErr := streamConnection.Send(senderRuntime, OutboundMessage{Payload: []byte("too late")})
    if false == errors.Is(sendErr, ErrStreamConnectionClosed) {
        t.Fatalf("expected ErrStreamConnectionClosed after the close, got %v", sendErr)
    }
}

func TestStreamConnection_SendRefusesANilRuntime(t *testing.T) {
    stream := newStreamConnection(nil, context.Background(), time.Second, newConnectionLiveness())

    var typedNil *runtimeWithoutContext
    for name, runtimeInstance := range map[string]containercontract.Runtime{
        "nil":        nil,
        "typed nil":  typedNil,
        "no context": &runtimeWithoutContext{},
    } {
        sendErr := stream.Send(runtimeInstance, OutboundMessage{Payload: []byte("frame")})
        if nil == sendErr || false == strings.Contains(sendErr.Error(), "the runtime is nil or carries no context") {
            t.Fatalf("%s: expected the nil-runtime refusal, got %v", name, sendErr)
        }
    }
}

type runtimeWithoutContext struct {
    containercontract.Runtime
}

func (instance *runtimeWithoutContext) Context() context.Context {
    return nil
}

func TestStreamConnection_ASendWaitingBehindAWriteIsBoundedByTheWriteTimeout(t *testing.T) {
    stream := newStreamConnection(nil, context.Background(), 50*time.Millisecond, newConnectionLiveness())
    stream.writeLock <- struct{}{}

    senderRuntime := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())

    startedAt := time.Now()
    sendErr := stream.Send(senderRuntime, OutboundMessage{Payload: []byte("frame")})

    if false == errors.Is(sendErr, context.DeadlineExceeded) {
        t.Fatalf("expected the write timeout to end the wait for the lock, got %v", sendErr)
    }

    if 2*time.Second < time.Since(startedAt) {
        t.Fatalf("the wait for the write lock outlived the write timeout: %s", time.Since(startedAt))
    }
}

func TestStreamHandlerConfig_ItsCallbacksCarryNoThirdPartyType(t *testing.T) {
    var thirdParty []string

    visited := map[reflect.Type]bool{}

    var judge func(owner string, typeInstance reflect.Type)
    judge = func(owner string, typeInstance reflect.Type) {
        if true == visited[typeInstance] {
            return
        }
        visited[typeInstance] = true

        switch typeInstance.Kind() {
        case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
            judge(owner, typeInstance.Elem())
            return
        case reflect.Map:
            judge(owner, typeInstance.Key())
            judge(owner, typeInstance.Elem())
            return
        case reflect.Func:
            for index := 0; index < typeInstance.NumIn(); index++ {
                judge(owner, typeInstance.In(index))
            }
            for index := 0; index < typeInstance.NumOut(); index++ {
                judge(owner, typeInstance.Out(index))
            }
            return
        }

        packagePath := typeInstance.PkgPath()
        if "" != packagePath && true == strings.Contains(packagePath, ".") && false == strings.HasPrefix(packagePath, "github.com/precision-soft/melody/") {
            thirdParty = append(thirdParty, owner+": "+packagePath)
            return
        }

        if reflect.Struct == typeInstance.Kind() && "" != packagePath && true == strings.HasPrefix(packagePath, "github.com/precision-soft/melody/") {
            for index := 0; index < typeInstance.NumField(); index++ {
                field := typeInstance.Field(index)
                judge(owner+"."+typeInstance.Name()+"."+field.Name, field.Type)
            }
        }
    }

    judgeFunction := func(owner string, functionType reflect.Type) {
        for index := 0; index < functionType.NumIn(); index++ {
            judge(owner, functionType.In(index))
        }
        for index := 0; index < functionType.NumOut(); index++ {
            judge(owner, functionType.Out(index))
        }
    }

    configType := reflect.TypeOf(StreamHandlerConfig{})
    for index := 0; index < configType.NumField(); index++ {
        field := configType.Field(index)
        if reflect.Func == field.Type.Kind() {
            judgeFunction("StreamHandlerConfig."+field.Name, field.Type)
        }
    }

    connectionType := reflect.TypeOf((*StreamConnection)(nil)).Elem()
    for index := 0; index < connectionType.NumMethod(); index++ {
        method := connectionType.Method(index)
        judgeFunction("StreamConnection."+method.Name, method.Type)
    }

    if 0 < len(thirdParty) {
        t.Fatalf("expected melody and standard library types only, got %v", thirdParty)
    }
}

func TestStreamConnection_ASendOnAStreamMarkedClosedIsRefusedBeforeTheWrite(t *testing.T) {
    stream := newStreamConnection(nil, context.Background(), time.Second, newConnectionLiveness())
    stream.closed.Store(true)

    senderRuntime := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
    if sendErr := stream.Send(senderRuntime, OutboundMessage{Payload: []byte("frame")}); false == errors.Is(sendErr, ErrStreamConnectionClosed) {
        t.Fatalf("expected ErrStreamConnectionClosed on a stream marked closed, got %v", sendErr)
    }
}

func TestCloseConnection_MarksTheStreamClosedBeforeTheConnectionContextEnds(t *testing.T) {
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

    connection, ctx := dialStream(t, "ws"+strings.TrimPrefix(server.URL, "http"))
    go func() {
        _, _, _ = connection.Read(ctx)
    }()

    liveness := newConnectionLiveness()
    stream := newStreamConnection(<-serverConnections, context.Background(), time.Second, liveness)

    readLoopDone := make(chan struct{})
    close(readLoopDone)
    closeConnection(stream, liveness, readLoopDone, 10*time.Millisecond)

    senderRuntime := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
    if sendErr := stream.Send(senderRuntime, OutboundMessage{Payload: []byte("frame")}); false == errors.Is(sendErr, ErrStreamConnectionClosed) {
        t.Fatalf("expected ErrStreamConnectionClosed once the teardown began, got %v", sendErr)
    }
}

func TestStreamHandler_AFailureCloseThePeerNeverEchoesReturnsTheHandlerWithinTheGrace(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
    config := demoStreamHandlerConfig(func(_ containercontract.Runtime, _ StreamConnection, _ InboundMessage) error {
        return errors.New("callback refused the frame")
    })
    config.CloseHandshakeGrace = bound.Of(50 * time.Millisecond)
    handler := NewStreamHandler(hub, config)

    handlerReturned := make(chan time.Time, 1)
    server := httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, request *nethttp.Request) {
        runtimeInstance := runtimeWithLogger(request.Context(), &messageCapturingLogger{})
        handler(runtimeInstance, writer, melodyhttp.NewRequest(request, runtimeInstance, melodyhttp.RequestConfig{}))
        handlerReturned <- time.Now()
    }))
    t.Cleanup(server.Close)

    connection, ctx := dialStream(t, "ws"+strings.TrimPrefix(server.URL, "http"))
    awaitSubscriberCount(t, hub, 1)

    sentAt := time.Now()
    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("frame")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    select {
    case returnedAt := <-handlerReturned:
        if 2*time.Second < returnedAt.Sub(sentAt) {
            t.Fatalf("expected the handler back within the close grace, it took %s", returnedAt.Sub(sentAt))
        }
    case <-time.After(10 * time.Second):
        t.Fatal("the handler never returned")
    }
}

func TestStreamHandler_AFailureDuringABroadcastFrameStillClosesWithStatus1011(t *testing.T) {
    testCases := map[string]func() error{
        "error": func() error {
            return errors.New("callback refused the frame")
        },
        "panic": func() error {
            panic("callback boom")
        },
    }

    for name, fail := range testCases {
        t.Run(name, func(t *testing.T) {
            hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})
            kept := make(chan *streamConnection, 1)

            wsUrl := serveStreamHandlerWithLogger(t, hub, demoStreamHandlerConfig(func(_ containercontract.Runtime, connection StreamConnection, _ InboundMessage) error {
                stream := connection.(*streamConnection)
                kept <- stream

                deadline := time.Now().Add(5 * time.Second)
                for false == stream.liveness.writeInFlight() {
                    if true == time.Now().After(deadline) {
                        return nil
                    }

                    goruntime.Gosched()
                }

                return fail()
            }), &messageCapturingLogger{})

            connection, ctx := dialStream(t, wsUrl)
            connection.SetReadLimit(-1)
            awaitSubscriberCount(t, hub, 1)

            if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("frame")); nil != writeErr {
                t.Fatalf("write: %v", writeErr)
            }

            stream := <-kept
            largePayload := strings.Repeat("a", 32<<20)
            hub.Broadcast("demo", melodyhttp.ServerSentEvent{Data: largePayload})

            deadline := time.Now().Add(5 * time.Second)
            for false == stream.isClosed() {
                if true == time.Now().After(deadline) {
                    t.Fatal("the callback never failed under the in-flight broadcast frame")
                }

                goruntime.Gosched()
            }

            _, payload, readErr := connection.Read(ctx)
            if nil != readErr || len(largePayload) != len(payload) {
                t.Fatalf("expected the broadcast frame whole, got %d bytes and %v", len(payload), readErr)
            }

            _, _, readErr = connection.Read(ctx)
            if coderwebsocket.StatusInternalError != coderwebsocket.CloseStatus(readErr) {
                t.Fatalf("expected the peer to read close status 1011 after the frame, got %v", readErr)
            }
        })
    }
}

func TestStreamConnection_ASendTheCloseMeetsMidWriteAnswersTheClosedSentinel(t *testing.T) {
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

    dialStream(t, "ws"+strings.TrimPrefix(server.URL, "http"))

    liveness := newConnectionLiveness()
    serverConnection := <-serverConnections
    stream := newStreamConnection(serverConnection, context.Background(), 10*time.Second, liveness)

    senderRuntime := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
    sendDone := make(chan error, 1)
    go func() {
        sendDone <- stream.Send(senderRuntime, OutboundMessage{Payload: []byte(strings.Repeat("a", 32<<20))})
    }()

    deadline := time.Now().Add(5 * time.Second)
    for false == liveness.writeInFlight() {
        if true == time.Now().After(deadline) {
            t.Fatal("the send never reached the write")
        }

        goruntime.Gosched()
    }

    stream.closed.Store(true)
    _ = serverConnection.CloseNow()

    select {
    case sendErr := <-sendDone:
        if false == errors.Is(sendErr, ErrStreamConnectionClosed) {
            t.Fatalf("expected ErrStreamConnectionClosed for a send the close met mid-write, got %v", sendErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("the send never returned")
    }
}

type doneSignalContext struct {
    context.Context
    armed  atomic.Bool
    once   sync.Once
    called chan struct{}
}

func (instance *doneSignalContext) Done() <-chan struct{} {
    if true == instance.armed.Load() {
        instance.once.Do(func() {
            close(instance.called)
        })
    }

    return instance.Context.Done()
}

func newDoneSignalContext(parent context.Context) *doneSignalContext {
    return &doneSignalContext{Context: parent, called: make(chan struct{})}
}

func awaitSend(t *testing.T, sendDone <-chan error) error {
    t.Helper()

    select {
    case sendErr := <-sendDone:
        return sendErr
    case <-time.After(2 * time.Second):
        t.Fatal("the send did not return within 2s")
    }

    return nil
}

func TestStreamConnection_TheRuntimeContextBoundsTheWaitForTheWriteLock(t *testing.T) {
    stream := newStreamConnection(nil, context.Background(), 10*time.Second, newConnectionLiveness())
    stream.writeLock <- struct{}{}

    runtimeContext, runtimeCancel := context.WithCancel(context.Background())
    runtimeCancel()
    senderRuntime := runtime.NewRequest(runtimeContext, container.NewContainer(container.ContainerConfig{}).NewScope())

    sendDone := make(chan error, 1)
    go func() {
        sendDone <- stream.Send(senderRuntime, OutboundMessage{Payload: []byte("frame")})
    }()

    if sendErr := awaitSend(t, sendDone); false == errors.Is(sendErr, context.Canceled) {
        t.Fatalf("expected the runtime's cancellation, got %v", sendErr)
    }
}

func TestStreamConnection_AClosingConnectionReleasesASendWaitingForTheWriteLock(t *testing.T) {
    connectionContext, connectionCancel := context.WithCancel(context.Background())
    defer connectionCancel()

    stream := newStreamConnection(nil, connectionContext, 10*time.Second, newConnectionLiveness())
    stream.writeLock <- struct{}{}

    runtimeContext := newDoneSignalContext(context.Background())
    senderRuntime := runtime.NewRequest(runtimeContext, container.NewContainer(container.ContainerConfig{}).NewScope())
    runtimeContext.armed.Store(true)

    sendDone := make(chan error, 1)
    go func() {
        sendDone <- stream.Send(senderRuntime, OutboundMessage{Payload: []byte("frame")})
    }()

    <-runtimeContext.called
    connectionCancel()

    if sendErr := awaitSend(t, sendDone); false == errors.Is(sendErr, ErrStreamConnectionClosed) {
        t.Fatalf("expected ErrStreamConnectionClosed once the connection closed under the wait, got %v", sendErr)
    }
}

func TestStreamConnection_AStreamMarkedClosedRefusesASendBeforeItWaitsForTheWriteLock(t *testing.T) {
    stream := newStreamConnection(nil, context.Background(), 50*time.Millisecond, newConnectionLiveness())
    stream.writeLock <- struct{}{}
    stream.closed.Store(true)

    senderRuntime := runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
    if sendErr := stream.Send(senderRuntime, OutboundMessage{Payload: []byte("frame")}); false == errors.Is(sendErr, ErrStreamConnectionClosed) {
        t.Fatalf("expected ErrStreamConnectionClosed before the wait for the lock, got %v", sendErr)
    }
}

func TestStreamConnection_AStreamClosedWhileASendWaitedForTheLockRefusesItBeforeTheWrite(t *testing.T) {
    stream := newStreamConnection(nil, context.Background(), 10*time.Second, newConnectionLiveness())
    stream.writeLock <- struct{}{}

    runtimeContext := newDoneSignalContext(context.Background())
    senderRuntime := runtime.NewRequest(runtimeContext, container.NewContainer(container.ContainerConfig{}).NewScope())
    runtimeContext.armed.Store(true)

    sendDone := make(chan error, 1)
    go func() {
        defer func() {
            recovered := recover()
            if nil != recovered {
                sendDone <- fmt.Errorf("the send reached the write: %v", recovered)
            }
        }()

        sendDone <- stream.Send(senderRuntime, OutboundMessage{Payload: []byte("frame")})
    }()

    <-runtimeContext.called
    stream.closed.Store(true)
    <-stream.writeLock

    if sendErr := awaitSend(t, sendDone); false == errors.Is(sendErr, ErrStreamConnectionClosed) {
        t.Fatalf("expected ErrStreamConnectionClosed after the lock, before the write, got %v", sendErr)
    }
}
