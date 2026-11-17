package encrypt

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "os"
    "strings"
    "testing"

    "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    "github.com/precision-soft/melody/v3/exception"
)

func judgeTable(columns [][]driver.Value, indexes [][]driver.Value, charsets [][]driver.Value) []scriptedSqlResponse {
    return []scriptedSqlResponse{
        {fragment: "SELECT COLUMN_NAME FROM", columns: []string{"COLUMN_NAME"}, rows: columns},
        {fragment: "STATISTICS", columns: []string{"INDEX_NAME", "COLUMN_NAME", "NON_UNIQUE"}, rows: indexes},
        {fragment: "CHARACTER_SET_NAME", columns: []string{"COLUMN_NAME", "CHARACTER_SET_NAME"}, rows: charsets},
        {fragment: "information_schema.COLUMNS", columns: []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"}, rows: [][]driver.Value{{"text", int64(65535), int64(65535)}}},
        {fragment: "MAX(LENGTH(", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
        {fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{}},
    }
}

var accountsColumns = [][]driver.Value{{"id"}, {"notes"}}
var accountsPrimary = [][]driver.Value{{"PRIMARY", "id", int64(0)}}

func withRows(responses []scriptedSqlResponse, columns []string, rows [][]driver.Value) []scriptedSqlResponse {
    responses[len(responses)-1] = scriptedSqlResponse{fragment: "ORDER BY", columns: columns, rows: rows}

    return responses
}

func readTheTable(stub *scriptedSqlDriver) bool {
    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "FROM `accounts`") {
            return true
        }
    }

    return false
}

func refusalContext(t *testing.T, err error) map[string]any {
    t.Helper()

    for current := err; nil != current; {
        if melodyErr, isMelodyErr := current.(*exception.Error); true == isMelodyErr {
            if _, hasTable := melodyErr.Context()["table"]; true == hasTable && nil == melodyErr.Unwrap() {
                return melodyErr.Context()
            }

            current = melodyErr.Unwrap()

            continue
        }

        break
    }

    t.Fatalf("expected a melody refusal, got %v", err)

    return nil
}

func everyQueryInTheStoredSpelling(t *testing.T, door string, stub *scriptedSqlDriver, callerSpellings ...string) {
    t.Helper()

    for _, query := range stub.recorded() {
        for _, spelling := range callerSpellings {
            if true == strings.Contains(query, spelling) {
                t.Fatalf("%s: expected every query in the stored spelling, got %q", door, query)
            }
        }
    }
}

func TestMigrate_ResolvesAColumnToTheSpellingTheTableStores(t *testing.T) {
    responses := withRows(
        judgeTable([][]driver.Value{{"id"}, {"email"}}, accountsPrimary, [][]driver.Value{{"email", "latin1"}}),
        []string{"id", "email"},
        [][]driver.Value{{"7", "plain"}},
    )
    migrator, stub := newScriptedMigrator(t, responses)

    if _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "ID", Columns: []string{"Email"}}); nil != runErr {
        t.Fatalf("expected the run to complete over the stored spelling, got %v", runErr)
    }

    everyQueryInTheStoredSpelling(t, "encrypt", stub, "`Email`", "`ID`")

    selected := false
    guarded := false
    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "SELECT `id`, `email` FROM `accounts`") {
            selected = true
        }

        if true == strings.HasPrefix(query, "UPDATE `accounts` SET `email` = ? WHERE `id` = ? AND CAST(CONVERT(`email` USING utf8mb4) AS BINARY) = ?") {
            guarded = true
        }
    }

    if false == selected || false == guarded {
        t.Fatalf("expected the page select and the guarded update over `email` with its charset, got %v", stub.recorded())
    }
}

func TestMigrate_ReencryptAndDecryptUseTheStoredSpellingToo(t *testing.T) {
    for door, run := range map[string]func(*Migrator, TableSpec) error{
        "reencrypt": func(migrator *Migrator, spec TableSpec) error {
            _, runErr := migrator.MigrateReencrypt(context.Background(), spec, "v1")

            return runErr
        },
        "decrypt": func(migrator *Migrator, spec TableSpec) error {
            _, runErr := migrator.MigrateDecrypt(context.Background(), spec)

            return runErr
        },
    } {
        migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil))

        if runErr := run(migrator, TableSpec{Table: "accounts", PrimaryKey: "ID", Columns: []string{"Notes"}}); nil != runErr {
            t.Fatalf("%s: run: %v", door, runErr)
        }

        if false == readTheTable(stub) {
            t.Fatalf("%s: expected the run to read the table", door)
        }

        everyQueryInTheStoredSpelling(t, door, stub, "`Notes`", "`ID`")
    }
}

