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
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

/* runWebsocketCheck stands up the websocket integration handler on an in-process http server, connects
two real websocket clients, and broadcasts one event through the hub — proving the upgrade handshake and
the server→clients fan-out that only shows up over a live socket. It then hands over to the keepalive half,
which proves the handler's liveness verdict on two sockets that differ only in whether the client pongs. This
section needs no external backend, so it always runs. */
func runWebsocketCheck() {
    hub := melodyhttp.NewServerSentEventHub()
    defer hub.Shutdown()

    /* IdleTimeout is required; this half of the check is about the fan-out, so it is set far past the
    check's own lifetime and no ping ever fires */
    streamHandler := melodywebsocket.NewStreamHandler(hub, melodywebsocket.Options{
        OriginPatterns: []string{"*"},
        IdleTimeout:    30 * time.Second,
    })

    server := serveWebsocketHandler(streamHandler)
    defer server.Close()

    websocketUrl := websocketUrlOf(server)

    dialCtx, dialCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer dialCancel()

    first, _, firstDialErr := coderwebsocket.Dial(dialCtx, websocketUrl, nil)
    if nil != firstDialErr {
        fail("websocket: dial first client: %v", firstDialErr)
    }
    defer first.Close(coderwebsocket.StatusNormalClosure, "")

    second, _, secondDialErr := coderwebsocket.Dial(dialCtx, websocketUrl, nil)
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

    runWebsocketKeepaliveCheck()
}

/* websocketKeepaliveInterval is the handler's IdleTimeout for the keepalive half. The handler pings every
interval, bounds each ping by one interval, and treats a timed-out ping as death only once the peer has been
silent for two of them, so the whole liveness verdict is reached in a few hundred milliseconds. */
const websocketKeepaliveInterval = 500 * time.Millisecond

/* websocketKeepaliveIdleSpan is how long the answering client is left idle: well past the two intervals that
condemn a silent peer, so a connection that survives it survives because its pongs were seen, not because the
reaper had not run yet. */
const websocketKeepaliveIdleSpan = 6 * websocketKeepaliveInterval

/* runWebsocketKeepaliveCheck exercises the keepalive half of the handler over two live sockets that differ in
one respect only: whether the client answers the server's pings.

coder/websocket processes a ping exclusively inside a read, so a client that keeps reading pongs back on its
own and a client that never reads never pongs at all — which is exactly what a half-open peer looks like from
the server: a socket whose writes still succeed and whose reads never arrive. The two properties asserted are
that answering keeps a connection alive indefinitely, and that falling silent ends it within a bounded time.
Each connection is served on its own topic so the hub's subscriber count reports one and not the other. */
func runWebsocketKeepaliveCheck() {
    hub := melodyhttp.NewServerSentEventHub()
    defer hub.Shutdown()

    streamHandler := melodywebsocket.NewStreamHandler(hub, melodywebsocket.Options{
        OriginPatterns: []string{"*"},
        IdleTimeout:    websocketKeepaliveInterval,
        TopicResolver: func(request httpcontract.Request) string {
            return request.HttpRequest().URL.Query().Get("topic")
        },
    })

    server := serveWebsocketHandler(streamHandler)
    defer server.Close()

    websocketUrl := websocketUrlOf(server)

    dialCtx, dialCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer dialCancel()

    answering, _, answeringDialErr := coderwebsocket.Dial(dialCtx, websocketUrl+"?topic=answering", nil)
    if nil != answeringDialErr {
        fail("websocket keepalive: dial the answering client: %v", answeringDialErr)
    }
    defer answering.Close(coderwebsocket.StatusNormalClosure, "")

    /* the read is what answers the pings; the channel also proves the socket still carries data at the end */
    answered := make(chan string, 4)
    go func() {
        for {
            _, payload, readErr := answering.Read(context.Background())
            if nil != readErr {
                close(answered)

                return
            }

            answered <- string(payload)
        }
    }()

    silent, _, silentDialErr := coderwebsocket.Dial(dialCtx, websocketUrl+"?topic=silent", nil)
    if nil != silentDialErr {
        fail("websocket keepalive: dial the silent client: %v", silentDialErr)
    }
    defer silent.CloseNow()

    waitForSubscriber(hub, "answering")
    waitForSubscriber(hub, "silent")

    idleSince := time.Now()

    /* the silent peer must be reaped: nothing but the ping loop can notice it, since a broadcast stream never
       expects a client frame and a write into a half-open socket succeeds while the send buffer has room */
    disconnectDeadline := idleSince.Add(10 * websocketKeepaliveInterval)
    for 0 < hub.SubscriberCount("silent") {
        if time.Now().After(disconnectDeadline) {
            fail(
                "websocket keepalive: the client that stopped answering pings was still subscribed %s after it fell silent — half-open detection did not fire",
                time.Since(idleSince).Round(time.Millisecond),
            )
        }

        time.Sleep(10 * time.Millisecond)
    }
    pass(
        "websocket keepalive disconnected the client that stopped answering pings after %s (idle interval %s)",
        time.Since(idleSince).Round(time.Millisecond),
        websocketKeepaliveInterval,
    )

    for time.Since(idleSince) < websocketKeepaliveIdleSpan {
        time.Sleep(10 * time.Millisecond)
    }

    if 1 != hub.SubscriberCount("answering") {
        fail(
            "websocket keepalive: the client that answers pings was dropped within %s of idling — a healthy peer was reaped",
            websocketKeepaliveIdleSpan,
        )
    }

    payload := "still here after idling"
    if 1 != hub.Broadcast("answering", melodyhttp.ServerSentEvent{Data: payload}) {
        fail("websocket keepalive: the broadcast to the answering client reached no subscriber")
    }

    select {
    case received, open := <-answered:
        if false == open {
            fail("websocket keepalive: the answering client's socket was closed instead of delivering the broadcast")
        }
        if payload != received {
            fail("websocket keepalive: the answering client got %q, wanted %q", received, payload)
        }
    case <-time.After(5 * time.Second):
        fail("websocket keepalive: the answering client never received the broadcast")
    }
    pass(
        "websocket keepalive kept the ping-answering client connected and live through %s of idling (%d intervals)",
        websocketKeepaliveIdleSpan,
        websocketKeepaliveIdleSpan/websocketKeepaliveInterval,
    )
}

/* serveWebsocketHandler adapts the melody handler (which upgrades the raw ResponseWriter) onto net/http so
httptest can serve it. */
func serveWebsocketHandler(streamHandler httpcontract.Handler) *httptest.Server {
    return httptest.NewServer(nethttp.HandlerFunc(func(writer nethttp.ResponseWriter, httpRequest *nethttp.Request) {
        runtimeInstance := newRuntime()

        request := melodyhttp.NewRequest(
            httpRequest,
            nil,
            runtimeInstance,
            melodyhttp.NewRequestContext("e2e", time.Now()),
        )

        _, _ = streamHandler(runtimeInstance, writer, request)
    }))
}

func websocketUrlOf(server *httptest.Server) string {
    return "ws" + strings.TrimPrefix(server.URL, "http")
}

func waitForSubscriber(hub *melodyhttp.ServerSentEventHub, topic string) {
    deadline := time.Now().Add(5 * time.Second)
    for 1 > hub.SubscriberCount(topic) {
        if time.Now().After(deadline) {
            fail("websocket keepalive: the %q client did not subscribe before the deadline", topic)
        }

        time.Sleep(10 * time.Millisecond)
    }
}
