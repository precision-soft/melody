# encrypt — transparent column encryption for bun

Go-native field encryption at rest for [bun](https://bun.uptrace.dev/) models, using **AES-256-GCM**. Designed as a **drop-in over existing plaintext tables**: deploy it, and existing plaintext rows keep reading while new writes are encrypted — no separate migration step required to start.

## Encoding & drop-in behaviour

Encrypted values are stored as:

```
<ENC>\0gcm1\0<keyId>:<base64(nonce || ciphertext+tag)>
```

The `<ENC>\0gcm1\0` marker lets reads distinguish ciphertext from plaintext:

- `Decrypt` returns an **unmarked** value (legacy/un-migrated plaintext) unchanged — that pass-through is what lets a column be encrypted one write at a time.
- `Decrypt` **fails** on a **marked** value whose payload no longer decodes (malformed body, invalid base64, shorter than a nonce). The marker is written by this cipher and nothing else, so a broken body behind it is damage, not plaintext — in practice a column too narrow for the ciphertext under a non-strict `sql_mode`, where MySQL truncates the write and only warns. Reading it back verbatim would hand the application a marker plus half a base64 blob as if it had stored them. `Scan` and the bulk `Migrator` report it for the same reason.
- `Encrypt` is a **no-op** on an already-marked value (no double-encryption). The marker alone is not trusted: a marked value passes through only if it actually decrypts under its (known) key, so a plaintext that merely *looks* like ciphertext is sealed normally instead of being stored raw and poisoning later reads. A marked value carrying an **unknown** `keyId` still passes through unchanged, so data sealed under a retired key is never double-encrypted.
- The `keyId` travels in the value, so decryption always uses the key that wrote it (rotation-safe).

> **A sealed value is binary, never a `string` argument.** The marker's two `\0` glue bytes are what makes it a marker, and bun's formatter drops embedded NUL bytes when it inlines a `string` into the SQL text. Through the model — `EncryptedString`, `EncryptedDeterministicString`, or a pointer to either — this never arises: their `Value()` returns `[]byte`, so bun emits a binary literal and every byte survives. It arises when userland writes the raw statement itself: `db.ExecContext(ctx, "update ... set email = ?", string(sealed))` stores the value with its glue bytes stripped, which no longer matches the marker, so the next read takes the unmarked pass-through and hands the application the base64 body as if it were plaintext. Pass `[]byte` for a sealed value in a raw statement, and see [Searchable (deterministic) encryption](#searchable-deterministic-encryption) for the same reason behind `CiphertextCandidates` returning `[][]byte`.

### Column sizing

Encryption **expands** the value, so a column that held the plaintext will not hold the ciphertext. The stored form is 11 marker characters, the key id, a colon, and the base64 of a 12-byte nonce + the plaintext + a 16-byte GCM tag:

```
width = 12 + len(keyId) + ceil(4 * (plaintextBytes + 28) / 3)
```

That is roughly **1.34 characters per plaintext byte, plus 52**. The expansion is over **bytes**, not characters, so a multi-byte UTF-8 character costs its full encoded length. Reference points for a two-character key id such as `v1`:

| Longest plaintext | Column width needed |
|-------------------|---------------------|
| 32 bytes          | 94                  |
| 64 bytes          | 137                 |
| 152 bytes         | 254                 |
| **153 bytes**     | **256** — a `VARCHAR(255)` no longer fits |
| 255 bytes         | 392                 |

Widen the column **before** running the bulk migration. `melody:encrypt:database` refuses to start when a target column is too narrow for the widest value it holds, and the refusal reports the `requiredWidth`; `Migrator.EnsureColumnCapacity` performs the same check for programmatic callers. The check matters most on a server whose `sql_mode` is not strict: MySQL then accepts an overflowing `UPDATE` with a warning, storing a **truncated** ciphertext that can never be decrypted again while reporting the row as migrated and exiting `0`.

A **key rotation** grows the column too: the key id is stored inside every sealed value, so rotating from `v1` to `2026-07-rotated` adds thirteen characters to every row at once, and a column sized exactly to the ciphertext it already holds is then too narrow for all of it. `mode=reencrypt` is checked against that growth as well — `Migrator.EnsureColumnCapacityForReencrypt` for programmatic callers — and the refusal names the `targetKeyId` alongside the `requiredWidth`. Rotating onto a key id of the same length or a shorter one needs no room and is never refused.

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
`Value`/`Scan` return an error if no cipher is configured, so a misconfigured app never silently writes plaintext into an "encrypted" column.

## Key rotation

`KeyProvider` exposes every active key; encrypt under a chosen key explicitly:

```go
provider := encrypt.NewStaticKeyProvider("v2", map[string][]byte{"v1": oldKey, "v2": newKey})
provider.ActiveKeyIds()          // ["v2", "v1"] — current first
cipher.EncryptWithKeyId(value, "v2")
```

Bulk re-encrypt a table after rotating keys with the `melody:encrypt:database` command (see below).

## Multiple key compartments (multi-context binaries)

`UseCipher` installs one process-wide default cipher — fine for one database, but a binary consolidating several apps should keep each context's keys in its own compartment instead of merging the key maps under a single current key. Install one named cipher per compartment and bind columns through a zero-size marker type:

```go
encrypt.UseCipherNamed("crm", encrypt.NewCipher(crmProvider))
encrypt.UseCipherNamed("billing", encrypt.NewCipher(billingProvider))

type CrmCipher struct{}

func (instance CrmCipher) CipherName() string {
    return "crm"
}

type Customer struct {
    Iban encrypt.EncryptedStringFor[CrmCipher] `bun:"iban"`
}
```

The marker parameterizes the generic column types `EncryptedStringFor[R]` and
`EncryptedDeterministicStringFor[R]`, so the binding lives in the Go type — the only channel available, because `database/sql` gives `Value()`/`Scan()` no context. Compartments are isolated: the `crm` cipher can never decrypt a `billing` ciphertext, and key rotation inside one compartment keeps working through the key id embedded in each ciphertext. The plain `EncryptedString` keeps using the default cipher.

Two designs were considered and rejected: per-column key ids over one merged `KeyProvider` (the compartments stay merged — either context can decrypt the other's rows, exactly the isolation loss the feature exists to prevent) and a cipher per `bunorm.Manager` (a `driver.Valuer` has no manager context; bun query hooks would miss raw SQL paths).

For the bulk command with several compartments, declare command contexts on the module — each gets its own `melody:encrypt:database:<name>`:

```go
app.RegisterModule(encrypt.NewModule(encrypt.ModuleConfig{
    Contexts: []encrypt.CommandContextConfig{
        {Name: "crm", Database: crmDatabase, Cipher: crmCipher},
        {Name: "billing", Database: billingDatabase, Cipher: billingCipher},
    },
}))
```

## Searchable (deterministic) encryption

Random nonces make a column un-queryable. For lookup columns, `EncryptedDeterministicString` derives the nonce from the plaintext, so equal plaintext yields equal ciphertext under a key:

```go
type User struct {
    Email encrypt.EncryptedDeterministicString `bun:"email"`
}

// build the right-hand side of an equality / IN lookup (one candidate per active key, rotation-safe):
candidates, _ := cipher.CiphertextCandidates("user@example.com")
db.NewSelect().Model(&user).Where("email IN (?)", bun.In(candidates)).Scan(ctx)
```

`CiphertextCandidates` returns `[][]byte` (not `[]string`) on purpose: the ciphertext marker carries `\0`
glue bytes, and bun inlines a `string` argument into the SQL text through a formatter that drops embedded NUL bytes — which would corrupt the right-hand side and break the match. As `[][]byte` each candidate is emitted as an `X'…'` binary literal, so every byte survives and the lookup compares equal to the stored
`EncryptedDeterministicString` column (whose `Value()` is likewise a binary `[]byte`).

> ⚠️ Deterministic mode **reveals plaintext equality** (equal values produce identical ciphertext). The
> nonce is keyed only by `(key, plaintext)`, so equal plaintext yields byte-identical ciphertext **across
> every deterministic column and table under the same key** — an observer of the stored values can correlate
> equal values across rows and tables, not just within one column. Use it only on low-entropy lookup fields,
> never on secrets where cross-column equality must stay hidden. Use `EncryptedString` (random nonce) everywhere else.

## Bulk migration

`Migrator` (and the `melody:encrypt:database` CLI command) stream a table in keyset-paginated batches:

| Mode        | What it does                                                                            |
|-------------|-----------------------------------------------------------------------------------------|
| `encrypt`   | encrypt plaintext columns (idempotent — already-encrypted values are skipped)           |
| `reencrypt` | decrypt with whichever key wrote each value, re-encrypt under `--target-key` (rotation) |
| `decrypt`   | rewrite columns as plaintext                                                            |

```bash
melody melody:encrypt:database --table=users --primary-key=id --column=email --column=ssn --mode=encrypt
melody melody:encrypt:database --table=users --column=email --mode=reencrypt --target-key=v2
```

> ⚠️ For a **deterministic/searchable** column set `TableSpec.Deterministic = true` (the programmatic
> `Migrator`) so `reencrypt` re-derives the plaintext-bound nonce under the target key and the column stays
> searchable. Re-encrypting a deterministic column without the flag rewrites it with random nonces and
> silently breaks `CiphertextCandidates` equality lookups.

## Register as a module

The `melody:encrypt:database` command is **not** registered automatically — like every Melody command it has to be wired into the application. The integration ships a self-registering module so a single call does it:

```go
app.RegisterModule(encrypt.NewModule(encrypt.ModuleConfig{
    Database: database, // *bun.DB (MySQL dialect)
    Cipher:   cipher,
}))
```

This implements [`CliModule`](../../../../v3/application/contract/cli_module.go) and registers the command through `RegisterCliCommands`. If you wire the application's `RegisterCliCommands` by hand instead, append the slice from `encrypt.Commands(database, cipher)`.

Registration is **skipped only when `Database`, `DatabaseFactory` and `Cipher` are all nil** — an entirely unconfigured module contributes no command. A partially configured one is a wiring mistake and **panics** at registration ([`module.go`](./module.go)):

- a `Database` (or `DatabaseFactory`) with no `Cipher`, or a `Cipher` with neither — `encrypt module needs a database and a cipher`;
- both `Database` and `DatabaseFactory` set — `encrypt module received both a database and a database factory - set exactly one`.

`ModuleConfig.Contexts` is **stricter**: there is no all-nil carve-out per entry. Every entry present in the slice is validated unconditionally, so a zero-value entry panics rather than being skipped — the empty-`Name` check runs first (`encrypt command context name is empty`), followed by the same needs-a-database-and-a-cipher and exactly-one-of-database-or-factory checks, each naming the offending context. Omit an entry entirely rather than leaving it blank.

## Testing / dev

`encrypt.NewFakeCipher()` is an identity cipher (no confidentiality) for tests and local development. Never install it in production.
