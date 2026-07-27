package encrypt

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
)

const defaultMigrateBatchSize = 500

type TableSpec struct {
    Table      string
    PrimaryKey string
    Columns    []string
    BatchSize  int

    Deterministic bool
}

type Migrator struct {
    db     *bun.DB
    cipher Cipher
}

func NewMigrator(db *bun.DB, cipher Cipher) *Migrator {
    if nil == db {
        exception.Panic(exception.NewError("migrator database is nil", nil, nil))
    }

    if nil == cipher {
        exception.Panic(exception.NewError("migrator cipher is nil", nil, nil))
    }

    if dialect.MySQL != db.Dialect().Name() {
        exception.Panic(exception.NewError(
            "migrator requires a mysql dialect",
            map[string]any{"dialect": db.Dialect().Name().String()},
            nil,
        ))
    }

    return &Migrator{db: db, cipher: cipher}
}

func (instance *Migrator) MigrateEncrypt(ctx context.Context, spec TableSpec) (int, error) {
    return instance.run(ctx, spec, instance.encryptTransform(spec))
}

func (instance *Migrator) MigrateReencrypt(ctx context.Context, spec TableSpec, targetKeyId string) (int, error) {
    return instance.run(ctx, spec, instance.reencryptTransform(spec, targetKeyId))
}

func (instance *Migrator) MigrateDecrypt(ctx context.Context, spec TableSpec) (int, error) {
    return instance.run(ctx, spec, func(value string) (string, error) {
        return instance.cipher.Decrypt(value)
    })
}

func (instance *Migrator) encryptTransform(spec TableSpec) func(string) (string, error) {
    return func(value string) (string, error) {
        if false == spec.Deterministic {
            return instance.cipher.Encrypt(value)
        }

        /* a value already sealed with a random nonce authenticates, so the deterministic seal would pass it through untouched: the column would stay randomized — every CiphertextCandidates equality lookup on it finding nothing — while the run reported the row as processed. Convert it in place, under the key it already carries so mode=encrypt never doubles as a key rotation. A value that authenticates under no key in the set is ordinary plaintext (marker-shaped or not) and is sealed under the current key, as the cipher does. A value carrying the marker whose payload no longer parses is a different case entirely — that shape is only ever produced by a truncated write, never by an application — and it stops the run rather than being sealed on top of the damage. */
        keyId, markerShaped, keyIdErr := keyIdOf(value)
        if nil != keyIdErr {
            return "", keyIdErr
        }

        if false == markerShaped {
            return instance.cipher.EncryptDeterministic(value)
        }

        plaintext, decryptErr := instance.cipher.Decrypt(value)
        if nil != decryptErr {
            return instance.cipher.EncryptDeterministic(value)
        }

        return instance.cipher.EncryptDeterministicWithKeyId(plaintext, keyId)
    }
}

func (instance *Migrator) reencryptTransform(spec TableSpec, targetKeyId string) func(string) (string, error) {
    return func(value string) (string, error) {
        currentKeyId, encrypted, keyIdErr := keyIdOf(value)
        if nil != keyIdErr {
            return "", keyIdErr
        }

        sameKey := true == encrypted && currentKeyId == targetKeyId

        plaintext, decryptErr := instance.cipher.Decrypt(value)
        if nil != decryptErr {
            return "", decryptErr
        }

        if true == spec.Deterministic {
            return instance.cipher.EncryptDeterministicWithKeyId(plaintext, targetKeyId)
        }

        if true == sameKey {
            deterministic, deterministicErr := instance.cipher.EncryptDeterministicWithKeyId(plaintext, targetKeyId)
            if nil != deterministicErr {
                return "", deterministicErr
            }

            if value != deterministic {
                return value, nil
            }
        }

        return instance.cipher.EncryptWithKeyId(plaintext, targetKeyId)
    }
}

