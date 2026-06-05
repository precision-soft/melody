# audit — per-field change audit trail for bun

Records who changed what, when, as a per-field before/after diff, for [bun](https://bun.uptrace.dev/)
models. Go-native equivalent of a Doctrine unit-of-work audit listener.

## Change set

`ChangeSet(before, after)` diffs two struct values by `bun` column name, recording only changed fields
(relations and the embedded base model are skipped). Sensitive fields are recorded as changed but masked
to `<redacted>`:

- tag a field `audit:"redact"`, or
- type it `encrypt.EncryptedString` (auto-redacted).

Additional fields can be dropped globally or per entity via the `Registry` (see below).

## Recording

### Automatic capture (recommended)

`Tracker` runs the bun write and records the matching entry in one call. Updates load the current row by
primary key first, so the diff has true before-values:

```go
recorder := audit.NewRecorder(auditDb, audit.DefaultTable)
tracker := audit.NewTracker(appDb, recorder)

ctx := audit.WithActor(ctx, "user-42")
tracker.Update(ctx, "user", "42", &user) // SELECT old, UPDATE, record diff
```

> bun exposes no unit-of-work changeset, and a global query hook cannot recover old values for an
> arbitrary `UPDATE`, so automatic capture is driven through these helpers (writes go through the model
> with a primary key). The lower-level `Recorder.Record{Insert,Update,Delete}` API is available when you
> already hold before/after yourself.

### Actor

The actor is read from context: `ctx = audit.WithActor(ctx, "user-42")`.

## Storage backends

The recorder writes through a pluggable `Storage`:

```go
recorder := audit.NewRecorderWithStorage(audit.NewBunStorage(auditDb), registry)
recorder = recorder.WithLogger(logger) // dead-letter: entries that fail to store are logged, not dropped
```

- `NewBunStorage(db)` — rows in an audit table (default).
- `NewFileStorage(path)` — JSON-lines append.
- any custom `Storage` implementation.

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
