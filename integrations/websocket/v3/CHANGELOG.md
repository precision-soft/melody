# Changelog

All notable changes to `precision-soft/melody/integrations/websocket` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Behavioural change**: `module.go` — a nil `Hub` or an empty `Path` is refused at boot instead of silently registering no route, where the endpoint did not exist, clients got 404 and every boot-time check read healthy, while the module already panics on a zero `IdleTimeout`; a module registered at all is a decision to serve the stream
- **Behavioural change**: `handler.go` — a connection against a hub that has already shut down is refused before the upgrade, reading the new `ServerSentEventHub.IsClosed`, instead of upgraded and closed instantly, where `Subscribe` on a shut-down hub handed back a closed channel the event loop read as an ordinary end of stream and the client got a `101` and an immediate close under a generic "read loop ended" debug line; a hub that shuts down during the connect window is named at debug before the connection closes
- **Behavioural change**: `handler.go` — a `TopicResolver` that returns an empty topic refuses the connection before the upgrade instead of subscribing it: an empty topic is only ever an extraction that failed, the nil-resolver default being the distinct `"default"`, and subscribing anyway pooled every mis-resolved client from every tenant on one shared `""` topic, a broadcast to `""` then delivered cross-tenant
- **Behavioural change**: an application passing a zero `IdleTimeout` now fails at construction with a diagnostic naming what to set: the ping loop is the only thing that can reap a peer that died silently, the read loop blocking with no deadline, the upgrade hijacking the connection out of the server's own timeouts, and a write into a half-open socket succeeding while buffer space remains, so abandoned connections accumulated without bound

### Fixed

- documentation: the refusal of a zero `IdleTimeout` carries the behavioural-change marker instead of the breaking one, since no compile notices it
- **Behavioural change**: handler: a configuration with `IdleTimeout` below `WriteTimeout` no longer turns transient write contention into a disconnect, a frame in flight excusing a timed-out ping until one interval past the configured write timeout, after which the write's own failure tears the connection down; a received pong refreshes the connection's liveness mark, where liveness advanced only when `Read` returned a data message and a receive-only client bridged onto a broadcast hub never advanced it, and a keepalive ping that could not be written is no longer read as a dead peer, since the library serialises control frames behind the frame in flight and a ping issued while flushing to a slow client timed out without reaching the socket; the excuse is scoped to the ping a frame actually blocked, sampled as the ping is issued, so a completed write excuses nothing and half-open detection survives a hub that keeps broadcasting
- `handler.go` — a connection refused before the upgrade answers 503 for a hub shutting down and 400 for an empty resolved topic, as the deliberate refusals they are, where it answered a 500 and an error record; the record of a panicking `OnMessage` carries `panicStack` and the panicked error whole, where flattening through `%v` kept only the message and lost the cause chain and context; the connection's logger is resolved at the upgrade, while the scope is alive, an error record with no logger going to the emergency logger, where a callback that outlived the close grace and then panicked resolved its logger against a closed request scope and the record was lost; `NewStreamHandler` refuses a nil hub at construction, where it panicked on the first connection attempt; a negative `ReadLimit` is passed through as coder/websocket's documented "no limit", where the positive-only guard discarded it and the library's 32 KiB default closed the connection with `1009` on the first oversized frame; and the read loop's terminating error is logged at debug level, where a peer killed for the read limit or a protocol violation vanished with no record

## [v3.1.2] - 2026-07-11 - Connection Reaping, Close Handshake, and Scope-Lifetime Hardening

### Fixed

- `handler.go` — the keepalive ping loop no longer disconnects healthy clients. A pong is processed only inside `connection.Read`, which the read loop leaves while it runs a synchronous `OnMessage` callback, so a ping issued in that window always timed out and was read as the peer's death. A timed-out ping now counts as death only when the read loop is neither inside a callback nor has seen a client frame within two intervals; a write failure still fails immediately, since the socket itself is gone.
- `handler.go` — a callback that never returns no longer holds its connection open forever. The excuse the ping loop grants a running `OnMessage` was unbounded, so a wedged callback answered every ping timeout with "the reader simply cannot answer": nothing else reaps a hijacked connection, and the descriptor, the hub subscription and the handler, read and ping goroutines leaked once per connection. The excuse now expires after ten ping intervals.
- `handler.go` — the closing handshake is bounded. `connection.Close` waits for the peer's close frame, which only the read loop ever reads, so closing a connection *because* that read loop is wedged waited out the library's five-second timeout while still holding the handler goroutine and the hub subscription. The handshake now gets one second before the deferred `CloseNow` frees the socket.
- `handler.go` — a still-running `OnMessage` callback no longer races the request-scope teardown. The handler waits, bounded by the close grace, for the read loop to exit before it returns, so a healthy in-flight callback cannot resolve a service against a scope the handler already closed.
- `handler.go` — the keepalive ping loop can no longer reap a healthy connection at the instant an `OnMessage` callback returns. The activity mark is refreshed before the running-callback count is cleared, so the ping loop never sees a window with no callback in flight and a stale activity time.
- `handler.go` — keepalive grace and activity windows are measured against a monotonic clock captured at connection accept. A wall-clock step could otherwise defeat the callback grace bound (leaking a connection) or reap a healthy one.

## [v3.1.1] - 2026-07-06 - Standalone Module Resolution Fix

### Fixed

- `go.mod` — the module pinned `melody/v3 v3.0.0` while importing the `http.ServerSentEventHub` API, which only exists from `v3.7.0`, so outside the repository workspace (`GOWORK=off`, or any consumer cloning just this module) the module did not resolve. The pin is raised to `v3.7.0` — the lowest framework version that provides every imported package — and the module-local `go.sum` is now complete for standalone builds.

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/websocket/v3.1.2...HEAD

[v3.1.2]: https://github.com/precision-soft/melody/compare/integrations/websocket/v3.1.1...integrations/websocket/v3.1.2

[v3.1.1]: https://github.com/precision-soft/melody/compare/integrations/websocket/v3.1.0...integrations/websocket/v3.1.1

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/websocket/v3.0.0...integrations/websocket/v3.1.0

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/websocket/v3.0.0
