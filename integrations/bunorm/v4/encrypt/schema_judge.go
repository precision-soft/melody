package encrypt

import (
    "context"
    "database/sql"
    "errors"
    "sort"
    "strings"

    "github.com/go-sql-driver/mysql"

    "github.com/precision-soft/melody/v4/exception"
)

/* sqlSession is what the run asks the database through: the pool, or one connection pinned for a whole run, so the sql_mode the judge reads is the one the writes run under */
type sqlSession interface {
    QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row
    ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error)
}

func (instance *Migrator) queries() sqlSession {
    if nil != instance.session {
        return instance.session
    }

    return instance.db.DB
}

/* pinSession answers a copy of the migrator whose every query runs on one connection of the pool, and the release that hands it back */
func (instance *Migrator) pinSession(ctx context.Context) (*Migrator, func(), error) {
    connection, connectionErr := instance.db.DB.Conn(ctx)
    if nil != connectionErr {
        return nil, nil, exception.NewError("migrate could not take a connection for the run", nil, connectionErr)
    }

    /* a pinned connection gets no bad-connection retry from the pool, so a stale one, closed by the server while idle, is traded once for a fresh one before anything is judged */
    if pingErr := connection.PingContext(ctx); nil != pingErr {
        _ = connection.Close()

        connection, connectionErr = instance.db.DB.Conn(ctx)
        if nil != connectionErr {
            return nil, nil, exception.NewError("migrate could not take a connection for the run", nil, connectionErr)
        }
    }

    pinned := *instance
    pinned.session = connection

    return &pinned, func() { _ = connection.Close() }, nil
}

/* judgeSpecificationAgainstTheSchema runs before the capacity check and the first row: it answers the specification with every column and the key in the spelling the table stores, the key proved unique and never NULL, and, for a decrypt, a session that refuses a lossy write. A column name is matched as MySQL matches it, letter case aside. */
func (instance *Migrator) judgeSpecificationAgainstTheSchema(ctx context.Context, specification TableSpecification, mode string) (TableSpecification, error) {
    if "" == specification.Table || "" == specification.PrimaryKey || 0 == len(specification.Columns) {
        return specification, exception.NewError("migrate specification needs a table, primary key and at least one column", nil, nil)
    }

    nullableByColumn, columnsErr := instance.tableColumns(ctx, specification.Table)
    if nil != columnsErr {
        return specification, columnsErr
    }

    storedByLower := make(map[string]string, len(nullableByColumn))
    for column := range nullableByColumn {
        storedByLower[strings.ToLower(column)] = column
    }

    resolve := func(column string, field string) (string, error) {
        stored, known := storedByLower[strings.ToLower(column)]
        if false == known {
            return "", exception.NewError("migrate names a column the table "+specification.Table+" does not have: "+column, map[string]any{"table": specification.Table, "column": column, "field": field}, nil)
        }

        return stored, nil
    }

    resolved := specification
    primaryKey, primaryKeyErr := resolve(specification.PrimaryKey, "TableSpecification.PrimaryKey")
    if nil != primaryKeyErr {
        return specification, primaryKeyErr
    }
    resolved.PrimaryKey = primaryKey

    resolved.Columns = make([]string, 0, len(specification.Columns))
    seen := make(map[string]string, len(specification.Columns))
    for _, column := range specification.Columns {
        stored, columnErr := resolve(column, "TableSpecification.Columns")
        if nil != columnErr {
            return specification, columnErr
        }

        if first, duplicate := seen[stored]; true == duplicate {
            return specification, exception.NewError("migrate names the column "+stored+" twice", map[string]any{"table": specification.Table, "column": stored, "spelledAs": []string{first, column}, "field": "TableSpecification.Columns"}, nil)
        }

        seen[stored] = column
        resolved.Columns = append(resolved.Columns, stored)
    }

    if 0 < len(specification.SealBindings) {
        resolved.SealBindings = make(map[string]ColumnSealBinding, len(specification.SealBindings))
        for column, binding := range specification.SealBindings {
            stored, bindingErr := resolve(column, "TableSpecification.SealBindings")
            if nil != bindingErr {
                return specification, bindingErr
            }

            resolved.SealBindings[stored] = binding
        }
    }

    if keyErr := instance.judgeTheKey(ctx, resolved.Table, resolved.PrimaryKey, nullableByColumn[resolved.PrimaryKey]); nil != keyErr {
        return specification, keyErr
    }

    if migrateModeDecrypt == mode {
        if modeErr := instance.judgeTheDecryptSession(ctx, resolved); nil != modeErr {
            return specification, modeErr
        }
    }

    return resolved, nil
}