/* EnsureColumnCapacity refuses a migration whose ciphertext could not fit back into the column it is read from.

Sealing expands. A value comes back wrapped in a marker, a key id, a nonce and an authentication tag, base64 encoded — roughly 1.34 characters per plaintext byte plus 52, so a VARCHAR(255) stops being able to hold its own contents at 153 bytes of plaintext. Under the strict sql_mode MySQL ships with, the UPDATE that overflows fails and the run stops with the row intact. Under sql_mode='' it SUCCEEDS with a warning: the column keeps a truncated ciphertext, the row is counted as migrated and the command exits zero. The plaintext is gone at that point and a truncated ciphertext can never authenticate again, so nothing is recoverable from it — and nothing in this module pins sql_mode.

The requirement is therefore established before the first row is written, by sealing a probe as long as the longest value the column actually holds with the very cipher the run will use, so it cannot drift from what seal produces. Values already sealed are left out of that measurement: the transform hands them back unchanged and they are already stored in the column, so they fit by definition — measuring them would make a second run over a migrated column demand a column wide enough to seal the ciphertext. A column with nothing left to seal is not measured at all.

What it cannot promise is a value written after it ran: a longer one inserted concurrently is still bounded only by the server's sql_mode. This catches the sizing mistake before it is applied to a whole table; it does not make a non-strict server safe.

This is the check for a run that only seals what is not sealed yet. A key rotation rewrites the sealed values too and grows them for a different reason, which EnsureColumnCapacityForReencrypt covers. */
func (instance *Migrator) EnsureColumnCapacity(ctx context.Context, spec TableSpec) error {
    return instance.ensureColumnCapacity(ctx, spec, "")
}

/* EnsureColumnCapacityForReencrypt is EnsureColumnCapacity for a key rotation, which grows values the plain check has no reason to look at.

A rotation rewrites what is ALREADY sealed, and re-sealing the same plaintext under a different key id changes the stored length by exactly the difference between the two key ids — everything else a seal emits (the marker, the nonce, the tag, the base64 padding) depends only on the plaintext. So a rotation from "v1" to "2026-07-rotated" adds thirteen characters to every single row, and a column sized exactly to the ciphertext it holds is then too narrow for all of it at once. On a non-strict server that is the whole column truncated to a ciphertext that can never authenticate again, one row at a time, with the run reporting success.

Because the growth is the same for every row sealed under the same key id, the widest result is found from the widest stored value rather than by reading any of them: one aggregate over the column is enough, and no row is fetched. The plaintext still left in the column is measured under the target key id too, since that is the key the rotation will seal it with. */
func (instance *Migrator) EnsureColumnCapacityForReencrypt(ctx context.Context, spec TableSpec, targetKeyId string) error {
    if "" == targetKeyId {
        return exception.NewError("migrate reencrypt needs the key id it will rotate to", map[string]any{"table": spec.Table}, nil)
    }

    return instance.ensureColumnCapacity(ctx, spec, targetKeyId)
}

/* ensureColumnCapacity is both checks: an empty targetKeyId means the run only seals what is not sealed yet, and a non-empty one means it also rewrites what is. */
func (instance *Migrator) ensureColumnCapacity(ctx context.Context, spec TableSpec, targetKeyId string) error {
    if "" == spec.Table || 0 == len(spec.Columns) {
        return exception.NewError("migrate spec needs a table and at least one column", nil, nil)
    }

    for _, column := range spec.Columns {
        longest, hasUnsealed, longestErr := instance.longestUnsealedLength(ctx, spec.Table, column)
        if nil != longestErr {
            return longestErr
        }

        required := 0

        if true == hasUnsealed {
            sealedLength, sealedErr := instance.sealedProbeLength(longest, targetKeyId)
            if nil != sealedErr {
                return sealedErr
            }

            required = sealedLength
        }

        if "" != targetKeyId {
            withoutKeyId, hasSealed, withoutKeyIdErr := instance.longestSealedLengthWithoutKeyId(ctx, spec.Table, column)
            if nil != withoutKeyIdErr {
                return withoutKeyIdErr
            }

            if true == hasSealed && required < withoutKeyId+len(targetKeyId) {
                required = withoutKeyId + len(targetKeyId)
            }
        }

        /* a column with nothing to write is not measured against at all, so a run over an empty table never has to explain a width */
        if 0 == required {
            continue
        }

        width, widthErr := instance.columnWidth(ctx, spec.Table, column)
        if nil != widthErr {
            return widthErr
        }

        if width < required {
            diagnostic := map[string]any{
                "table":         spec.Table,
                "column":        column,
                "width":         width,
                "requiredWidth": required,
            }

            if true == hasUnsealed {
                diagnostic["longestPlaintextBytes"] = longest
            }

            if "" != targetKeyId {
                diagnostic["targetKeyId"] = targetKeyId
            }

            return exception.NewError(
                "migrate target column is too narrow to hold the encrypted value; widen it before running",
                diagnostic,
                nil,
            )
        }
    }

    return nil
}

