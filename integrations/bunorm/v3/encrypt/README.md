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
- `Encrypt` is a **no-op** on an already-marked value (no double-encryption).
- The `keyId` travels in the value, so decryption always uses the key that wrote it (rotation-safe).

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

> ⚠️ Deterministic mode **reveals plaintext equality** (equal values produce identical ciphertext). Use it
> only on low-entropy lookup fields, never on secrets. Use `EncryptedString` (random nonce) everywhere else.

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

## Testing / dev

`encrypt.NewFakeCipher()` is an identity cipher (no confidentiality) for tests and local development.
Never install it in production.
