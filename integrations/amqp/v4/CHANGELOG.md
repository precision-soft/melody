# Changelog

All notable changes to `precision-soft/melody/integrations/amqp/v4` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- This major includes every change of the third major up to v3.3.0; that history is in the third major's changelog, and the third-major fixes released after it are listed below as carried.

### Fixed

- `publish_half.go`, `transport.go`, `server_sent_event_backplane.go` — carried from the third major (v3.3.2): a publish whose write outlived its budget on a connection the transport or the backplane dialed itself lets go of that connection, with the channels that ride it, in the same critical section that decides to cut it, so the `Send`'s one retry and the backplane's next publish redial instead of being handed the cut connection back; the client reports a connection closed only once the close the cut starts has returned, which the cut does not wait for, and an attempt reaching the cut connection in that window failed with `amqp channel open failed` on the transport and `amqp sse backplane channel open failed` on the backplane (6 of 200 wedged sends on the live suite, 0 of 200 after the fix); a `Close` that finds a write still blocked on the owned connection already cut reports it, where it would have found nothing left to close.
- `publish_half.go`, `transport.go`, `server_sent_event_backplane.go` — carried from the third major (v3.3.2): the abandon of a write that outlived its budget cuts the connection that write's channel rides, never another: each owner keeps the connection its publish channel was opened on beside the channel, and cuts and lets go of it only while it still holds it, where it cut whichever connection it held by then; a write wedged on a connection whose own shutdown had begun, while the consume or listen loop had already redialed, cut the fresh healthy connection, left the write where it was and held the `Send` for the whole join bound; a write on a connection the owner no longer holds is waited for under the same bound, with nothing cut.
- `transport.go`, `server_sent_event_backplane.go`, `publish_half.go` — carried from the third major (v3.3.3): the error a timed-out publish answers on a connection the transport or the backplane dialed itself names the connection the write rides as torn down, where it said the owned connection was cut or closed also when the write rode a connection the owner had already replaced, whose own shutdown was ending it and which nothing cut; the verdicts' and the cut's documentation read the same, and neither they nor `connection.go`'s `CloseWithContext` say any longer that the client's close first takes the connection mutex.

### Security

- `go.mod` — carried from the third major (v3.3.1): `github.com/rabbitmq/amqp091-go` is required at v1.14.0, the fix for [GO-2026-6700](https://pkg.go.dev/vuln/GO-2026-6700), reachable through `Provider.Open`, which dials with `amqp091.DialConfig`.

### Documentation

- readme: carried from the third major (unreleased): the dead-lettering section states the headers a replay from the dead-letter queue deletes, the retry and failure-transport counters and the broker's `x-death`, and the one it keeps, `x-message-type`.
