# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v3.1.0] - 2026-06-08 - Column Encryption, Field-Level Audit Trail, and Read/Write Split

### Added

- `v3/encrypt/` — transparent column encryption at rest with AES-256-GCM, designed as a drop-in over plaintext tables. `Cipher` is an interface (`NewCipher(KeyProvider)`) with a process-wide `UseCipher`; encrypted values carry a marker `<ENC>\0gcm1\0<keyId>:<base64(nonce||ciphertext)>` so reads can tell ciphertext from plaintext: `Decrypt` passes an unmarked legacy value through unchanged (existing rows read correctly and are encrypted on the next write) and `Encrypt` is idempotent on an already-marked value (double-encryption guard). `EncryptedString` (a `driver.Valuer`/`sql.Scanner`) **fails closed** — `Value`/`Scan` error if no cipher is configured rather than silently persisting plaintext — and masks its plaintext in `fmt`/`slog`/error output (`String`/`LogValue` return `<redacted>`). Key rotation: `KeyProvider` adds `ActiveKeyIds()` and `Cipher.EncryptWithKeyId(plaintext, keyId)`; key ids are validated against `^[A-Za-z0-9_.-]{1,32}$`. Searchable encryption: `EncryptDeterministic` derives the nonce from the plaintext (equal plaintext → equal ciphertext under a key) for encrypted-column equality lookups, exposed through the `EncryptedDeterministicString` column type and `Cipher.CiphertextCandidates(plaintext)` (one candidate per active key for rotation-safe `WHERE col IN (...)`) — it reveals plaintext equality, so only for low-entropy lookup fields. `NewFakeCipher()` is an identity cipher for tests/dev. Bulk migration: `Migrator` + the `melody:encrypt:database` command bulk-encrypt, re-encrypt (rotate key), or decrypt a table's columns with keyset pagination; `MigrateReencrypt` skips rows already written under the target key (genuinely idempotent for random-nonce columns rather than rewriting every row on each run), `NewMigrator` rejects a non-MySQL dialect up front, and rows are updated one at a time so a failed run is safe to re-run. `gcmForKey` enforces the standard GCM nonce size so the keyless `looksEncrypted` structural check and the real decode can never disagree.
- `v3/audit/` — per-field before/after audit trail with pluggable storage. `ChangeSet` diffs two struct values by `bun` column name (skipping relations and the embedded base model), masking `<redacted>` fields tagged `audit:"redact"` or typed `encrypt.EncryptedString`; per-entity/global ignored fields are configurable via `Registry`. `NewRecorder(auditDatabase, table)` is kept; `NewRecorderWithStorage(storage, registry)` writes through a `Storage` interface (`NewBunStorage` rows, `NewFileStorage` JSON-lines, or custom) and `WithLogger` dead-letters an entry that fails to store (logged, not dropped). `Registry` routes entities to per-entity audit tables (`EnsureSchema` creates them) and `BeginTransaction` groups a unit of work's entries through a shared `melody_audit_transaction` table + `transaction_id`. `Tracker` (`Insert`/`Update`/`Delete`) is the automatic-capture path: it runs the bun write and records the entry in one call, loading the current row by primary key first so updates get true before-values (bun exposes no unit-of-work changeset, so capture is driven through these helpers). The actor is read from context via `WithActor`.
- `v3/split.go` — `NewReadWriteSplitter(registry, primaryName, ...replicaNames)` over a `ManagerRegistry`: `Writer()` resolves the primary manager and `Reader()` distributes reads round-robin across the configured replica managers (an atomic counter; falls back to the primary when no replicas are configured or a replica is unavailable).
- `v3/audit/` — `NewAsyncStorage(delegate, bufferSize)` wraps any `Storage` to persist entries on a background worker so an audited write never blocks the request path; it dead-letters to the configured logger on queue overflow or backend failure (never rolling back the business transaction) and `Close` drains the queue. `Dropped()` and `Failed()` expose per-instance counts of entries discarded (queue full / closed) and entries the delegate could not persist, so operators can alarm on silent audit loss — useful when several instances run behind a load balancer, each with its own buffer that a hard kill would lose (call `Close` during a graceful drain). A `Save` racing or following `Close` is dead-lettered instead of panicking on a closed channel. `FileStorage.Save` now `fsync`s after each batch so a crash cannot lose the last buffered lines.
- `v3/encrypt/` — `Cipher.EncryptDeterministicWithKeyId(plaintext, keyId)` plus `Migrator`'s `TableSpec.Deterministic` flag re-derive a searchable column's plaintext-bound nonce under the target key during a key-rotation re-encrypt, so a deterministic column stays searchable through rotation (the random-nonce `EncryptWithKeyId` would have silently broken equality lookups).
- `v3/encrypt/command.go` — `Commands(database, cipher)` returns the `melody:encrypt:database` command as a `[]cli/contract.Command`, so userland registers the integration's built-in command in one call.

### Changed

- `v3/audit/` — `Tracker.Insert`/`Update`/`Delete` now run the data write and the audit-entry insert inside a single transaction (`RunInTx`): a failure to persist the audit row rolls the data change back, so a data mutation can no longer be committed without its audit record. `BeginTransaction` and the `BunStorage` write join an ambient transaction when one is present in the context.

