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

/* MigrateEncrypt runs EnsureColumnCapacity before modifying rows. Call EnsureColumnCapacity separately for a preflight without migration. */
func (instance *Migrator) MigrateEncrypt(ctx context.Context, spec TableSpec) (int, error) {
    if capacityErr := instance.ensureColumnCapacity(ctx, spec, ""); nil != capacityErr {
        return 0, capacityErr
    }

    return instance.run(ctx, spec, instance.encryptTransform(spec))
}

/* MigrateReencrypt runs its own EnsureColumnCapacityForReencrypt first, for the reason on MigrateEncrypt. */
func (instance *Migrator) MigrateReencrypt(ctx context.Context, spec TableSpec, targetKeyId string) (int, error) {
    if "" == targetKeyId {
        return 0, exception.NewError("migrate reencrypt needs the key id it will rotate to", map[string]any{"table": spec.Table}, nil)
    }

    if capacityErr := instance.ensureColumnCapacity(ctx, spec, targetKeyId); nil != capacityErr {
        return 0, capacityErr
    }

    return instance.run(ctx, spec, instance.reencryptTransform(spec, targetKeyId))
}

func (instance *Migrator) MigrateDecrypt(ctx context.Context, spec TableSpec) (int, error) {
    return instance.run(ctx, spec, func(value string) (string, error) {
        return instance.cipher.Decrypt(value)
    })
}

func (instance *Migrator) encryptTransform(spec TableSpec) func(string) (string, error) {
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

/* EnsureColumnCapacity checks that currently unsealed values will fit after encryption before rewriting any row. It measures the longest eligible value and seals a probe of that size with the migration's cipher, so the required capacity includes the actual envelope and encoding overhead.

   Already-sealed values are excluded because the transform preserves them; malformed sealed values are rejected by the transform. A column with nothing left to seal needs no capacity measurement. Key rotation requires EnsureColumnCapacityForReencrypt instead, because it rewrites existing ciphertext too.

   This check is a preflight, not a concurrency guarantee. A longer value inserted afterwards can still overflow. MySQL without strict sql_mode may truncate ciphertext and report success, making it undecryptable; production migrations must also enforce an appropriate server mode or prevent concurrent writes. */
func (instance *Migrator) EnsureColumnCapacity(ctx context.Context, spec TableSpec) error {
    return instance.ensureColumnCapacity(ctx, spec, "")
}

/* EnsureColumnCapacityForReencrypt checks the widest existing ciphertext and remaining plaintext under the target key id. Key-id growth increases ciphertext width. It uses aggregate lengths without fetching rows and does not protect against concurrent changes after preflight. */
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

func (instance *Migrator) longestUnsealedLength(ctx context.Context, table string, column string) (int, bool, error) {

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

    var cursor any
    hasCursor := false
    processed := 0
    skippedCount := 0
    skippedSample := make([]string, 0, skippedSampleSize)

    for {
        var rows *sql.Rows
        var queryErr error

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
            return processed, scanErr
        }

        if 0 == len(batch) {
            break
        }

        for _, row := range batch {
            cursor = row.primaryKeyArgument
            hasCursor = true

            applied, updateErr := instance.applyRow(ctx, spec, row, transform)
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
    if true == errors.Is(cause, context.Canceled) || true == errors.Is(cause, context.DeadlineExceeded) {
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
    arguments = append(arguments, row.primaryKeyArgument)
    arguments = append(arguments, valueArguments...)

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

    affected, affectedErr := result.RowsAffected()
    if nil != affectedErr {
        return true, nil
    }

    return 0 < affected, nil
}

type migrateRow struct {
    primaryKey string

    primaryKeyArgument any
    values     []sql.NullString
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
