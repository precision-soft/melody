package encrypt

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "net/url"
    "os"
    "strings"
    "testing"
    "time"

    "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    "github.com/precision-soft/melody/v4/exception"
)

/* judgeTable scripts accounts(id, notes) with the given columns, indexes, charsets and sql_mode, and one sealed-free row */
func judgeTable(columns [][]driver.Value, indexes [][]driver.Value, charsets [][]driver.Value, sqlMode string) []scriptedSqlResponse {
    return []scriptedSqlResponse{
        {fragment: "IS_NULLABLE", columns: []string{"COLUMN_NAME", "IS_NULLABLE"}, rows: columns},
        {fragment: "STATISTICS", columns: []string{"INDEX_NAME", "COLUMN_NAME", "NON_UNIQUE"}, rows: indexes},
        {fragment: "CHARACTER_SET_NAME", columns: []string{"COLUMN_NAME", "CHARACTER_SET_NAME"}, rows: charsets},
        {fragment: "@@SESSION.sql_mode", columns: []string{"@@SESSION.sql_mode"}, rows: [][]driver.Value{{sqlMode}}},
        {fragment: "information_schema.COLUMNS", columns: []string{"DATA_TYPE", "CHARACTER_MAXIMUM_LENGTH", "CHARACTER_OCTET_LENGTH"}, rows: [][]driver.Value{{"text", int64(65535), int64(65535)}}},
        {fragment: "MAX(LENGTH(", columns: []string{"longest"}, rows: [][]driver.Value{{nil}}},
        {fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{}},
    }
}

var accountsColumns = [][]driver.Value{{"id", "NO"}, {"notes", "YES"}}
var accountsPrimary = [][]driver.Value{{"PRIMARY", "id", int64(0)}}

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

func TestMigrate_ResolvesAColumnToTheSpellingTheTableStores(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, judgeTable([][]driver.Value{{"id", "NO"}, {"email", "YES"}}, accountsPrimary, [][]driver.Value{{"email", "latin1"}}, ""))
    stub.responses[len(stub.responses)-1] = scriptedSqlResponse{fragment: "ORDER BY", columns: []string{"id", "email"}, rows: [][]driver.Value{}}

    if _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "ID", Columns: []string{"Email"}}); nil != runErr {
        t.Fatalf("expected the run to complete over the stored spelling, got %v", runErr)
    }

    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "`Email`") || true == strings.Contains(query, "`ID`") {
            t.Fatalf("expected every query in the stored spelling, got %q", query)
        }
    }

    selected := false
    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "CONVERT(`email` USING utf8mb4)") || (true == strings.Contains(query, "SELECT `id`, `email` FROM `accounts`")) {
            selected = true
        }
    }

    if false == selected {
        t.Fatalf("expected the run over `email` with its charset, got %v", stub.recorded())
    }
}

/* two binding keys naming one stored column are refused, as two columns are: kept, the binding map iteration picks would differ from one run to the next */
func TestMigrate_RefusesAColumnBoundTwiceBeforeReadingTheTable(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil, "STRICT_TRANS_TABLES"))

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{
        Table:        "accounts",
        PrimaryKey:   "id",
        Columns:      []string{"notes"},
        SealBindings: map[string]ColumnSealBinding{"notes": {Label: "accounts.notes"}, "NOTES": {Label: "accounts.notes"}},
    })
    if nil == runErr || true == readTheTable(stub) {
        t.Fatalf("expected a refusal before the table is read, got %v", runErr)
    }

    if "TableSpecification.SealBindings" != refusalContext(t, runErr)["field"] {
        t.Fatalf("expected the field named, got %v", runErr)
    }
}

func TestMigrate_RefusesAnUnknownColumnAndOneNamedTwiceBeforeReadingTheTable(t *testing.T) {
    for name, columns := range map[string][]string{"unknown": {"missing"}, "named twice": {"notes", "NOTES"}} {
        migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil, "STRICT_TRANS_TABLES"))

        _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: columns})
        if nil == runErr || true == readTheTable(stub) {
            t.Fatalf("%s: expected a refusal before the table is read, got %v", name, runErr)
        }

        if "TableSpecification.Columns" != refusalContext(t, runErr)["field"] {
            t.Fatalf("%s: expected the field named, got %v", name, runErr)
        }
    }
}