func TestMigrate_RefusesAnUnknownColumnAndOneNamedTwiceBeforeReadingTheTable(t *testing.T) {
    for name, testCase := range map[string]struct {
        spec    TableSpec
        message string
        field   string
    }{
        "unknown column":     {spec: TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"missing"}}, message: "migrate names a column the table accounts does not have: missing", field: "TableSpec.Columns"},
        "named twice":        {spec: TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes", "NOTES"}}, message: "migrate names the column notes twice", field: "TableSpec.Columns"},
        "unknown key column": {spec: TableSpec{Table: "accounts", PrimaryKey: "uuid", Columns: []string{"notes"}}, message: "migrate names a column the table accounts does not have: uuid", field: "TableSpec.PrimaryKey"},
    } {
        migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil))

        _, runErr := migrator.MigrateEncrypt(context.Background(), testCase.spec)
        if nil == runErr || testCase.message != runErr.Error() || true == readTheTable(stub) {
            t.Fatalf("%s: expected %q before the table is read, got %v", name, testCase.message, runErr)
        }

        context := refusalContext(t, runErr)
        if testCase.field != context["field"] || "accounts" != context["table"] {
            t.Fatalf("%s: expected the field and the table named, got %v", name, context)
        }

        if "named twice" == name && "[notes NOTES]" != fmt.Sprint(context["spelledAs"]) {
            t.Fatalf("%s: expected both spellings named, got %v", name, context)
        }
    }

    migrator, stub := newScriptedMigrator(t, judgeTable([][]driver.Value{}, accountsPrimary, nil))
    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
    if nil == runErr || "migrate names a table the database does not have: accounts" != runErr.Error() || true == readTheTable(stub) {
        t.Fatalf("unknown table: expected the refusal before the table is read, got %v", runErr)
    }

    if "TableSpec.Table" != refusalContext(t, runErr)["field"] {
        t.Fatalf("unknown table: expected the field named, got %v", runErr)
    }
}

func TestMigrate_JudgesThePagingKey(t *testing.T) {
    doors := map[string]func(*Migrator) error{
        "encrypt": func(migrator *Migrator) error {
            _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})

            return runErr
        },
        "reencrypt": func(migrator *Migrator) error {
            _, runErr := migrator.MigrateReencrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}, "v1")

            return runErr
        },
        "decrypt": func(migrator *Migrator) error {
            _, runErr := migrator.MigrateDecrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})

            return runErr
        },
    }

    for name, testCase := range map[string]struct {
        columns [][]driver.Value
        indexes [][]driver.Value
        found   string
    }{
        "primary":          {columns: accountsColumns, indexes: accountsPrimary},
        "unique":           {columns: accountsColumns, indexes: [][]driver.Value{{"uniq_id", "id", int64(0)}}},
        "composite":        {columns: accountsColumns, indexes: [][]driver.Value{{"PRIMARY", "id", int64(0)}, {"PRIMARY", "notes", int64(0)}}, found: "part of the composite unique index PRIMARY"},
        "not unique":       {columns: accountsColumns, indexes: [][]driver.Value{{"idx_id", "id", int64(1)}}, found: "no unique index"},
        "no index":         {columns: accountsColumns, indexes: [][]driver.Value{}, found: "no unique index"},
        "another column's": {columns: accountsColumns, indexes: [][]driver.Value{{"PRIMARY", "notes", int64(0)}}, found: "no unique index"},
    } {
        for door, run := range doors {
            migrator, stub := newScriptedMigrator(t, judgeTable(testCase.columns, testCase.indexes, nil))

            runErr := run(migrator)
            if "" == testCase.found {
                if nil != runErr || false == readTheTable(stub) {
                    t.Fatalf("%s/%s: expected the key admitted and the table read, got %v", name, door, runErr)
                }

                continue
            }

            expected := "the paging key is not unique: id of accounts, " + testCase.found
            if nil == runErr || expected != runErr.Error() || true == readTheTable(stub) {
                t.Fatalf("%s/%s: expected %q before the table is read, got %v", name, door, expected, runErr)
            }

            context := refusalContext(t, runErr)
            if testCase.found != context["found"] || "id" != context["primaryKey"] || "TableSpec.PrimaryKey" != context["field"] {
                t.Fatalf("%s/%s: expected the key, the field and the finding named, got %v", name, door, context)
            }
        }
    }
}

