package encrypt

import (
    "context"
    "database/sql"
    "errors"
    "fmt"
    "strconv"
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
    if capacityErr := instance.ensureColumnCapacity(ctx, spec, ""); nil != capacityErr {
        return 0, instance.classifyRunError(spec, 0, "", capacityErr)
    }

    return instance.run(ctx, spec, instance.encryptTransform(spec))
}

func (instance *Migrator) MigrateReencrypt(ctx context.Context, spec TableSpec, targetKeyId string) (int, error) {
    if "" == targetKeyId {
        return 0, exception.NewError("migrate reencrypt needs the key id it will rotate to", map[string]any{"table": spec.Table}, nil)
    }

    if capacityErr := instance.ensureColumnCapacity(ctx, spec, targetKeyId); nil != capacityErr {
        return 0, instance.classifyRunError(spec, 0, "", capacityErr)
    }

    return instance.run(ctx, spec, instance.reencryptTransform(spec, targetKeyId))
}

/* MigrateDecrypt stops the run on a row whose stored value is of a seal format this build does not read, or whose marker body does not parse, whatever the cipher's Decrypt answers for it, and on a row whose decrypted value is itself marker-shaped: written bare, such a value would be read as a seal by a column type that still decrypts. */
func (instance *Migrator) MigrateDecrypt(ctx context.Context, spec TableSpec) (int, error) {
    return instance.run(ctx, spec, func(value string) (string, error) {
        if _, _, keyIdErr := keyIdOf(value); nil != keyIdErr {
            return "", keyIdErr
        }

        plaintext, decryptErr := instance.cipher.Decrypt(value)
        if nil != decryptErr {
            return "", decryptErr
        }

        if true == carriesMarkerFamily(plaintext) {
            return "", exception.NewError(
                "migrate decrypt found a stored value whose plaintext is itself an encrypted value; written bare it would be read as one, so the row is left sealed",
                map[string]any{"mode": "decrypt"},
                nil,
            )
        }

        return plaintext, nil
    })
}

func errSealStoredAsDataThroughForeignCipher(keyId string) error {
    return exception.NewError(
        "migrate found a stored value whose plaintext is itself an encrypted value, which a cipher implemented outside this package cannot re-seal as data; the row is left as it is",
        map[string]any{"keyId": keyId},
        nil,
    )
}

/* strict read side: a marker whose body does not decrypt stops the run instead of sealing over the only copy */
func (instance *Migrator) encryptTransform(spec TableSpec) func(string) (string, error) {
    sealer, isSealer := instance.cipher.(valueSealer)

    return func(value string) (string, error) {
        keyId, markerShaped, keyIdErr := keyIdOf(value)
        if nil != keyIdErr {
            return "", keyIdErr
        }

        if false == markerShaped {
            if false == spec.Deterministic {
                return instance.cipher.Encrypt(value)
            }

            return instance.cipher.EncryptDeterministic(value)
        }

        plaintext, decryptErr := instance.cipher.Decrypt(value)
        if nil != decryptErr {
            return "", exception.NewError(
                "migrate found a stored encrypted value that no longer decrypts; restore the key or repair the row before re-running",
                map[string]any{"keyId": keyId},
                decryptErr,
            )
        }

        if false == spec.Deterministic {
            return value, nil
        }

        if false == isSealer {
            if true == carriesMarkerFamily(plaintext) {
                return "", errSealStoredAsDataThroughForeignCipher(keyId)
            }

            return instance.cipher.EncryptDeterministicWithKeyId(plaintext, keyId)
        }

        stored, _ := sealer.authenticatedSeal(value)
        if true == stored.deterministic {
            return value, nil
        }

        return sealer.sealValueWithKeyId(plaintext, keyId, true)
    }
}

