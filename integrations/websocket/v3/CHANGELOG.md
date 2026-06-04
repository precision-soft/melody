# Changelog

All notable changes to `precision-soft/melody/integrations/websocket` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial Melody v3 binding of the WebSocket integration — bidirectional streaming on `coder/websocket`, bridging the core `http.SseHub`. Developed v3-first; v1 and v2 bindings to follow.
- `handler.go` — `NewStreamHandler(hub, Options)`: upgrades the connection, subscribes to a resolved topic, writes broadcast `SseEvent` data as text frames, reads inbound frames into an optional `OnMessage` callback, and returns `(nil, nil)` on disconnect. `Options` carries `TopicResolver`, `OnMessage`, `SubscribeBuffer`, and `OriginPatterns`.
- `handler_test.go` — in-process E2E (httptest server + `websocket.Dial` + `hub.Broadcast`); no external service required.