func TestMigrate_AViewIsNotJudgedForItsKey(t *testing.T) {
    for name, testCase := range map[string]struct {
        tableType string
        spec      TableSpec
        message   string
    }{
        "view":                 {tableType: "VIEW", spec: TableSpec{Table: "accounts", PrimaryKey: "ID", Columns: []string{"Notes"}}},
        "view, unknown column": {tableType: "VIEW", spec: TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"missing"}}, message: "migrate names a column the table accounts does not have: missing"},
        "base table":           {tableType: "BASE TABLE", spec: TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}, message: "the paging key is not unique: id of accounts, no unique index"},
    } {
        responses := append([]scriptedSqlResponse{{fragment: "TABLE_TYPE", columns: []string{"TABLE_TYPE"}, rows: [][]driver.Value{{testCase.tableType}}}}, judgeTable(accountsColumns, [][]driver.Value{}, nil)...)
        migrator, stub := newScriptedMigrator(t, responses)

        _, runErr := migrator.MigrateEncrypt(context.Background(), testCase.spec)
        if "" != testCase.message {
            if nil == runErr || testCase.message != runErr.Error() || true == readTheTable(stub) {
                t.Fatalf("%s: expected %q before the table is read, got %v", name, testCase.message, runErr)
            }

            continue
        }

        if nil != runErr || false == readTheTable(stub) {
            t.Fatalf("%s: expected the run over the view, got %v", name, runErr)
        }

        everyQueryInTheStoredSpelling(t, name, stub, "`Notes`", "`ID`")
    }
}

func TestMigrate_ATableTypeLookupFailureIsReported(t *testing.T) {
    lookupErr := errors.New("table type lookup refused")
    responses := append([]scriptedSqlResponse{{fragment: "TABLE_TYPE", err: lookupErr}}, judgeTable(accountsColumns, accountsPrimary, nil)...)
    migrator, stub := newScriptedMigrator(t, responses)

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
    if nil == runErr || false == errors.Is(runErr, lookupErr) || true == readTheTable(stub) {
        t.Fatalf("expected the lookup failure before the table is read, got %v", runErr)
    }
}

func TestMigrate_AFunctionalIndexIsAPartThatIsNotTheKey(t *testing.T) {
    for name, testCase := range map[string]struct {
        indexes [][]driver.Value
        refused bool
    }{
        "a functional index beside the primary key":     {indexes: [][]driver.Value{{"PRIMARY", "id", int64(0)}, {"idx_fn", nil, int64(1)}}},
        "a unique index over the key and an expression": {indexes: [][]driver.Value{{"uniq_mix", "id", int64(0)}, {"uniq_mix", nil, int64(0)}}, refused: true},
    } {
        migrator, _ := newScriptedMigrator(t, judgeTable(accountsColumns, testCase.indexes, nil))

        _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
        if true == testCase.refused {
            if nil == runErr || false == strings.Contains(runErr.Error(), "the paging key is not unique") {
                t.Fatalf("%s: expected the key refused, got %v", name, runErr)
            }

            continue
        }

        if nil != runErr {
            t.Fatalf("%s: expected the run to complete, got %v", name, runErr)
        }
    }
}

func TestMigrate_WithholdsTheServerMessage(t *testing.T) {
    doors := map[string]func(*Migrator) error{
        "encrypt": func(migrator *Migrator) error {
            _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})

            return runErr
        },
        "decrypt": func(migrator *Migrator) error {
            _, runErr := migrator.MigrateDecrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})

            return runErr
        },
    }

    sealed, sealErr := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)})).Encrypt("the secret plaintext")
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    stored := map[string]string{"encrypt": "the secret plaintext", "decrypt": sealed}

    for _, number := range []uint16{1062, 1366, 3988} {
        for door, run := range doors {
            migrator, stub := newScriptedMigrator(t, withRows(judgeTable(accountsColumns, accountsPrimary, nil), []string{"id", "notes"}, [][]driver.Value{{"7", stored[door]}}))
            stub.execErr = &mysql.MySQLError{Number: number, Message: "Duplicate entry 'the secret plaintext' for key 'notes'"}

            runErr := run(migrator)
            if nil == runErr {
                t.Fatalf("%d/%s: expected the refused write reported", number, door)
            }

            rendered := fmt.Sprintf("%v %s %+v", runErr, strings.Join(exception.BuildCauseChain(runErr, 8), ": "), exception.LogContext(runErr))
            if true == strings.Contains(rendered, "secret") || false == strings.Contains(rendered, fmt.Sprint(number)) || false == strings.Contains(rendered, "migrate update failed") {
                t.Fatalf("%d/%s: expected the number without the quoted value, got %s", number, door, rendered)
            }
        }
    }
}

