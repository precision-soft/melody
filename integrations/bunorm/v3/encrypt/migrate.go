package encrypt

import (
    "context"
    "database/sql"
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

        /* a value already sealed with a random nonce authenticates, so the deterministic seal would pass it through untouched: the column would stay randomized — every CiphertextCandidates equality lookup on it finding nothing — while the run reported the row as processed. Convert it in place, under the key it already carries so mode=encrypt never doubles as a key rotation. A value that authenticates under no key in the set is ordinary plaintext (marker-shaped or not) and is sealed under the current key, as the cipher does. */
        keyId, markerShaped := keyIdOf(value)
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
        currentKeyId, encrypted := keyIdOf(value)
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
        valuePredicates = append(valuePredicates, quoteIdentifier(column)+" = ?")
        valueArguments = append(valueArguments, value.String)
    }

    if 0 == len(assignments) {
        return true, nil
    }

    arguments := make([]any, 0, len(setArguments)+1+len(valueArguments))
    arguments = append(arguments, setArguments...)
    arguments = append(arguments, row.primaryKey)
    arguments = append(arguments, valueArguments...)

    /* @important guard each assignment on the value read for this row so a concurrent application write between the select and this update is not silently reverted; a row that changed under us matches zero rows and is re-encrypted on the next run. */
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
