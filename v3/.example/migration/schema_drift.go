package migration

import (
    "context"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
)

/* schemaResetCommand is the one door that brings a volume to the present schema: this application has no history,
   so a volume in an older shape is reset, not repaired step by step. */
const schemaResetCommand = "example:db:reset --force"

/* expectedTable is a table as the set's own statements create it: its name and its columns, in the order written. */
type expectedTable struct {
    name           string
    columnNameList []string
}

/* expectedSchemaOf reads the tables a set creates out of the set's own CREATE TABLE statements, so the schema a
   volume is checked against is the one the set would build and cannot drift from it by being written twice. A
   statement that creates no table — the constraint the catalogue adds after its tables — adds no expectation. */
func expectedSchemaOf(statementList []string) []expectedTable {
    tableList := make([]expectedTable, 0, len(statementList))

    for _, statement := range statementList {
        table, isCreate := expectedTableOf(statement)
        if false == isCreate {
            continue
        }

        tableList = append(tableList, table)
    }

    return tableList
}

/* expectedTableOf reads one CREATE TABLE statement: the table named after the keywords, and every item of the
   parenthesised body that opens with a column name rather than with a constraint keyword. Items are split on the
   commas at depth one and outside a quoted literal, so a type's own parentheses — VARCHAR(255), DATETIME(6), a
   key's column list — and a comma or a parenthesis inside a COMMENT or a DEFAULT stay inside their item. */
func expectedTableOf(statement string) (expectedTable, bool) {
    const createPrefix = "CREATE TABLE IF NOT EXISTS "

    if false == strings.HasPrefix(statement, createPrefix) {
        return expectedTable{}, false
    }

    remainder := statement[len(createPrefix):]

    bodyStart := strings.Index(remainder, "(")
    bodyEnd := strings.LastIndex(remainder, ")")
    if -1 == bodyStart || bodyEnd <= bodyStart {
        return expectedTable{}, false
    }

    table := expectedTable{name: unquotedIdentifier(strings.TrimSpace(remainder[:bodyStart]))}

    for _, item := range splitAtDepthOne(remainder[bodyStart+1 : bodyEnd]) {
        firstToken := strings.Fields(item)
        if 0 == len(firstToken) || true == isConstraintKeyword(firstToken[0]) {
            continue
        }

        table.columnNameList = append(table.columnNameList, unquotedIdentifier(firstToken[0]))
    }

    return table, true
}

func splitAtDepthOne(body string) []string {
    var itemList []string

    depth := 0
    itemStart := 0
    quote := rune(0)
    escaped := false

    for index, character := range body {
        if 0 != quote {
            /* inside a string literal mysql reads a backslash as escaping the character after it, so an escaped
               quote does not close the literal; inside a backquoted identifier a backslash is itself */
            if true == escaped {
                escaped = false

                continue
            }

            if '\\' == character && '`' != quote {
                escaped = true

                continue
            }

            if character == quote {
                quote = 0
            }

            continue
        }

        switch character {
        case '\'', '"', '`':
            quote = character
        case '(':
            depth++
        case ')':
            depth--
        case ',':
            if 0 == depth {
                itemList = append(itemList, strings.TrimSpace(body[itemStart:index]))
                itemStart = index + 1
            }
        }
    }

    return append(itemList, strings.TrimSpace(body[itemStart:]))
}

func isConstraintKeyword(token string) bool {
    switch strings.ToUpper(token) {
    case "PRIMARY", "UNIQUE", "KEY", "INDEX", "CONSTRAINT", "FOREIGN", "CHECK":
        return true
    default:
        return false
    }
}

func unquotedIdentifier(identifier string) string {
    return strings.Trim(identifier, "`\"")
}

/* tableDrift is what a volume holds apart from the present schema, for one table: the columns the code reads that
   the table lacks, and the columns the table carries that no statement of the set declares. */
type tableDrift struct {
    table                string
    missingColumnList    []string
    unexpectedColumnList []string
}