func (instance *Migrator) reencryptTransform(spec TableSpec, targetKeyId string) func(string) (string, error) {
    sealer, isSealer := instance.cipher.(valueSealer)

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

        if true == isSealer {
            if true == spec.Deterministic {
                return sealer.sealValueWithKeyId(plaintext, targetKeyId, true)
            }

            /* the value decrypted above, so a sealed one authenticates; an unsealed value reads as random and is sealed so */
            stored, _ := sealer.authenticatedSeal(value)
            if true == sameKey {
                if false == stored.deterministic {
                    return value, nil
                }

                return sealer.sealValueWithKeyId(plaintext, targetKeyId, false)
            }

            return sealer.sealValueWithKeyId(plaintext, targetKeyId, stored.deterministic)
        }

        if true == carriesMarkerFamily(plaintext) {
            return "", errSealStoredAsDataThroughForeignCipher(currentKeyId)
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

            return instance.cipher.EncryptWithKeyId(plaintext, targetKeyId)
        }

        /* a foreign cipher exposes no mode, so seal deterministically under the carried key: an equal seal is deterministic */
        if true == encrypted {
            storedDeterministic, deterministicErr := instance.cipher.EncryptDeterministicWithKeyId(plaintext, currentKeyId)
            if nil != deterministicErr {
                return "", deterministicErr
            }

            if value == storedDeterministic {
                return instance.cipher.EncryptDeterministicWithKeyId(plaintext, targetKeyId)
            }
        }

        return instance.cipher.EncryptWithKeyId(plaintext, targetKeyId)
    }
}

func (instance *Migrator) EnsureColumnCapacity(ctx context.Context, spec TableSpec) error {
    return instance.ensureColumnCapacity(ctx, spec, "")
}

func (instance *Migrator) EnsureColumnCapacityForReencrypt(ctx context.Context, spec TableSpec, targetKeyId string) error {
    if "" == targetKeyId {
        return exception.NewError("migrate reencrypt needs the key id it will rotate to", map[string]any{"table": spec.Table}, nil)
    }

    return instance.ensureColumnCapacity(ctx, spec, targetKeyId)
}

