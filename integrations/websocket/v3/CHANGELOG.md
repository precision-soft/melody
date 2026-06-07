# Changelog

All notable changes to `precision-soft/melody/integrations/websocket` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.0.0] - 2026-06-07 - Initial Release — WebSocket Streaming Bridging the Server-Sent Events Hub

### Added

- Initial Melody v3 binding of the WebSocket integration — bidirectional streaming on `coder/websocket`, bridging the core `http.ServerSentEventHub`. Developed v3-first; v1 and v2 bindings to follow.
- `handler.go` — `NewStreamHandler(hub, Options)`: upgrades the connection, subscribes to a resolved topic, writes broadcast `ServerSentEvent` data (text frames by default, binary when `Options.BinaryWrites` is set), reads inbound frames into an optional `OnMessage(runtime, coderwebsocket.MessageType, payload)` callback (the message type lets the callback distinguish text from binary), and returns `(nil, nil)` on disconnect. `Options` carries `TopicResolver`, `OnMessage`, `SubscribeBuffer`, `WriteTimeout`, `OriginPatterns`, and `BinaryWrites`.
- `handler.go` — `Options.ReadLimit` caps the byte size of a single inbound message (0 keeps coder/websocket's 32 KiB default); `Options.OnMessage` is documented as running on the connection's read goroutine, in order, and required to be non-blocking (a slow callback stalls the read loop and delays close/ping detection).
- `handler_test.go` — in-process E2E (httptest server + `websocket.Dial` + `hub.Broadcast`); no external service required. The subscriber-registration wait now polls with a yield + 2s deadline instead of a tight busy-loop that could starve the server goroutine on a constrained host (the cause of an intermittent "broadcast reached 0 subscribers" failure).

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/websocket/v3.0.0...HEAD

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/websocket/v3.0.0
