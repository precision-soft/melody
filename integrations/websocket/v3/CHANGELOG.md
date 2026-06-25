# Changelog

All notable changes to `precision-soft/melody/integrations/websocket` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.1.0] - 2026-06-25 - Idle-Timeout Ping Keepalive

### Added

- `handler.go` — `Options.IdleTimeout` (opt-in; zero keeps the previous behavior) enables a websocket keepalive: when set, the handler sends a ping every `IdleTimeout` and closes the connection if the peer does not pong within that window, so an idle or half-open client cannot pin a goroutine and connection indefinitely — a hijacked websocket connection is not covered by `http.Server` read timeouts, so a slow/silent client was otherwise a resource-exhaustion vector. A healthy receive-only subscriber stays connected because its read loop answers the pings; an unresponsive peer is detected and disconnected. Covered by broker-free in-process E2E tests in `handler_test.go`.

## [v3.0.0] - 2026-06-16 - Initial Release — WebSocket Streaming Bridging the Server-Sent Events Hub

### Added

- Initial Melody v3 binding of the WebSocket integration — bidirectional streaming on `coder/websocket`, bridging the core `http.ServerSentEventHub`. Developed v3-first; v1 and v2 bindings to follow.
- `module.go` — `NewModule(ModuleConfig{Hub, Options, Path, RouteName})` self-registering application module that registers the WebSocket stream route on the configured server-sent-event hub via one `app.RegisterModule(...)` (skipped when no hub or path is configured).
- `handler.go` — `NewStreamHandler(hub, Options)`: upgrades the connection, subscribes to a resolved topic, writes broadcast `ServerSentEvent` data (text frames by default, binary when `Options.BinaryWrites` is set), reads inbound frames into an optional `OnMessage(runtime, coderwebsocket.MessageType, payload)` callback (the message type lets the callback distinguish text from binary), and returns `(nil, nil)` on disconnect. `Options` carries `TopicResolver`, `OnMessage`, `SubscribeBuffer`, `WriteTimeout`, `OriginPatterns`, and `BinaryWrites`.
- `handler.go` — `Options.ReadLimit` caps the byte size of a single inbound message (0 keeps coder/websocket's 32 KiB default); `Options.OnMessage` is documented as running on the connection's read goroutine, in order, and required to be non-blocking (a slow callback stalls the read loop and delays close/ping detection).
- `handler_test.go` — in-process E2E (httptest server + `websocket.Dial` + `hub.Broadcast`); no external service required. The subscriber-registration wait now polls with a yield + 2s deadline instead of a tight busy-loop that could starve the server goroutine on a constrained host (the cause of an intermittent "broadcast reached 0 subscribers" failure).

### Fixed

- `handler.go` — a panic in the user `OnMessage` callback no longer crashes the whole process. The callback runs on the connection's read goroutine, which is spawned outside the kernel's panic recovery, so a single malformed client frame that made `OnMessage` panic took the server down. The callback is now invoked through a recovering wrapper that logs the panic and closes the connection, matching how the kernel and event dispatcher recover user-code panics.
- `handler.go` — a server-initiated termination (hub shutdown, subscriber unsubscribe, context cancellation) now performs the WebSocket close handshake (`Close(StatusNormalClosure, …)`) instead of only tearing down the socket with `CloseNow`, so a spec-conformant client sees a normal `1000` closure rather than abnormal `1006` — avoiding reconnect storms during a graceful rolling deploy. `CloseNow` remains the deferred backstop.

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/websocket/v3.1.0...HEAD

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/websocket/v3.0.0...integrations/websocket/v3.1.0

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/websocket/v3.0.0
