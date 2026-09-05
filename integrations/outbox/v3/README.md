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

| Column              | Purpose                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
|---------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `id`                | Row id; also the stable message id (`melody-outbox-<id>`) consumers deduplicate on.                                                                                                                                                                                                                                                                                                                                                                                 |
| `type_name`         | Codec type name used to rebuild the Go message.                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `payload`           | Codec-encoded message body.                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `status`            | `pending` → `inflight` → `sent` / `dead`.                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `attempts`          | Send failures — drives backoff and the `MaxAttempts` dead-letter.                                                                                                                                                                                                                                                                                                                                                                                                   |
| `delivery_attempts` | Every claim — drives the `MaxDeliveryAttempts` crash-poison cap.                                                                                                                                                                                                                                                                                                                                                                                                    |
| `available_at`      | When the row is next due (backoff scheduling + visibility timeout).                                                                                                                                                                                                                                                                                                                                                                                                 |
| `last_error`        | Last send/decode failure WITH its cause chain (capped to fit the narrowest default column), for operators.                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `claim_token`       | Fencing token, rewritten on every claim. Every transition write (`RecordDeliveryAttempt`, `MarkSent`, `MarkDead`, the retry reschedule) is guarded on `claim_token = ?` as well as the in-flight status, so a stale run whose claim already lapsed and was re-claimed by another instance matches no row and cannot clobber the new owner. Guarding on status alone is not enough: a re-claim returns the row to the very in-flight state the stale run also holds. |

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

The relay sends through its transport and never closes it, and the registered `*Relay` has no `Close` of its own — so the transport behind a prebuilt relay is closed by nobody unless the composition root registers it with the container, through `messagebus.RegisterTransports` or as a service of its own, for the ordered teardown to close. The same rule the [`ModuleConfig`](./module.go) GoDoc gives for factories holds here: a transport (or database) opened privately and handed to the relay lives exactly as long as the process. The framework's `v3/.example` registers its outbox transport as `service.outbox.transport` for that reason.

### Relay defaults

Every [`RelayConfig`](./relay.go) tunable is optional; a non-positive value resolves to the default below:

| Field                 | Default                  | Meaning                                                                                                                                                                                                                                                          |
|-----------------------|--------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `BatchSize`           | `100`                    | Rows claimed per `RunOnce`; clamped from above too (100000), because it flows into a slice pre-allocation. `Store.ClaimDueMessages` applies the same cap to the limit it is handed and refuses a non-positive one by name, since bun writes no `LIMIT` for it and the claim would take the whole table.                                                                                                                                                                                                                                      |
| `MaxAttempts`         | `12`                     | Send failures after which the row is dead-lettered.                                                                                                                                                                                                              |
| `MaxDeliveryAttempts` | `2 × MaxAttempts` (`24`) | Claims after which the row is dead-lettered as crash-poison. Must exceed `MaxAttempts` — every retry re-claim counts toward it, so a value at or below it (including an unset zero) is raised to the default rather than dead-lettering a normally-retrying row. |
| `InitialBackoff`      | `15s`                    | Delay before the first retry.                                                                                                                                                                                                                                    |
| `MaxBackoff`          | `10m`                    | Cap on the grown delay.                                                                                                                                                                                                                                          |
| `BackoffFactor`       | `2.0`                    | Growth per prior attempt. A value below `1` resolves to the default.                                                                                                                                                                                             |
| `VisibilityTimeout`   | `5m`                     | How long a claimed row stays hidden before it re-surfaces — the net that recovers rows whose claimer crashed. Must comfortably exceed the time to drain one batch.                                                                                               |
| `LockTtl`             | `30s`                    | Lease TTL for the optional single-drainer `Locker`; the lease is refreshed at half of it. A refresh that fails is not retried: the claimed batch is drained to its end under its claim tokens, then the failure is reported.                                                                                                                                                                        |
| `LockName`            | `melody:outbox:relay`    | Lock name used with that `Locker`.                                                                                                                                                                                                                               |

`Locker` itself has no default — without one, `SKIP LOCKED` alone already makes concurrent relays safe.

### Run the relay

```sh
melody:outbox:relay [--interval 1s] [--idle-backoff 5s] [--limit 0]
```

The command drains batches until interrupted (SIGINT/SIGTERM stop the loop; a signal that lands mid-batch aborts the drain and whatever stayed claimed re-surfaces after `VisibilityTimeout`): `--interval` is the poll cadence, `--idle-backoff` the sleep after an empty batch, `--limit N` stops after `N` batches (for cron-style invocations). Consecutive batch failures back off exponentially (capped at one minute) so a repository outage does not tight-loop. A cancellation exits clean, but a genuine batch failure that merely coincides with the parent context's deadline is returned — a supervisor sees the failed drain. `Relay.RunOnce` stays available for embedding the drain into a custom loop.

## Delivery semantics

- **At-least-once**: a transport success followed by a crash before `MarkSent` redelivers the row. Every publish carries the deterministic `messagebus.MessageIdStamp` `melody-outbox-<id>` (the AMQP transport round-trips it as the message id) so consumers can deduplicate.
- **Retry**: a failed send reschedules the row with exponential backoff (`InitialBackoff` × `BackoffFactor`, capped at `MaxBackoff`, overflow-safe) and dead-letters it at `MaxAttempts`.
- **Poison**: an undecodable row dead-letters immediately; a row that keeps crashing or hanging the relay between claim and resolve dead-letters at `MaxDeliveryAttempts` (default 2 × `MaxAttempts`). A panic raised by the codec's `Decode` or the transport's `Send` is contained per row and charged as the failure the collaborator would have returned — a decode panic dead-letters the row, a send panic takes the retry path — with the panic in `last_error` under a `panic: ` prefix and an `outbox delivery panicked` record in the process log; the batch goes on. A panic raised by the repository is not contained.
- **Crash recovery**: a claimed row re-surfaces after `VisibilityTimeout` if its claimer died.
- **Clocks**: the visibility window is computed and compared on the application hosts' wall clocks, not the database's, so inter-replica clock skew subtracts directly from `VisibilityTimeout` — a replica running ahead can re-claim early and duplicate a publish. Keep the fleet's clocks synced; the `MessageIdStamp` dedup covers the residual duplicates the at-least-once contract already admits.
- **Locker (optional)**: with a `lock/contract.Locker` only one instance drains at a time; the lease is refreshed mid-batch (twice per `LockTtl`). The lease is an optimisation, not what keeps two instances apart — the claim tokens are — so a lease lost mid-batch does not abort the drain: the claimed rows stay this run's, the batch is drained to its end, and the refresh failure is reported after it, for the command's loop to log and back off on.
