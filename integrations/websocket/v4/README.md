# Melody WebSocket integration (v4)

Bidirectional WebSocket streaming for Melody, built on [`coder/websocket`](https://github.com/coder/websocket). It bridges the core [`http.ServerSentEventHub`](https://github.com/precision-soft/melody) so the same topic-keyed fan-out powers both Server-Sent Events and WebSockets.

## Version lines

This integration ships for the third and the fourth major (`github.com/precision-soft/melody/integrations/websocket/v4`); no v1 or v2 bindings are currently planned.

## Installation

```bash
go get github.com/precision-soft/melody/integrations/websocket/v4
```

```go
import melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v4"
```

## Usage

```go
hub := melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{})

handler := melodywebsocket.NewStreamHandler(hub, melodywebsocket.StreamHandlerConfig{
    TopicResolver: func(request httpcontract.Request) string {
        return request.Header("X-Device-Id")
    },
    OnMessage: func(runtimeInstance containercontract.Runtime, connection melodywebsocket.StreamConnection, message melodywebsocket.InboundMessage) error {
        /* message.Binary tells a binary frame from a text one; nil keeps the connection */
        sendErr := connection.Send(runtimeInstance, melodywebsocket.OutboundMessage{Payload: append([]byte("received: "), message.Payload...)})
        /* a client that left during the reply is ordinary churn, not a failure to journal at error */
        if true == errors.Is(sendErr, melodywebsocket.ErrStreamConnectionClosed) {
            return nil
        }

        return sendErr
    },
})

// register handler on a route, e.g. GET /ws
// broadcast from anywhere (e.g. a message handler):
hub.Broadcast(runtimeInstance, "device-42", melodyhttp.ServerSentEvent{Event: "task.cancelled", Data: payloadJson})
```

The handler upgrades the connection, subscribes to the resolved topic, writes each broadcast `ServerSentEvent`'s data to the socket, and reads inbound frames (dispatched to `OnMessage` as an `InboundMessage`, with the `StreamConnection` they arrived on). It returns `(nil, nil)` once the client disconnects, so the kernel writes nothing further.

### Register as a module

Bundle the stream routes as a self-registering application module — one `RegisterModule` call registers every declared stream, each on its own hub. `StreamConfig.Hub` is the hub's provider, called on every connection with the connection's runtime, so a hub its container provider builds at boot — after the routes registered — is reached through the runtime; its error is the request's, and a provider answering a nil hub refuses the request before the upgrade. The handler configuration is resolved once, at the route registration. An application registers the module once, since a module's name is its identity, so two endpoints are two entries in `ModuleConfig.Streams`, not two modules. Refused at boot, naming the field and the stream's index: a module with no stream, a stream with a nil hub provider or an empty path, and, on a module declaring more than one stream, an empty `RouteName`, since the default `melody.websocket` names a single route; a handler limit outside its rule is refused naming the `StreamHandlerConfig` field. An unregistered route has no later consumer to fail loudly, so the missing wiring would otherwise surface only as client 404s under a healthy-looking boot. Every stream is judged and its handler built before the first route registers, so a refused module leaves no route behind.

```go
app.RegisterModule(melodywebsocket.NewModule(melodywebsocket.ModuleConfig{
    Streams: []melodywebsocket.StreamConfig{
        {
            Hub: func(runtimeInstance containercontract.Runtime) (*melodyhttp.ServerSentEventHub, error) {
                return runtime.FromRuntime[*melodyhttp.ServerSentEventHub](runtimeInstance, serviceServerSentEventHub)
            },
            Path:    "/ws",
            Handler: melodywebsocket.StreamHandlerConfig{IdleTimeout: bound.Of(15 * time.Second), OriginPatterns: []string{"app.example.com"}},
        },
    },
}))
```

## Footguns & caveats

- The hub is shared with Server-Sent Events: a single `hub.Broadcast(runtimeInstance, topic, event)` reaches both Server-Sent Events and WebSocket subscribers of that topic.
- Only the event `Data` is written to the socket — the `Event`/`Id`/`Retry` fields of a `ServerSentEvent` are not. Encode structured payloads (for example JSON) into `Data` before broadcasting.
- Outbound frames are **text** by default; set `StreamHandlerConfig.BinaryWrites = true` to write `websocket.MessageBinary` instead ([`writeMessageType`](./handler.go)). It is an all-or-nothing switch for the whole connection — pick binary when `Data` carries a non-UTF-8 payload (Protobuf, MessagePack, raw bytes), since a text frame's payload must be valid UTF-8. It does not affect `OnMessage`, whose `InboundMessage.Binary` reports whatever the client sent, nor `Send`, whose `OutboundMessage.Binary` chooses the frame type of each reply.
- `OriginPatterns` is passed to `websocket.Accept`: left empty, only a page of the same origin may open the socket; a pattern admits the hosts it matches, and `*` admits every origin, so any page a visitor opens can ride the visitor's session cookie into the socket.
- `StreamHandlerConfig.ReadLimit` caps a single inbound message's byte size: left out it is 32 KiB, written on every connection, and `bound.UnlimitedCount()` disables the limit entirely; raise it only if you expect larger frames.
- `TopicResolver` returning an empty topic **refuses the connection** before the upgrade: an empty topic is only ever an extraction that failed (the nil-resolver default is the distinct `"default"`), and subscribing such connections would pool every mis-resolved client on one shared topic.
- A connection against a hub that has already **shut down** is refused before the upgrade — the handler reads `ServerSentEventHub.IsClosed`. `Subscribe` on a shut-down hub hands back a subscriber whose channel is already closed, so upgrading would give the client a `101` and an instant, unexplained close; the refusal is recorded instead. Connect once the hub is running.
- `StreamHandlerConfig.IdleTimeout` is the keepalive ping interval: 30s when left out, at least 1s, unlimited refused — `NewStreamHandler` panics outside that rule naming the field, and so does a module wired with such a value. The ping is the only thing that can reap a peer which vanished without a FIN: `websocket.Accept` hijacks the connection out of `http.Server`'s read/write timeouts, the read loop blocks with no deadline of its own, and a write into a half-open socket keeps succeeding while the send buffer has room. The 30s default suits a browser client — RFC 6455 obliges a client to answer a ping, and browsers answer inside the protocol stack where page JavaScript never sees it, so a receive-only subscriber stays connected. Every interval the handler pings and closes the connection if the peer has answered nothing for two intervals, which is the only way a half-open client is detected on a receive-only stream. A frame in flight excuses the one ping it queues behind itself, up to
  `StreamHandlerConfig.WriteTimeout` plus one interval, after which the write's own failure closes the connection. `StreamHandlerConfig.WriteTimeout` is 10s when left out, at least 1ms, unlimited refused.
- On teardown (hub shutdown, write failure, context cancellation) the handler waits a short close grace, `StreamHandlerConfig.CloseHandshakeGrace`, 1s when left out, for an in-flight `OnMessage` to finish, and for the peer's echo of a 1011 close: a peer that never echoes it does not hold the handler past the grace, the library closing the transport itself within its own five seconds; a callback that crosses teardown and outruns that grace is abandoned — its next scope resolution panics, is recovered and logged, and the message is lost. The grace is deliberately much shorter than the generous window the keepalive reaper grants a running callback: one wedged callback must not stall hub shutdown and process exit for minutes.
- `OnMessage` answers an error: nil keeps the connection; a non-nil error is logged once at error (`websocket OnMessage failed`) and closes the connection with status 1011 (internal error), and a panicking callback is recovered, logged with its stack and closes with 1011 too. The 1011 close is sent once a broadcast frame being written has left, so the peer reads the frame whole and then the status. A callback that returns the error of a `Send` the client hung up on is journaled at error like any other failure: filter `ErrStreamConnectionClosed`, as the example above does, to keep normal client churn out of the error records.
- `StreamConnection.Send(runtimeInstance, OutboundMessage{Binary, Payload})` writes one frame to the connection: from `OnMessage`, or from any goroutine that kept the connection. Every data frame, the broadcast ones included, is written under one writer lock, so concurrent sends never interleave; the wait for the lock and the write together are bounded by `StreamHandlerConfig.WriteTimeout` and by the runtime's context, a nil runtime is refused, and once the connection is closing or closed `Send` answers `ErrStreamConnectionClosed`, joined with the library's error when the close meets the write. A runtime context that ends while the frame is being written makes `coder/websocket` close the whole connection, for the broadcasts and every other sender too, so a short per-call deadline on a kept connection is a kill switch; one that ends while `Send` waits for the writer lock costs nothing.
- `OnMessage` runs on the connection's read goroutine, in order, and **must not block** — a slow callback stalls the read loop and delays close/ping detection. Hand long work to your own queue/worker and return promptly.
- The integration test is in-process (httptest server + `websocket.Dial`); no external service is required.
