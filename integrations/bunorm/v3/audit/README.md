# audit — per-field change audit trail for bun

Records who changed what, when, as a per-field before/after diff, for [bun](https://bun.uptrace.dev/)
models. Go-native equivalent of a Doctrine unit-of-work audit listener.

## Change set

`ChangeSet(before, after)` diffs two struct values by `bun` column name, recording only changed fields (relations and `bun.BaseModel` are skipped; the exported fields of other embedded structs are flattened in, matching how bun promotes their columns). Sensitive fields are recorded as changed but masked to `<redacted>`:

- tag a field `audit:"redact"`, or
- type it as any encrypted column type — `encrypt.EncryptedString`, `encrypt.EncryptedDeterministicString`, or the compartment-bound `encrypt.EncryptedStringFor[R]` / `EncryptedDeterministicStringFor[R]` forms — all recognised through the `encrypt.EncryptedColumn` marker interface (auto-redacted).

Additional fields can be dropped globally or per entity via the `Registry` (see below). Ignored fields are matched by **bun column name** (the first tag segment, or the Go field name where no tag names one) — the same name the change-set records.

## Recording

### Automatic capture (recommended)

`Tracker` runs the bun write and records the matching entry in one call. An **update** loads the current row by primary key first (`SELECT ... FOR UPDATE`), so the recorded before-image is the row as the database held it, never the model the caller happened to pass:

```go
recorder := audit.NewRecorder(auditDb, audit.DefaultTable)
tracker := audit.NewTracker(appDb, recorder)

ctx := audit.WithActor(ctx, "user-42")
tracker.Update(ctx, "user", "42", &user)              /* SELECT old, UPDATE, record diff — all in one transaction */
tracker.Delete(ctx, "user", "42", &User{Id: 42})      /* DELETE, record the identifier — no before-image unless the entity opted in */
```

A **delete** does not load a before-image by default: it records the identifier, the actor and the time, and the trail asserts nothing about the deleted row's column values (see [Deletes](#deletes) for why, and for the per-entity `CaptureDeleteBeforeImage` opt-in that does load and lock the row). Two consequences follow from that default:

- an audited delete costs no extra `SELECT` round trip, and deleting a row that is already absent is a silent no-op — nothing is deleted and nothing is audited (unlike `Update`, which errors when the row it must read is gone).
- deleted contents are **not** recoverable from the trail. A compliance trail that must retain them has to opt the entity in — see [Deletes](#deletes).

Pass `""` as the id to let the `Tracker` derive it from the model's bun primary key after the write — the common case for an autoincrement key, unknown until the INSERT populates the model. A caller-supplied id still wins, and a composite key is joined with `:`.

> bun exposes no unit-of-work changeset, and a global query hook cannot recover old values for an
> arbitrary `UPDATE`, so automatic capture is driven through these helpers (writes go through the model
> with a primary key). Each `Tracker` operation runs the data write and the audit-entry insert in a single
> transaction (`RunInTx`): if the audit row fails to persist, the data change is rolled back, so a mutation
> is never committed without its audit record. The lower-level `Recorder.Record{Insert,Update,Delete}` API
> is available when you already hold before/after yourself; wrap the context in `audit.WithDatabase(ctx, tx)`
> to have those calls write the audit entry through your own unit-of-work transaction, keeping it atomic
> with the data change. The two doors are **alternatives**: a `Tracker` refuses a context that carries a
> caller-made `WithDatabase` binding, because running its own transaction inside yours does not compose —
> it deadlocks the pool — and the refusal names the `Recorder` path to take instead.

### Actor

The actor is read from context: `ctx = audit.WithActor(ctx, "user-42")`.

## Storage backends

The recorder writes through a pluggable `Storage`:

```go
recorder := audit.NewRecorderWithStorage(audit.NewBunStorage(auditDb), registry)
recorder = recorder.WithLogger(logger) // dead-letter: entries that fail to store are logged, not dropped
```

- `NewBunStorage(db)` — rows in an audit table (default).
- `NewFileStorage(path)` — JSON-lines append (fsynced per batch).
- `NewAsyncStorage(delegate, bufferSize)` — wraps any of the above to persist on a background worker so an audited write never blocks the request path; overflow/backend failures dead-letter to the logger and `Close` drains the queue, bounded by a five-second grace after which the wedged save is cancelled and the remainder dead-lettered. A save whose context carries a database binding — a `Tracker`'s unit of work, or a `WithDatabase` caller — goes through the delegate **synchronously**, so the atomicity promise above survives the async wrapper; only the unbound path is queued. Without a `WithLogger` the drops are silent — the `Dropped()`/`Failed()` counters are then the only signal.
- any custom `Storage` implementation.

A recorder built by hand over an `AsyncStorage` should use `NewRecorderOwningStorage`, whose `Close` closes the storage — the drain goroutine ends nowhere else. The default `NewRecorderWithStorage` does **not** own the storage: on the container path both are registered services and the container closes each one itself.

> Table names registered via `NewRegistry`/`Registry.Register` must be plain SQL identifiers
> (`^[A-Za-z_][A-Za-z0-9_]*$`) — they flow unquoted through `ModelTableExpr` into DDL/DML, so an invalid
> name panics at registration.

## Per-entity tables, ignored fields & transaction grouping

`Registry` routes entities to dedicated tables and configures ignored fields:

```go
registry := audit.NewRegistry("melody_audit", "updated_at"). // global ignored fields
    Register("user", audit.EntityOptions{Table: "user_audit", IgnoredFields: []string{"last_login"}})

registry.EnsureSchema(ctx, auditDb) // create audit + transaction tables if absent
```

Group one unit of work's entries under a shared `melody_audit_transaction` row:

```go
ctx, txId, _ := audit.BeginTransaction(ctx, auditDb, "user-42", map[string]any{"request": reqId})
// every entry recorded with ctx now carries transaction_id = txId
```

### Deletes

A delete records the identifier, the actor and the time — not the fields of the model you passed. The caller usually holds only the primary key, so those fields are zero values the delete never read, and recording them would assert them as the deleted row's contents for a row that is gone.

When an entity's deleted contents must be recoverable from the trail, opt in for that entity:

```go
registry.Register("user", audit.EntityOptions{CaptureDeleteBeforeImage: true})
```

The delete then loads and locks the row before removing it, so the trail carries its real values. That costs one select and a row lock on every delete of that entity, and deleting a row that is already absent becomes an error. A delete that matched no row is never recorded.