/* longestUnsealedLength reports the length in BYTES of the longest value in the column that is not already sealed, and whether any such value exists. Bytes rather than characters because that is the unit the seal expands over: a multi-byte character costs the ciphertext its full encoded length. */
func (instance *Migrator) longestUnsealedLength(ctx context.Context, table string, column string) (int, bool, error) {
    /* the cast keeps a case-insensitive collation from reading a plaintext that merely looks like the marker as already sealed; markerPrefix carries no LIKE metacharacter, so the pattern needs no escaping */
    probeSql := fmt.Sprintf(
        "SELECT MAX(LENGTH(%s)) FROM %s WHERE %s NOT LIKE ?",
        quoteIdentifier(column),
        quoteIdentifier(table),
        binaryComparison(column),
    )

    var longest sql.NullInt64

    scanErr := instance.db.DB.QueryRowContext(ctx, probeSql, markerPrefix+"%").Scan(&longest)
    if nil != scanErr {
        return 0, false, exception.NewError(
            "migrate could not measure the widest value of the target column",
            map[string]any{"table": table, "column": column},
            scanErr,
        )
    }

    if false == longest.Valid {
        return 0, false, nil
    }

    return int(longest.Int64), true, nil
}

/* longestSealedLengthWithoutKeyId reports the length in BYTES of the widest value already sealed in the column with the key id it carries taken back out of it, and whether any sealed value exists at all. Adding the length of another key id to that gives what the same value occupies once it has been rotated onto that key, which is what makes a rotation measurable without reading a single row.

The key id is the run of bytes between the marker and the first colon: neither the key id alphabet nor base64 contains one, so the first colon is the separator seal wrote and no other. The column is read as bytes throughout — LENGTH counts bytes, so the substring has to be taken over bytes as well, and a value with a multi-byte character in front of the colon would otherwise be measured against the wrong offset.

A stored value that is not shaped like a seal at all yields a longer "key id" and therefore a shorter remainder, which understates the requirement — but such a value stops the rotation the moment its row is read, before anything is written over it. */
func (instance *Migrator) longestSealedLengthWithoutKeyId(ctx context.Context, table string, column string) (int, bool, error) {
    probeSql := fmt.Sprintf(
        "SELECT MAX(LENGTH(%s) - LENGTH(SUBSTRING_INDEX(SUBSTRING(%s, ?), ':', 1))) FROM %s WHERE %s LIKE ?",
        quoteIdentifier(column),
        binaryComparison(column),
        quoteIdentifier(table),
        binaryComparison(column),
    )

    var longest sql.NullInt64

    scanErr := instance.db.DB.QueryRowContext(ctx, probeSql, len(markerPrefix)+1, markerPrefix+"%").Scan(&longest)
    if nil != scanErr {
        return 0, false, exception.NewError(
            "migrate could not measure the widest encrypted value of the target column",
            map[string]any{"table": table, "column": column},
            scanErr,
        )
    }

    if false == longest.Valid {
        return 0, false, nil
    }

    return int(longest.Int64), true, nil
}