func TestMigrate_JudgesThePagingKey(t *testing.T) {
    for name, testCase := range map[string]struct {
        columns [][]driver.Value
        indexes [][]driver.Value
        refused bool
    }{
        "primary":            {columns: accountsColumns, indexes: accountsPrimary},
        "unique not null":    {columns: accountsColumns, indexes: [][]driver.Value{{"uniq_id", "id", int64(0)}}},
        "unique nullable":    {columns: [][]driver.Value{{"id", "YES"}, {"notes", "YES"}}, indexes: [][]driver.Value{{"uniq_id", "id", int64(0)}}, refused: true},
        "composite":          {columns: accountsColumns, indexes: [][]driver.Value{{"PRIMARY", "id", int64(0)}, {"PRIMARY", "notes", int64(0)}}, refused: true},
        "not unique":         {columns: accountsColumns, indexes: [][]driver.Value{{"idx_id", "id", int64(1)}}, refused: true},
        "no index":           {columns: accountsColumns, indexes: [][]driver.Value{}, refused: true},
        "another column's":   {columns: accountsColumns, indexes: [][]driver.Value{{"PRIMARY", "notes", int64(0)}}, refused: true},
    } {
        migrator, stub := newScriptedMigrator(t, judgeTable(testCase.columns, testCase.indexes, nil, "STRICT_TRANS_TABLES"))

        _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
        if testCase.refused != (nil != runErr && true == strings.Contains(runErr.Error(), "the paging key is not unique")) {
            t.Fatalf("%s: expected refused %v, got %v", name, testCase.refused, runErr)
        }

        if true == testCase.refused && true == readTheTable(stub) {
            t.Fatalf("%s: expected the refusal before the table is read", name)
        }
    }
}

func TestMigrateDecrypt_RefusesANarrowCharsetUnderANonStrictSession(t *testing.T) {
    for name, testCase := range map[string]struct {
        charset any
        sqlMode string
        refused bool
    }{
        "latin1 non-strict":        {charset: "latin1", sqlMode: "NO_ENGINE_SUBSTITUTION", refused: true},
        "latin1 strict trans":      {charset: "latin1", sqlMode: "ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES"},
        "latin1 strict all":        {charset: "latin1", sqlMode: "STRICT_ALL_TABLES"},
        "utf8mb4 non-strict":       {charset: "utf8mb4", sqlMode: ""},
        "a binary type non-strict": {charset: nil, sqlMode: ""},
    } {
        migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, [][]driver.Value{{"notes", testCase.charset}}, testCase.sqlMode))

        _, runErr := migrator.MigrateDecrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
        refused := nil != runErr && true == strings.Contains(runErr.Error(), "non-strict sql_mode")
        if testCase.refused != refused {
            t.Fatalf("%s: expected refused %v, got %v", name, testCase.refused, runErr)
        }

        if true == refused {
            context := refusalContext(t, runErr)
            if "notes" != context["column"] || "latin1" != context["characterSet"] || testCase.sqlMode != context["sqlMode"] || true == readTheTable(stub) {
                t.Fatalf("%s: expected the column, charset and mode named before the table is read, got %v", name, context)
            }
        }
    }
}

func TestMigrateEncrypt_IsNotRefusedByTheDecryptSessionCheck(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, [][]driver.Value{{"notes", "latin1"}}, ""))

    if _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}); nil != runErr {
        t.Fatalf("expected an encrypt not judged on the session's mode, got %v", runErr)
    }

    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "sql_mode") {
            t.Fatal("expected an encrypt not to read the sql_mode")
        }
    }
}