func TestMigrate_AWithheldRefusalKeepsItsTypedNumber(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, withRows(judgeTable(accountsColumns, accountsPrimary, nil), []string{"id", "notes"}, [][]driver.Value{{"7", "plain"}}))
    stub.execErr = &mysql.MySQLError{Number: 1644, SQLState: [5]byte{'4', '5', '0', '0', '0'}, Message: "trigger refused plain"}

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})

    var serverErr *mysql.MySQLError
    chain := strings.Join(exception.BuildCauseChain(runErr, 8), ": ")
    if false == errors.As(runErr, &serverErr) || 1644 != serverErr.Number || [5]byte{'4', '5', '0', '0', '0'} != serverErr.SQLState {
        t.Fatalf("expected the number and the SQLSTATE typed in the chain, got %v", runErr)
    }

    if true == strings.Contains(chain+fmt.Sprint(exception.LogContext(runErr)), "plain") || false == strings.Contains(chain, "1644 (45000)") {
        t.Fatalf("expected the number in the chain, the value withheld, got %s", chain)
    }
}

func TestMigrate_ARefusalThatIsNotTheServersIsKeptWhole(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, withRows(judgeTable(accountsColumns, accountsPrimary, nil), []string{"id", "notes"}, [][]driver.Value{{"7", "plain"}}))
    stub.execErr = context.Canceled

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
    if nil == runErr || false == errors.Is(runErr, context.Canceled) || false == strings.Contains(runErr.Error(), "interrupted") {
        t.Fatalf("expected the cancelled write named an interruption, got %v", runErr)
    }

    var serverErr *mysql.MySQLError
    if true == errors.As(runErr, &serverErr) {
        t.Fatalf("expected no server refusal in the chain, got %v", serverErr)
    }
}

func TestMigrate_ACancellationOnTheSchemaLookupIsAnInterruption(t *testing.T) {
    migrator, _ := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil))
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    for door, run := range map[string]func() (int, error){
        "encrypt":   func() (int, error) { return migrator.MigrateEncrypt(cancelledContext, TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}) },
        "reencrypt": func() (int, error) { return migrator.MigrateReencrypt(cancelledContext, TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}, "v1") },
        "decrypt":   func() (int, error) { return migrator.MigrateDecrypt(cancelledContext, TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}) },
    } {
        _, runErr := run()

        var interruption *exception.Error
        if false == errors.As(runErr, &interruption) || false == strings.Contains(interruption.Message(), "interrupted") || "accounts" != interruption.Context()["table"] || 0 != interruption.Context()["processed"] {
            t.Fatalf("%s: expected the cancelled lookup named an interruption with the table and nothing processed, got %v", door, runErr)
        }
    }
}

func TestEnsureColumnCapacity_ReadsTheColumnInTheStoredSpelling(t *testing.T) {
    for door, run := range map[string]func(*Migrator, TableSpec) error{
        "encrypt": func(migrator *Migrator, spec TableSpec) error {
            return migrator.EnsureColumnCapacity(context.Background(), spec)
        },
        "reencrypt": func(migrator *Migrator, spec TableSpec) error {
            return migrator.EnsureColumnCapacityForReencrypt(context.Background(), spec, "v1")
        },
    } {
        migrator, stub := newScriptedMigrator(t, judgeTable([][]driver.Value{{"id"}, {"email"}}, accountsPrimary, [][]driver.Value{{"email", "latin1"}}))

        if capacityErr := run(migrator, TableSpec{Table: "accounts", Columns: []string{"Email"}}); nil != capacityErr {
            t.Fatalf("%s: capacity: %v", door, capacityErr)
        }

        converted := false
        for _, query := range stub.recorded() {
            if true == strings.Contains(query, "CONVERT(`email` USING utf8mb4)") {
                converted = true
            }
        }

        if false == converted {
            t.Fatalf("%s: expected the width probe on the stored latin1 column, got %v", door, stub.recorded())
        }

        everyQueryInTheStoredSpelling(t, door, stub, "`Email`")
    }
}

