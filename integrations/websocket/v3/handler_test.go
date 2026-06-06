package websocket_test

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    coderwebsocket "github.com/coder/websocket"

    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
    "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/runtime"
)

func TestStreamHandler_BroadcastReachesClient(t *testing.T) {
    hub := melodyhttp.NewServerSentEventHub()

    handler := melodywebsocket.NewStreamHandler(hub, melodywebsocket.Options{
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