func (instance *Migrator) ensureColumnCapacity(ctx context.Context, spec TableSpec, targetKeyId string) error {
    if "" == spec.Table || 0 == len(spec.Columns) {
        return exception.NewError("migrate spec needs a table and at least one column", nil, nil)
    }

    charsetByColumn, charsetErr := instance.columnCharsets(ctx, spec.Table)
    if nil != charsetErr {
        return charsetErr
    }

    for _, column := range spec.Columns {
        longest, hasUnsealed, longestErr := instance.longestUnsealedLength(ctx, spec.Table, column, charsetByColumn[column])
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
            withoutKeyId, hasSealed, withoutKeyIdErr := instance.longestSealedLengthWithoutKeyId(ctx, spec.Table, column, charsetByColumn[column])
            if nil != withoutKeyIdErr {
                return withoutKeyIdErr
            }

            if true == hasSealed && required < withoutKeyId+len(targetKeyId) {
                required = withoutKeyId + len(targetKeyId)
            }
        }

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

func (instance *Migrator) longestUnsealedLength(ctx context.Context, table string, column string, charset string) (int, bool, error) {
    /* the cast keeps a case-insensitive collation from reading a plaintext that merely looks like the marker as already sealed; the probe leaves out the whole marker family, since a value of a format this build does not read stops the run at its row instead of being sized as plaintext, and markerFamily carries no LIKE metacharacter, so the pattern needs no escaping */
    probeSql := fmt.Sprintf(
        "SELECT MAX(LENGTH(%s)) FROM %s WHERE %s NOT LIKE ?",
        columnBytes(column, charset),
        quoteIdentifier(table),
        binaryComparison(column, charset),
    )

    var longest sql.NullInt64

    scanErr := instance.db.DB.QueryRowContext(ctx, probeSql, markerFamily+"%").Scan(&longest)
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

func (instance *Migrator) longestSealedLengthWithoutKeyId(ctx context.Context, table string, column string, charset string) (int, bool, error) {
    probeSql := fmt.Sprintf(
        "SELECT MAX(LENGTH(%s) - LENGTH(SUBSTRING_INDEX(SUBSTRING(%s, ?), ':', 1))) FROM %s WHERE %s LIKE ?",
        columnBytes(column, charset),
        binaryComparison(column, charset),
        quoteIdentifier(table),
        binaryComparison(column, charset),
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

/* a VARCHAR(n) limits characters, so CHARACTER_MAXIMUM_LENGTH; the TEXT family limits bytes, so CHARACTER_OCTET_LENGTH */
func (instance *Migrator) columnWidth(ctx context.Context, table string, column string) (int, error) {
    var dataType sql.NullString
    var width sql.NullInt64
    var octetWidth sql.NullInt64

    scanErr := instance.db.DB.QueryRowContext(
        ctx,
        "SELECT DATA_TYPE, CHARACTER_MAXIMUM_LENGTH, CHARACTER_OCTET_LENGTH FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?",
        table,
        column,
    ).Scan(&dataType, &width, &octetWidth)

    if nil != scanErr {
        if true == errors.Is(scanErr, sql.ErrNoRows) {
            return 0, exception.NewError("migrate target column does not exist", map[string]any{"table": table, "column": column}, nil)
        }

        return 0, exception.NewError("migrate column width lookup failed", map[string]any{"table": table, "column": column}, scanErr)
    }

    if false == width.Valid {
        return 0, exception.NewError("migrate target column is not a character column", map[string]any{"table": table, "column": column}, nil)
    }

    switch strings.ToLower(dataType.String) {
    case "tinytext", "text", "mediumtext", "longtext":
        if true == octetWidth.Valid {
            return clampWidthToInt(octetWidth.Int64), nil
        }
    }

    return clampWidthToInt(width.Int64), nil
}

func clampWidthToInt(width int64) int {
    maxInt := int64(^uint(0) >> 1)
    if width > maxInt {
        return int(maxInt)
    }

    return int(width)
}

const sealedProbeFiller = "a"

const (
    base64GroupPlaintextBytes    = 3
    base64GroupEncodedCharacters = 4
)

func (instance *Migrator) sealedProbeLength(plaintextByteLength int, keyId string) (int, error) {
    if 0 > plaintextByteLength {
        return 0, exception.NewError("migrate cannot measure a negative plaintext width", map[string]any{"plaintextByteLength": plaintextByteLength}, nil)
    }

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

    charsetByColumn, charsetErr := instance.columnCharsets(ctx, spec.Table)
    if nil != charsetErr {
        return 0, instance.classifyRunError(spec, 0, "", charsetErr)
    }

    guardByColumn := make([]string, 0, len(spec.Columns))
    for _, column := range spec.Columns {
        guardByColumn = append(guardByColumn, binaryComparison(column, charsetByColumn[column])+" = ?")
    }

    var cursor any
    hasCursor := false
    processed := 0
    skippedCount := 0
    skippedSample := make([]string, 0, skippedSampleSize)

    for {
        var rows *sql.Rows
        var queryErr error

        /* the first page must not be keyset-filtered: WHERE pk > '' coerces to pk > 0 and would skip zero or negative keys */
        if false == hasCursor {
            rows, queryErr = instance.db.DB.QueryContext(ctx, firstSelectSql, batchSize)
        } else {
            rows, queryErr = instance.db.DB.QueryContext(ctx, nextSelectSql, cursor, batchSize)
        }
        if nil != queryErr {
            return processed, instance.classifyRunError(spec, processed, "migrate select failed", queryErr)
        }

        batch, scanErr := scanMigrateRows(rows, len(spec.Columns))
        rows.Close()
        if nil != scanErr {
            return processed, instance.classifyRunError(spec, processed, "", scanErr)
        }

        if 0 == len(batch) {
            break
        }

        for _, row := range batch {
            cursor = row.primaryKeyArgument
            hasCursor = true

            applied, updateErr := instance.applyRow(ctx, spec, guardByColumn, row, transform)
            if nil != updateErr {
                return processed, instance.classifyRunError(spec, processed, "", updateErr)
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

func (instance *Migrator) classifyRunError(spec TableSpec, processed int, message string, cause error) error {
    if true == exception.ChainIs(cause, context.Canceled) || true == exception.ChainIs(cause, context.DeadlineExceeded) {
        return exception.NewError(
            "migrate was interrupted by the caller's context; the run is incomplete and a re-run picks up the remaining rows",
            map[string]any{"table": spec.Table, "processed": processed},
            cause,
        )
    }

    if "" == message {
        return cause
    }

    return exception.NewError(message, map[string]any{"table": spec.Table}, cause)
}

func (instance *Migrator) applyRow(ctx context.Context, spec TableSpec, guardByColumn []string, row migrateRow, transform func(string) (string, error)) (bool, error) {
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
            return false, exception.NewError("migrate transform failed", map[string]any{"table": spec.Table, "column": column, "id": row.primaryKey}, transformErr)
        }

        if transformed == value.String {
            continue
        }

        assignments = append(assignments, quoteIdentifier(column)+" = ?")
        setArguments = append(setArguments, transformed)
        valuePredicates = append(valuePredicates, guardByColumn[index])
        valueArguments = append(valueArguments, value.String)
    }

    if 0 == len(assignments) {
        return true, nil
    }

    arguments := make([]any, 0, len(setArguments)+1+len(valueArguments))
    arguments = append(arguments, setArguments...)
    arguments = append(arguments, row.primaryKeyArgument)
    arguments = append(arguments, valueArguments...)

    /* guard on the value read for this row, byte-exact, so a concurrent write since the select is not silently reverted */
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

    /* a driver that cannot report the affected count is treated as applied: a false skip would fail a completed run */
    affected, affectedErr := result.RowsAffected()
    if nil != affectedErr {
        return true, nil
    }

    return 0 < affected, nil
}

type migrateRow struct {
    primaryKey string
    /* the parsed integer on an integer column: MySQL compares a string parameter as doubles, which collapse keys above 2^53 */
    primaryKeyArgument any
    values             []sql.NullString
}

func scanMigrateRows(rows *sql.Rows, columnCount int) ([]migrateRow, error) {
    var batch []migrateRow

    primaryKeyTypeName := ""
    if columnTypes, columnTypesErr := rows.ColumnTypes(); nil == columnTypesErr && 0 < len(columnTypes) {
        primaryKeyTypeName = columnTypes[0].DatabaseTypeName()
    }

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

        /* a NULL cursor cannot drive the keyset: rendered as "", the next `pk > ''` coerces to `pk > 0` on an integer column */
        if false == primaryKey.Valid {
            return nil, exception.NewError("migrate read a NULL primary key value; the --primary-key column cannot serve as the pagination cursor", nil, nil)
        }

        batch = append(batch, migrateRow{
            primaryKey:         primaryKey.String,
            primaryKeyArgument: typedPrimaryKeyArgument(primaryKey.String, primaryKeyTypeName),
            values:             values,
        })
    }

    if rowsErr := rows.Err(); nil != rowsErr {
        return nil, exception.NewError("migrate row iteration failed", nil, rowsErr)
    }

    return batch, nil
}

func typedPrimaryKeyArgument(primaryKey string, databaseTypeName string) any {
    if false == isIntegerDatabaseType(databaseTypeName) {
        return primaryKey
    }

    if signedValue, signedErr := strconv.ParseInt(primaryKey, 10, 64); nil == signedErr {
        return signedValue
    }

    if unsignedValue, unsignedErr := strconv.ParseUint(primaryKey, 10, 64); nil == unsignedErr {
        return unsignedValue
    }

    return primaryKey
}

func isIntegerDatabaseType(databaseTypeName string) bool {
    normalized := strings.ToUpper(strings.TrimSpace(databaseTypeName))
    normalized = strings.TrimPrefix(normalized, "UNSIGNED ")
    normalized = strings.TrimSuffix(normalized, " UNSIGNED")

    switch normalized {
    case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "INT2", "INT4", "INT8":
        return true
    default:
        return false
    }
}

/* the guard compares bytes; CAST because COLLATE utf8mb4_bin is rejected on latin1 and the BINARY operator is deprecated */
func binaryComparison(column string, charset string) string {
    return "CAST(" + columnBytes(column, charset) + " AS BINARY)"
}

func columnBytes(column string, charset string) string {
    if "" == charset || "utf8mb4" == strings.ToLower(charset) || "binary" == strings.ToLower(charset) {
        return quoteIdentifier(column)
    }

    return "CONVERT(" + quoteIdentifier(column) + " USING utf8mb4)"
}

func (instance *Migrator) columnCharsets(ctx context.Context, table string) (map[string]string, error) {
    rows, queryErr := instance.db.DB.QueryContext(
        ctx,
        "SELECT COLUMN_NAME, CHARACTER_SET_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
        table,
    )
    if nil != queryErr {
        return nil, exception.NewError("migrate column charset lookup failed", map[string]any{"table": table}, queryErr)
    }
    defer rows.Close()

    charsetByColumn := map[string]string{}
    for rows.Next() {
        var column string
        var charset sql.NullString
        if scanErr := rows.Scan(&column, &charset); nil != scanErr {
            return nil, exception.NewError("migrate column charset lookup failed", map[string]any{"table": table}, scanErr)
        }

        charsetByColumn[column] = charset.String
    }

    if rowsErr := rows.Err(); nil != rowsErr {
        return nil, exception.NewError("migrate column charset lookup failed", map[string]any{"table": table}, rowsErr)
    }

    return charsetByColumn, nil
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