func liveJudgeDatabase(t *testing.T, table string, createSql string, inserts ...string) *sql.DB {
    t.Helper()

    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping the live schema judge pin")
    }

    sqlDb, openErr := sql.Open("mysql", dsn)
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    t.Cleanup(func() { _ = sqlDb.Close() })

    ctx := context.Background()
    _, _ = sqlDb.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)
    if _, createErr := sqlDb.ExecContext(ctx, createSql); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }
    t.Cleanup(func() { _, _ = sqlDb.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

    for _, insertSql := range inserts {
        if _, insertErr := sqlDb.ExecContext(ctx, insertSql); nil != insertErr {
            t.Fatalf("insert: %v", insertErr)
        }
    }

    return sqlDb
}

func liveEmails(t *testing.T, sqlDb *sql.DB, table string, key string) []string {
    t.Helper()

    rows, queryErr := sqlDb.QueryContext(context.Background(), "SELECT email FROM "+table+" ORDER BY "+key+", email")
    if nil != queryErr {
        t.Fatalf("read back: %v", queryErr)
    }
    defer rows.Close()

    emails := []string{}
    for rows.Next() {
        var email string
        if scanErr := rows.Scan(&email); nil != scanErr {
            t.Fatalf("scan: %v", scanErr)
        }

        emails = append(emails, email)
    }

    return emails
}

func TestMigrateEncrypt_LiveANonUniqueKeyIsRefusedBeforeTheFirstRow(t *testing.T) {
    table := "migrate_judge_non_unique_record"
    sqlDb := liveJudgeDatabase(
        t,
        table,
        "CREATE TABLE "+table+" (grp INT NULL, email VARCHAR(255) NOT NULL, KEY (grp))",
        "INSERT INTO "+table+" (grp, email) VALUES (1, 'a@x'), (1, 'b@x'), (1, 'c@x'), (2, 'd@x')",
    )
    migrator := NewMigrator(bun.NewDB(sqlDb, mysqldialect.New()), NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()})))

    processed, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: table, PrimaryKey: "grp", Columns: []string{"email"}, BatchSize: 2})
    if nil == runErr || false == strings.Contains(runErr.Error(), "the paging key is not unique: grp of "+table) || 0 != processed {
        t.Fatalf("expected the non-unique key refused before the first row, got processed=%d %v", processed, runErr)
    }

    if "[a@x b@x c@x d@x]" != fmt.Sprint(liveEmails(t, sqlDb, table, "grp")) {
        t.Fatalf("expected every row untouched, got %v", liveEmails(t, sqlDb, table, "grp"))
    }
}

func TestMigrateEncrypt_LiveAColumnSpelledInAnotherCaseConverts(t *testing.T) {
    table := "migrate_judge_spelling_record"
    sqlDb := liveJudgeDatabase(
        t,
        table,
        "CREATE TABLE "+table+" (id INT NOT NULL PRIMARY KEY, email VARCHAR(255) CHARACTER SET latin1 NULL)",
        "INSERT INTO "+table+" (id, email) VALUES (1, 'café'), (2, 'plain')",
    )
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()}))
    migrator := NewMigrator(bun.NewDB(sqlDb, mysqldialect.New()), cipher)

    processed, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: table, PrimaryKey: "ID", Columns: []string{"Email"}})
    if nil != runErr || 2 != processed {
        t.Fatalf("expected both rows converted, got processed=%d %v", processed, runErr)
    }

    for index, plaintext := range []string{"café", "plain"} {
        decrypted, decryptErr := cipher.Decrypt(liveEmails(t, sqlDb, table, "id")[index])
        if nil != decryptErr || plaintext != decrypted {
            t.Fatalf("row %d: expected the seal of %q, got %q %v", index+1, plaintext, decrypted, decryptErr)
        }
    }
}

