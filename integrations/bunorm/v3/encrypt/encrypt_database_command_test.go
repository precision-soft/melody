package encrypt

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"

    "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    melodycli "github.com/precision-soft/melody/v3/cli"
    "github.com/precision-soft/melody/v3/exception"
)

func TestEncryptDatabaseCommand_ConcurrentRunsShareOneResolvedMigrator(t *testing.T) {
    var resolveCount atomic.Int32

    command := NewEncryptDatabaseCommandFromResolver(
        func() (*bun.DB, error) {
            resolveCount.Add(1)
            time.Sleep(50 * time.Millisecond)

            return newMysqlDatabase(), nil
        },
        NewFakeCipher(),
    )

    migrators := make(chan *Migrator, 8)

    var waitGroup sync.WaitGroup
    for index := 0; index < 8; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            migrator, resolveErr := command.resolveMigrator()
            if nil != resolveErr {
                t.Errorf("unexpected resolve error: %v", resolveErr)

                return
            }

            migrators <- migrator
        }()
    }

    waitGroup.Wait()
    close(migrators)

    first := <-migrators
    if nil == first {
        t.Fatal("expected a resolved migrator")
    }

    for migrator := range migrators {
        if first != migrator {
            t.Fatal("expected every concurrent run to share the one memoized migrator")
        }
    }

    if 1 != resolveCount.Load() {
        t.Fatalf("expected the database resolver to run exactly once, ran %d times", resolveCount.Load())
    }
}

func runEncryptDatabaseCommand(t *testing.T, command *EncryptDatabaseCommand, extraArgs []string) error {
    t.Helper()

    return melodycli.DispatchCommand(
        context.Background(),
        command,
        fakeRuntime{},
        append([]string{command.Name()}, extraArgs...),
        io.Discard,
    )
}

func TestEncryptDatabaseCommand_RefusesANegativeBatch(t *testing.T) {
    command := NewEncryptDatabaseCommand(newMysqlDatabase(), NewFakeCipher())

    runErr := runEncryptDatabaseCommand(t, command, []string{"--table", "accounts", "--column", "iban", "--batch", "-1"})
    if nil == runErr {
        t.Fatalf("expected the negative batch to be refused")
    }

    if false == strings.Contains(runErr.Error(), "--batch must not be negative") {
        t.Fatalf("expected the refusal to name the flag, got: %v", runErr)
    }
}

func TestEncryptDatabaseCommand_RefusesAMissingTableOrColumnByName(t *testing.T) {
    for _, arguments := range [][]string{
        {},
        {"--table", "accounts"},
        {"--column", "iban"},
        {"--table", "accounts", "--column", ""},
        {"--table", "accounts", "--column", "iban", "--column", ""},
        {"--table", "accounts", "--column", "iban", "--primary-key", ""},
        {"--table", "", "--column", "iban"},
    } {
        command := NewEncryptDatabaseCommand(newMysqlDatabase(), NewFakeCipher())

        runErr := runEncryptDatabaseCommand(t, command, arguments)
        if nil == runErr {
            t.Fatalf("expected %v to be refused", arguments)
        }

        if false == strings.Contains(runErr.Error(), "--table, --primary-key and at least one --column are required, none of them empty") {
            t.Fatalf("expected the refusal of %v to name the flags, got: %v", arguments, runErr)
        }
    }
}

func TestEncryptDatabaseCommand_CarriesTheProcessedCountOnFailure(t *testing.T) {
    command := NewEncryptDatabaseCommand(newMysqlDatabase(), NewFakeCipher())

    runErr := runEncryptDatabaseCommand(t, command, []string{"--table", "accounts", "--column", "iban"})
    if nil == runErr {
        t.Fatalf("expected the offline database to fail the run")
    }

    if false == strings.Contains(runErr.Error(), "encrypt database migration failed") {
        t.Fatalf("expected the failure wrap, got: %v", runErr)
    }

    logContext := exception.LogContext(runErr)
    if _, hasProcessed := logContext["processedRows"]; false == hasProcessed {
        t.Fatalf("expected the failure to carry the processed count, got context: %v", logContext)
    }
}

