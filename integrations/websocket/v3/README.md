# Melody WebSocket integration (v3)

Bidirectional WebSocket streaming for Melody, built on [`coder/websocket`](https://github.com/coder/websocket). It bridges the core [`http.ServerSentEventHub`](https://github.com/precision-soft/melody) so the same topic-keyed fan-out powers both Server-Sent Events and WebSockets.

## Version lines

This integration is v3-only (`github.com/precision-soft/melody/integrations/websocket/v3`); no v1 or v2 bindings are currently planned.

## Installation

```sh
go get github.com/precision-soft/melody/integrations/websocket/v3
```

```go
import melodywebsocket "github.com/precision-soft/melody/integrations/websocket/v3"
```

## Usage

```go
hub := melodyhttp.NewServerSentEventHub()

handler := melodywebsocket.NewStreamHandler(hub, melodywebsocket.Options{
	TopicResolver: func(request httpcontract.Request) string {
		return request.Header("X-Device-Id")
	},
	OnMessage: func(runtimeInstance runtimecontract.Runtime, messageType coderwebsocket.MessageType, payload []byte) {
		// handle inbound client messages (messageType distinguishes text from binary)
	},
})

// register handler on a route, e.g. GET /ws
// broadcast from anywhere (e.g. a message handler):
hub.Broadcast("device-42", melodyhttp.ServerSentEvent{Event: "task.cancelled", Data: payloadJson})
```

The handler upgrades the connection, subscribes to the resolved topic, writes each broadcast `ServerSentEvent`'s data to the socket, and reads inbound frames (dispatched to `OnMessage`). It returns `(nil, nil)` once the client disconnects, so the kernel writes nothing further.

### Register as a module

Bundle the stream route as a self-registering application module — one `RegisterModule` call registers the route on the configured hub (skipped when no hub or path is configured):

```go
app.RegisterModule(melodywebsocket.NewModule(melodywebsocket.ModuleConfig{
    Hub:     hub,
    Path:    "/ws",
    Options: melodywebsocket.Options{OriginPatterns: []string{"*"}},
}))
```

## Footguns & caveats

- The hub is shared with Server-Sent Events: a single `hub.Broadcast(topic, event)` reaches both Server-Sent Events and WebSocket subscribers of that topic.
- Only the event `Data` is written to the socket — the `Event`/`Id`/`Retry` fields of a `ServerSentEvent` are not. Encode structured payloads (for example JSON) into `Data` before broadcasting.
- Outbound frames are **text** by default; set `Options.BinaryWrites = true` to write `websocket.MessageBinary` instead ([`writeMessageType`](./handler.go)). It is an all-or-nothing switch for the whole connection — pick binary when `Data` carries a non-UTF-8 payload (Protobuf, MessagePack, raw bytes), since a text frame's payload must be valid UTF-8. It does not affect `OnMessage`, whose `messageType` argument reports whatever the client sent.
- `OriginPatterns` is passed to `websocket.Accept`; set it for browser clients on other origins.
- `Options.ReadLimit` caps a single inbound message's byte size (0 keeps coder/websocket's 32 KiB default); raise it only if you expect larger frames.
- `Options.IdleTimeout` is **required and must be positive** — `NewStreamHandler` panics on a zero, and so does a module wired with one. It is the keepalive ping interval, and the ping is the only thing that can reap a peer which vanished without a FIN: `websocket.Accept` hijacks the connection out of `http.Server`'s read/write timeouts, the read loop blocks with no deadline of its own, and a write into a half-open socket keeps succeeding while the send buffer has room. `30 * time.Second` suits a browser client — RFC 6455 obliges a client to answer a ping, and browsers answer inside the protocol stack where page JavaScript never sees it, so a receive-only subscriber stays connected. Every interval the handler pings and closes the connection if the peer has answered nothing for two intervals, which is the only way a half-open client is detected on a receive-only stream. A frame in flight excuses the one ping it queues behind itself, up to `Options.WriteTimeout` plus one interval, after which the write's own failure closes the connection.
- `OnMessage` runs on the connection's read goroutine, in order, and **must not block** — a slow callback stalls the read loop and delays close/ping detection. Hand long work to your own queue/worker and return promptly.
- The integration test is in-process (httptest server + `websocket.Dial`); no external service is required.