func (instance *Migrator) columnWidth(ctx context.Context, table string, column string) (int, error) {
    var width sql.NullInt64

    scanErr := instance.db.DB.QueryRowContext(
        ctx,
        "SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?",
        table,
        column,
    ).Scan(&width)

    if nil != scanErr {
        if true == errors.Is(scanErr, sql.ErrNoRows) {
            return 0, exception.NewError("migrate target column does not exist", map[string]any{"table": table, "column": column}, nil)
        }

        return 0, exception.NewError("migrate column width lookup failed", map[string]any{"table": table, "column": column}, scanErr)
    }

    /* a column with no character length is not a string column at all (an integer, a date, a timestamp), so no ciphertext can be written to it at any width */
    if false == width.Valid {
        return 0, exception.NewError("migrate target column is not a character column", map[string]any{"table": table, "column": column}, nil)
    }

    return int(width.Int64), nil
}

/* sealedProbeFiller is one ASCII byte, so a probe of n of them is exactly n plaintext bytes. */
const sealedProbeFiller = "a"

/* base64GroupPlaintextBytes and base64GroupEncodedCharacters are the invariant that lets a width be computed instead of allocated: raw-standard base64 turns every three input bytes into exactly four characters, and the seal's base64 covers a buffer that grows one byte per plaintext byte. */
const (
    base64GroupPlaintextBytes    = 3
    base64GroupEncodedCharacters = 4
)

/* sealedProbeLength measures what the cipher produces for a plaintext of the given byte length. Measuring beats computing: the seal's marker, key id, nonce, tag and base64 padding all feed the width, and a probe through the live cipher stays correct through any of them changing.

The probe is sealed under the key the run will actually use, which is not the current one when a rotation names another: a key id is part of what is stored, so measuring under a shorter one would report a width the run then overflows. An empty key id means the run seals under the current key, as an ordinary encryption does.

Everything a seal emits is ASCII, so the byte count it returns is also a character count and can be compared with a column width straight away. */
func (instance *Migrator) sealedProbeLength(plaintextByteLength int, keyId string) (int, error) {
    if 0 > plaintextByteLength {
        return 0, exception.NewError("migrate cannot measure a negative plaintext width", map[string]any{"plaintextByteLength": plaintextByteLength}, nil)
    }

    /* the probe is at most two bytes long whatever the column holds, and the rest is arithmetic that is exact rather than approximate. A seal is a fixed prefix followed by the raw-standard base64 of a buffer that grows one byte per plaintext byte, and raw-standard base64 encodes every three input bytes as exactly four characters: EncodedLen(x+3) == EncodedLen(x)+4 in integer arithmetic, with no rounding anywhere. So the width for n bytes is the width for n mod 3 bytes plus four for every whole group of three beyond it.

    Sealing n bytes to find out was what this did, and it is unusable at the widths it is asked about: `longest` comes from SELECT MAX(LENGTH(col)), so a 64 MiB row cost some 320 MB resident to measure, and a LONGTEXT may hold 4 GiB — a measurement that costs more memory than the batched migration it is measuring FOR, and an out-of-memory kill is not something the caller can recover from. Measuring the remainder through the live cipher keeps what measuring was for: the marker, the key id, the nonce, the tag and the base64 alphabet are still read off a real seal rather than assumed here, so any of them changing is still followed. */
    remainderLength := plaintextByteLength % base64GroupPlaintextBytes
    wholeGroupCount := (plaintextByteLength - remainderLength) / base64GroupPlaintextBytes

    probe := strings.Repeat(sealedProbeFiller, remainderLength)

    var sealed string
    var sealErr error

    if "" == keyId {
        sealed, sealErr = instance.cipher.Encrypt(probe)
    } else {
        sealed, sealErr = instance.cipher.EncryptWithKeyId(probe, keyId)
    }

    if nil != sealErr {
        return 0, exception.NewError("migrate could not measure the encrypted width", map[string]any{"keyId": keyId}, sealErr)
    }

    return len(sealed) + wholeGroupCount*base64GroupEncodedCharacters, nil
}

const skippedSampleSize = 10