func TestMigrate_WithholdsTheServerMessageOfADuplicateOrAnIncorrectString(t *testing.T) {
    for _, number := range []uint16{1062, 1366} {
        responses := judgeTable(accountsColumns, accountsPrimary, nil, "STRICT_TRANS_TABLES")
        responses[len(responses)-1] = scriptedSqlResponse{fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{{"7", "the secret plaintext"}}}
        migrator, stub := newScriptedMigrator(t, responses)
        stub.execErr = &mysql.MySQLError{Number: number, Message: "Duplicate entry 'the secret plaintext' for key 'notes'"}

        _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
        if nil == runErr {
            t.Fatalf("%d: expected the refused write reported", number)
        }

        rendered := fmt.Sprintf("%v %+v", runErr, exception.LogContext(runErr, exception.LogContextConfig{}))
        if true == strings.Contains(rendered, "the secret plaintext") || false == strings.Contains(rendered, fmt.Sprint(number)) {
            t.Fatalf("%d: expected the number without the quoted value, got %s", number, rendered)
        }
    }
}

func liveDecryptCharsetTable(t *testing.T, sqlMode string) (*Migrator, *sql.DB, string) {
    t.Helper()

    dsn := os.Getenv("MYSQL_DSN")
    if "" == dsn {
        t.Skip("MYSQL_DSN not set; skipping the live decrypt charset pin")
    }

    separator := "?"
    if true == strings.Contains(dsn, "?") {
        separator = "&"
    }

    sqlDb, openErr := sql.Open("mysql", dsn+separator+"sql_mode="+url.QueryEscape("'"+sqlMode+"'"))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }
    t.Cleanup(func() { _ = sqlDb.Close() })

    ctx := context.Background()
    table := "migrate_decrypt_charset_record"
    sqlDb.ExecContext(ctx, "DROP TABLE IF EXISTS "+table)
    if _, createErr := sqlDb.ExecContext(ctx, "CREATE TABLE "+table+" (id BIGINT NOT NULL PRIMARY KEY, notes VARCHAR(1024) CHARACTER SET latin1 NULL)"); nil != createErr {
        t.Fatalf("create: %v", createErr)
    }
    t.Cleanup(func() { _, _ = sqlDb.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

    cipher := NewCipher(NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(3)}}))
    sealed, sealErr := cipher.Encrypt("secret žluťoučký kůň")
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    if _, insertErr := sqlDb.ExecContext(ctx, "INSERT INTO "+table+" (id, notes) VALUES (1, ?)", sealed); nil != insertErr {
        t.Fatalf("insert: %v", insertErr)
    }

    return NewMigrator(bun.NewDB(sqlDb, mysqldialect.New()), cipher), sqlDb, sealed
}

func storedNotes(t *testing.T, sqlDb *sql.DB) string {
    t.Helper()

    var notes string
    if scanErr := sqlDb.QueryRowContext(context.Background(), "SELECT notes FROM migrate_decrypt_charset_record WHERE id = 1").Scan(&notes); nil != scanErr {
        t.Fatalf("read back: %v", scanErr)
    }

    return notes
}

func TestMigrateDecrypt_LiveANonStrictSessionIsRefusedAndTheSealKept(t *testing.T) {
    migrator, sqlDb, sealed := liveDecryptCharsetTable(t, "NO_ENGINE_SUBSTITUTION")

    _, runErr := migrator.MigrateDecrypt(context.Background(), TableSpecification{Table: "migrate_decrypt_charset_record", PrimaryKey: "id", Columns: []string{"Notes"}})
    if nil == runErr || false == strings.Contains(runErr.Error(), "non-strict sql_mode") {
        t.Fatalf("expected the non-strict session refused, got %v", runErr)
    }

    if sealed != storedNotes(t, sqlDb) {
        t.Fatal("expected the stored seal byte-identical after the refusal")
    }
}

