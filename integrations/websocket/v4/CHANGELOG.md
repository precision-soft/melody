# Changelog

All notable changes to `precision-soft/melody/integrations/websocket/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.2.0; that history is in the third major's changelog.
- `handler.go`: `Options.CloseHandshakeGrace` bounds the closing handshake on the teardown path, 1s when left out as the constant was, at least 1ms, unlimited refused.

### Changed

- websocket: **Breaking**: `Options.SubscribeBuffer` is removed; the stream handler subscribes with the hub's `ServerSentEventHubConfig.SubscriberBuffer`, since the fourth major's `ServerSentEventHub.Subscribe` takes the topic alone.
- `handler.go`: the log records are built through `exception.LogContext(err, exception.LogContextConfig{...})`, the fourth major's form, which takes the extra context in a struct; nothing of this module's own surface changes.
- **Breaking** — `module.go`: `ModuleConfig` declares its routes as `Streams []StreamConfig`, each `StreamConfig` carrying the `Hub`, `Options`, `RouteName` and `Path` the module carried, since the framework refuses a second module of one name and two endpoints were two modules; a module with no stream, a stream with a nil hub or an empty path and, on a module of more than one stream, an empty `RouteName` are refused at boot naming the field and the index, every stream judged and its handler built before the first route registers.
- **Breaking** — `handler.go`: `Options.WriteTimeout` and `Options.IdleTimeout` are `bound.Duration` and `Options.ReadLimit` a `bound.Count`, resolved once at `NewStreamHandler`, a value outside its rule panicking naming the field. An absent `IdleTimeout` pings every 30s where it was refused, and one below 1s or unlimited is refused; an absent `WriteTimeout` is 10s as before, a zero or unlimited one refused where a non-positive one took 10s; an absent `ReadLimit` writes 32 KiB on every connection, the library's own figure, an unlimited one disables the limit, and a negative one is refused where it meant no limit.