func (instance *Migrator) run(ctx context.Context, spec TableSpec, transform func(string) (string, error)) (int, error) {
    if "" == spec.Table || "" == spec.PrimaryKey || 0 == len(spec.Columns) {
        return 0, exception.NewError("migrate spec needs a table, primary key and at least one column", nil, nil)
    }

    batchSize := spec.BatchSize
    if 0 >= batchSize {
        batchSize = defaultMigrateBatchSize
    }

    selectColumns := append([]string{spec.PrimaryKey}, spec.Columns...)
    selectClause := strings.Join(quoteIdentifiers(selectColumns), ", ")
    firstSelectSql := fmt.Sprintf(
        "SELECT %s FROM %s ORDER BY %s ASC LIMIT ?",
        selectClause,
        quoteIdentifier(spec.Table),
        quoteIdentifier(spec.PrimaryKey),
    )
    nextSelectSql := fmt.Sprintf(
        "SELECT %s FROM %s WHERE %s > ? ORDER BY %s ASC LIMIT ?",
        selectClause,
        quoteIdentifier(spec.Table),
        quoteIdentifier(spec.PrimaryKey),
        quoteIdentifier(spec.PrimaryKey),
    )

    var cursor string
    hasCursor := false
    processed := 0
    skippedCount := 0
    skippedSample := make([]string, 0, skippedSampleSize)

    for {
        var rows *sql.Rows
        var queryErr error

        /* @important the first page must not be keyset-filtered: WHERE pk > '' coerces to pk > 0 on an integer key and would silently skip rows whose primary key is zero or negative. */
        if false == hasCursor {
            rows, queryErr = instance.db.DB.QueryContext(ctx, firstSelectSql, batchSize)
        } else {
            rows, queryErr = instance.db.DB.QueryContext(ctx, nextSelectSql, cursor, batchSize)
        }
        if nil != queryErr {
            return processed, exception.NewError("migrate select failed", map[string]any{"table": spec.Table}, queryErr)
        }

        batch, scanErr := scanMigrateRows(rows, len(spec.Columns))
        rows.Close()
        if nil != scanErr {
            return processed, scanErr
        }

        if 0 == len(batch) {
            break
        }

        for _, row := range batch {
            cursor = row.primaryKey
            hasCursor = true

            applied, updateErr := instance.applyRow(ctx, spec, row, transform)
            if nil != updateErr {
                return processed, updateErr
            }

            if false == applied {
                skippedCount++
                if skippedSampleSize > len(skippedSample) {
                    skippedSample = append(skippedSample, row.primaryKey)
                }

                continue
            }

            processed++
        }

        if len(batch) < batchSize {
            break
        }
    }

    /* @important a guarded update that matched no row left the column in its original state, so reporting success would let a deployment gated on the exit code proceed over values that were never migrated; the run is reported as incomplete and a re-run picks the rows up under their new values */
    if 0 < skippedCount {
        return processed, exception.NewError(
            "migrate left rows untouched because they changed under the run; re-run to pick them up",
            map[string]any{
                "table":             spec.Table,
                "skippedCount":      skippedCount,
                "skippedPrimaryKey": skippedSample,
            },
            nil,
        )
    }

    return processed, nil
}

