package main

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "time"

    coderwebsocket "github.com/coder/websocket"
    melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

/* runWebsocketCheck stands up the websocket integration handler on an in-process http server, connects
two real websocket clients, and broadcasts one event through the hub — proving the upgrade handshake and
the server→clients fan-out that only shows up over a live socket. This section needs no external backend,
so it always runs. */
func runWebsocketCheck() {
    hub := melodyhttp.NewServerSentEventHub()
    defer hub.Shutdown()

    streamHandler := melodywebsocket.NewStreamHandler(hub, melodywebsocket.Options{
        OriginPatterns: []string{"*"},
    })

    /* adapt the melody handler (which upgrades the raw ResponseWriter) onto net/http for httptest */
    adapter := nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, httpRequest *nethttp.Request) {
        runtimeInstance := newRuntime()

        request := melodyhttp.NewRequest(
            httpRequest,
            nil,
            runtimeInstance,
            melodyhttp.NewRequestContext("e2e", time.Now()),
        )

        _, _ = streamHandler(runtimeInstance, writer, request)
    })

    server := httptest.NewServer(adapter)
    defer server.Close()

    wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

    dialCtx, dialCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer dialCancel()

    first, _, firstDialErr := coderwebsocket.Dial(dialCtx, wsURL, nil)
    if nil != firstDialErr {
        fail("websocket: dial first client: %v", firstDialErr)
    }
    defer first.Close(coderwebsocket.StatusNormalClosure, "")

    second, _, secondDialErr := coderwebsocket.Dial(dialCtx, wsURL, nil)
    if nil != secondDialErr {
        fail("websocket: dial second client: %v", secondDialErr)
    }
    defer second.Close(coderwebsocket.StatusNormalClosure, "")

    pass("websocket handshake accepted for two clients")

    /* with no TopicResolver every connection subscribes to the "default" topic; wait until both
       server-side subscriptions are registered so the broadcast cannot race ahead of them */
    subscribeDeadline := time.Now().Add(5 * time.Second)
    for 2 > hub.SubscriberCount("default") {
        if time.Now().After(subscribeDeadline) {
            fail("websocket: only %d of 2 clients subscribed before the deadline", hub.SubscriberCount("default"))
        }

        time.Sleep(10 * time.Millisecond)
    }

    payload := "broadcast to every socket"
    delivered := hub.Broadcast("default", melodyhttp.ServerSentEvent{Data: payload})
    if 2 != delivered {
        fail("websocket: broadcast reached %d subscribers, wanted 2", delivered)
    }

    for index, client := range []*coderwebsocket.Conn{first, second} {
        readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)

        _, received, readErr := client.Read(readCtx)
        readCancel()

        if nil != readErr {
            fail("websocket: client %d read: %v", index+1, readErr)
        }
        if payload != string(received) {
            fail("websocket: client %d got %q, wanted %q", index+1, received, payload)
        }
    }

    pass("websocket broadcast delivered to both clients over the live socket")
}
