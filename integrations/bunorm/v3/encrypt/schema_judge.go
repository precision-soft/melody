package encrypt

import (
    "context"
    "database/sql"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

/* a column is matched as MySQL matches it, letter case aside, and the run carries the spelling the table stores */
func (instance *Migrator) judgeSpecAgainstTheSchema(ctx context.Context, spec TableSpec, mode string) (TableSpec, error) {
    if "" == spec.Table || "" == spec.PrimaryKey || 0 == len(spec.Columns) {
        return spec, exception.NewError("migrate spec needs a table, primary key and at least one column", nil, nil)
    }

    storedByLower, columnsErr := instance.tableColumns(ctx, spec.Table)
    if nil != columnsErr {
        return spec, columnsErr
    }

    resolved := spec
    primaryKey, primaryKeyErr := resolveColumn(storedByLower, spec.Table, spec.PrimaryKey, "TableSpec.PrimaryKey")
    if nil != primaryKeyErr {
        return spec, primaryKeyErr
    }
    resolved.PrimaryKey = primaryKey

    resolved.Columns = make([]string, 0, len(spec.Columns))
    seen := make(map[string]string, len(spec.Columns))
    for _, column := range spec.Columns {
        stored, columnErr := resolveColumn(storedByLower, spec.Table, column, "TableSpec.Columns")
        if nil != columnErr {
            return spec, columnErr
        }

        if first, duplicate := seen[stored]; true == duplicate {
            return spec, exception.NewError("migrate names the column "+stored+" twice", map[string]any{"table": spec.Table, "column": stored, "spelledAs": []string{first, column}, "field": "TableSpec.Columns"}, nil)
        }

        seen[stored] = column
        resolved.Columns = append(resolved.Columns, stored)
    }

    if keyErr := instance.judgeTheKey(ctx, resolved.Table, resolved.PrimaryKey); nil != keyErr {
        return spec, keyErr
    }

    if migrateModeDecrypt == mode {
        if sessionErr := instance.judgeTheDecryptSession(ctx, resolved); nil != sessionErr {
            return spec, sessionErr
        }
    }

    return resolved, nil
}

/* ucs2 holds the basic plane only */
func holdsEveryRune(charset string) bool {
    switch charset {
    case "", "utf8mb4", "binary", "utf16", "utf16le", "utf32", "gb18030":
        return true
    }

    return false
}

/* a relaxed session writes '?' for a rune the column or the connection cannot hold, over the only copy of the plaintext */
func (instance *Migrator) judgeTheDecryptSession(ctx context.Context, spec TableSpec) error {
    charsetByColumn, charsetErr := instance.columnCharsets(ctx, spec.Table)
    if nil != charsetErr {
        return charsetErr
    }

    var sqlMode string
    var clientCharset string
    if scanErr := instance.queries().QueryRowContext(ctx, "SELECT @@SESSION.sql_mode, @@SESSION.character_set_client").Scan(&sqlMode, &clientCharset); nil != scanErr {
        return exception.NewError("migrate sql_mode lookup failed", map[string]any{"table": spec.Table}, scanErr)
    }

    for _, sessionMode := range strings.Split(strings.ToUpper(sqlMode), ",") {
        if "STRICT_TRANS_TABLES" == sessionMode || "STRICT_ALL_TABLES" == sessionMode {
            return nil
        }
    }

    if "utf8mb4" != strings.ToLower(clientCharset) {
        return exception.NewError(
            "migrate decrypt would write through a connection of character set "+clientCharset+", which is not utf8mb4, under a non-strict sql_mode ("+sqlMode+"); set STRICT_TRANS_TABLES or STRICT_ALL_TABLES for the session or connect with charset=utf8mb4",
            map[string]any{"table": spec.Table, "clientCharacterSet": clientCharset, "sqlMode": sqlMode},
            nil,
        )
    }

    for _, column := range spec.Columns {
        charset := strings.ToLower(charsetByColumn[column])
        if true == holdsEveryRune(charset) {
            continue
        }

        return exception.NewError(
            "migrate decrypt would write into the column "+column+" of character set "+charset+", which is not utf8mb4, under a non-strict sql_mode ("+sqlMode+"); set STRICT_TRANS_TABLES or STRICT_ALL_TABLES for the session or convert the column to utf8mb4",
            map[string]any{"table": spec.Table, "column": column, "characterSet": charset, "sqlMode": sqlMode},
            nil,
        )
    }

    return nil
}

func resolveColumn(storedByLower map[string]string, table string, column string, field string) (string, error) {
    stored, known := storedByLower[strings.ToLower(column)]
    if false == known {
        return "", exception.NewError("migrate names a column the table "+table+" does not have: "+column, map[string]any{"table": table, "column": column, "field": field}, nil)
    }

    return stored, nil
}

func (instance *Migrator) tableColumns(ctx context.Context, table string) (map[string]string, error) {
    rows, queryErr := instance.queries().QueryContext(
        ctx,
        "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
        table,
    )
    if nil != queryErr {
        return nil, exception.NewError("migrate column lookup failed", map[string]any{"table": table}, queryErr)
    }
    defer rows.Close()

    storedByLower := map[string]string{}
    for rows.Next() {
        var column string
        if scanErr := rows.Scan(&column); nil != scanErr {
            return nil, exception.NewError("migrate column lookup failed", map[string]any{"table": table}, scanErr)
        }

        storedByLower[strings.ToLower(column)] = column
    }

    if rowsErr := rows.Err(); nil != rowsErr {
        return nil, exception.NewError("migrate column lookup failed", map[string]any{"table": table}, rowsErr)
    }

    if 0 == len(storedByLower) {
        return nil, exception.NewError("migrate names a table the database does not have: "+table, map[string]any{"table": table, "field": "TableSpec.Table"}, nil)
    }

    return storedByLower, nil
}

/* a view lists no index of the table under it, so its key is left unjudged */
func (instance *Migrator) isView(ctx context.Context, table string) (bool, error) {
    var tableType string
    scanErr := instance.queries().QueryRowContext(
        ctx,
        "SELECT TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
        table,
    ).Scan(&tableType)
    if nil != scanErr {
        return false, exception.NewError("migrate table type lookup failed", map[string]any{"table": table}, scanErr)
    }

    return "VIEW" == tableType, nil
}

/* the run pages by `key > ?`, which skips one of two rows sharing a key; a NULL key is refused where the rows are read */
func (instance *Migrator) judgeTheKey(ctx context.Context, table string, key string) error {
    view, viewErr := instance.isView(ctx, table)
    if nil != viewErr {
        return viewErr
    }

    if true == view {
        return nil
    }

    rows, queryErr := instance.queries().QueryContext(
        ctx,
        "SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
        table,
    )
    if nil != queryErr {
        return exception.NewError("migrate index lookup failed", map[string]any{"table": table}, queryErr)
    }
    defer rows.Close()

    type index struct {
        columns []string
        unique  bool
    }

    indexes := map[string]*index{}
    for rows.Next() {
        var name string
        var column sql.NullString
        var nonUnique int64
        if scanErr := rows.Scan(&name, &column, &nonUnique); nil != scanErr {
            return exception.NewError("migrate index lookup failed", map[string]any{"table": table}, scanErr)
        }

        if _, known := indexes[name]; false == known {
            indexes[name] = &index{unique: 0 == nonUnique}
        }

        /* a functional key part has no column name: it is a part that is not the key */
        indexes[name].columns = append(indexes[name].columns, column.String)
    }

    if rowsErr := rows.Err(); nil != rowsErr {
        return exception.NewError("migrate index lookup failed", map[string]any{"table": table}, rowsErr)
    }

    found := "no unique index"
    names := make([]string, 0, len(indexes))
    for name := range indexes {
        names = append(names, name)
    }
    sort.Strings(names)

    for _, name := range names {
        candidate := indexes[name]
        if false == candidate.unique {
            continue
        }

        if 1 < len(candidate.columns) {
            for _, column := range candidate.columns {
                if true == strings.EqualFold(column, key) {
                    found = "part of the composite unique index " + name
                }
            }

            continue
        }

        if false == strings.EqualFold(candidate.columns[0], key) {
            continue
        }

        return nil
    }

    return exception.NewError(
        "the paging key is not unique: "+key+" of "+table+", "+found,
        map[string]any{"table": table, "primaryKey": key, "field": "TableSpec.PrimaryKey", "found": found},
        nil,
    )
}

func (instance *Migrator) resolveColumnSpelling(ctx context.Context, spec TableSpec) (TableSpec, error) {
    if "" == spec.Table || 0 == len(spec.Columns) {
        return spec, nil
    }

    storedByLower, columnsErr := instance.tableColumns(ctx, spec.Table)
    if nil != columnsErr {
        return spec, columnsErr
    }

    resolved := spec
    resolved.Columns = make([]string, 0, len(spec.Columns))
    for _, column := range spec.Columns {
        stored, columnErr := resolveColumn(storedByLower, spec.Table, column, "TableSpec.Columns")
        if nil != columnErr {
            return spec, columnErr
        }

        resolved.Columns = append(resolved.Columns, stored)
    }

    return resolved, nil
}
