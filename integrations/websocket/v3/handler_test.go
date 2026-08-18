package websocket

import (
    "context"
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
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
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

    /* broadcast only after several ping intervals have elapsed; the client's blocking Read below auto-answers the server pings in the meantime, so a healthy receive-only subscriber must stay connected through the keepalive loop */
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

    /* the client never reads, so it never answers the server's keepalive pings (coder/websocket only replies to pings while the application reads); the server must time out the ping and tear the half-open connection down, dropping the subscription */
    disconnectDeadline := time.Now().Add(3 * time.Second)
    for hub.SubscriberCount("demo") > 0 {
        if true == time.Now().After(disconnectDeadline) {
            t.Fatalf("expected the keepalive loop to disconnect an unresponsive client and drop its subscription")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

/* the accept-time activity mark is offset zero and only a data message advances it, so a receive-only client bridged onto a broadcast hub — the handler's primary case — leaves the activity window dead for exactly the connections it is meant to protect unless a received pong records liveness. */
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

    serviceContainer := container.NewContainer()
    serverRuntime := runtime.New(loopContext, serviceContainer.NewScope(), serviceContainer)

    go readLoop(loopContext, loopCancel, serverConnection, serverRuntime, Options{}, liveness)
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

/* a small kernel send buffer keeps the server parked inside connection.Write for as long as the client drains slowly, so the scenario reproduces without pushing megabytes. */
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

/* a keepalive ping is serialised behind the data frame the handler is flushing, so a ping issued while a slow client drains that frame never reaches the socket: its timeout tested nothing and must not be read as a dead peer. Any IdleTimeout below WriteTimeout would otherwise turn transient write contention into a disconnect mid-frame. */
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

/* a write that COMPLETED is no evidence the peer is alive: a write into a half-open connection succeeds for as long as the socket send buffer has room. A hub that keeps broadcasting must therefore not keep a peer that never pongs alive, or the descriptor, the hub subscription and the goroutines survive until the send buffer fills — minutes at a modest broadcast rate — while the ping loop that exists to detect exactly this excuses every timeout. */
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

    /* a small event every third of an interval, as a hub bridging a busy topic would: every write completes into the send buffer while the peer answers nothing */
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

/* @info A pong is processed only inside connection.Read, and the read loop is not inside Read while it runs a synchronous OnMessage callback. A ping issued in that window always times out, so treating that timeout as death disconnects perfectly healthy clients whenever a callback outlives the ping interval. */
func TestStreamHandler_SlowOnMessageDoesNotDisconnectHealthyClient(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    callbackEntered := make(chan struct{})

    handler := NewStreamHandler(hub, Options{
        TopicResolver:  func(request httpcontract.Request) string { return "demo" },
        OriginPatterns: []string{"*"},
        IdleTimeout:    100 * time.Millisecond,
        OnMessage: func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte) {
            close(callbackEntered)

            /* outlive several ping intervals while the read loop cannot answer pongs */
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

    /* the callback is still running; once it returns, a broadcast must still reach this client */
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

/* @info A callback that never returns must not excuse pings forever: nothing else reaps a hijacked connection, so the descriptor, the hub subscription and the handler/read/ping goroutines would leak once per connection for the process lifetime. */
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

    /* the grace is ten intervals; give it that plus slack, then the subscription must be gone */
    reapDeadline := time.Now().Add(3 * time.Second)
    for 0 < hub.SubscriberCount("demo") {
        if true == time.Now().After(reapDeadline) {
            t.Fatalf("a connection wedged in OnMessage was never reaped: the hub subscription is still registered")
        }
        time.Sleep(5 * time.Millisecond)
    }
}

/* @info A synchronous OnMessage callback holds the scope-backed runtime handed to it. If the handler returns to the kernel while the callback is still running, the kernel's deferred scope teardown races the callback and its next service resolution hits a closed scope. The handler must wait for the read loop — hence the callback — before returning. */
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

            /* keep processing briefly, then resolve a service through the scope-backed
               runtime; a healthy callback must never observe a closed scope */
            time.Sleep(200 * time.Millisecond)

            _, getErr := runtimeInstance.Scope().Get("service")
            if nil != getErr && true == strings.Contains(getErr.Error(), "scope is closed") {
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
        /* mimic the kernel's deferred scope.Close() the instant the handler returns */
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

    /* keep answering control frames so an unfixed graceful close completes fast and the
       handler returns (and closes the scope) well before the callback resolves */
    go func() {
        _, _, _ = connection.Read(ctx)
    }()

    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("work")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    <-callbackEntered

    /* end the connection from the server side while the callback is still running */
    hub.Shutdown()

    outcome := <-resolveOutcome
    if "alive" != outcome {
        t.Fatalf("an in-flight OnMessage observed a closed scope (%s): the handler returned to the kernel without waiting for the read loop", outcome)
    }
}

/* @info A connection reaped while wedged in a callback must free its descriptor and handler goroutine when the close grace lapses, not seconds later. An abandoned graceful close otherwise wins coder/websocket's close CAS and holds the transport for the library's full handshake timeout, while the deferred CloseNow loses that CAS and blocks the same span. */
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

    /* the client never reads again, so it never answers a close handshake: an unfixed
       graceful close can only end at coder/websocket's five-second timeout */
    if writeErr := connection.Write(ctx, coderwebsocket.MessageText, []byte("wedge")); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    <-callbackEntered

    /* reap the wedged connection from the server side */
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

/* @info leaveCallback must refresh the activity mark before it clears the running-callback count. If it clears the count first, a ping loop sampling cannotAnswer in that window sees no callback running yet still reads the stale pre-callback activity mark, and reaps a healthy connection at the instant its callback returns. */
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

    /* observer mirrors the non-callback branch of cannotAnswer: with no callback
       running the activity mark must already be fresh, or the connection is reaped.
       A stale mark seen at callbacks==0 is only reported when it is a STABLE state
       (callbacks still zero and the same mark on re-read), so a torn read that
       straddles the next iteration's freshly-entered callback cannot masquerade as
       the pre-decrement window the reorder closes. */
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

/* @info The liveness windows must be measured against a monotonic base captured at accept time, not the wall clock. A wall-clock timestamp (time.Since(time.Unix(0, n))) lets a backward clock step excuse a wedged callback past the grace and leak the connection, or a forward step reap a healthy one. */
func TestConnectionLiveness_GraceUsesMonotonicBase(t *testing.T) {
    liveness := newConnectionLiveness()

    /* a time.Time carrying a monotonic reading differs from its own wall-clock
       rounding; one stripped of it (as time.Unix(0, n) yields) does not */
    if liveness.base == liveness.base.Round(0) {
        t.Fatalf("connectionLiveness base carries no monotonic reading; the grace windows would follow the wall clock")
    }

    window := 40 * time.Millisecond
    grace := 200 * time.Millisecond

    liveness.enterCallback()

    if false == liveness.cannotAnswer(false, window, grace, grace) {
        t.Fatalf("a callback within the grace must be excused")
    }

    /* a callback whose start is older than the grace, measured from the monotonic
       base, must no longer be excused */
    liveness.callbackStartedOffset.Store(int64(liveness.elapsed()) - int64(2*grace))
    if true == liveness.cannotAnswer(false, window, grace, grace) {
        t.Fatalf("a callback that outran the grace must no longer be excused")
    }
}

/* a ping cannot even be written while the handler is flushing a data frame, so the timeout of a ping issued in that span is not evidence of a dead peer; a ping issued with the writer free tested the peer for real, and a write that completed in the meantime excuses nothing. */
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

    /* a write into a half-open connection completes for as long as the send buffer has room, so once it is done it is no evidence at all: the next ping goes out with the control-frame queue free and its timeout must be read as a death */
    liveness.writeStartedOffset.Store(int64(liveness.elapsed()))

    if true == liveness.cannotAnswer(false, window, callbackGrace, graceForWrite) {
        t.Fatalf("a write that completed before the ping was issued must not excuse the ping's timeout")
    }
}

/* a zero IdleTimeout leaves the connection with no reaper at all: Accept hijacks it out of http.Server's timeouts, the read loop blocks with no deadline and a write into a half-open socket still succeeds, so a peer that vanishes without a fin holds its descriptor, its hub subscription and its goroutines for the life of the process. Construction must refuse it, and say why. */
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

/* a positive IdleTimeout is accepted and yields a usable handler. */
func TestNewStreamHandler_AcceptsAPositiveIdleTimeout(t *testing.T) {
    if nil == NewStreamHandler(melodyhttp.NewServerSentEventHub(), Options{IdleTimeout: time.Second}) {
        t.Fatal("expected a handler for a positive IdleTimeout")
    }
}