func TestMigrateDecrypt_LiveAStrictSessionRefusesTheLossyWriteAndKeepsTheSeal(t *testing.T) {
    table := "migrate_judge_lossy_record"
    sqlDb := liveJudgeDatabase(t, table, "CREATE TABLE "+table+" (id BIGINT NOT NULL PRIMARY KEY, email VARCHAR(1024) CHARACTER SET latin1 NULL)")
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()}))
    sealed, sealErr := cipher.Encrypt("secret žluťoučký kůň")
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    if _, insertErr := sqlDb.ExecContext(context.Background(), "INSERT INTO "+table+" (id, email) VALUES (1, ?)", sealed); nil != insertErr {
        t.Fatalf("insert: %v", insertErr)
    }

    migrator := NewMigrator(bun.NewDB(sqlDb, mysqldialect.New()), cipher)

    _, runErr := migrator.MigrateDecrypt(context.Background(), TableSpec{Table: table, PrimaryKey: "id", Columns: []string{"email"}})
    var serverErr *mysql.MySQLError
    if nil == runErr || false == strings.Contains(runErr.Error(), "migrate update failed") || false == errors.As(runErr, &serverErr) || (3988 != serverErr.Number && 1366 != serverErr.Number) {
        t.Fatalf("expected the write refused by the server, got %v", runErr)
    }

    rendered := fmt.Sprintf("%v %s %+v", runErr, strings.Join(exception.BuildCauseChain(runErr, 8), ": "), exception.LogContext(runErr))
    if true == strings.Contains(rendered, "secret") || true == strings.Contains(rendered, "\\xC5") {
        t.Fatalf("expected no plaintext in the refusal, got %s", rendered)
    }

    if sealed != liveEmails(t, sqlDb, table, "id")[0] {
        t.Fatal("expected the stored seal byte-identical after the refused write")
    }
}

func TestMigrateEncrypt_LiveANullableUniqueKeyRunsAndANullKeyIsRefused(t *testing.T) {
    table := "migrate_judge_nullable_unique_record"
    sqlDb := liveJudgeDatabase(
        t,
        table,
        "CREATE TABLE "+table+" (code BIGINT NULL, email VARCHAR(1024) NOT NULL, UNIQUE KEY uq_code (code))",
        "INSERT INTO "+table+" (code, email) VALUES (1, 'a@x'), (2, 'b@x')",
    )
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()}))
    migrator := NewMigrator(bun.NewDB(sqlDb, mysqldialect.New()), cipher)

    processed, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: table, PrimaryKey: "code", Columns: []string{"email"}})
    if nil != runErr || 2 != processed {
        t.Fatalf("expected both rows converted over the nullable unique key, got processed=%d %v", processed, runErr)
    }

    if _, insertErr := sqlDb.ExecContext(context.Background(), "INSERT INTO "+table+" (code, email) VALUES (NULL, 'c@x')"); nil != insertErr {
        t.Fatalf("insert: %v", insertErr)
    }

    processed, runErr = migrator.MigrateDecrypt(context.Background(), TableSpec{Table: table, PrimaryKey: "code", Columns: []string{"email"}})
    if nil == runErr || false == strings.Contains(runErr.Error(), "migrate read a NULL primary key value") || 0 != processed {
        t.Fatalf("expected the NULL key refused before a row is written, got processed=%d %v", processed, runErr)
    }

    if "c@x" != liveEmails(t, sqlDb, table, "code")[0] {
        t.Fatalf("expected the NULL-keyed row untouched, got %v", liveEmails(t, sqlDb, table, "code"))
    }
}

func TestMigrateEncrypt_LiveAViewOverAPrimaryKeyedTableConverts(t *testing.T) {
    table := "migrate_judge_view_base_record"
    view := "migrate_judge_view_record"
    sqlDb := liveJudgeDatabase(
        t,
        table,
        "CREATE TABLE "+table+" (id INT NOT NULL PRIMARY KEY, email VARCHAR(1024) NULL)",
        "INSERT INTO "+table+" (id, email) VALUES (1, 'a@x'), (2, 'b@x')",
    )
    ctx := context.Background()
    _, _ = sqlDb.ExecContext(ctx, "DROP VIEW IF EXISTS "+view)
    if _, createErr := sqlDb.ExecContext(ctx, "CREATE VIEW "+view+" AS SELECT id, email FROM "+table); nil != createErr {
        t.Fatalf("create view: %v", createErr)
    }
    t.Cleanup(func() { _, _ = sqlDb.ExecContext(context.Background(), "DROP VIEW IF EXISTS "+view) })

    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newRampKey()}))
    migrator := NewMigrator(bun.NewDB(sqlDb, mysqldialect.New()), cipher)

    processed, runErr := migrator.MigrateEncrypt(ctx, TableSpec{Table: view, PrimaryKey: "ID", Columns: []string{"Email"}})
    if nil != runErr || 2 != processed {
        t.Fatalf("expected both rows converted through the view, got processed=%d %v", processed, runErr)
    }

    for index, plaintext := range []string{"a@x", "b@x"} {
        decrypted, decryptErr := cipher.Decrypt(liveEmails(t, sqlDb, table, "id")[index])
        if nil != decryptErr || plaintext != decrypted {
            t.Fatalf("row %d: expected the seal of %q, got %q %v", index+1, plaintext, decrypted, decryptErr)
        }
    }
}