func TestMigrateDecrypt_LiveAStrictSessionRefusesTheLossyWriteAndKeepsTheSeal(t *testing.T) {
    migrator, sqlDb, sealed := liveDecryptCharsetTable(t, "STRICT_TRANS_TABLES")

    _, runErr := migrator.MigrateDecrypt(context.Background(), TableSpecification{Table: "migrate_decrypt_charset_record", PrimaryKey: "id", Columns: []string{"notes"}})
    var serverErr *mysql.MySQLError
    if nil == runErr || false == strings.Contains(runErr.Error(), "migrate update failed") || true == strings.Contains(runErr.Error(), "non-strict") || false == errors.As(runErr, &serverErr) {
        t.Fatalf("expected the write refused by the server, the judge passing, got %v", runErr)
    }

    rendered := fmt.Sprintf("%v %+v", runErr, exception.LogContext(runErr, exception.LogContextConfig{}))
    /* MySQL 8.4 answers this write with 3988, a conversion impossible for the parameter, whose message names no value; a 1366 or a 1062, which do quote it, is withheld by the run */
    if true == strings.Contains(rendered, "secret") || true == strings.Contains(rendered, "\\xC5") {
        t.Fatalf("expected no plaintext in the refusal, got %s", rendered)
    }

    if sealed != storedNotes(t, sqlDb) {
        t.Fatal("expected the stored seal byte-identical after the refused write")
    }
}

func TestMigrate_TheJudgeAndTheWholeRunShareOneConnection(t *testing.T) {
    responses := judgeTable(accountsColumns, accountsPrimary, [][]driver.Value{{"notes", "latin1"}}, "STRICT_TRANS_TABLES")
    responses[len(responses)-1] = scriptedSqlResponse{fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{{"7", "plain"}}}
    migrator, stub := newScriptedMigrator(t, responses)
    migrator.db.DB.SetMaxIdleConns(0)

    if _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}); nil != runErr {
        t.Fatalf("run: %v", runErr)
    }

    used := map[int]bool{}
    for _, fragment := range []string{"IS_NULLABLE", "STATISTICS", "CHARACTER_SET_NAME", "ORDER BY", "UPDATE "} {
        for number := range stub.connectionsOf(fragment) {
            used[number] = true
        }
    }

    if 1 != len(used) {
        t.Fatalf("expected the judge and every query of the run on one connection, got %v", used)
    }
}

func TestMigrate_AFunctionalIndexIsAPartThatIsNotTheKey(t *testing.T) {
    for name, testCase := range map[string]struct {
        indexes [][]driver.Value
        refused bool
    }{
        "a functional index beside the primary key": {indexes: [][]driver.Value{{"PRIMARY", "id", int64(0)}, {"idx_lower", nil, int64(1)}}},
        "a unique index over the key and an expression": {indexes: [][]driver.Value{{"uniq_mix", "id", int64(0)}, {"uniq_mix", nil, int64(0)}}, refused: true},
    } {
        migrator, _ := newScriptedMigrator(t, judgeTable(accountsColumns, testCase.indexes, nil, "STRICT_TRANS_TABLES"))

        _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
        if testCase.refused != (nil != runErr && true == strings.Contains(runErr.Error(), "the paging key is not unique")) {
            t.Fatalf("%s: expected refused %v, got %v", name, testCase.refused, runErr)
        }
    }
}

func TestMigrate_ReencryptAndDecryptUseTheStoredSpellingToo(t *testing.T) {
    for door, run := range map[string]func(*Migrator, TableSpecification) error{
        "reencrypt": func(migrator *Migrator, specification TableSpecification) error {
            _, runErr := migrator.MigrateReencrypt(context.Background(), specification, "v1")

            return runErr
        },
        "decrypt": func(migrator *Migrator, specification TableSpecification) error {
            _, runErr := migrator.MigrateDecrypt(context.Background(), specification)

            return runErr
        },
    } {
        migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil, "STRICT_TRANS_TABLES"))

        if runErr := run(migrator, TableSpecification{Table: "accounts", PrimaryKey: "ID", Columns: []string{"Notes"}}); nil != runErr {
            t.Fatalf("%s: run: %v", door, runErr)
        }

        for _, query := range stub.recorded() {
            if true == strings.Contains(query, "`Notes`") || true == strings.Contains(query, "`ID`") {
                t.Fatalf("%s: expected every query in the stored spelling, got %q", door, query)
            }
        }
    }
}