### Fixed

- `v3/audit/` — `ChangeSet` now recurses into promoted (exported, anonymous) embedded structs other than `bun.BaseModel`, so an embedded struct's columns are captured in the diff instead of being silently dropped. Table names passed to `NewRegistry`/`Registry.Register` are validated against a strict SQL-identifier pattern (panic on violation) since they flow unquoted through `ModelTableExpr` into DDL/DML.
- `v3/encrypt/` — documented that deterministic encryption yields byte-identical ciphertext for equal plaintext across every deterministic column and table under the same key (cross-column/cross-table equality is observable), not just within one column.
- `v3/encrypt/` — `EncryptedString.Value` now returns the ciphertext as `[]byte` rather than `string`, so the `\x00` bytes in the `<ENC>\0gcm1\0…` marker survive persistence: bun inlines a `driver.Valuer` string into the MySQL statement text and its string formatter drops embedded NUL bytes, which silently corrupted the marker so a subsequent read no longer recognized the value as ciphertext and returned it unencrypted (encryption-at-rest was a no-op for the `EncryptedString` column type on bun + MySQL). Returning `[]byte` makes bun emit an `X'…'` binary literal that preserves every byte; `Scan` already accepts both `string` and `[]byte`, so reads are unaffected.
- `v3/encrypt/` — `EncryptedDeterministicString.Value` carried the identical NUL-drop defect and now also returns `[]byte`, and `Cipher.CiphertextCandidates` now returns `[][]byte` instead of `[]string` for the same reason: the candidates form the right-hand side of a `WHERE col IN (…)` lookup and were likewise NUL-stripped when bun inlined them as strings, which would have broken every deterministic equality lookup once the column stored intact binary ciphertext. As `[][]byte` each candidate is emitted as an `X'…'` binary literal, so the lookup matches the stored value byte-for-byte. The README lookup example is updated accordingly.
- `v3/audit/` — `ChangeSet` now also masks `EncryptedDeterministicString` columns as `<redacted>` (previously only `EncryptedString` was masked), so the low-entropy lookup PII these columns hold — e.g. an email used for searchable encryption — no longer leaks into the audit trail in cleartext. The promoted-embed walk excludes the deterministic type as well.
- `v3/audit/` — a pointer-typed encrypted column (`*EncryptedString`/`*EncryptedDeterministicString`, the natural shape for a nullable encrypted field) is now redacted the same as the value-typed form. `isRedactedField` compared the field type by exact value type only, so a pointer field did not match and — because the encrypted types at the time defined no `MarshalJSON` (one is added below) — its plaintext was serialized into the audit `changes` column (and the dead-letter log). The matcher now dereferences pointer types before comparing.
- `v3/encrypt/` — `Migrator.MigrateEncrypt` and the `melody:encrypt:database` command now honor deterministic encryption. `MigrateEncrypt` consults `TableSpec.Deterministic` (it previously always wrote random-nonce ciphertext, so bulk-encrypting an existing column mapped to `EncryptedDeterministicString` produced values that `CiphertextCandidates` equality lookups could never match — the data was present but silently unsearchable), and the command gains a `--deterministic` flag to drive it. `MigrateReencrypt` no longer takes the same-key fast-path skip when `TableSpec.Deterministic` is set: the wire format cannot distinguish a random-nonce from a deterministic ciphertext, so converting an existing random-nonce column to a searchable one under the same key id now actually rewrites the rows instead of skipping them. Deterministic re-encryption is idempotent (stable ciphertext), so rows already stored in deterministic form are left unchanged.
- `v3/encrypt/` + `v3/audit/` — `EncryptedString` and `EncryptedDeterministicString` now define a redacting `MarshalJSON` (returning `<redacted>`, consistent with their `String`/`LogValue` masking). `ChangeSet`'s `isRedactedField` only inspects a column's own top-level type, so an encrypted value nested inside a **named** (non-embedded) struct field, a slice, a map, or an array was emitted whole and its plaintext was serialized into the audit `changes` column. Redacting at the JSON layer closes the leak at any nesting depth, complementing the field-level matcher above; database storage is unaffected because the encrypted form is produced by `Value` (a `driver.Valuer`), not by JSON.
- `v3/audit/` — a plain `audit:"redact"`-tagged column nested inside a **named** (non-embedded) struct, slice, array, map, or pointer-to-struct field is now redacted too. `ChangeSet` captures such a composite field whole and `isRedactedField` inspected only the column's own top-level tag/type, so a `redact` tag one level down had no effect and its plaintext was serialized into the `changes` column (encrypted types were already safe via their `MarshalJSON`, but a plain `redact` tag has no serialization guard). The matcher now scans the field's type tree and redacts the whole containing field when any member carries `redact`.
- `v3/encrypt/` — `Migrator.MigrateReencrypt` in **randomized** mode (`TableSpec.Deterministic = false`) now rewrites a value that is deterministically encrypted under the target key instead of skipping it. The same-key fast-path skip could not tell a deterministic ciphertext from a random-nonce one (the wire format is identical), so re-encrypting a searchable column to random nonces under the same key id silently left it deterministic — and still equality-searchable. The transform now re-derives the deterministic ciphertext to detect this case and rewrites it with a random nonce, while still skipping a genuinely random-nonce value to avoid pointless churn.
- `v3/audit/` — `AsyncStorage.WithLogger` no longer races the background drain goroutine. The goroutine starts in the constructor, so attaching a logger via the builder while a failing save called `deadLetter` was an unsynchronised read/write of the `logger` field; it is now guarded by a dedicated mutex (separate from the queue mutex to avoid lock nesting in the `Save` → `deadLetter` path).
- `v3/audit/` — a `audit:"redact"`-tagged member reached through an `interface{}`/`any` column is now redacted too. `ChangeSet` walks a field's **static** type tree to find a nested `redact` tag, but an interface field hides its concrete type, so a redact-tagged struct assigned into an `any` column was serialized whole and leaked its plaintext into the `changes` column (encrypted types stay safe via their `MarshalJSON`, but a plain `redact` tag has no serialization guard). The matcher now also inspects the **dynamic** type of the captured value, complementing the static walk that already covers concretely-typed composite fields.
- `v3/audit/` — redaction detection now also inspects **map keys**, not only map values. A `map[encrypt.EncryptedString]V` (or a map keyed by any redact-tagged type) leaked its key plaintext into the `changes` column because `encoding/json` serializes a string-kind map key directly from its underlying value, bypassing the key type's `MarshalJSON`. Both the static type walk and the dynamic value walk now follow the map key as well as the value, so the whole containing field is redacted when a sensitive type or `redact` tag appears in either position.
- `v3/audit/` — the dynamic-type redaction above now descends through **nested** interface values, not just an interface used as the column itself. The previous check ran a static type walk on `reflect.TypeOf(value)`, which still could not see a `redact`-tagged struct carried by an `any` field one level deeper (`struct{ Inner any }`) or held as an interface value inside a map/slice (`map[string]any`), so such plaintext still leaked into the `changes` column. Redaction detection now walks the captured **value**, following interface, pointer, slice/array element, and map value boundaries to their concrete types, so a `redact` tag (or an encrypted column type) is honored at any depth regardless of interface indirection.

