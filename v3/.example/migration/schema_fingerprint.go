package migration

import (
    "context"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "errors"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun"
)

/* SchemaFingerprintTableName and ArchiveSchemaFingerprintTableName are the tables each set records the fingerprint of
   the schema it built in, on the set's own database: two names, so the plan the reset prints for the two databases
   names two tables. */
const (
    SchemaFingerprintTableName        = "melody_example_v3_schema_fingerprint"
    ArchiveSchemaFingerprintTableName = "melody_example_v3_archive_schema_fingerprint"
)

const createSchemaFingerprintTableSql = "CREATE TABLE IF NOT EXISTS `" + SchemaFingerprintTableName + "` (" +
    "`set_name` VARCHAR(64) NOT NULL, " +
    "`fingerprint` CHAR(64) NOT NULL, " +
    "PRIMARY KEY (`set_name`))"

const createArchiveSchemaFingerprintTableSql = "CREATE TABLE IF NOT EXISTS " + ArchiveSchemaFingerprintTableName + " (" +
    "set_name VARCHAR(64) NOT NULL PRIMARY KEY, " +
    "fingerprint CHAR(64) NOT NULL" +
    ")"

/* the row is written only where none is: several processes may apply a set at once, and a row the volume already
   holds is the one the check reads — the set runs on a volume only once, recorded by name */
const recordSchemaFingerprintSql = "INSERT IGNORE INTO `" + SchemaFingerprintTableName + "` (`set_name`, `fingerprint`) VALUES (?, ?)"

const recordArchiveSchemaFingerprintSql = "INSERT INTO " + ArchiveSchemaFingerprintTableName + " (set_name, fingerprint) VALUES (?, ?) ON CONFLICT (set_name) DO NOTHING"

/* catalogueSchemaFingerprint and archiveSchemaFingerprint are the fingerprints of the schema each set builds: every
   statement that shapes it, the constraint the catalogue adds after its tables included. */
var catalogueSchemaFingerprint = schemaFingerprintOf(append(append([]string{}, schemaUpStatementList...), createUserUsernameIndexSql)...)

var archiveSchemaFingerprint = schemaFingerprintOf(archiveUpStatementList...)

/* schemaFingerprintOf hashes the statements that build a schema, in order: any change to any of them — a column's
   type or collation, a key, a constraint, a table — is another fingerprint. */
func schemaFingerprintOf(statementList ...string) string {
    hash := sha256.New()
    for _, statement := range statementList {
        hash.Write([]byte(statement))
        hash.Write([]byte{0})
    }

    return hex.EncodeToString(hash.Sum(nil))
}

func recordSchemaFingerprint(ctx context.Context, database *bun.DB, statement string, setName string, fingerprint string) error {
    _, execErr := database.ExecContext(ctx, statement, setName, fingerprint)

    return execErr
}

/* refuseSchemaFingerprint refuses a volume whose set was applied from other statements than this code's. The set
   is recorded as applied by name and its tables are created IF NOT EXISTS, so a volume built before a statement
   changed keeps what it was built with, and the columns alone do not show it: a type, a collation, a key or a
   constraint changed under the same column names — the identifier columns moved to utf8mb4_bin were exactly such
   a change — passed the comparison of the columns untouched. The fingerprint the set recorded when it built the
   volume is compared with the one this code would record; a volume that holds none was built before the set
   recorded one. Refused, the refusal names both and the one door that brings the volume here, and it is not
   remembered, so the resolution after the reset goes on. */
func refuseSchemaFingerprint(ctx context.Context, database *bun.DB, setName string, tableName string, expectedFingerprint string) error {
    heldFingerprint := ""

    scanErr := database.QueryRowContext(
        ctx,
        "SELECT fingerprint FROM "+tableName+" WHERE set_name = ?",
        setName,
    ).Scan(&heldFingerprint)
    if nil != scanErr && false == errors.Is(scanErr, sql.ErrNoRows) {
        return exception.NewError(
            "migration: reading the schema fingerprint the volume holds did not complete on the "+setName+" set",
            exceptioncontract.Context{"set": setName, "table": tableName},
            scanErr,
        )
    }

    if expectedFingerprint == heldFingerprint {
        return nil
    }

    described := "holds none"
    if "" != heldFingerprint {
        described = "holds " + shortFingerprint(heldFingerprint)
    }

    return exception.NewError(
        "migration: the "+setName+" set finds the volume built from another schema than this code (the volume "+described+", this code "+shortFingerprint(expectedFingerprint)+"); run "+schemaResetCommand,
        exceptioncontract.Context{
            "set":                 setName,
            "heldFingerprint":     heldFingerprint,
            "expectedFingerprint": expectedFingerprint,
            "remedy":              schemaResetCommand,
        },
        nil,
    )
}

func shortFingerprint(fingerprint string) string {
    if 12 >= len(fingerprint) {
        return fingerprint
    }

    return fingerprint[:12]
}

/* refuseAdoption refuses to apply a set over tables it did not build. The set runs on a volume only when the
   volume does not record it, and its tables are created IF NOT EXISTS, so on a volume that already held them —
   provisioned before the set, or whose record was lost — every CREATE was a no-op and the set then wrote THIS
   code's fingerprint over tables built from anything: the fingerprint vouched for statements that never ran, and
   a collation, a key or a constraint changed under the same column names passed the check that exists to see
   it. A set that is not recorded finds none of its own tables, or refuses naming the ones it found and the one
   door that brings the volume here. The migration lock serializes the processes applying a set, so a second
   process finds the set recorded and never reaches this read. */
func refuseAdoption(ctx context.Context, database *bun.DB, setName string, tableNameList []string) error {
    placeholderList := make([]string, 0, len(tableNameList))
    argumentList := make([]any, 0, len(tableNameList))
    for _, tableName := range tableNameList {
        placeholderList = append(placeholderList, "?")
        argumentList = append(argumentList, tableName)
    }

    rows, queryErr := database.QueryContext(
        ctx,
        "SELECT table_name FROM information_schema.tables WHERE table_schema = "+informationSchemaScopeOf(database)+" AND table_name IN ("+strings.Join(placeholderList, ", ")+")",
        argumentList...,
    )
    if nil != queryErr {
        return exception.NewError(
            "migration: reading which of its tables the volume already holds did not complete on the "+setName+" set",
            exceptioncontract.Context{"set": setName},
            queryErr,
        )
    }
    defer rows.Close()

    presentList := []string{}
    for rows.Next() {
        tableName := ""
        if scanErr := rows.Scan(&tableName); nil != scanErr {
            return exception.NewError(
                "migration: reading which of its tables the volume already holds did not complete on the "+setName+" set",
                exceptioncontract.Context{"set": setName},
                scanErr,
            )
        }

        presentList = append(presentList, tableName)
    }
    if rowsErr := rows.Err(); nil != rowsErr {
        return exception.NewError(
            "migration: reading which of its tables the volume already holds did not complete on the "+setName+" set",
            exceptioncontract.Context{"set": setName},
            rowsErr,
        )
    }

    if 0 == len(presentList) {
        return nil
    }

    sort.Strings(presentList)

    return exception.NewError(
        "migration: the "+setName+" set is not recorded on this volume but finds its tables already there ("+strings.Join(presentList, ", ")+"); it does not adopt tables it did not build — run "+schemaResetCommand,
        exceptioncontract.Context{
            "set":          setName,
            "presentTables": presentList,
            "remedy":       schemaResetCommand,
        },
        nil,
    )
}
