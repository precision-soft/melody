package websocket

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
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