func TestEnsureColumnCapacity_ReadsTheColumnInTheStoredSpelling(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, judgeTable([][]driver.Value{{"id", "NO"}, {"email", "YES"}}, accountsPrimary, [][]driver.Value{{"email", "latin1"}}, ""))

    if capacityErr := migrator.EnsureColumnCapacity(context.Background(), TableSpecification{Table: "accounts", Columns: []string{"Email"}}); nil != capacityErr {
        t.Fatalf("capacity: %v", capacityErr)
    }

    converted := false
    for _, query := range stub.recorded() {
        if true == strings.Contains(query, "CONVERT(`email` USING utf8mb4)") {
            converted = true
        }
    }

    if false == converted {
        t.Fatalf("expected the width probe on the stored latin1 column, got %v", stub.recorded())
    }
}

func TestMigrate_ABindingSpelledUnlikeItsColumnBindsTheStoredColumn(t *testing.T) {
    preserveRegistries(t)
    responses := judgeTable([][]driver.Value{{"id", "NO"}, {"email", "YES"}}, accountsPrimary, nil, "STRICT_TRANS_TABLES")
    responses[len(responses)-1] = scriptedSqlResponse{fragment: "ORDER BY", columns: []string{"id", "email"}, rows: [][]driver.Value{}}
    migrator, _ := newScriptedMigrator(t, responses)

    if _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"Email"}, SealBindings: map[string]ColumnSealBinding{"email": {Label: "accounts.email"}}}); nil != runErr {
        t.Fatalf("expected the binding resolved to the stored column, got %v", runErr)
    }
}

func TestMigrate_AWithheldRefusalKeepsItsTypedNumber(t *testing.T) {
    responses := judgeTable(accountsColumns, accountsPrimary, nil, "STRICT_TRANS_TABLES")
    responses[len(responses)-1] = scriptedSqlResponse{fragment: "ORDER BY", columns: []string{"id", "notes"}, rows: [][]driver.Value{{"7", "plain"}}}
    migrator, stub := newScriptedMigrator(t, responses)
    stub.execErr = &mysql.MySQLError{Number: 1644, SQLState: [5]byte{'4', '5', '0', '0', '0'}, Message: "trigger refused plain"}

    _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})

    var serverErr *mysql.MySQLError
    chain := strings.Join(exception.BuildCauseChain(runErr, 8), ": ")
    if false == errors.As(runErr, &serverErr) || 1644 != serverErr.Number || true == strings.Contains(chain+fmt.Sprint(exception.LogContext(runErr, exception.LogContextConfig{})), "plain") || false == strings.Contains(chain, "1644 (45000)") {
        t.Fatalf("expected the number typed and in the chain, the value withheld, got %s", chain)
    }
}

func TestMigrate_AStalePinnedConnectionIsTradedOnceBeforeTheJudge(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil, "STRICT_TRANS_TABLES"))
    stub.failFirstPing = true

    if _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}); nil != runErr {
        t.Fatalf("expected the run on a fresh connection, got %v", runErr)
    }

    if used := stub.connectionsOf("IS_NULLABLE"); 1 != len(used) || true == used[1] {
        t.Fatalf("expected the judge on the connection taken after the failed ping, got %v", used)
    }
}

func TestMigrate_ARefusedJudgeHandsItsConnectionBack(t *testing.T) {
    migrator, _ := newScriptedMigrator(t, judgeTable(accountsColumns, [][]driver.Value{}, nil, "STRICT_TRANS_TABLES"))
    migrator.db.DB.SetMaxOpenConns(1)

    for attempt := 0; attempt < 2; attempt++ {
        bounded, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        _, runErr := migrator.MigrateEncrypt(bounded, TableSpecification{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
        cancel()

        if nil == runErr || false == strings.Contains(runErr.Error(), "the paging key is not unique") {
            t.Fatalf("attempt %d: expected the judge's refusal, the connection handed back by the one before, got %v", attempt, runErr)
        }
    }
}