func (instance *Migrator) applyRow(ctx context.Context, spec TableSpec, row migrateRow, transform func(string) (string, error)) (bool, error) {
    assignments := make([]string, 0, len(spec.Columns))
    setArguments := make([]any, 0, len(spec.Columns))
    valuePredicates := make([]string, 0, len(spec.Columns))
    valueArguments := make([]any, 0, len(spec.Columns))

    for index, column := range spec.Columns {
        value := row.values[index]
        if false == value.Valid {
            continue
        }

        transformed, transformErr := transform(value.String)
        if nil != transformErr {
            return false, exception.NewError("migrate transform failed", map[string]any{"table": spec.Table, "column": column}, transformErr)
        }

        if transformed == value.String {
            continue
        }

        assignments = append(assignments, quoteIdentifier(column)+" = ?")
        setArguments = append(setArguments, transformed)
        valuePredicates = append(valuePredicates, binaryComparison(column)+" = ?")
        valueArguments = append(valueArguments, value.String)
    }

    if 0 == len(assignments) {
        return true, nil
    }

    arguments := make([]any, 0, len(setArguments)+1+len(valueArguments))
    arguments = append(arguments, setArguments...)
    arguments = append(arguments, row.primaryKey)
    arguments = append(arguments, valueArguments...)

    /* @important guard each assignment on the value read for this row so a concurrent application write between the select and this update is not silently reverted; a row that changed under us matches zero rows and is re-encrypted on the next run. The comparison is byte-exact, which is what makes the guard hold at all — see binaryComparison. */
    whereClause := quoteIdentifier(spec.PrimaryKey) + " = ?"
    for _, predicate := range valuePredicates {
        whereClause += " AND " + predicate
    }

    updateSql := fmt.Sprintf(
        "UPDATE %s SET %s WHERE %s",
        quoteIdentifier(spec.Table),
        strings.Join(assignments, ", "),
        whereClause,
    )

    result, execErr := instance.db.DB.ExecContext(ctx, updateSql, arguments...)
    if nil != execErr {
        return false, exception.NewError("migrate update failed", map[string]any{"table": spec.Table, "id": row.primaryKey}, execErr)
    }

    /* @important a driver that cannot report the affected count is treated as having applied the update: reporting a false skip would fail a run that in fact completed */
    affected, affectedErr := result.RowsAffected()
    if nil != affectedErr {
        return true, nil
    }

    return 0 < affected, nil
}

type migrateRow struct {
    primaryKey string
    values     []sql.NullString
}

func scanMigrateRows(rows *sql.Rows, columnCount int) ([]migrateRow, error) {
    var batch []migrateRow

    for rows.Next() {
        primaryKey := sql.NullString{}
        values := make([]sql.NullString, columnCount)

        targets := make([]any, 0, columnCount+1)
        targets = append(targets, &primaryKey)
        for index := range values {
            targets = append(targets, &values[index])
        }

        if scanErr := rows.Scan(targets...); nil != scanErr {
            return nil, exception.NewError("migrate row scan failed", nil, scanErr)
        }

        batch = append(batch, migrateRow{primaryKey: primaryKey.String, values: values})
    }

    if rowsErr := rows.Err(); nil != rowsErr {
        return nil, exception.NewError("migrate row iteration failed", nil, rowsErr)
    }

    return batch, nil
}

/* binaryComparison renders a column so the pre-image guard compares bytes rather than characters.

A bare `col = ?` is evaluated under the COLUMN's collation, and every collation the migrator will meet in practice equates values the guard exists to tell apart. Under MySQL 8's default utf8mb4_0900_ai_ci a concurrent write that only changed casing — a normalisation of an address to lower case, the single most likely write to race a bulk encryption — still matches the value that was read, so the update applies, that write is destroyed, and the stored ciphertext decrypts to the casing the application had already replaced. Under any PAD SPACE collation (utf8mb4_general_ci and the whole 5.7 family) a write that only added or removed trailing whitespace slips through the same way. Neither is counted as skipped, so the run reports success and the loss is silent.

Casting to BINARY drops the collation out of the comparison entirely: bytes must match, trailing spaces included, whatever charset the column carries — which `COLLATE utf8mb4_bin` cannot promise, since it is rejected outright on a latin1 column. The cast form is used rather than the BINARY operator, which MySQL has deprecated. The primary key predicate still drives the row lookup, so making this one predicate non-indexable costs nothing. */
func binaryComparison(column string) string {
    return "CAST(" + quoteIdentifier(column) + " AS BINARY)"
}

func quoteIdentifier(identifier string) string {
    return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func quoteIdentifiers(identifiers []string) []string {
    quoted := make([]string, 0, len(identifiers))
    for _, identifier := range identifiers {
        quoted = append(quoted, quoteIdentifier(identifier))
    }

    return quoted
}