## [v3.0.1] - 2026-03-08 - Tidy v3 go.sum Dependencies

### Changed

- `v3/go.sum` — resolved transitive dependency checksums; no API changes

## [v3.0.0] - 2026-03-08 - Introduce v3 Module Path Migration

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/v3` — Go v3 migration; consumers must update imports from `/integrations/bunorm/v2` to `/integrations/bunorm/v3`

### Changed

- Code duplicated into `integrations/bunorm/v3/`; `go.mod` module path updated
- Dependencies pinned to `github.com/precision-soft/melody/v3` and other v3 module paths
- README relative path links updated to reflect v3 directory structure

## [v2.0.0] - 2026-02-17 - Introduce v2 Module Path and Provider.Open Signature Change

### Breaking Changes

- `go.mod` — module path changed to `github.com/precision-soft/melody/integrations/bunorm/v2` — Go v2 migration
- `provider.go` — `Provider.Open()` signature changed from `Open(resolver containercontract.Resolver) (*bun.DB, error)` to `Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error)` — provider no longer reads from container; caller supplies pre-built params and a logger

### Changed

- Code migrated into `integrations/bunorm/v2/` with matching module path
- `go.mod` — dependency on `github.com/precision-soft/melody` bumped from v1.3.2 to v1.6.3

### Added

- `connection_params.go` — `bunorm.ConnectionParams` struct (`Host`, `Port`, `Database`, `User`, `Password`) with `SafeContext()` method that elides the password for logging
- `provider_definition.go` — `ProviderDefinition.Params` field holds connection parameters separately from the definition name

## [v1.0.0] - 2026-02-05 - Initial Release — Bun ORM Integration

### Added

- `provider.go` — `bunorm.Provider` — dialect-agnostic database provider interface
- `provider_definition.go` — `bunorm.ProviderDefinition` — registers multiple database providers with default-provider support
- `manager_registry.go` — `bunorm.ManagerRegistry` — caches and manages `*bunorm.Manager` instances (1:1 per provider definition); exposes `Manager(name)` / `MustManager(name)` / `DefaultManager()` / `MustDefaultManager()` / `DefaultDatabase()` / `MustDefaultDatabase()` accessors
- `manager.go` — `bunorm.Manager` — owns a single `*bun.DB`; exposes `Database()` and `Close()` methods
- `errors.go` — error sentinels: `ErrResolverIsRequired`, `ErrNoProviderDefinitions`, `ErrProviderDefinitionNameIsRequired`, `ErrProviderIsRequired`, `ErrProviderDefinitionNameMustBeUnique`, `ErrMultipleDefaultProviderDefinitions`
- `README.md` — service registration patterns

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v3.1.0...HEAD

[v3.1.0]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v3.0.1...integrations/bunorm/v3.1.0

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v3.0.0...integrations/bunorm/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v1.0.0
