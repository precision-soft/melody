# Melody outbox integration (v3)

A transactional-outbox implementation for the [`messagebus`](https://github.com/precision-soft/melody) package over [Bun](https://bun.uptrace.dev/): business writes and the messages they produce commit in one database transaction, and a relay drains the outbox table to any `messagebus/contract.Transport` (for example the AMQP integration) with retry, exponential backoff and a dead-letter terminal state.

## Installation

```sh
go get github.com/precision-soft/melody/integrations/outbox/v3
```

```go
import outbox "github.com/precision-soft/melody/integrations/outbox/v3"
```

## Requirements

Claiming is atomic through `SELECT ... FOR UPDATE SKIP LOCKED`, so the backing database must support it: **PostgreSQL** or **MySQL 8+**. Two relay instances never publish the same row even without a distributed lock.

## The table

`Store.EnsureSchema` creates the `melody_outbox` table for demos and tests (production schemas belong in `bunorm/migrate` migrations):

| Column              | Purpose                                                                             |
|---------------------|-------------------------------------------------------------------------------------|
| `id`                | Row id; also the stable message id (`melody-outbox-<id>`) consumers deduplicate on. |
| `type_name`         | Codec type name used to rebuild the Go message.                                     |
| `payload`           | Codec-encoded message body.                                                         |
| `status`            | `pending` → `inflight` → `sent` / `dead`.                                           |
| `attempts`          | Send failures — drives backoff and the `MaxAttempts` dead-letter.                   |
| `delivery_attempts` | Every claim — drives the `MaxDeliveryAttempts` crash-poison cap.                    |
| `available_at`      | When the row is next due (backoff scheduling + visibility timeout).                 |
| `last_error`        | Last send/decode error, for operators.                                              |

## Usage

### Encode/decode your messages

The application supplies the `MessageCodec` because only it knows its message types — a JSON codec over a small type registry is the typical implementation:

```go
type Codec struct{}

func (instance Codec) Encode(message any) (string, []byte, error) { /* switch on your types */ }

func (instance Codec) Decode(typeName string, payload []byte) (any, error) { /* inverse */ }
```

### Enqueue inside your transaction

```go
store := outbox.NewStore(database, Codec{})

transactionErr := database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
    if insertErr := tx.NewInsert().Model(&order).Scan(ctx); nil != insertErr {
        return insertErr
    }

    /* commits atomically with the business write — the point of the pattern */
    return store.Enqueue(ctx, tx, OrderPlaced{OrderId: order.Id})
})
```

### Wire the relay through the module

```go
relay := outbox.NewRelay(outbox.RelayConfig{
    Repository: store,
    Transport:  amqpTransport,
    Codec:      Codec{},
    /* optional: Locker (e.g. the rueidis locker) as a single-drainer optimization; SKIP LOCKED already makes concurrent relays safe */
    /* optional overrides: BatchSize, MaxAttempts, InitialBackoff, MaxBackoff, BackoffFactor, VisibilityTimeout, MaxDeliveryAttempts, LockName, LockTtl */
})

app.RegisterModule(
    outbox.NewModule(outbox.ModuleConfig{
        Store: store,
        Relay: relay,
    }),
)
```

The module registers the store and relay under the canonical container names `service.outbox.store` / `service.outbox.relay` (resolvable through `StoreMustFromContainer` / `RelayMustFromContainer`) and exposes the relay lifecycle command.

### Run the relay

```sh
melody:outbox:relay [--interval 1s] [--idle-backoff 5s] [--limit 0]
```

The command drains batches until interrupted (SIGINT/SIGTERM stop the loop; a signal that lands mid-batch aborts the drain and whatever stayed claimed re-surfaces after `VisibilityTimeout`): `--interval` is the poll cadence, `--idle-backoff` the sleep after an empty batch, `--limit N` stops after `N` batches (for cron-style invocations). Consecutive batch failures back off exponentially (capped at one minute) so a repository outage does not tight-loop. `Relay.RunOnce` stays available for embedding the drain into a custom loop.

## Delivery semantics

- **At-least-once**: a transport success followed by a crash before `MarkSent` redelivers the row. Every publish carries the deterministic `messagebus.MessageIdStamp` `melody-outbox-<id>` (the AMQP transport round-trips it as the message id) so consumers can deduplicate.
- **Retry**: a failed send reschedules the row with exponential backoff (`InitialBackoff` × `BackoffFactor`, capped at `MaxBackoff`, overflow-safe) and dead-letters it at `MaxAttempts`.
- **Poison**: an undecodable row dead-letters immediately; a row that keeps crashing or hanging the relay between claim and resolve dead-letters at `MaxDeliveryAttempts` (default 2 × `MaxAttempts`).
- **Crash recovery**: a claimed row re-surfaces after `VisibilityTimeout` if its claimer died.
- **Locker (optional)**: with a `lock/contract.Locker` only one instance drains at a time; the lease is refreshed mid-batch (twice per `LockTtl`) and the drain aborts if the lease is lost.
