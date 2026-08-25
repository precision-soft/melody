# Changelog

All notable changes to `precision-soft/melody/integrations/websocket` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `module.go` — **Behavioural change**: a nil `Hub` or an empty `Path` is refused at boot instead of silently registering no route. An unregistered route has no later consumer to fail loudly — the endpoint simply does not exist, clients get 404 and every boot-time check reads healthy — while the same module already panics one field over on a zero `IdleTimeout`; a module registered at all is a decision to serve the stream
- `handler.go` — **Behavioural change**: a `TopicResolver` that returns an empty topic refuses the connection before the upgrade instead of subscribing it. An empty topic is only ever an extraction that failed — the nil-resolver default is the distinct `"default"` — and subscribing anyway pooled every mis-resolved client from every tenant on one shared `""` topic, reported to each of them as an established stream; a broadcast to `""` produced by the same defect on the publish side was then delivered cross-tenant
- a zero `IdleTimeout` is refused when the handler is built. The ping loop is the only thing that can reap a peer that died silently: the read loop blocks with no deadline, the upgrade hijacks the connection so the server's own timeouts no longer apply, and a write into a half-open socket keeps succeeding while buffer space remains — so connections opened and abandoned accumulate without bound. **Breaking**: an application passing the zero value now fails at construction with a diagnostic naming what to set

### Fixed

- `handler.go` — `NewStreamHandler` refuses a nil hub at construction: it passed boot and panicked on the first connection attempt, deep inside the request path, where every sibling constructor of a required dependency reports the wiring error at boot
- `handler.go` — a negative `ReadLimit` is passed through as coder/websocket's documented "no limit" instead of being silently discarded by the positive-only guard, which left the library's 32 KiB default armed for exactly the payloads the option was set to allow — the first oversized frame closed the connection with `1009` and no explanation
- `handler.go` — the `OnMessage` panic record preserves the panicked error whole: flattening through `%v` kept only the message, so an error carrying a cause chain and context lost both, and "where in the callback and why" was unrecoverable from the log
- `handler.go` — the read loop's terminating error is logged at debug level: a peer killed for exceeding the read limit or for a protocol violation used to vanish with no record anywhere, indistinguishable from a clean goodbye
- handler: a received pong refreshes the connection's liveness mark, and a keepalive ping that could not be written is no longer read as a dead peer. Liveness previously advanced only when `Read` returned a data message, so a receive-only client bridged onto a broadcast hub never advanced it at all and the activity window was dead for exactly the connections it protects; and because the library serialises control frames behind the frame in flight, a ping issued while the handler was flushing to a slow client timed out without ever reaching the socket, and the connection was cancelled mid-write. The excuse is scoped to the ping a frame actually blocked: whether a write was in flight is sampled as the ping is issued, so a write that has completed excuses nothing — a write into a half-open connection succeeds for as long as the socket send buffer has room, and half-open detection must survive a hub that keeps broadcasting. **Behavioural change**: a configuration with `IdleTimeout` below
  `WriteTimeout` no longer turns transient write contention into a disconnect — a frame in flight excuses a timed-out ping until one interval past the configured write timeout, after which the write's own failure tears the connection down

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
