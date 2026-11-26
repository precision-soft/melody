# Melody outbox integration (v4)

A transactional-outbox implementation for the [`messagebus`](https://github.com/precision-soft/melody) package over [Bun](https://bun.uptrace.dev/): business writes and the messages they produce commit in one database transaction, and a relay drains the outbox table to any `messagebus/contract.Transport` (for example the AMQP integration) with retry, exponential backoff and a dead-letter terminal state.

## Installation

```bash
go get github.com/precision-soft/melody/integrations/outbox/v4
```

```go
import outbox "github.com/precision-soft/melody/integrations/outbox/v4"
```

## Requirements

Claiming is atomic through `SELECT ... FOR UPDATE SKIP LOCKED`, so the backing database must support it: **PostgreSQL** or **MySQL 8+**. Two relay instances never publish the same row even without a distributed lock.

## The table

`Store.EnsureSchema(runtimeInstance)` creates the store's table for demos and tests (production schemas belong in `bunorm/migrate` migrations), a nil runtime refused naming `outbox.Store.EnsureSchema`. The table is `StoreConfig.TableName`, `DefaultTableName` (`melody_outbox`) when left out, a plain identifier of at most 64 characters (`^[A-Za-z_][A-Za-z0-9_]{0,63}$`) refused at `NewStore` naming the field; `Store.TableName()` answers it. Every query binds that table, so two stores over one database write disjoint tables:

| Column              | Purpose                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
|---------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `id`                | Row id; also the stable message id (`melody-outbox-<id>`) consumers deduplicate on.                                                                                                                                                                                                                                                                                                                                                                                 |
| `type_name`         | Codec type name used to rebuild the Go message.                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `payload`           | Codec-encoded message body.                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `status`            | `pending` → `inflight` → `sent` / `dead`.                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `attempts`          | Send failures — drives backoff and the `MaxAttempts` dead-letter.                                                                                                                                                                                                                                                                                                                                                                                                   |
| `delivery_attempts` | Every delivery the relay attempts — drives the `MaxDeliveryAttempts` crash-poison cap; a claim charges none, so a hand-back leaves it as it is.                                                                                                                                                                                                                                                                                                                                                                         |
| `available_at`      | When the row is next due (backoff scheduling + visibility timeout).                                                                                                                                                                                                                                                                                                                                                                                                 |
| `sent_at`           | When `MarkSent` resolved the row, NULL until then (`TIMESTAMPTZ` on PostgreSQL, `DATETIME` on MySQL); `PruneSent` judges it, and `EnsureSchema` adds it to a table created without it.                                                                                                                                                                                                                                                                              |
| `headers`           | The JSON object of the enqueue's `EnqueueOptions.Headers`, NULL for none (`TEXT`); the relay publishes it as a `messagebus.HeadersStamp`, and a row whose stored headers do not decode is dead-lettered with `headers: ` before its error. `EnsureSchema` adds it to an older table. |
| `transport`         | The destination `EnqueueOptions.Transport` named, NULL for none (`VARCHAR(64)`); a relay claims the rows of its `RelayConfig.TransportName` alone. `EnsureSchema` adds it to an older table. |
| `last_error`        | Last send/decode failure WITH its cause chain (capped to fit the narrowest default column), for operators.                                                                                                                                                                                                                                                                                                                                                          |
| `claim_token`       | Fencing token, rewritten on every claim. Every transition write (`RecordDeliveryAttempt`, `MarkSent`, `MarkDead`, the retry reschedule, the uncharged hand-back) is guarded on `claim_token = ?` as well as the in-flight status, so a stale run whose claim already lapsed and was re-claimed by another instance matches no row and cannot clobber the new owner. Guarding on status alone is not enough: a re-claim returns the row to the very in-flight state the stale run also holds. |

## Usage

### Encode/decode your messages

The application supplies the `MessageCodec` because only it knows its message types — a JSON codec over a small type registry is the typical implementation:

```go
type Codec struct{}

func (instance Codec) Encode(message any) (string, []byte, error) { /* switch on your types */ }

func (instance Codec) Decode(typeName string, payload []byte) (any, error) { /* inverse */ }
```

For a table owned by the application's migrations, the two columns of an older table:

```sql
-- MySQL
ALTER TABLE melody_outbox ADD COLUMN headers TEXT NULL, ADD COLUMN transport VARCHAR(64) NULL;
-- PostgreSQL
ALTER TABLE melody_outbox ADD COLUMN IF NOT EXISTS headers TEXT NULL, ADD COLUMN IF NOT EXISTS transport VARCHAR(64) NULL;
```

With two relays, an index on `(transport, status, available_at)` keeps each claim on its own rows.

### Enqueue inside your transaction

```go
store := outbox.NewStore(database, Codec{}, outbox.StoreConfig{})

transactionErr := database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
    if insertErr := tx.NewInsert().Model(&order).Scan(ctx); nil != insertErr {
        return insertErr
    }

    /* commits atomically with the business write — the point of the pattern */
    return store.Enqueue(ctx, tx, OrderPlaced{OrderId: order.Id}, outbox.EnqueueOptions{
        Headers: map[string]string{"request-id": requestId},
    })
})
```

`Enqueue` stays on a context, since it runs inside the caller's transaction. [`EnqueueOptions`](./repository.go) declares beside the message:

- `Headers` travel with the message to the consumer as a `messagebus.HeadersStamp`, judged by `messagebus.ValidateHeaders` under `StoreConfig.Headers` (32 headers and 8 KiB by default, unlimited refused), a refusal caused by `messagebus.ErrHeadersRefused` and naming `StoreConfig.Headers.MaxCount` / `.MaxBytes`;
- `Transport` names the relay that publishes the row: the relay whose `RelayConfig.TransportName` equals it; left out, the relay that names none. A name is 1 to 64 characters of letters, digits, `.`, `_`, `:` and `-` beginning with a letter or a digit, refused naming `EnqueueOptions.Transport`. A row named for a transport no relay names is never published.

### Wire the relay through the module

```go
app.RegisterModule(
    outbox.NewModule(outbox.ModuleConfig{
        Store: func(resolver containercontract.Resolver) (*outbox.Store, error) {
            database, resolveErr := container.FromResolver[*bun.DB](resolver.Runtime().Context(), resolver, serviceDatabase)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return outbox.NewStore(database, Codec{}, outbox.StoreConfig{}), nil
        },
        Relay: func(resolver containercontract.Resolver) (*outbox.Relay, error) {
            store, resolveErr := container.FromResolver[*outbox.Store](resolver.Runtime().Context(), resolver, outbox.ServiceStore)
            if nil != resolveErr {
                return nil, resolveErr
            }

            return outbox.NewRelay(store, amqpTransport, Codec{}, outbox.RelayConfig{
                /* optional: Locker (e.g. the rueidis locker) as a single-drainer optimization; SKIP LOCKED already makes concurrent relays safe */
                /* optional overrides: BatchSize, MaxAttempts, Backoff, VisibilityTimeout, MaxDeliveryAttempts, LockName, LockTtl, ResolutionTimeout, UnavailableBackoff */
            }), nil
        },
    }),
)
```

`ModuleConfig.Store` and `ModuleConfig.Relay` are the providers the module registers under the canonical container names `service.outbox.store` / `service.outbox.relay`, run at first resolution, so a database not available before boot is opened when the store is first used; a test that wants a prebuilt relay registers `func(resolver) (*outbox.Relay, error) { return relay, nil }` (resolvable through `StoreMustFromResolver` / `RelayMustFromResolver`; code that holds a runtime, a handler enqueuing a message, reads the store with `StoreMustFromRuntime(runtimeInstance)`) and exposes the relay lifecycle command. A nil `Relay` registers no relay and no command, and a `RelayCommand` configuration with no `Relay` fails the boot naming the field, since no command would read it.

The relay sends through its transport and never closes it, and the registered `*Relay` has no `CloseWithContext` of its own — so the transport behind the relay is closed by nobody unless the composition root registers it with the container, through `messagebus.RegisterTransports` or as a service of its own, for the ordered teardown to close. The same rule the [`ModuleConfig`](./module.go) GoDoc gives for factories holds here: a transport (or database) opened privately and handed to the relay lives exactly as long as the process. The framework's `v4/.example` registers its outbox transport as `service.outbox.transport` for that reason.

### Relay defaults

The repository, the transport and the codec are `NewRelay`'s arguments, a nil one refused naming the argument. Every [`RelayConfig`](./relay.go) tunable is optional; a field left out takes the default below, and a value outside its rule, a zero or an unlimited one included, panics at `NewRelay` naming the field:

| Field                   | Default                  | Meaning                                                                                                                                                                                                                                                                                                |
|-------------------------|--------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `BatchSize`             | `100`                    | Rows claimed per `RunOnce`; at least 1 and at most 100000, refused above it because it flows into a slice pre-allocation. `Store.ClaimDueMessages` refuses a limit above the same ceiling and a non-positive one by name, since bun writes no `LIMIT` for it and the claim would take the whole table. |
| `MaxAttempts`           | `12`                     | Send failures after which the row is dead-lettered; at least 1.                                                                                                                                                                                                                                        |
| `MaxDeliveryAttempts`   | `2 × MaxAttempts` (`24`) | Claims after which the row is dead-lettered as crash-poison. Must exceed `MaxAttempts` — every retry re-claim counts toward it, so a value at or below it is refused naming both fields rather than dead-lettering a normally-retrying row; the derived default saturates at `math.MaxInt`.            |
| `Backoff.Initial`       | `15s`                    | Delay before the first retry; at least 1ms, never above `Backoff.Max`, refused naming both.                                                                                                                                                                                                            |
| `Backoff.Max`           | `10m`                    | Cap on the grown delay; at least 1ms.                                                                                                                                                                                                                                                                  |
| `Backoff.Factor`        | `2.0`                    | Growth per prior attempt. A value below `1`, or NaN, is refused.                                                                                                                                                                                                                                       |
| `VisibilityTimeout`     | `5m`                     | How long a claimed row stays hidden before it re-surfaces — the net that recovers rows whose claimer crashed. Must comfortably exceed the time to drain one batch; at least 1s, a lease like `LockTtl`.                                                                                                |
| `LockTtl`               | `30s`                    | Lease TTL for the optional single-drainer `Locker`; at least 1s. The lease is refreshed at half of it. A refresh that fails is not retried: the claimed batch is drained under its claim tokens, to its end or to a failed send, then the failure is reported.                                         |
| `TransportName`         | empty                    | The destination this relay drains: `ClaimDueMessages` is handed `ClaimOptions{Transport: TransportName}`, so a relay claims the rows enqueued for its name alone and the relay that names none only the rows that name none. One relay per name; a malformed name panics naming the field. |
| `LockName`              | `<namespace>:outbox:relay`| Lock name used with that `Locker`, verbatim when set; the default opens with the application namespace, read at each run from the runtime's configuration, `melody` without one.                                                                                                                       |
| `ResolutionTimeout`     | `5s`                     | Bound on one resolution write — sent, rescheduled or dead — run on its own detached context so a signal between a send and its mark does not republish the row; at least 1ms.                                                                                                                          |
| `UnavailableBackoff`    | `15s`, `1m`, `2.0`       | A `bound.Backoff`: the wait of a row whose send met an unreachable transport (a failure marked by `exception.MarkInfrastructureFailure`), no attempt charged; `Initial` first, grown by its own `Factor` per consecutive outage pass up to `Max`; both at least 1ms.                                   |

`Locker` itself has no default — without one, `SKIP LOCKED` alone already makes concurrent relays safe.

### Run the relay

```bash
melody:outbox:relay [--interval 1s] [--idle-backoff 1s] [--limit 0]
```

The command drains batches until interrupted (SIGINT/SIGTERM stop the loop; a signal that lands mid-batch aborts the drain: a send it cuts hands its row back due at once with its attempts unchanged, and the rows the batch had not reached are handed back through `Repository.ReleaseClaims`, re-surfacing after `VisibilityTimeout` when the release fails): `--interval` is the poll cadence, `--idle-backoff` the sleep after an empty batch (the interval when absent), `--limit N` stops after `N` batches (for cron-style invocations). Consecutive batch failures back off exponentially, by `RelayCommandConfig.ErrorBackoff`, a `bound.Backoff` whose `Initial` left out is the run's `--interval` (capped at `Max`), doubled per consecutive failure and capped at `Max` (one minute when left out, at least 1ms, unlimited refused), so a repository outage does not tight-loop; the module builds its command with `ModuleConfig.RelayCommand`, and `NewRelayCommand(config)` resolves `service.outbox.relay` from the run's scope at the first batch, keeping it for the run, a failed resolution backed off and retried like a failed batch and a nil yield an error. `NewLazyRepository`, built during boot over the container with no `service.outbox.store` registered, fails the boot, naming the line that called it. A cancellation exits clean, but a genuine batch failure that merely coincides with the parent context's deadline is returned — a supervisor sees the failed drain. `Relay.RunOnce` stays available for embedding the drain into a custom loop.

## The repository

[`Repository`](./repository.go) is the persistence the relay drives, implemented by `*Store` and by `NewLazyRepository`. Every door takes the runtime it runs under first and reads its context; a nil runtime, a typed nil and one carrying no context are refused with `the runtime is nil or carries no context`. Beside the six doors the relay resolves rows through, three maintenance doors:

- `ReleaseClaims(runtimeInstance, ids, claimToken) (released int, err error)` hands back the rows among `ids` still claimed under the token: pending again and available now, `attempts` and `delivery_attempts` unchanged; rows re-claimed under another token, sent or dead are untouched, empty `ids` release nothing, and an empty token or more ids than 100000 are refused before the store is touched. The relay calls it when a batch ends on a failure, with the rows after the one it stopped at only: the row whose write failed keeps its claim until `VisibilityTimeout`, so a row the transport published and whose `MarkSent` failed is not published again on the next pass, and a failure at the last row releases nothing. A failed hold write stops at the row it failed for, a cancelled hold at the first row it did not write. The release runs detached from the run's cancellation and bounded by `ResolutionTimeout`; a failed release writes one `outbox relay claim release failed` ERROR record and never replaces the batch's own error, the visibility timeout re-surfacing the rows.
- `RequeueDead(runtimeInstance, RequeueDeadOptions{Ids, Limit}) (requeued int, err error)` returns dead rows to pending, available now, `attempts` and `delivery_attempts` reset to 0 and `last_error` kept; a nil or empty `Ids` selects every dead row, so a caller that filtered its list down to nothing checks it before calling; `Limit` bounds the rows touched, 0 meaning 100000, a negative one or one above it refused, and more `Ids` than the limit are refused naming `RequeueDeadOptions.Ids` rather than cut. The selection and the update are two statements: a concurrent requeue can answer a count below the rows selected, never requeue a row twice.
- `PruneSent(runtimeInstance, PruneSentOptions{SentBefore, Limit}) (pruned int, err error)` deletes rows sent (`sent_at`, written by `MarkSent`) before `SentBefore`, a sent row with no send time never pruned; a zero `SentBefore` is refused naming `PruneSentOptions.SentBefore` before the store is touched; `Limit` as above.

## Delivery semantics

- **At-least-once**: a transport success followed by a crash before `MarkSent` redelivers the row. Every publish carries the deterministic `messagebus.MessageIdStamp` `melody-outbox-<id>` (the AMQP transport round-trips it as the message id) so consumers can deduplicate. During an outage the count of such duplicates has no bound: a confirmation the broker answers late, a channel lost with its connection before it, or a nack, is marked as an outage and republished on every outage pass whose probe fails too, so a consumer that does not deduplicate gets as many copies as the outage has passes. A nack does not mean no queue took the message: an exchange fanning it out to several queues, one of them full under `reject-publish`, is nacked while every other queue enqueues it, so those queues get one more copy per pass for as long as the full one stays full.
- **Retry**: a failed send the run's cancellation did not cut reschedules the row with exponential backoff (`Backoff.Initial` × `Backoff.Factor` per prior attempt, capped at `Backoff.Max`, overflow-safe) and dead-letters it at `MaxAttempts`, and, once that write succeeded, writes one `outbox message send failed` ERROR record naming the row, its type and the attempt it was charged, with the transport's cause; a contained send panic keeps its own `outbox delivery panicked` record instead.
- **An unreachable transport charges nothing**: a send the transport marks with `exception.MarkInfrastructureFailure` is taken for an outage. The amqp transport marks every publish failure but the broker's verdict on the message — an unroutable return, a channel the broker closed over a live connection (an oversize body, a missing exchange) — the caller's own cancellation and a closing transport; a nack is an outage, so a queue full under `reject-publish` holds its rows uncharged for as long as it is full, except that when the same transport still delivers another row, the probe below charges the nacked one. The relay then sends the next row of the batch once, as a probe: delivered, the failure was the message's after all and is charged an attempt, so a message a healthy broker refuses cannot be retried uncharged for ever; failed too, or with no next row, the outage is counted once and the failed row, with a probe that was marked as well, goes back through `Repository.RescheduleUncharged`: pending again after `UnavailableBackoff.Initial` (growing by `UnavailableBackoff.Factor` per consecutive outage pass up to `UnavailableBackoff.Max`), its `attempts` unchanged and the claim's delivery attempt taken back, so an outage of any length reaches neither `MaxAttempts` nor `MaxDeliveryAttempts`. The first hand-back of an outage whose write succeeds writes one `outbox message send failed` ERROR record carrying `transportUnavailable`, the later ones none; a delivered row, or a failure the broker answered, ends the outage, the delivered row writing `outbox relay transport answers again` at INFO.
- **A failed send ends the batch**: an unmarked failure is charged to the failed row alone, and the relay stops at the first failed send, marked or not, rather than try every claimed row against the same transport. The rows the batch had not reached go back to pending uncharged, available again when the failed row is tried again (or would have been, when the failure dead-lettered it), with a `last_error` naming the row that held them. The price is head-of-line blocking: once the broker is back, the held rows wait out the
  failed row's current backoff (at most `Backoff.Max`), and a message the broker refuses holds every row behind it for its own backoff each time it is due, until it reaches `MaxAttempts`.
- **Poison**: an undecodable row dead-letters immediately; a row that keeps crashing or hanging the relay between claim and resolve dead-letters once the delivery attempts the relay records for it — one each time a claim's run reaches the row, a batch-mate it never reached charged nothing and a hand-back after an outage taking its claim's back — exceed `MaxDeliveryAttempts` (default 2 × `MaxAttempts`; the attempt after the cap is the one that buries it). A panic raised by the codec's `Decode` or the transport's `Send` is contained per row and charged as the failure the collaborator would have returned — a decode panic dead-letters the row, a send panic takes the retry path — with the panic in `last_error` under a `panic: ` prefix and an `outbox delivery panicked` record in the process log; after a decode panic the batch goes on, a send panic ends it like any failed send. A panic raised by the repository is not contained.
- **Crash recovery**: a claimed row re-surfaces after `VisibilityTimeout` if its claimer died.
- **Clocks**: the visibility window is computed and compared on the application hosts' wall clocks, not the database's, so inter-replica clock skew subtracts directly from `VisibilityTimeout` — a replica running ahead can re-claim early and duplicate a publish. Keep the fleet's clocks synced; the `MessageIdStamp` dedup covers the residual duplicates the at-least-once contract already admits.
- **Locker (optional)**: with a `lock/contract.Locker` only one instance drains at a time; the lease is refreshed mid-batch (twice per `LockTtl`). The lease is an optimisation, not what keeps two instances apart — the claim tokens are — so a lease lost mid-batch does not abort the drain: the claimed rows stay this run's, the batch is drained to its end or to a failed send, and the refresh failure is reported after it, for the command's loop to log and back off on. A failed `Acquire` is a different answer from a lease another instance holds: the lease store unreachable, the run returns the error before it claims a row, so with a `Locker` wired an outage of the lease store stops delivery. An application that would rather deliver through it, the claim tokens keeping concurrent relays apart, wraps its `Locker` so that an `Acquire` error answers acquired, logging it, and the `Release` and `Refresh` of such a lease do nothing.
