# Melody WebSocket integration (v3)

Bidirectional WebSocket streaming for Melody, built on [`coder/websocket`](https://github.com/coder/websocket). It bridges the core [`http.ServerSentEventHub`](https://github.com/precision-soft/melody) so the same topic-keyed fan-out powers both Server-Sent Events and WebSockets.

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
- Only the event `Data` is written to the socket (as a text frame). Encode structured payloads (for example JSON) into `Data` before broadcasting.
- `OriginPatterns` is passed to `websocket.Accept`; set it for browser clients on other origins.
- `Options.ReadLimit` caps a single inbound message's byte size (0 keeps coder/websocket's 32 KiB default); raise it only if you expect larger frames.
- `OnMessage` runs on the connection's read goroutine, in order, and **must not block** — a slow callback stalls the read loop and delays close/ping detection. Hand long work to your own queue/worker and return promptly.
- The integration test is in-process (httptest server + `websocket.Dial`); no external service is required.
