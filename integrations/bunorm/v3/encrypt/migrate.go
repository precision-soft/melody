package encrypt

import (
    "context"
    "database/sql"
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/uptrace/bun"
)

const defaultMigrateBatchSize = 500

/**
 * TableSpec describes a table whose string columns hold (or should hold) encrypted values.
 * PrimaryKey must be an orderable column; it drives keyset pagination so the migration streams
 * the table in bounded batches without holding a long transaction.
 */
type TableSpec struct {
    Table      string
    PrimaryKey string
    Columns    []string
    BatchSize  int
}

/**
 * Migrator performs bulk encrypt / re-encrypt / decrypt over a table's columns using the cipher
 * directly (not the process-wide EncryptedString cipher). All operations are idempotent thanks to
 * the encryption marker: encrypting an already-encrypted value is a no-op, and decrypting plaintext
 * passes through. SQL is emitted for the MySQL dialect (backtick-quoted identifiers).
 */
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

    return &Migrator{db: db, cipher: cipher}
}

/** MigrateEncrypt encrypts every plaintext value in the spec's columns; already-encrypted values are left as-is. */
func (instance *Migrator) MigrateEncrypt(ctx context.Context, spec TableSpec) (int, error) {
    return instance.run(ctx, spec, func(value string) (string, error) {
        return instance.cipher.Encrypt(value)
    })
}

/** MigrateReencrypt decrypts each value with whichever key wrote it, then re-encrypts under targetKeyId (key rotation). */
func (instance *Migrator) MigrateReencrypt(ctx context.Context, spec TableSpec, targetKeyId string) (int, error) {
    return instance.run(ctx, spec, func(value string) (string, error) {
        plaintext, decryptErr := instance.cipher.Decrypt(value)
        if nil != decryptErr {
            return "", decryptErr
        }

        return instance.cipher.EncryptWithKeyId(plaintext, targetKeyId)
    })
}

/** MigrateDecrypt rewrites every value as plaintext; values that are already plaintext pass through. */
func (instance *Migrator) MigrateDecrypt(ctx context.Context, spec TableSpec) (int, error) {
    return instance.run(ctx, spec, func(value string) (string, error) {
        return instance.cipher.Decrypt(value)
    })
}

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
    selectSql := fmt.Sprintf(
        "SELECT %s FROM %s WHERE %s > ? ORDER BY %s ASC LIMIT ?",
        selectClause,
        quoteIdentifier(spec.Table),
        quoteIdentifier(spec.PrimaryKey),
        quoteIdentifier(spec.PrimaryKey),
    )

    var cursor string
    processed := 0

    for {
        rows, queryErr := instance.db.DB.QueryContext(ctx, selectSql, cursor, batchSize)
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

            if updateErr := instance.applyRow(ctx, spec, row, transform); nil != updateErr {
                return processed, updateErr
            }

            processed++
        }

        if len(batch) < batchSize {
            break
        }
    }

    return processed, nil
}

func (instance *Migrator) applyRow(ctx context.Context, spec TableSpec, row migrateRow, transform func(string) (string, error)) error {
    assignments := make([]string, 0, len(spec.Columns))
    arguments := make([]any, 0, len(spec.Columns)+1)

    for index, column := range spec.Columns {
        value := row.values[index]
        if false == value.Valid {
            continue
        }

        transformed, transformErr := transform(value.String)
        if nil != transformErr {
            return exception.NewError("migrate transform failed", map[string]any{"table": spec.Table, "column": column}, transformErr)
        }

        if transformed == value.String {
            continue
        }

        assignments = append(assignments, quoteIdentifier(column)+" = ?")
        arguments = append(arguments, transformed)
    }

    if 0 == len(assignments) {
        return nil
    }

    arguments = append(arguments, row.primaryKey)
    updateSql := fmt.Sprintf(
        "UPDATE %s SET %s WHERE %s = ?",
        quoteIdentifier(spec.Table),
        strings.Join(assignments, ", "),
        quoteIdentifier(spec.PrimaryKey),
    )

    if _, execErr := instance.db.DB.ExecContext(ctx, updateSql, arguments...); nil != execErr {
        return exception.NewError("migrate update failed", map[string]any{"table": spec.Table, "id": row.primaryKey}, execErr)
    }

    return nil
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