func TestEncryptDatabaseCommand_WritesTheCauseChainOfARefusedRun(t *testing.T) {
    command := NewEncryptDatabaseCommand(
        bun.NewDB(sql.OpenDB(refusingConnector{message: "connection refused\r\x1b[2Kforged"}), mysqldialect.New()),
        NewFakeCipher(),
    )
    output := &bytes.Buffer{}
    errorOutput := &bytes.Buffer{}

    var runErr error
    engineCommand := &clicontract.CommandContext{
        Name:      command.Name(),
        Flags:     command.Flags(),
        Writer:    output,
        ErrWriter: errorOutput,
        ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {
        },
        Action: func(_ context.Context, actionCommand *clicontract.CommandContext) error {
            runErr = command.Run(fakeRuntime{}, actionCommand)

            return runErr
        },
    }
    _ = engineCommand.Run(context.Background(), []string{command.Name(), "--table", "accounts", "--column", "iban"})

    if nil == runErr {
        t.Fatalf("expected the refusing database to fail the run")
    }

    if "encrypt database migration failed" != runErr.Error() {
        t.Fatalf("expected the error to keep its message, got: %v", runErr)
    }

    expected := `encrypt database migration failed: migrate could not take a connection for the run: connection refused\r\x1b[2Kforged` + "\n"
    if expected != errorOutput.String() {
        t.Fatalf("expected the error stream to read %q, got %q", expected, errorOutput.String())
    }

    if 0 != output.Len() {
        t.Fatalf("expected nothing where the command's document goes, got %q", output.String())
    }
}

func TestEncryptDatabaseCommand_ATypedNilErrorWriterFallsBackToStandardError(t *testing.T) {
    var nilBuffer *bytes.Buffer

    for name, errorWriter := range map[string]io.Writer{"untyped nil": nil, "typed nil": nilBuffer} {
        engineCommand := &clicontract.CommandContext{ErrWriter: errorWriter}
        if os.Stderr != commandErrorWriter(engineCommand) {
            t.Fatalf("%s: expected standard error, got %T", name, commandErrorWriter(engineCommand))
        }
    }

    errorOutput := &bytes.Buffer{}
    if errorOutput != commandErrorWriter(&clicontract.CommandContext{ErrWriter: errorOutput}) {
        t.Fatalf("expected the command's own error writer")
    }
}

type refusingConnector struct {
    message string
}

func (instance refusingConnector) Connect(context.Context) (driver.Conn, error) {
    return nil, errors.New(instance.message)
}

func (instance refusingConnector) Driver() driver.Driver {
    return fakeDriver{}
}

func TestEncryptDatabaseCommand_IntegerFlagsParseInBaseTen(t *testing.T) {
    for _, flagName := range []string{"batch"} {
        for spelling, expected := range map[string]int{"010": 10, "08": 8} {
            observed := -1

            engineCommand := &clicontract.CommandContext{
                Name:  "probe",
                Flags: (&EncryptDatabaseCommand{}).Flags(),
                ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {
                },
                Action: func(_ context.Context, actionCommand *clicontract.CommandContext) error {
                    observed = actionCommand.Int(flagName)

                    return nil
                },
            }

            if runErr := engineCommand.Run(context.Background(), []string{"probe", "--" + flagName + "=" + spelling}); nil != runErr {
                t.Fatalf("expected --%s=%s to parse, got %v", flagName, spelling, runErr)
            }

            if expected != observed {
                t.Fatalf("expected --%s=%s to read as %d, got %d", flagName, spelling, expected, observed)
            }
        }
    }
}

func TestEncryptDatabaseCommand_AWithheldRefusalReachesTheErrorStreamWithItsNumber(t *testing.T) {
    cipher := NewCipher(NewStaticKeyProvider("v1", map[string][]byte{"v1": newKey(1)}))
    sealed, sealErr := cipher.Encrypt("top-secret@example.com")
    if nil != sealErr {
        t.Fatalf("seal: %v", sealErr)
    }

    migrator, stub := newScriptedMigrator(t, withRows(judgeTable(accountsColumns, accountsPrimary, nil), []string{"id", "notes"}, [][]driver.Value{{"7", sealed}}))
    stub.execErr = &mysql.MySQLError{Number: 1062, SQLState: [5]byte{'2', '3', '0', '0', '0'}, Message: "Duplicate entry 'top-secret@example.com' for key 'accounts.notes'"}

    command := NewEncryptDatabaseCommand(migrator.db, cipher)
    errorOutput := &bytes.Buffer{}

    var runErr error
    engineCommand := &clicontract.CommandContext{
        Name:      command.Name(),
        Flags:     command.Flags(),
        Writer:    &bytes.Buffer{},
        ErrWriter: errorOutput,
        ExitErrHandler: func(context.Context, *clicontract.CommandContext, error) {
        },
        Action: func(_ context.Context, actionCommand *clicontract.CommandContext) error {
            runErr = command.Run(fakeRuntime{}, actionCommand)

            return runErr
        },
    }
    _ = engineCommand.Run(context.Background(), []string{command.Name(), "--table", "accounts", "--column", "notes", "--mode", "decrypt"})

    if nil == runErr {
        t.Fatalf("expected the refused write to fail the run")
    }

    if false == strings.Contains(errorOutput.String(), "Error 1062 (23000)") || true == strings.Contains(errorOutput.String(), "top-secret") {
        t.Fatalf("expected the error stream to carry the number without the value, got %q", errorOutput.String())
    }
}
