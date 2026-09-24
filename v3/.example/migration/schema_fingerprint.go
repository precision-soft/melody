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
    "`state` VARCHAR(16) NOT NULL, " +
    "PRIMARY KEY (`set_name`))"

const createArchiveSchemaFingerprintTableSql = "CREATE TABLE IF NOT EXISTS " + ArchiveSchemaFingerprintTableName + " (" +
    "set_name VARCHAR(64) NOT NULL PRIMARY KEY, " +
    "fingerprint CHAR(64) NOT NULL, " +
    "state VARCHAR(16) NOT NULL" +
    ")"

/* schemaSetBuilding and schemaSetBuilt are the two states of a set's row: written as building before the set's
   first statement and sealed as built after its last, so a volume a set began and did not finish says so — its
   next run finishes it — while a volume holding the set's tables with no row of its own was built by something else */
const (
    schemaSetBuilding = "building"
    schemaSetBuilt    = "built"
)

/* schemaSetRecord is where one set writes the row that vouches for the volume, in its own database's dialect */
type schemaSetRecord struct {
    setName         string
    tableName       string
    createTableSql  string
    markBuildingSql string
    sealSql         string
    readSql         string
    fingerprint     string
}

var catalogueSchemaSetRecord = schemaSetRecord{
    setName:         catalogMigrationSetName,
    tableName:       SchemaFingerprintTableName,
    createTableSql:  createSchemaFingerprintTableSql,
    markBuildingSql: "INSERT INTO `" + SchemaFingerprintTableName + "` (`set_name`, `fingerprint`, `state`) VALUES (?, ?, '" + schemaSetBuilding + "')",
    sealSql:         "UPDATE `" + SchemaFingerprintTableName + "` SET `state` = '" + schemaSetBuilt + "' WHERE `set_name` = ?",
    readSql:         "SELECT fingerprint, state FROM " + SchemaFingerprintTableName + " WHERE set_name = ?",
    fingerprint:     catalogueSchemaFingerprint,
}

var archiveSchemaSetRecord = schemaSetRecord{
    setName:         archiveMigrationSetName,
    tableName:       ArchiveSchemaFingerprintTableName,
    createTableSql:  createArchiveSchemaFingerprintTableSql,
    markBuildingSql: "INSERT INTO " + ArchiveSchemaFingerprintTableName + " (set_name, fingerprint, state) VALUES (?, ?, '" + schemaSetBuilding + "')",
    sealSql:         "UPDATE " + ArchiveSchemaFingerprintTableName + " SET state = '" + schemaSetBuilt + "' WHERE set_name = ?",
    readSql:         "SELECT fingerprint, state FROM " + ArchiveSchemaFingerprintTableName + " WHERE set_name = ?",
    fingerprint:     archiveSchemaFingerprint,
}

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

/* heldSchemaSetRecord is the row a set wrote on the volume, and whether there is one */
type heldSchemaSetRecord struct {
    present     bool
    fingerprint string
    state       string
}

func readSchemaSetRecord(ctx context.Context, database *bun.DB, record schemaSetRecord) (heldSchemaSetRecord, error) {
    held := heldSchemaSetRecord{}

    scanErr := database.QueryRowContext(ctx, record.readSql, record.setName).Scan(&held.fingerprint, &held.state)
    if nil != scanErr {
        if true == errors.Is(scanErr, sql.ErrNoRows) {
            return heldSchemaSetRecord{}, nil
        }

        return heldSchemaSetRecord{}, exception.NewError(
            "migration: reading the schema fingerprint the volume holds did not complete on the "+record.setName+" set",
            exceptioncontract.Context{"set": record.setName, "table": record.tableName},
            scanErr,
        )
    }

    held.present = true

    return held, nil
}

/* sealSchemaSet marks the set's row built, after its last statement: from here the row vouches that every
   statement of this code ran on the volume */
