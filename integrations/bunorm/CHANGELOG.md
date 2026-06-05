# Changelog

All notable changes to `precision-soft/melody/integrations/bunorm` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `v3/encrypt/` — transparent column encryption at rest with AES-256-GCM, designed as a drop-in over plaintext tables. `Cipher` is an interface (`NewCipher(KeyProvider)`) with a process-wide `UseCipher`; encrypted values carry a marker `<ENC>\0gcm1\0<keyId>:<base64(nonce||ciphertext)>` so reads can tell ciphertext from plaintext: `Decrypt` passes an unmarked legacy value through unchanged (existing rows read correctly and are encrypted on the next write) and `Encrypt` is idempotent on an already-marked value (double-encryption guard). `EncryptedString` (a `driver.Valuer`/`sql.Scanner`) **fails closed** — `Value`/`Scan` error if no cipher is configured rather than silently persisting plaintext — and masks its plaintext in `fmt`/`slog`/error output (`String`/`LogValue` return `<redacted>`). Key rotation: `KeyProvider` adds `ActiveKeyIds()` and `Cipher.EncryptWithKeyId(plaintext, keyId)`; key ids are validated against `^[A-Za-z0-9_.-]{1,32}$`. Searchable encryption: `EncryptDeterministic` derives the nonce from the plaintext (equal plaintext → equal ciphertext under a key) for encrypted-column equality lookups, exposed through the `EncryptedDeterministicString` column type and `Cipher.CiphertextCandidates(plaintext)` (one candidate per active key for rotation-safe `WHERE col IN (...)`) — it reveals plaintext equality, so only for low-entropy lookup fields. `NewFakeCipher()` is an identity cipher for tests/dev. Bulk migration: `Migrator` + the `melody:encrypt:database` command bulk-encrypt, re-encrypt (rotate key), or decrypt a table's columns with keyset pagination.
- `v3/audit/` — per-field before/after audit trail with pluggable storage. `ChangeSet` diffs two struct values by `bun` column name (skipping relations and the embedded base model), masking `<redacted>` fields tagged `audit:"redact"` or typed `encrypt.EncryptedString`; per-entity/global ignored fields are configurable via `Registry`. `NewRecorder(auditDatabase, table)` is kept; `NewRecorderWithStorage(storage, registry)` writes through a `Storage` interface (`NewBunStorage` rows, `NewFileStorage` JSON-lines, or custom) and `WithLogger` dead-letters an entry that fails to store (logged, not dropped). `Registry` routes entities to per-entity audit tables (`EnsureSchema` creates them) and `BeginTransaction` groups a unit of work's entries through a shared `melody_audit_transaction` table + `transaction_id`. `Tracker` (`Insert`/`Update`/`Delete`) is the automatic-capture path: it runs the bun write and records the entry in one call, loading the current row by primary key first so updates get true before-values (bun exposes no unit-of-work changeset, so capture is driven through these helpers). The actor is read from context via `WithActor`.
- `v3/split.go` — `NewReadWriteSplitter(registry, primaryName, ...replicaNames)` over a `ManagerRegistry`: `Writer()` resolves the primary manager and `Reader()` distributes reads round-robin across the configured replica managers (an atomic counter; falls back to the primary when no replicas are configured or a replica is unavailable).

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

[Unreleased]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v3.0.1...HEAD

[v3.0.1]: https://github.com/precision-soft/melody/compare/integrations/bunorm/v3.0.0...integrations/bunorm/v3.0.1

[v3.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v3.0.0

[v2.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v2.0.0

[v1.0.0]: https://github.com/precision-soft/melody/releases/tag/integrations/bunorm/v1.0.0