/* tableColumns answers whether each column of the table admits NULL, keyed by its stored spelling */
func (instance *Migrator) tableColumns(ctx context.Context, table string) (map[string]bool, error) {
    rows, queryErr := instance.queries().QueryContext(
        ctx,
        "SELECT COLUMN_NAME, IS_NULLABLE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?",
        table,
    )
    if nil != queryErr {
        return nil, exception.NewError("migrate column lookup failed", map[string]any{"table": table}, queryErr)
    }
    defer rows.Close()

    nullableByColumn := map[string]bool{}
    for rows.Next() {
        var column string
        var nullable string
        if scanErr := rows.Scan(&column, &nullable); nil != scanErr {
            return nil, exception.NewError("migrate column lookup failed", map[string]any{"table": table}, scanErr)
        }

        nullableByColumn[column] = "YES" == strings.ToUpper(nullable)
    }

    if rowsErr := rows.Err(); nil != rowsErr {
        return nil, exception.NewError("migrate column lookup failed", map[string]any{"table": table}, rowsErr)
    }

    if 0 == len(nullableByColumn) {
        return nil, exception.NewError("migrate names a table the database does not have: "+table, map[string]any{"table": table, "field": "TableSpecification.Table"}, nil)
    }

    return nullableByColumn, nil
}

/* judgeTheKey refuses a paging key that is not the only column of a unique index, or, for a unique index that is not the primary key, a key that admits NULL: `key > ?` never visits a NULL row, and a key two rows share skips one of them */
func (instance *Migrator) judgeTheKey(ctx context.Context, table string, key string, nullable bool) error {
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

        if "PRIMARY" == name || false == nullable {
            return nil
        }

        found = "the unique index " + name + " admits NULL"
    }

    return exception.NewError(
        "the paging key is not unique: "+key+" of "+table+", "+found,
        map[string]any{"table": table, "primaryKey": key, "field": "TableSpecification.PrimaryKey", "found": found},
        nil,
    )
}

/* judgeTheDecryptSession refuses a decrypt into a character column whose charset is not utf8mb4 under a sql_mode that is not strict: MySQL would write the runes it cannot hold as '?' and the row would lose its plaintext, where a strict session refuses the write and the row keeps its seal. The mode is read on the session the writes run on. */
func (instance *Migrator) judgeTheDecryptSession(ctx context.Context, specification TableSpecification) error {
    charsetByColumn, charsetErr := instance.columnCharsets(ctx, specification.Table)
    if nil != charsetErr {
        return charsetErr
    }

    narrow := ""
    narrowCharset := ""
    for _, column := range specification.Columns {
        charset := strings.ToLower(charsetByColumn[column])
        if "" == charset || "utf8mb4" == charset || "binary" == charset {
            continue
        }

        narrow = column
        narrowCharset = charset

        break
    }

    if "" == narrow {
        return nil
    }

    var sqlMode string
    if scanErr := instance.queries().QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&sqlMode); nil != scanErr {
        return exception.NewError("migrate sql_mode lookup failed", map[string]any{"table": specification.Table}, scanErr)
    }

    for _, mode := range strings.Split(strings.ToUpper(sqlMode), ",") {
        if "STRICT_TRANS_TABLES" == mode || "STRICT_ALL_TABLES" == mode {
            return nil
        }
    }

    return exception.NewError(
        "migrate decrypt would write into the column "+narrow+" of character set "+narrowCharset+", which is not utf8mb4, under a non-strict sql_mode ("+sqlMode+"); set STRICT_TRANS_TABLES or STRICT_ALL_TABLES for the session or convert the column to utf8mb4",
        map[string]any{"table": specification.Table, "column": narrow, "characterSet": narrowCharset, "sqlMode": sqlMode},
        nil,
    )
}

/* serverMessageWithheld is the message a withheld server refusal carries in place of the server's own */
const serverMessageWithheld = "the server's message is withheld: it may quote the value"

/* withholdQuotedValue replaces a server refusal of the run's write by one that keeps its number and SQLSTATE and drops the server's message: a duplicate entry, an incorrect or invalid string, a truncation or a trigger's SIGNAL quote the statement's values, the written plaintext under a decrypt and the guarded plaintext under an encrypt. The replacement stays a *mysql.MySQLError, so a caller matching the number through errors.As still matches. */
func withholdQuotedValue(execErr error) (error, bool) {
    var serverErr *mysql.MySQLError
    if false == errors.As(execErr, &serverErr) {
        return execErr, false
    }

    withheld := &mysql.MySQLError{Number: serverErr.Number, SQLState: serverErr.SQLState, Message: serverMessageWithheld}

    return exception.NewError("the server refused the write", map[string]any{"mysqlErrorNumber": serverErr.Number}, withheld), true
}

/* resolveColumnSpelling answers the specification with its columns in the table's spelling, for the capacity doors called on their own, outside a run */
func (instance *Migrator) resolveColumnSpelling(ctx context.Context, specification TableSpecification) (TableSpecification, error) {
    if "" == specification.Table || 0 == len(specification.Columns) {
        return specification, nil
    }

    nullableByColumn, columnsErr := instance.tableColumns(ctx, specification.Table)
    if nil != columnsErr {
        return specification, columnsErr
    }

    storedByLower := make(map[string]string, len(nullableByColumn))
    for column := range nullableByColumn {
        storedByLower[strings.ToLower(column)] = column
    }

    resolved := specification
    resolved.Columns = make([]string, 0, len(specification.Columns))
    for _, column := range specification.Columns {
        stored, known := storedByLower[strings.ToLower(column)]
        if false == known {
            return specification, exception.NewError("migrate names a column the table "+specification.Table+" does not have: "+column, map[string]any{"table": specification.Table, "column": column, "field": "TableSpecification.Columns"}, nil)
        }

        resolved.Columns = append(resolved.Columns, stored)
    }

    return resolved, nil
}