/* refuseSchemaDrift compares every table the set creates with the table the volume holds and refuses a volume that is not in the present shape. The set is recorded as applied by name and its tables are created IF NOT EXISTS, so a volume provisioned before a column was added passes it untouched and would fail at the first request that reads the column; refused here, the refusal names the set, each table, the columns on either side and the reset command. It is not remembered, so the resolution after the reset goes on. */
func refuseSchemaDrift(ctx context.Context, database *bun.DB, setName string, expectedSchema []expectedTable) error {
    var driftList []tableDrift

    for _, table := range expectedSchema {
        liveColumnNameList, readErr := liveColumnNameListOf(ctx, database, table.name)
        if nil != readErr {
            return exception.NewError(
                "migration: reading the schema the volume holds did not complete on the "+setName+" set",
                exceptioncontract.Context{"set": setName, "table": table.name},
                readErr,
            )
        }

        drift := driftOf(table, liveColumnNameList)
        if 0 < len(drift.missingColumnList) || 0 < len(drift.unexpectedColumnList) {
            driftList = append(driftList, drift)
        }
    }

    if 0 == len(driftList) {
        return nil
    }

    described := make([]string, 0, len(driftList))
    driftContext := make(map[string]any, len(driftList))

    for _, drift := range driftList {
        part := drift.table
        if 0 < len(drift.missingColumnList) {
            part += " lacks " + strings.Join(drift.missingColumnList, ", ")
        }

        if 0 < len(drift.unexpectedColumnList) {
            if 0 < len(drift.missingColumnList) {
                part += " and"
            }

            part += " carries " + strings.Join(drift.unexpectedColumnList, ", ")
        }

        described = append(described, part)
        driftContext[drift.table] = map[string]any{
            "missing":    drift.missingColumnList,
            "unexpected": drift.unexpectedColumnList,
        }
    }

    return exception.NewError(
        "migration: the "+setName+" set finds the volume in another shape than this code ("+strings.Join(described, "; ")+"); run "+schemaResetCommand,
        exceptioncontract.Context{
            "set":    setName,
            "drift":  driftContext,
            "remedy": schemaResetCommand,
        },
        nil,
    )
}

/* driftOf compares the columns a table should carry with the ones it does, each side sorted. */
func driftOf(table expectedTable, liveColumnNameList []string) tableDrift {
    liveSet := make(map[string]bool, len(liveColumnNameList))
    for _, columnName := range liveColumnNameList {
        liveSet[strings.ToLower(columnName)] = true
    }

    expectedSet := make(map[string]bool, len(table.columnNameList))
    drift := tableDrift{table: table.name}

    for _, columnName := range table.columnNameList {
        expectedSet[strings.ToLower(columnName)] = true

        if false == liveSet[strings.ToLower(columnName)] {
            drift.missingColumnList = append(drift.missingColumnList, columnName)
        }
    }

    for _, columnName := range liveColumnNameList {
        if false == expectedSet[strings.ToLower(columnName)] {
            drift.unexpectedColumnList = append(drift.unexpectedColumnList, columnName)
        }
    }

    sort.Strings(drift.missingColumnList)
    sort.Strings(drift.unexpectedColumnList)

    return drift
}

/* informationSchemaScopeOf names, in the handle's own dialect, the schema the information schema is read in: the
   database the handle is connected to — mysql names it DATABASE(), postgres names the schema it resolves
   unqualified tables in current_schema(). */
func informationSchemaScopeOf(database *bun.DB) string {
    if dialect.PG == database.Dialect().Name() {
        return "current_schema()"
    }

    return "DATABASE()"
}

/* liveColumnNameListOf reads the columns a table carries from the information schema of the database the handle is
   connected to. */
func liveColumnNameListOf(ctx context.Context, database *bun.DB, tableName string) ([]string, error) {
    rows, queryErr := database.QueryContext(
        ctx,
        "SELECT column_name FROM information_schema.columns WHERE table_schema = "+informationSchemaScopeOf(database)+" AND table_name = ?",
        tableName,
    )
    if nil != queryErr {
        return nil, queryErr
    }
    defer rows.Close()

    var columnNameList []string
    for rows.Next() {
        var columnName string
        if scanErr := rows.Scan(&columnName); nil != scanErr {
            return nil, scanErr
        }

        columnNameList = append(columnNameList, columnName)
    }

    return columnNameList, rows.Err()
}
