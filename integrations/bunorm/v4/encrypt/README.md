# encrypt — transparent column encryption for bun

Go-native field encryption at rest for [bun](https://bun.uptrace.dev/) models, using **AES-256-GCM**. Designed as a **drop-in over existing plaintext tables**: deploy it, and existing plaintext rows keep reading while new writes are encrypted — no separate migration step required to start.

## Encoding & drop-in behaviour

Encrypted values are stored as:

```
<ENC>\0gcm1\0<keyId>:<base64(nonce || ciphertext+tag)>
```

The `<ENC>\0gcm1\0` marker lets reads distinguish ciphertext from plaintext:

- `Decrypt` returns an **unmarked** value (legacy/un-migrated plaintext) unchanged — that pass-through is what lets a column be encrypted one write at a time.
- `Decrypt` **fails** on a **marked** value whose payload no longer decodes (malformed body, a key id outside `^[A-Za-z0-9_.-]{1,32}$` — the grammar the write side seals under, so such an id never reaches the `KeyProvider` —, invalid base64, shorter than a nonce). The marker is written by this cipher and nothing else, so a broken body behind it is damage, not plaintext — in practice a column too narrow for the ciphertext under a non-strict `sql_mode`, where MySQL truncates the write and only warns. Reading it back verbatim would hand the application a marker plus half a base64 blob as if it had stored them. `Scan` and the bulk `Migrator` report it for the same reason.
- `Decrypt` **fails** as well on a value that begins with `<ENC>\0` under a format this build does not read, `ErrSealFormatUnknown` in the chain, the format named only when it is a lower-case token of at most 16 characters: the NUL after `<ENC>` is the claim of provenance, so a seal of a newer build or a damaged marker is never served as text, and the `Scan` of every column type over the package cipher and every `Migrator` mode, whatever its cipher, stop on it the same way; through a wrapped or foreign cipher a column's `Value` refuses to write a value of that family, as it refuses a `gcm1` one.
- `Encrypt` is a **no-op** on an already-marked value (no double-encryption). The deterministic doors ask one question more: a marked value that authenticates but was sealed with a random nonce is not passed through by `EncryptDeterministic` — it is re-sealed deterministically under the key id it already carries (never the door's, so it is not a rotation), because a random-nonce seal in a deterministic column is a value `CiphertextCandidates` can never produce and the equality lookup would miss the row; a seal that is deterministic already passes through, whatever key it carries. The marker alone is not trusted: a marked value passes through only if it actually decrypts under its (known) key, so a plaintext that merely *looks* like ciphertext is sealed normally instead of being stored raw and poisoning later reads. A marked value carrying an **unknown** `keyId` is sealed the same way — to the write path it is application data, since this cipher only ever writes ids from its own key
  set. That is why a retired key must **stay in the `KeyProvider`** until every value sealed under it has been re-encrypted: drop it early and the next write that touches such a value seals it a second time, unrecoverably. The bulk `Migrator` is the strict counterpart: reading STORED values, it stops the run on any marked value that no longer decrypts — a missing key or a truncated write — instead of sealing over it.
- The column types (`EncryptedString`, `EncryptedStringFor`, `EncryptedDeterministicString`, `EncryptedDeterministicStringFor`) do **not** take that pass-through: their `Value` seals every application value, a marked one included, and reading the column back returns the value as it was written. Otherwise a column an attacker can write and later read in the clear would hand back the plaintext of any seal of the compartment, from any row, column or table. The `Migrator` keeps that guarantee across its runs: it decides on the STORED value alone — whether it is sealed, under which key, deterministically or not — and seals the value it decrypted as data, so a rotation or a re-run never turns a seal stored as data back into a bare seal. A `Cipher` implemented outside this package, a wrapper over `NewCipher` for metrics or tracing included, loses the seal-as-data door: the column types refuse a value that carries the encryption marker instead of storing a bare seal, and the `Migrator` stops
  on such a row (see `decrypt` below). Install the package cipher directly where applications may store ciphertext as data.
- The `keyId` travels in the value, so decryption always uses the key that wrote it (rotation-safe).

> **A sealed value is binary, never a `string` argument.** The marker's two `\0` glue bytes are what makes it a marker, and bun's formatter drops embedded NUL bytes when it inlines a `string` into the SQL text. Through the model — `EncryptedString`, `EncryptedDeterministicString`, or a pointer to either — this never arises: their `Value()` returns `[]byte`, so bun emits a binary literal and every byte survives. It arises when userland writes the raw statement itself: `db.ExecContext(ctx, "update ... set email = ?", string(sealed))` stores the value with its glue bytes stripped, which no longer matches the marker, so the next read takes the unmarked pass-through and hands the application the base64 body as if it were plaintext. Pass `[]byte` for a sealed value in a raw statement, and see [Searchable (deterministic) encryption](#searchable-deterministic-encryption) for the same reason behind `CiphertextCandidates` returning `[][]byte`.

### Column sizing

Encryption **expands** the value, so a column that held the plaintext will not hold the ciphertext. The stored form is 11 marker characters, the key id, a colon, and the base64 of a 12-byte nonce + the plaintext + a 16-byte GCM tag:

```
width = 12 + len(keyId) + ceil(4 * (plaintextBytes + 28) / 3)
```

That is roughly **1.34 characters per plaintext byte, plus 52**. The expansion is over **bytes**, not characters, so a multi-byte UTF-8 character costs its full encoded length. Reference points for a two-character key id such as `v1`:

| Longest plaintext | Column width needed                       |
|-------------------|-------------------------------------------|
| 32 bytes          | 94                                        |
| 64 bytes          | 137                                       |
| 152 bytes         | 254                                       |
| **153 bytes**     | **256** — a `VARCHAR(255)` no longer fits |
| 255 bytes         | 392                                       |

Widen the column **before** running the bulk migration. `MigrateEncrypt` and `MigrateReencrypt` refuse to start when a target column is too narrow for the widest value it holds — the check runs inside the migration itself, so the CLI command and the programmatic caller are covered alike, and the refusal reports the `requiredWidth`; the standalone `Migrator.EnsureColumnCapacity` answers the same question without running anything. The TEXT family is measured in bytes (`CHARACTER_OCTET_LENGTH`), which is what bounds the pure-ASCII ciphertext there; a `VARCHAR(n)` in its `n` characters. The check matters most on a server whose `sql_mode` is not strict: MySQL then accepts an overflowing `UPDATE` with a warning, storing a **truncated** ciphertext that can never be decrypted again while reporting the row as migrated and exiting `0`.

A **key rotation** grows the column too: the key id is stored inside every sealed value, so rotating from `v1` to `2026-07-rotated` adds thirteen characters to every row at once, and a column sized exactly to the ciphertext it already holds is then too narrow for all of it. `mode=reencrypt` is checked against that growth as well — `Migrator.EnsureColumnCapacityForReencrypt` for programmatic callers — and the refusal names the `targetKeyId` alongside the `requiredWidth`. Rotating onto a key id of the same length or a shorter one needs no room and is never refused.

> **Nullable columns:** `EncryptedString`/`EncryptedDeterministicString` are non-nullable value types — a SQL `NULL` scans to the Go zero value `""`, and writing it back encrypts the empty string into a non-`NULL` ciphertext (and, for a deterministic column, makes the row match an equality search on the empty plaintext). For a nullable column declare the field as a **pointer** (`*EncryptedString` / `*EncryptedDeterministicString`): bun leaves a `NULL` column as a `nil` pointer and never calls `Value`, so `NULL` round-trips faithfully.

## Quick start

```go
provider := encrypt.NewStaticKeyProvider(encrypt.KeyRing{
    CurrentKeyId: "v1",
    Keys:         map[string][]byte{"v1": key32Bytes},
})

app.RegisterModule(encrypt.NewModule(encrypt.ModuleConfig{
    Cipher: encrypt.NewCipher(provider),
}))
```

The module installs the cipher the columns read when the application registers its services, in every process mode — http, worker and cli alike — so every process that reads an encrypted column registers the module. It also publishes the cipher as the `encrypt.ServiceCipher` service, for a component that takes it by injection, so an application registers one module with a `Cipher`: the container refuses a second, before it installs anything. A later application booted in the same process — a test booting several — replaces the ciphers of the earlier one.

Generate every key from `crypto/rand` (32 bytes). `NewStaticKeyProvider` refuses a `KeyRing` whose `CurrentKeyId` is empty or names no key, a key id outside `^[A-Za-z0-9_.-]{1,32}$`, a key of the wrong length and a key of **all zero bytes** — the shape of a key that was never generated — naming the field, and judges nothing else about a key: its strength is yours. The provider keeps a copy of the ring.

Type a column as `EncryptedString` to encrypt it transparently:

```go
type User struct {
    Id    int64                  `bun:"id,pk"`
    Email encrypt.EncryptedString `bun:"email"`
}
```

`EncryptedString` masks its plaintext in `fmt`/`slog`/error/json output (`String`/`LogValue`/`MarshalJSON` return
`<redacted>`); use an explicit `string(value)` conversion to read the real value. The redaction is one-way on purpose: `UnmarshalJSON` decodes a plaintext string as itself and leaves the value untouched on `null`, but **refuses the placeholder** — a document this package redacted, decoded back into the column, would otherwise seal `<redacted>` over the secret on the next write. It **fails closed** —
`Value`/`Scan` return an error if no cipher is installed — no module registered with a `Cipher` — so a misconfigured app never silently writes plaintext into an "encrypted" column.

## Key rotation

`KeyProvider` exposes every active key; encrypt under a chosen key explicitly:

```go
provider := encrypt.NewStaticKeyProvider(encrypt.KeyRing{
    CurrentKeyId: "v2",
    Keys:         map[string][]byte{"v1": oldKey, "v2": newKey},
})
provider.ActiveKeyIds()          // ["v2", "v1"] — current first
cipher.EncryptWithKeyId(value, "v2")
```

Bulk re-encrypt a table after rotating keys with the `melody:encrypt:database` command (see below).

## Multiple key compartments (multi-context binaries)

`ModuleConfig.Cipher` is one default cipher — fine for one database, but a binary consolidating several apps should keep each context's keys in its own compartment instead of merging the key maps under a single current key. Declare one context per compartment on the module, which installs its cipher under its name, and bind columns through a zero-size marker type:

```go
app.RegisterModule(encrypt.NewModule(encrypt.ModuleConfig{
    Contexts: []encrypt.CommandContextConfig{
        {Name: "crm", Cipher: encrypt.NewCipher(crmProvider)},
        {Name: "billing", Cipher: encrypt.NewCipher(billingProvider)},
    },
}))

type CrmCipher struct{}

func (instance CrmCipher) CipherName() string {
    return "crm"
}

type Customer struct {
    Iban encrypt.EncryptedStringFor[CrmCipher] `bun:"iban"`
}
```

The marker parameterizes the generic column types `EncryptedStringFor[R]` and
`EncryptedDeterministicStringFor[R]`, so the binding lives in the Go type — the only channel available, because `database/sql` gives `Value()`/`Scan()` no context. Compartments are isolated only as far as their keys are: the name selects the registry entry and is not bound into the ciphertext, so two compartments whose providers hold the same key under the same id decrypt each other's ciphertext. Give each compartment keys of its own. Key rotation inside one compartment keeps working through the key id embedded in each ciphertext. The plain `EncryptedString` keeps using the default cipher.

## Binding a seal to its column

A marker that also implements `SealBinding()` binds its columns' seals to a label, `<table>.<column>` with each part `[A-Za-z0-9_]{1,64}`. Its `EncryptedStringFor[R]` and `EncryptedDeterministicStringFor[R]` write the format `gcm2`: the label, the key id and the format are authenticated as associated data, and the deterministic nonce is taken over them too, so the seal opens through a column of that marker alone and equal values of two bound columns are not equal seals. A seal without a binding, `gcm1`, is what every unbound column and the third major write, and still opens. Embed `DefaultCipherRef` to bind a column of the default cipher; any other marker naming the empty compartment is refused.

```go
type CustomerIban struct{}

func (instance CustomerIban) CipherName() string {
    return "crm"
}

func (instance CustomerIban) SealBinding() string {
    return "customer.iban"
}

app.RegisterModule(encrypt.NewModule(encrypt.ModuleConfig{
    Contexts:     []encrypt.CommandContextConfig{{Name: "crm", Cipher: crmCipher}},
    SealBindings: []encrypt.SealBindingConfig{{Ref: CustomerIban{}}},
}))

type Customer struct {
    Iban encrypt.EncryptedStringFor[CustomerIban] `bun:"iban"`
}
```

Every bound marker is declared in `ModuleConfig.SealBindings`, a zero-size value type whose methods have value receivers, `DefaultCipherRef` embedded by value. A declaration the module cannot hold is refused at registration, naming the field and the index: a nil or pointer `Ref`, a label outside the grammar, a cipher name the module installs no cipher under, a marker with fields or with `SealBinding` on its pointer receiver, a marker declared twice, a label declared twice over one cipher instance under one name or two, or a cipher implemented outside the package, a wrapper included, since sealing unbound would drop the binding without a word. An undeclared bound marker is refused at its column's first `Value` or `Scan`.

`SealBindingConfig.RefuseUnbound`, false when left out, is the switch that closes the downgrade: until it is set, the column also reads a `gcm1` seal and a value without the marker, so a `gcm1` seal of the same key written into the column by someone who can write the database still opens there. Set, the column refuses both with `ErrUnboundValueRefused`, on the format alone and before any key is asked for; a non-NULL empty string is a value without the marker too, so a `NOT NULL DEFAULT ''` column needs its empty values sealed. Bind a column holding values in three steps: deploy the marker and its declaration without the switch, so new writes are `gcm2` and the old rows still read; run `melody:encrypt:database --mode=encrypt` over the column, which seals each value without the marker and re-seals each `gcm1` seal under the binding with the key and mode it carries; then deploy `RefuseUnbound: true`. `Cipher.Decrypt` and the plain column types refuse a bound seal with `ErrSealBound`.

| An attacker who can write the database… | Unbound column | Bound, admitting | Bound, `RefuseUnbound` |
|---|---|---|---|
| moves a seal from another column or table of the key | opens | a bound one fails; an unbound one opens | fails |
| moves a seal between two rows of the same column | opens | opens | opens |
| writes a bare value into the column | served | served | refused |
| compares equal values across two deterministic columns | equal seals | different seals for bound rows; the `gcm1` rows still equal | different seals |
| plants a seal before the bulk run that binds the column | taken as the application's | taken, if unbound | — (the run already happened) |

The bulk run binds what it reads: a seal moved in from another bound column stops it at its row, but a `gcm1` seal written into the column before the run, by the third major or any unbound column of the key, opens and is re-sealed under the binding, since `gcm1` carries no provenance. The window closes for good at the third step; run the second before untrusted writers can reach the column and keep the window short. The command's closing record names the label each column ran under, `sealBindings`, and counts, per column value, `boundFromUnbound` and `sealedFromPlaintext` beside `rows`, so a run that bound nothing and a surprise are both visible. A key per compartment gives the cross-column separation without any of this.

Two designs were considered and rejected: per-column key ids over one merged `KeyProvider` (the compartments stay merged — either context can decrypt the other's rows, exactly the isolation loss the feature exists to prevent) and a cipher per `bunorm.Manager` (a `driver.Valuer` has no manager context; bun query hooks would miss raw SQL paths).

For the bulk command with several compartments, give the contexts their databases — each context with a `Database` or a `DatabaseFactory` gets its own `melody:encrypt:database:<name>`, and one with neither registers no command:

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

For an `EncryptedDeterministicStringFor[R]` column, `encrypt.CiphertextCandidatesFor[R]("user@example.com")` reads the cipher, the binding and its switch from where the column reads them: a bound marker answers a `gcm2` candidate per active key, followed by the `gcm1` ones while its binding admits unbound values. `Cipher.CiphertextCandidates` answers `gcm1` alone and finds no row a bound column wrote.

`CiphertextCandidates` returns `[][]byte` (not `[]string`) on purpose: the ciphertext marker carries `\0`
glue bytes, and bun inlines a `string` argument into the SQL text through a formatter that drops embedded NUL bytes — which would corrupt the right-hand side and break the match. As `[][]byte` each candidate is emitted as an `X'…'` binary literal, so every byte survives and the lookup compares equal to the stored
`EncryptedDeterministicString` column (whose `Value()` is likewise a binary `[]byte`).

> ⚠️ Deterministic mode **reveals plaintext equality** (equal values produce identical ciphertext). The
> nonce of an unbound column is keyed only by `(key, plaintext)`, so equal plaintext yields byte-identical ciphertext **across
> every unbound deterministic column and table under the same key**; a bound column's nonce takes its label, so equality shows within that column alone — an observer of the stored values can correlate
> equal values across rows and tables, not just within one column. Use it only on low-entropy lookup fields,
> never on secrets where cross-column equality must stay hidden. Use `EncryptedString` (random nonce) everywhere else.

## Bulk migration

`Migrator` (and the `melody:encrypt:database` CLI command) stream a table in keyset-paginated batches of `TableSpec.BatchSize` rows, a `bound.Count`, 500 when left out, at least 1, unlimited refused; a run outside the rule is refused before its first query, naming the field. `--batch` reads `0` as left out, and a negative value is refused naming the flag:

| Mode        | What it does                                                                            |
|-------------|-----------------------------------------------------------------------------------------|
| `encrypt`   | encrypt plaintext columns (idempotent — already-encrypted values are skipped)           |
| `reencrypt` | decrypt with whichever key wrote each value, re-encrypt under `--target-key` (rotation) |
| `decrypt`   | rewrite columns as plaintext                                                            |

```bash
melody melody:encrypt:database --table=users --primary-key=id --column=email --column=ssn --mode=encrypt
melody melody:encrypt:database --table=users --column=email --mode=reencrypt --target-key=v2
```

`decrypt` **stops** on a row whose decrypted value is itself a seal of this cipher — an application stored a ciphertext as data — and names the row in the error's `id`, leaving it sealed: written bare, the value would be read as a seal by a column type that still decrypts, and would hand back another row's secret. Rewrite or clear that value through the application, then re-run. With a `Cipher` implemented outside this package, `reencrypt` and a deterministic `encrypt` stop on such a row the same way, since its own doors may pass the seal through. The column types over such a cipher refuse the write of a marker-shaped value for the same reason. `encrypt` over a column that still holds plaintext cannot tell a ciphertext an application stored there before the column was encrypted from one it wrote itself, and skips both as already sealed: run it before untrusted writers can reach the column. Over a bound column it re-seals such a `gcm1` seal under the binding instead, the window [Binding a seal to its column](#binding-a-seal-to-its-column) describes.

A column `TableSpec.SealBindings` binds, `ColumnSealBinding{Label, RefuseUnbound}`, is read and written under its label. `encrypt` seals a value without the marker under the binding and the current key, re-seals a `gcm1` seal under it with the key and mode it carries, and leaves a bound seal as it is, so a second run changes nothing; `reencrypt` writes every value bound under `--target-key`, a `gcm1` seal under that key being re-sealed with its mode kept; `decrypt` opens under the binding. Every mode stops at the row of a seal of another binding, and, with `RefuseUnbound`, of a `gcm1` seal or a value without the marker, with no update; `decrypt` over a binding with `RefuseUnbound` is refused before the first query, since the column could not read the bare values it writes, so the switch comes off first; a column the spec leaves unbound stops at a bound seal with `ErrSealBound`, the message naming `TableSpec.SealBindings` and `--seal-binding`. A binding for a column the spec does not list, a label outside the grammar and any binding over a cipher implemented outside the package are refused before the first query. The command binds a `--column` whose `<table>.<column>` is a declared label by itself, the module handing each command the declarations of its compartment; `--seal-binding column=label` binds a column of a table spelled otherwise to a declared label, and a malformed pair, an unlisted column, a column named twice, an undeclared label and a label other than the one the column's own `<table>.<column>` declares are refused naming the flag before any query, as is a `--table` and `--column` differing from a declared label only by case. A command reads the declarations of its own compartment: a column whose marker names a context is migrated through `melody:encrypt:database:<name>`, even when the default cipher is the same instance.

```bash
melody melody:encrypt:database --table=customer_archive --column=iban --seal-binding=iban=customer.iban --mode=encrypt
```

> ⚠️ A `reencrypt` onto another key keeps each value's mode, so a searchable column stays searchable
> without the flag. Set `TableSpec.Deterministic = true` / `--deterministic` to CONVERT a random column to
> searchable while rotating; to convert a searchable column to random, run `reencrypt` onto the key it
> already carries without the flag. Over a bound column, a `gcm1` seal under that key is first re-sealed
> bound with its mode kept, so the conversion takes a second run.

## Register as a module

The module installs the ciphers and wires the `melody:encrypt:database` command, which like every Melody command has to be registered with the application:

```go
app.RegisterModule(encrypt.NewModule(encrypt.ModuleConfig{
    Cipher:   cipher,
    Database: database, // *bun.DB (MySQL dialect)
}))
```

This implements [`ServiceModule`](../../../../v4/application/contract/service_module.go), whose `RegisterServices` installs the ciphers and publishes `encrypt.ServiceCipher`, and [`CliModule`](../../../../v4/application/contract/cli_module.go), whose `RegisterCliCommands` builds the commands. A `DatabaseFactory` in place of `Database` resolves the database at the first command run, so a process in another mode never opens it. If you wire the application's `RegisterCliCommands` by hand instead, append the slice from `encrypt.Commands(cipher, encrypt.EncryptDatabaseCommandConfig{Database, DatabaseResolver, CommandName})`, or the one command `encrypt.NewEncryptDatabaseCommand` builds from the same arguments, exactly one of the two database fields set. The command runs on the cipher it is handed, but the columns read only what the module installs: register the module with the `Cipher` in that process as well.

A config the module cannot wire **panics** at registration, naming the field ([`module.go`](./module.go)), and nothing is installed:

- no `Cipher` and no `Contexts` — `encrypt module needs a cipher or at least one context`;
- a `Database` (or `DatabaseFactory`) with no `Cipher` — `encrypt module database needs a cipher`, judged first;
- both `Database` and `DatabaseFactory` set — `encrypt module received both a database and a database factory - set exactly one`;
- a prebuilt `Database`, the module's or a context's, not of the MySQL dialect — `encrypt module database requires a mysql dialect`; a factory's database is judged at the first command run;
- a context with an empty or repeated `Name`, with no `Cipher`, or with both a `Database` and a `DatabaseFactory`, each naming the context's index.

## Formatting boundaries

The column types redact their plaintext when `fmt` calls their `Format` method, which it does for the value itself under every verb, `%#v` included, but `%p` and `%w`: fmt prints the operand of either by reflection, the plaintext with it. It is not an access-control boundary: a value held in an unexported field of another struct is printed by reflection without calling any method, so `fmt.Sprintf("%+v", holder)` prints the plaintext, and an explicit `string(value)` conversion is the plaintext. Format the encrypted value itself, and do not log the structs that hold it or the key material.

## Testing / dev

`encrypt.NewFakeCipher()` is an identity cipher (no confidentiality) for tests and local development. Never install it in production.
