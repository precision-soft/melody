# Changelog

All notable changes to `precision-soft/melody/integrations/websocket/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog.
- `handler.go`: `Options.CloseHandshakeGrace` bounds the closing handshake on the teardown path, 1s when left out as the constant was, at least 1ms, unlimited refused.

### Changed

- **Breaking** with the framework: the fourth major's runtime is read from `v4/container/contract` and built through `v4/container/runtime`, a runtime resolving through its scope only; `runtime.New` and `Runtime.Container()` are gone. See the framework's `UPGRADE.md`.
- websocket: **Breaking**: `Options.SubscribeBuffer` is removed; the stream handler subscribes with the hub's `ServerSentEventHubConfig.SubscriberBuffer`, since the fourth major's `ServerSentEventHub.Subscribe` takes the topic alone.
- `handler.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `module.go`: `ModuleConfig` declares its routes as `Streams []StreamConfig`, each `StreamConfig` carrying the `Hub`, `Options`, `RouteName` and `Path` the module carried, since the framework refuses a second module of one name and two endpoints were two modules; a module with no stream, a stream with a nil hub or an empty path and, on a module of more than one stream, an empty `RouteName` are refused at boot naming the field and the index, every stream judged and its handler built before the first route registers.
- **Breaking** — `handler.go`: `Options.WriteTimeout` and `Options.IdleTimeout` are `bound.Duration` and `Options.ReadLimit` a `bound.Count`, resolved once at `NewStreamHandler`, a value outside its rule panicking naming the field. An absent `IdleTimeout` pings every 30s where it was refused, and one below 1s or unlimited is refused; an absent `WriteTimeout` is 10s as before, a zero or unlimited one refused where a non-positive one took 10s; an absent `ReadLimit` writes 32 KiB on every connection, the library's own figure, an unlimited one disables the limit, and a negative one is refused where it meant no limit.
- **Breaking** — `handler.go`, `module.go`: `Options` is `StreamHandlerConfig` and `StreamConfig.Options` is `StreamConfig.Handler`, the four limits refused naming `StreamHandlerConfig.<field>`; `OnMessage` is `func(runtimeInstance, connection StreamConnection, message InboundMessage) error`, carrying no `coder/websocket` type: `InboundMessage{Binary, Payload}` is the frame, a nil error keeps the connection, a non-nil error is logged once at error and closes the connection with status 1011, and a panicking callback closes with 1011 where it closed with 1000.
- `handler.go`: `StreamConnection.Send(runtimeInstance, OutboundMessage{Binary, Payload})` writes a frame to the connection a message arrived on, from the callback or any goroutine keeping it, under the one writer lock every data frame takes, bounded by `StreamHandlerConfig.WriteTimeout` and the runtime's context; a nil runtime is refused and a closing or closed connection answers `ErrStreamConnectionClosed`, joined with the library's error when the close meets the write; a runtime context ending mid-write closes the connection, as the library does.
- `handler.go`: a failing or panicking `OnMessage` leaves the connection context alive and the 1011 close is sent once a broadcast frame in flight has left, so the peer reads the status rather than an abnormal closure; a peer that never echoes the 1011 close does not hold the handler past `StreamHandlerConfig.CloseHandshakeGrace`.
