package migrate

import (
    "bytes"
    "encoding/json"
    "errors"
    "strings"
    "testing"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/cli"
    "github.com/precision-soft/melody/v3/cli/output"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

func newBufferedOutput(noColor bool) (*commandOutput, *bytes.Buffer) {
    buffer := &bytes.Buffer{}
    option := output.DefaultOption()
    option.NoColor = noColor

    return newCommandOutput(buffer, option), buffer
}

func TestCommandOutput_PrintSuccess(t *testing.T) {
    colored, coloredBuffer := newBufferedOutput(false)
    colored.printSuccess("done")

    expectedColored := cli.AnsiGreen + "done" + cli.AnsiReset + "\n"
    if expectedColored != coloredBuffer.String() {
        t.Fatalf("colored success = %q, want %q", coloredBuffer.String(), expectedColored)
    }

    plain, plainBuffer := newBufferedOutput(true)
    plain.printSuccess("done")

    if "done\n" != plainBuffer.String() {
        t.Fatalf("plain success = %q, want %q", plainBuffer.String(), "done\n")
    }
}

func TestCommandOutput_PrintWarning(t *testing.T) {
    colored, coloredBuffer := newBufferedOutput(false)
    colored.printWarning("careful")

    expectedColored := cli.AnsiYellow + cli.AnsiBold + "WARNING: careful" + cli.AnsiReset + "\n"
    if expectedColored != coloredBuffer.String() {
        t.Fatalf("colored warning = %q, want %q", coloredBuffer.String(), expectedColored)
    }

    plain, plainBuffer := newBufferedOutput(true)
    plain.printWarning("careful")

    if "WARNING: careful\n" != plainBuffer.String() {
        t.Fatalf("plain warning = %q, want %q", plainBuffer.String(), "WARNING: careful\n")
    }
}

func TestCommandOutput_PrintError(t *testing.T) {
    silent, silentBuffer := newBufferedOutput(true)
    silent.printError(nil)

    if 0 != silentBuffer.Len() {
        t.Fatalf("nil error produced output: %q", silentBuffer.String())
    }

    colored, coloredBuffer := newBufferedOutput(false)
    colored.printError(errors.New("boom"))

    expectedColored := cli.AnsiRed + cli.AnsiBold + "ERROR: boom" + cli.AnsiReset + "\n"
    if expectedColored != coloredBuffer.String() {
        t.Fatalf("colored error = %q, want %q", coloredBuffer.String(), expectedColored)
    }

    plain, plainBuffer := newBufferedOutput(true)
    plain.printError(errors.New("boom"))

    if "ERROR: boom\n" != plainBuffer.String() {
        t.Fatalf("plain error = %q, want %q", plainBuffer.String(), "ERROR: boom\n")
    }
}

func TestCommandOutput_PrintDatabaseBlockWithNullDatabase(t *testing.T) {
    instance, buffer := newBufferedOutput(true)

    instance.printDatabaseBlock(&databaseIdentity{
        CurrentDatabase: nil,
        Hostname:        "db.internal",
        Port:            3306,
        CurrentUser:     "app@%",
        Version:         "8.0.36",
    })

    rendered := buffer.String()

    if false == strings.Contains(rendered, "DATABASE") {
        t.Fatalf("missing DATABASE header in %q", rendered)
    }

    if false == strings.Contains(rendered, "| database | <null>") {
        t.Fatalf("nil current database not rendered as <null> in %q", rendered)
    }

    if false == strings.Contains(rendered, "| host     | db.internal") {
        t.Fatalf("missing host row in %q", rendered)
    }

    if false == strings.Contains(rendered, "| port     | 3306") {
        t.Fatalf("missing port row in %q", rendered)
    }

    if false == strings.Contains(rendered, "| user     | app@%") {
        t.Fatalf("missing user row in %q", rendered)
    }

    if false == strings.Contains(rendered, "| version  | 8.0.36") {
        t.Fatalf("missing version row in %q", rendered)
    }
}

func TestCommandOutput_PrintDatabaseBlockTruncatesLongValues(t *testing.T) {
    instance, buffer := newBufferedOutput(true)

    databaseName := "a_very_long_database_name_beyond_column"
    instance.printDatabaseBlock(&databaseIdentity{
        CurrentDatabase: &databaseName,
        Hostname:        "an-extremely-long-hostname.example.com",
        Port:            5432,
        CurrentUser:     "user",
        Version:         "16",
    })

    rendered := buffer.String()

    if false == strings.Contains(rendered, "| database | a_very_long_dat... |") {
        t.Fatalf("long database name not truncated to 18 characters in %q", rendered)
    }

    if false == strings.Contains(rendered, "| host     | an-extremely-lo... |") {
        t.Fatalf("long hostname not truncated to 18 characters in %q", rendered)
    }
}

func TestCommandOutput_PrintDetailsBlockOrdersAndFiltersKeys(t *testing.T) {
    instance, buffer := newBufferedOutput(true)

    instance.printDetailsBlock(map[string]string{
        "name":       "create_users",
        "manager":    "<default>",
        "status":     "1 pending",
        "unexpected": "must not render",
    })

    rendered := buffer.String()

    if false == strings.Contains(rendered, "DETAILS") {
        t.Fatalf("missing DETAILS header in %q", rendered)
    }

    if true == strings.Contains(rendered, "unexpected") || true == strings.Contains(rendered, "must not render") {
        t.Fatalf("unknown key rendered in %q", rendered)
    }

    managerIndex := strings.Index(rendered, "| manager |")
    statusIndex := strings.Index(rendered, "| status  |")
    nameIndex := strings.Index(rendered, "| name    |")

    if 0 > managerIndex || 0 > statusIndex || 0 > nameIndex {
        t.Fatalf("missing detail rows in %q", rendered)
    }

    if false == (managerIndex < statusIndex && statusIndex < nameIndex) {
        t.Fatalf("detail rows are not rendered in the fixed key order: %q", rendered)
    }

    if true == strings.Contains(rendered, "| applied |") {
        t.Fatalf("absent key rendered in %q", rendered)
    }
}

func TestCommandOutput_PrintMigrationsBlock(t *testing.T) {
    empty, emptyBuffer := newBufferedOutput(true)
    empty.printMigrationsBlock("applied", "APPLIED", []string{})

    if 0 != emptyBuffer.Len() {
        t.Fatalf("empty migrations list produced output: %q", emptyBuffer.String())
    }

    instance, buffer := newBufferedOutput(true)
    longName := strings.Repeat("m", 60)
    instance.printMigrationsBlock("pending", "PENDING", []string{"20240101000000_create_users", longName})

    rendered := buffer.String()

    if false == strings.Contains(rendered, "PENDING") {
        t.Fatalf("missing block title in %q", rendered)
    }

    if false == strings.Contains(rendered, "| 20240101000000_create_users") {
        t.Fatalf("missing migration row in %q", rendered)
    }

    if false == strings.Contains(rendered, strings.Repeat("m", 46)+"...") {
        t.Fatalf("long migration name not truncated to 49 characters in %q", rendered)
    }
}

func TestCommandOutput_PrintFilesBlock(t *testing.T) {
    empty, emptyBuffer := newBufferedOutput(true)
    empty.printFilesBlock([]string{})

    if 0 != emptyBuffer.Len() {
        t.Fatalf("empty files list produced output: %q", emptyBuffer.String())
    }

    instance, buffer := newBufferedOutput(true)
    instance.printFilesBlock([]string{"/tmp/migrations/20240101000000_create_users.go"})

    rendered := buffer.String()

    if false == strings.Contains(rendered, "FILES") {
        t.Fatalf("missing FILES header in %q", rendered)
    }

    if false == strings.Contains(rendered, "  /tmp/migrations/20240101000000_create_users.go\n") {
        t.Fatalf("missing indented file line in %q", rendered)
    }
}

func TestCommandOutput_Newline(t *testing.T) {
    instance, buffer := newBufferedOutput(true)
    instance.newline()

    if "\n" != buffer.String() {
        t.Fatalf("newline wrote %q, want %q", buffer.String(), "\n")
    }
}

func TestTruncateString(t *testing.T) {
    if "short" != truncateString("short", 18) {
        t.Fatalf("short string was modified: %q", truncateString("short", 18))
    }

    exact := strings.Repeat("x", 18)
    if exact != truncateString(exact, 18) {
        t.Fatalf("string of exactly max length was modified: %q", truncateString(exact, 18))
    }

    truncated := truncateString(strings.Repeat("x", 30), 18)
    if strings.Repeat("x", 15)+"..." != truncated {
        t.Fatalf("truncated string = %q, want 15 characters plus ellipsis", truncated)
    }

    if 18 != len(truncated) {
        t.Fatalf("truncated string length = %d, want 18", len(truncated))
    }
}

/* the format widths pad by rune count, so the budget is runes too: a byte-sliced multi-byte value would be truncated even when it fits the column, with the cut landing mid-rune and rendering as a replacement character */
func TestTruncateString_CountsRunesNotBytes(t *testing.T) {
    seventeenRunes := strings.Repeat("愛", 17)
    if seventeenRunes != truncateString(seventeenRunes, 18) {
        t.Fatalf("a value that fits the column in runes was truncated: %q", truncateString(seventeenRunes, 18))
    }

    truncated := truncateString(strings.Repeat("愛", 30), 18)
    if strings.Repeat("愛", 15)+"..." != truncated {
        t.Fatalf("truncated string = %q, want 15 runes plus ellipsis", truncated)
    }

    if false == utf8.ValidString(truncated) {
        t.Fatalf("truncation split a rune: %q", truncated)
    }
}

/* a budget with no room for the ellipsis answers the clipped runes alone instead of slicing negative */
func TestTruncateString_SurvivesABudgetSmallerThanTheEllipsis(t *testing.T) {
    if "ab" != truncateString("abcdef", 2) {
        t.Fatalf("expected the clipped runes alone, got %q", truncateString("abcdef", 2))
    }

    if "" != truncateString("abcdef", 0) {
        t.Fatalf("expected nothing for a zero budget, got %q", truncateString("abcdef", 0))
    }

    if "" != truncateString("abcdef", -1) {
        t.Fatalf("expected nothing for a negative budget, got %q", truncateString("abcdef", -1))
    }
}

/* the machine document is not shaped by a display flag: verbosity decides how the TEXT renders — the readme says so in as many words — while json is the contract, and gating the blocks on --verbose left db:migrate --format=json answering an empty data object for a run that applied five migrations. */
func TestCommandOutput_WantsDetailFollowsTheFormatNotTheVerbosity(t *testing.T) {
    for _, testCase := range []struct {
        format          output.Format
        verbose         bool
        expectedOutcome bool
    }{
        {output.FormatJson, false, true},
        {output.FormatJson, true, true},
        {output.FormatTable, false, false},
        {output.FormatTable, true, true},
    } {
        option := output.DefaultOption()
        option.Format = testCase.format
        option.Verbose = testCase.verbose

        if testCase.expectedOutcome != newCommandOutput(&bytes.Buffer{}, option).wantsDetail() {
            t.Fatalf("format %s verbose %v: expected %v", testCase.format, testCase.verbose, testCase.expectedOutcome)
        }
    }
}

/* the document key and the display title were one string, so the same set of migrations arrived under APPLIED from db:status and under APPLIED MIGRATIONS from db:migrate, with no enumerable set of keys and a rename for readability breaking every consumer in silence */
func TestCommandOutput_PrintMigrationsBlockKeysTheDocumentApartFromTheTitle(t *testing.T) {
    jsonOption := output.DefaultOption()
    jsonOption.Format = output.FormatJson

    jsonBuffer := &bytes.Buffer{}
    jsonInstance := newCommandOutput(jsonBuffer, jsonOption)
    jsonInstance.printMigrationsBlock("applied", "APPLIED MIGRATIONS", []string{"20240101000000_create_users"})

    if _, keyedOnTheKey := jsonInstance.migrations["applied"]; false == keyedOnTheKey {
        t.Fatalf("expected the document to be keyed on the key, got %v", jsonInstance.migrations)
    }

    if _, keyedOnTheTitle := jsonInstance.migrations["APPLIED MIGRATIONS"]; true == keyedOnTheTitle {
        t.Fatalf("expected the display title to stay out of the document, got %v", jsonInstance.migrations)
    }

    /* the title is still what a person reads */
    textInstance, textBuffer := newBufferedOutput(true)
    textInstance.printMigrationsBlock("applied", "APPLIED MIGRATIONS", []string{"20240101000000_create_users"})

    if false == strings.Contains(textBuffer.String(), "APPLIED MIGRATIONS") {
        t.Fatalf("expected the display title in the text block, got %q", textBuffer.String())
    }

    if true == strings.Contains(textBuffer.String(), "applied\n") {
        t.Fatalf("expected the document key to stay out of the text block, got %q", textBuffer.String())
    }
}

/* "no current database" was the string <null>, indistinguishable from a database named literally <null> and readable only by a consumer who knew melody's own placeholder; the text block keeps rendering it, because a person reads an empty cell as a missing value either way */
func TestCommandOutput_TheAbsentDatabaseIsJsonNull(t *testing.T) {
    jsonOption := output.DefaultOption()
    jsonOption.Format = output.FormatJson

    buffer := &bytes.Buffer{}
    instance := newCommandOutput(buffer, jsonOption)
    instance.printDatabaseBlock(&databaseIdentity{Hostname: "localhost", Port: 5432, CurrentUser: "app"})

    if finishErr := instance.finish("db:status", time.Now(), nil); nil != finishErr {
        t.Fatalf("unexpected error: %v", finishErr)
    }

    document := struct {
        Data struct {
            Database map[string]any `json:"database"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v, got %q", decodeErr, buffer.String())
    }

    databaseValue, hasDatabaseKey := document.Data.Database["database"]
    if false == hasDatabaseKey {
        t.Fatalf("expected the database key to stay present, got %#v", document.Data.Database)
    }

    if nil != databaseValue {
        t.Fatalf("expected the absent database to be json null, got %#v", databaseValue)
    }

    /* a named database still arrives as its name, or the guard above would pass for a field that never carries anything */
    named := "orders"
    namedBuffer := &bytes.Buffer{}
    namedInstance := newCommandOutput(namedBuffer, jsonOption)
    namedInstance.printDatabaseBlock(&databaseIdentity{CurrentDatabase: &named})

    if finishErr := namedInstance.finish("db:status", time.Now(), nil); nil != finishErr {
        t.Fatalf("unexpected error: %v", finishErr)
    }

    namedDocument := struct {
        Data struct {
            Database map[string]any `json:"database"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal(namedBuffer.Bytes(), &namedDocument); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v, got %q", decodeErr, namedBuffer.String())
    }

    if "orders" != namedDocument.Data.Database["database"] {
        t.Fatalf("expected the named database in the document, got %q", namedBuffer.String())
    }
}

/* TestCommandOutput_FinishCarriesTheFailureDetailsAndCause pins the two fields the json envelope always declared and always answered null. The machine document is the contract a pipeline reads, and it was the one rendering that threw away what the error already carried: at the same instant, over the same value, the journal filed the connection, the pool sizing and the whole cause chain while stdout answered a single sentence beside `"details":null, "cause":null`. */
func TestCommandOutput_FinishCarriesTheFailureDetailsAndCause(t *testing.T) {
    buffer := &bytes.Buffer{}
    outputInstance := newCommandOutput(buffer, output.Option{Format: output.FormatJson})

    runErr := exception.NewError(
        "database connection failed",
        exceptioncontract.Context{"host": "mysql", "port": "3306"},
        exception.NewError(
            "Error 1045 (28000): Access denied for user 'melody'",
            nil,
            errors.New("dial refused"),
        ),
    )

    if finishErr := outputInstance.finish("db:migrate", time.Now(), runErr); nil == finishErr {
        t.Fatal("expected the command's own failure to stay the verdict")
    }

    document := struct {
        Error *struct {
            Code    string         `json:"code"`
            Message string         `json:"message"`
            Details map[string]any `json:"details"`
            Cause   *struct {
                Message string              `json:"message"`
                Details map[string][]string `json:"details"`
            } `json:"cause"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v; rendered %q", decodeErr, buffer.String())
    }

    if nil == document.Error {
        t.Fatalf("expected the failure inside the envelope, got %q", buffer.String())
    }

    if "mysql" != document.Error.Details["host"] || "3306" != document.Error.Details["port"] {
        t.Fatalf("expected the failure's own context in the details, got %#v", document.Error.Details)
    }

    if nil == document.Error.Cause {
        t.Fatalf("expected the failure to carry its cause, got %q", buffer.String())
    }

    if false == strings.Contains(document.Error.Cause.Message, "Access denied") {
        t.Fatalf("expected the cause to be the link under the failure, got %q", document.Error.Cause.Message)
    }

    if 2 > len(document.Error.Cause.Details["chain"]) {
        t.Fatalf("expected the whole chain beneath the failure, got %#v", document.Error.Cause.Details)
    }

    if false == strings.Contains(strings.Join(document.Error.Cause.Details["chain"], " | "), "dial refused") {
        t.Fatalf("expected the chain to reach the bottom, got %#v", document.Error.Cause.Details["chain"])
    }
}

/* an error carrying no context still answers an object, and one carrying no cause answers a null there: a field whose json type changes with the outcome cannot be consumed, while a cause that genuinely does not exist is honestly absent */
func TestCommandOutput_FinishKeepsTheDetailsAnObjectWithoutAContext(t *testing.T) {
    buffer := &bytes.Buffer{}
    outputInstance := newCommandOutput(buffer, output.Option{Format: output.FormatJson})

    if finishErr := outputInstance.finish("db:migrate", time.Now(), errors.New("bare failure")); nil == finishErr {
        t.Fatal("expected the command's own failure to stay the verdict")
    }

    document := struct {
        Error *struct {
            Details map[string]any `json:"details"`
            Cause   *struct {
                Message string `json:"message"`
            } `json:"cause"`
        } `json:"error"`
    }{}
    if decodeErr := json.Unmarshal(buffer.Bytes(), &document); nil != decodeErr {
        t.Fatalf("failed to decode the document: %v; rendered %q", decodeErr, buffer.String())
    }

    if nil == document.Error.Details {
        t.Fatalf("expected an empty details object rather than null, got %q", buffer.String())
    }

    if 0 != len(document.Error.Details) {
        t.Fatalf("expected the details to be empty for a bare failure, got %#v", document.Error.Details)
    }

    if nil != document.Error.Cause {
        t.Fatalf("expected no cause for a failure that has none, got %#v", document.Error.Cause)
    }
}

/* the error text came off the wire and the identity fields are the server's own answers, so the terminal rendering must escape control characters — visibly, before the cell widths are measured — while the json branch is left to its encoder. */
func TestCommandOutput_PrintErrorEscapesControlCharacters(t *testing.T) {
    plain, plainBuffer := newBufferedOutput(true)
    plain.printError(errors.New("boom\x1b[2J\rforged"))

    if `ERROR: boom\x1b[2J\rforged`+"\n" != plainBuffer.String() {
        t.Fatalf("expected the control characters escaped visibly, got %q", plainBuffer.String())
    }
}

func TestCommandOutput_PrintDatabaseBlockEscapesBeforeMeasuringTheCell(t *testing.T) {
    databaseName := "shop"
    identity := &databaseIdentity{
        CurrentDatabase: &databaseName,
        Hostname:        "db-host",
        Port:            3306,
        CurrentUser:     "app@%",
        Version:         "8.4\x1b[2J",
    }

    plain, plainBuffer := newBufferedOutput(true)
    plain.printDatabaseBlock(identity)

    rendered := plainBuffer.String()

    expectedVersionRow := `| version  | 8.4\x1b[2J         |`
    if false == strings.Contains(rendered, expectedVersionRow) {
        t.Fatalf("expected the version cell padded over its escaped spelling, got:\n%s", rendered)
    }

    if true == strings.Contains(rendered, "\x1b") {
        t.Fatalf("a raw escape byte reached the terminal:\n%s", rendered)
    }
}

func TestCommandOutput_PrintMigrationsBlockEscapesTheNames(t *testing.T) {
    plain, plainBuffer := newBufferedOutput(true)
    plain.printMigrationsBlock("applied", "APPLIED MIGRATIONS", []string{"20240101000000_create\rusers"})

    rendered := plainBuffer.String()

    if false == strings.Contains(rendered, `20240101000000_create\rusers`) {
        t.Fatalf("expected the carriage return escaped visibly, got:\n%s", rendered)
    }

    if true == strings.Contains(rendered, "\r") {
        t.Fatalf("a raw carriage return reached the terminal:\n%s", rendered)
    }
}