func sealSchemaSet(ctx context.Context, database *bun.DB, record schemaSetRecord) error {
    _, execErr := database.ExecContext(ctx, record.sealSql, record.setName)

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
func refuseSchemaFingerprint(ctx context.Context, database *bun.DB, record schemaSetRecord) error {
    setName := record.setName
    expectedFingerprint := record.fingerprint

    held, readErr := readSchemaSetRecord(ctx, database, record)
    if nil != readErr {
        return readErr
    }

    heldFingerprint := held.fingerprint

    /* a row still building vouches for nothing: the set began on this volume and did not finish, and only a run
       that reaches its seal says every statement ran */
    if expectedFingerprint == heldFingerprint && schemaSetBuilt == held.state {
        return nil
    }

    described := "holds none"
    if true == held.present && schemaSetBuilt != held.state {
        described = "holds an unfinished build of " + shortFingerprint(heldFingerprint)
    } else if true == held.present {
        described = "holds " + shortFingerprint(heldFingerprint)
    }

    return exception.NewError(
        "migration: the "+setName+" set finds the volume built from another schema than this code (the volume "+described+", this code "+shortFingerprint(expectedFingerprint)+"); run "+schemaResetCommand,
        exceptioncontract.Context{
            "set":                 setName,
            "heldFingerprint":     heldFingerprint,
            "heldState":           held.state,
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

/* beginSchemaSet opens a set's run on a volume: it refuses to apply the set over tables it did not build, and it
   writes the set's row as building before the first statement. The set runs on a volume only when the volume does
   not record it, and its tables are created IF NOT EXISTS, so on a volume that already held them — provisioned
   before the set, or whose record was lost — every CREATE was a no-op, and a fingerprint written over them vouched
   for statements that never ran. The row tells the two volumes that hold tables apart: one the set itself began
   and did not finish carries the set's own row, still building under this code's fingerprint, and the run goes on
   and finishes it, as a set applied until its last statement succeeds is meant to; one built by anything else
   carries no such row and is refused, naming the tables it found and the one door that brings the volume here. A
   record table standing alone and empty is the one step of a run that stopped between creating it and writing the
   row. The migration lock serializes the processes applying a set, so a second process finds the set recorded and
   never reaches this read. */
func beginSchemaSet(ctx context.Context, database *bun.DB, record schemaSetRecord, tableNameList []string) error {
    setName := record.setName

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

    if 0 < len(presentList) {
        held := heldSchemaSetRecord{}
        for _, presentTable := range presentList {
            if record.tableName != presentTable {
                continue
            }

            readHeld, readErr := readSchemaSetRecord(ctx, database, record)
            if nil != readErr {
                return readErr
            }
            held = readHeld
        }

        if true == held.present && schemaSetBuilding == held.state && record.fingerprint == held.fingerprint {
            return nil
        }

        standsAloneAndEmpty := 1 == len(presentList) && record.tableName == presentList[0] && false == held.present
        if false == standsAloneAndEmpty {
            return adoptionRefusal(setName, presentList, held)
        }
    }

    if _, createErr := database.ExecContext(ctx, record.createTableSql); nil != createErr {
        return createErr
    }

    _, markErr := database.ExecContext(ctx, record.markBuildingSql, setName, record.fingerprint)

    return markErr
}

/* adoptionRefusal names the tables a set found on a volume it did not build, and a row of another code's unfinished
   build when the volume holds one */
func adoptionRefusal(setName string, presentList []string, held heldSchemaSetRecord) error {
    sort.Strings(presentList)

    message := "migration: the " + setName + " set is not recorded on this volume but finds its tables already there (" + strings.Join(presentList, ", ") + "); it does not adopt tables it did not build — run " + schemaResetCommand
    if true == held.present && schemaSetBuilding == held.state {
        message = "migration: the " + setName + " set finds an unfinished build of another schema on this volume (" + shortFingerprint(held.fingerprint) + ", tables " + strings.Join(presentList, ", ") + "); it does not finish a build of other statements — run " + schemaResetCommand
    }

    return exception.NewError(
        message,
        exceptioncontract.Context{
            "set":           setName,
            "presentTables": presentList,
            "heldState":     held.state,
            "remedy":        schemaResetCommand,
        },
        nil,
    )
}
