# encrypt — transparent column encryption for bun

Go-native field encryption at rest for [bun](https://bun.uptrace.dev/) models, using **AES-256-GCM**.
Designed as a **drop-in over existing plaintext tables**: deploy it, and existing plaintext rows keep
reading while new writes are encrypted — no separate migration step required to start.

## Encoding & drop-in behaviour

Encrypted values are stored as:

```
<ENC>\0gcm1\0<keyId>:<base64(nonce || ciphertext+tag)>
```

The `<ENC>\0gcm1\0` marker lets reads distinguish ciphertext from plaintext:

- `Decrypt` returns an **unmarked** value (legacy/un-migrated plaintext) unchanged.
- `Encrypt` is a **no-op** on an already-marked value (no double-encryption). The marker alone is not trusted: a marked value passes through only if it actually decrypts under its (known) key, so a plaintext that merely *looks* like ciphertext is sealed normally instead of being stored raw and poisoning later reads. A marked value carrying an **unknown** `keyId` still passes through unchanged, so data sealed under a retired key is never double-encrypted.
- The `keyId` travels in the value, so decryption always uses the key that wrote it (rotation-safe).

> **Nullable columns:** `EncryptedString`/`EncryptedDeterministicString` are non-nullable value types — a SQL `NULL` scans to the Go zero value `""`, and writing it back encrypts the empty string into a non-`NULL` ciphertext (and, for a deterministic column, makes the row match an equality search on the empty plaintext). For a nullable column declare the field as a **pointer** (`*EncryptedString` / `*EncryptedDeterministicString`): bun leaves a `NULL` column as a `nil` pointer and never calls `Value`, so `NULL` round-trips faithfully.

## Quick start

```go
provider := encrypt.NewStaticKeyProvider("v1", map[string][]byte{"v1": key32Bytes})
encrypt.UseCipher(encrypt.NewCipher(provider)) // process-wide, set once at boot
```

Type a column as `EncryptedString` to encrypt it transparently:

```go
type User struct {
    Id    int64                  `bun:"id,pk"`
    Email encrypt.EncryptedString `bun:"email"`
}
```

`EncryptedString` masks its plaintext in `fmt`/`slog`/error output (`String`/`LogValue` return
`<redacted>`); use an explicit `string(value)` conversion to read the real value. It **fails closed** —
`Value`/`Scan` return an error if no cipher is configured, so a misconfigured app never silently writes
plaintext into an "encrypted" column.

## Key rotation

`KeyProvider` exposes every active key; encrypt under a chosen key explicitly:

```go
provider := encrypt.NewStaticKeyProvider("v2", map[string][]byte{"v1": oldKey, "v2": newKey})
provider.ActiveKeyIds()          // ["v2", "v1"] — current first
cipher.EncryptWithKeyId(value, "v2")
```

Bulk re-encrypt a table after rotating keys with the `melody:encrypt:database` command (see below).

## Searchable (deterministic) encryption

Random nonces make a column un-queryable. For lookup columns, `EncryptedDeterministicString` derives the
nonce from the plaintext, so equal plaintext yields equal ciphertext under a key:

```go
type User struct {
    Email encrypt.EncryptedDeterministicString `bun:"email"`
}

// build the right-hand side of an equality / IN lookup (one candidate per active key, rotation-safe):
candidates, _ := cipher.CiphertextCandidates("user@example.com")
db.NewSelect().Model(&user).Where("email IN (?)", bun.In(candidates)).Scan(ctx)
```

`CiphertextCandidates` returns `[][]byte` (not `[]string`) on purpose: the ciphertext marker carries `\0`
glue bytes, and bun inlines a `string` argument into the SQL text through a formatter that drops embedded
NUL bytes — which would corrupt the right-hand side and break the match. As `[][]byte` each candidate is
emitted as an `X'…'` binary literal, so every byte survives and the lookup compares equal to the stored
`EncryptedDeterministicString` column (whose `Value()` is likewise a binary `[]byte`).

> ⚠️ Deterministic mode **reveals plaintext equality** (equal values produce identical ciphertext). The
> nonce is keyed only by `(key, plaintext)`, so equal plaintext yields byte-identical ciphertext **across
> every deterministic column and table under the same key** — an observer of the stored values can correlate
> equal values across rows and tables, not just within one column. Use it only on low-entropy lookup fields,
> never on secrets where cross-column equality must stay hidden. Use `EncryptedString` (random nonce) everywhere else.

## Bulk migration

`Migrator` (and the `melody:encrypt:database` CLI command) stream a table in keyset-paginated batches:

| Mode | What it does |
|------|--------------|
| `encrypt`   | encrypt plaintext columns (idempotent — already-encrypted values are skipped) |
| `reencrypt` | decrypt with whichever key wrote each value, re-encrypt under `--target-key` (rotation) |
| `decrypt`   | rewrite columns as plaintext |

```bash
melody melody:encrypt:database --table=users --primary-key=id --column=email --column=ssn --mode=encrypt
melody melody:encrypt:database --table=users --column=email --mode=reencrypt --target-key=v2
```

> ⚠️ For a **deterministic/searchable** column set `TableSpec.Deterministic = true` (the programmatic
> `Migrator`) so `reencrypt` re-derives the plaintext-bound nonce under the target key and the column stays
> searchable. Re-encrypting a deterministic column without the flag rewrites it with random nonces and
> silently breaks `CiphertextCandidates` equality lookups.

## Register as a module

The `melody:encrypt:database` command is **not** registered automatically — like every Melody command it
has to be wired into the application. The integration ships a self-registering module so a single call does it:

```go
app.RegisterModule(encrypt.NewModule(encrypt.ModuleConfig{
    Database: database, // *bun.DB (MySQL dialect)
    Cipher:   cipher,
}))
```

This implements [`CliModule`](../../../../v3/application/contract/cli_module.go) and registers the command
through `RegisterCliCommands`. Registration is skipped when `Database` or `Cipher` is nil. If you wire the
application's `RegisterCliCommands` by hand instead, append the slice from `encrypt.Commands(database, cipher)`.

## Testing / dev

`encrypt.NewFakeCipher()` is an identity cipher (no confidentiality) for tests and local development.
Never install it in production.
