package encrypt

import (
    "bytes"
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "io"
    "os"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    clicontract "github.com/precision-soft/melody/v4/cli/contract"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"

    melodycli "github.com/precision-soft/melody/v4/cli"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/logging"
)

/* the in-process cron runner dispatches overlapping executions of one command, so concurrent first runs must share one memoized migrator and run the database resolver exactly once — an unsynchronized memo races, and every loser opens a connection pool nothing ever closes. */
func TestEncryptDatabaseCommand_ConcurrentRunsShareOneResolvedMigrator(t *testing.T) {
    var resolveCount atomic.Int32

    command := NewEncryptDatabaseCommand(
        NewFakeCipher(),
        EncryptDatabaseCommandConfig{
            DatabaseResolver: func(ctx context.Context) (*bun.DB, error) {
                resolveCount.Add(1)
                time.Sleep(50 * time.Millisecond)

                return newMysqlDatabase(), nil
            },
        },
    )

    migrators := make(chan *Migrator, 8)

    var waitGroup sync.WaitGroup
    for index := 0; index < 8; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            migrator, resolveErr := command.resolveMigrator(t.Context())
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
        melodycli.DispatchConfig{},
    )
}

/* a negative batch silently became the default of 500, so the operator who believed they had throttled the run had not */
func TestEncryptDatabaseCommand_RefusesANegativeBatch(t *testing.T) {
    command := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})

    runErr := runEncryptDatabaseCommand(t, command, []string{"--table", "accounts", "--column", "iban", "--batch", "-1"})
    if nil == runErr {
        t.Fatalf("expected the negative batch to be refused")
    }

    if false == strings.Contains(runErr.Error(), "--batch must be at least 1") {
        t.Fatalf("expected the refusal to name the flag, got: %v", runErr)
    }

    if "TableSpec.BatchSize" != exception.LogContext(runErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name TableSpec.BatchSize, got: %v", runErr)
    }
}

/* a run without --table or --column, or with one of them present and empty, is refused by naming the three flags in the message itself, since a reason carried as the cause does not reach the terminal through the exit line */
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
        command := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})

        runErr := runEncryptDatabaseCommand(t, command, arguments)
        if nil == runErr {
            t.Fatalf("expected %v to be refused", arguments)
        }

        if false == strings.Contains(runErr.Error(), "--table, --primary-key and at least one --column are required, none of them empty") {
            t.Fatalf("expected the refusal of %v to name the flags, got: %v", arguments, runErr)
        }
    }
}

/* a run that stops halfway leaves the column mixed; the rows already converted are the one number that says what a re-run costs */
func TestEncryptDatabaseCommand_CarriesTheProcessedCountOnFailure(t *testing.T) {
    command := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})

    runErr := runEncryptDatabaseCommand(t, command, []string{"--table", "accounts", "--column", "iban"})
    if nil == runErr {
        t.Fatalf("expected the offline database to fail the run")
    }

    if false == strings.Contains(runErr.Error(), "encrypt database migration failed") {
        t.Fatalf("expected the failure wrap, got: %v", runErr)
    }

    logContext := exception.LogContext(runErr, exception.LogContextConfig{})
    if _, hasProcessed := logContext["processedRows"]; false == hasProcessed {
        t.Fatalf("expected the failure to carry the processed count, got context: %v", logContext)
    }
}

/* the reason of a refused run lives in its cause chain, which the exit line does not print, so the run writes the chain on the error stream, never where the command's document goes, escaped since a driver refusal echoes the values it is handed, and keeps the error's message for the journal */
func TestEncryptDatabaseCommand_WritesTheCauseChainOfARefusedRun(t *testing.T) {
    command := NewEncryptDatabaseCommand(
        NewFakeCipher(),
        EncryptDatabaseCommandConfig{
            Database: bun.NewDB(sql.OpenDB(refusingConnector{message: "connection refused\r\x1b[2Kforged"}), mysqldialect.New()),
        },
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

    expected := `encrypt database migration failed: migrate column charset lookup failed: connection refused\r\x1b[2Kforged` + "\n"
    if expected != errorOutput.String() {
        t.Fatalf("expected the error stream to read %q, got %q", expected, errorOutput.String())
    }

    if 0 != output.Len() {
        t.Fatalf("expected nothing where the command's document goes, got %q", output.String())
    }
}

/* a typed nil set as the error writer is a stream left unset, as the engine reads it: the run falls back to standard error and answers its refusal rather than panicking on the write */
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

/* the `--batch` flag parses in base ten, as the standard integer flags do: with the engine's inferred base --batch=010 would read as eight and --batch=08 would be refused */
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

func TestNewEncryptDatabaseCommand_RefusesNeitherOrBothDatabaseFields(t *testing.T) {
    resolver := func(ctx context.Context) (*bun.DB, error) {
        return newMysqlDatabase(), nil
    }

    for name, config := range map[string]EncryptDatabaseCommandConfig{
        "neither": {},
        "both":    {Database: newMysqlDatabase(), DatabaseResolver: resolver},
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isErr := recover().(error)
                if false == isErr || false == strings.Contains(recoveredErr.Error(), "needs exactly one of a database and a database resolver") {
                    t.Fatalf("expected the exclusive pair refused, got %v", recoveredErr)
                }

                fields := exception.LogContext(recoveredErr, exception.LogContextConfig{})["fields"]
                if "[EncryptDatabaseCommandConfig.Database EncryptDatabaseCommandConfig.DatabaseResolver]" != fmt.Sprintf("%v", fields) {
                    t.Fatalf("expected the two fields named, got %v", fields)
                }
            }()

            NewEncryptDatabaseCommand(NewFakeCipher(), config)
        })
    }
}

func TestNewEncryptDatabaseCommand_RefusesANilOrTypedNilCipher(t *testing.T) {
    var typedNil *fakeCipher

    for name, cipher := range map[string]Cipher{"nil": nil, "typed nil": typedNil} {
        t.Run(name, func(t *testing.T) {
            assertPanicsWithMessage(t, "encrypt database command cipher is nil", func() {
                NewEncryptDatabaseCommand(cipher, EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})
            })
        })
    }
}

func TestNewEncryptDatabaseCommand_NamesTheCommand(t *testing.T) {
    defaultNamed := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})
    if "melody:encrypt:database" != defaultNamed.Name() {
        t.Fatalf("expected the default name, got %q", defaultNamed.Name())
    }

    named := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{
        DatabaseResolver: func(ctx context.Context) (*bun.DB, error) {
            return newMysqlDatabase(), nil
        },
        CommandName: "melody:encrypt:database:crm",
    })
    if "melody:encrypt:database:crm" != named.Name() {
        t.Fatalf("expected the given name, got %q", named.Name())
    }
}

func TestNewEncryptDatabaseCommand_BuildsEagerlyFromADatabaseAndLazilyFromAResolver(t *testing.T) {
    eager := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase()})
    if nil == eager.migrator {
        t.Fatalf("expected the migrator built at construction from a prebuilt database")
    }

    resolverCalls := 0
    lazy := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{
        DatabaseResolver: func(ctx context.Context) (*bun.DB, error) {
            resolverCalls++

            return newMysqlDatabase(), nil
        },
    })
    if nil != lazy.migrator || 0 != resolverCalls {
        t.Fatalf("expected the resolver untouched at construction, got %d calls", resolverCalls)
    }
}

func TestBatchSizeOf_ReadsZeroAsAbsent(t *testing.T) {
    if false == batchSizeOf(0).IsAbsent() {
        t.Fatalf("expected zero to leave the size out")
    }

    if value, isSet := batchSizeOf(7).Value(); true != isSet || 7 != value {
        t.Fatalf("expected a positive flag handed through, got %v %v", value, isSet)
    }
}

func TestEncryptDatabaseCommand_RefusesANegativeBatchBeforeResolvingTheDatabase(t *testing.T) {
    resolverCalls := 0
    command := NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{
        DatabaseResolver: func(ctx context.Context) (*bun.DB, error) {
            resolverCalls++

            return nil, errors.New("the resolver ran")
        },
    })

    runErr := runEncryptDatabaseCommand(t, command, []string{"--table", "accounts", "--column", "iban", "--batch", "-1"})
    if nil == runErr || false == strings.Contains(runErr.Error(), "--batch must be at least 1") {
        t.Fatalf("expected the flag refusal, got %v", runErr)
    }

    if "TableSpec.BatchSize" != exception.LogContext(runErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the field named, got %v", runErr)
    }

    if false == strings.Contains(fmt.Sprintf("%v", exception.BuildCauseChain(runErr, 8)), "the limit is below its minimum") {
        t.Fatalf("expected the rule's refusal as the cause, got %v", exception.BuildCauseChain(runErr, 8))
    }

    if 0 != resolverCalls {
        t.Fatalf("expected the resolver untouched, got %d calls", resolverCalls)
    }
}

func TestEncryptDatabaseCommand_BindsADeclaredColumn(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    command, stub := newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}}, SealBindingConfig{Ref: accountsNotesBinding{}})

    if runErr := runCommandLine(t, command, logging.NewNopLogger(), "--table", "accounts", "--column", "notes"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    assertBoundSeal(t, cipherInstance, updatedValues(stub)["1"], "plain", "v1", false)
}

func TestEncryptDatabaseCommand_RunsAnUndeclaredColumnUnbound(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    command, stub := newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}}, SealBindingConfig{Ref: defaultSecretBinding{}})

    if runErr := runCommandLine(t, command, logging.NewNopLogger(), "--table", "accounts", "--column", "notes"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    if written := updatedValues(stub)["1"]; false == strings.HasPrefix(written, markerPrefix) {
        t.Fatalf("expected an unbound gcm1 seal, got %q", written)
    }
}

/* a physical table spelled otherwise than the label binds its column through the flag */
func TestEncryptDatabaseCommand_SealBindingFlagNamesTheLabel(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    command, stub := newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}}, SealBindingConfig{Ref: accountsNotesBinding{}, RefuseUnbound: true})

    if runErr := runCommandLine(t, command, logging.NewNopLogger(), "--table", "accounts_archive", "--column", "notes", "--seal-binding", "notes=accounts.notes"); nil == runErr || false == exception.ChainIs(runErr, ErrUnboundValueRefused) {
        t.Fatalf("expected the flag to carry the declaration's RefuseUnbound onto the plaintext row, got %v", runErr)
    }

    command, stub = newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}}, SealBindingConfig{Ref: accountsNotesBinding{}})

    if runErr := runCommandLine(t, command, logging.NewNopLogger(), "--table", "accounts_archive", "--column", "notes", "--seal-binding", "notes=accounts.notes"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    assertBoundSeal(t, cipherInstance, updatedValues(stub)["1"], "plain", "v1", false)
}

func TestEncryptDatabaseCommand_RefusesASealBindingFlagItCannotResolve(t *testing.T) {
    cipherInstance := newRotationCipher(t)

    for name, testCase := range map[string]struct {
        flagValues []string
        message    string
    }{
        "no separator":        {flagValues: []string{"notes"}, message: "--seal-binding must be column=label"},
        "no column":           {flagValues: []string{"=accounts.notes"}, message: "--seal-binding must be column=label"},
        "no label":            {flagValues: []string{"notes="}, message: "--seal-binding must be column=label"},
        "an unlisted column":  {flagValues: []string{"body=accounts.notes"}, message: "--seal-binding names a column that is not one of --column"},
        "an undeclared label": {flagValues: []string{"notes=leads.notes"}, message: "--seal-binding names a label that is not a declared seal binding of this command"},
        "a column twice":      {flagValues: []string{"notes=accounts.notes", "notes=accounts.notes"}, message: "--seal-binding names a column twice"},
        "another label over the column's own": {flagValues: []string{"notes=accounts.body"}, message: "--seal-binding names a label other than the column's own declared seal binding"},
    } {
        t.Run(name, func(t *testing.T) {
            flagValues := testCase.flagValues
            command, stub := newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}}, SealBindingConfig{Ref: accountsNotesBinding{}}, SealBindingConfig{Ref: accountsBodyBinding{}})

            arguments := []string{"--table", "accounts", "--column", "notes"}
            for _, flagValue := range flagValues {
                arguments = append(arguments, "--seal-binding", flagValue)
            }

            runErr := runCommandLine(t, command, logging.NewNopLogger(), arguments...)
            /* the flag's refusal is read on the terminal, so its message is what says which part of the line is wrong */
            if nil == runErr || testCase.message != runErr.Error() {
                t.Fatalf("expected %q, got %v", testCase.message, runErr)
            }

            if queries := runQueries(stub); 0 != len(queries) {
                t.Fatalf("expected no query before the refusal, saw %v", queries)
            }
        })
    }
}

func TestEncryptDatabaseCommand_LogsWhatItBound(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    unbound, _ := cipherInstance.Encrypt("unbound")
    command, _ := newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}, {"2", unbound}, {"3", unbound}}, SealBindingConfig{Ref: accountsNotesBinding{}})

    logger := &recordingLogger{}
    if runErr := runCommandLine(t, command, logger, "--table", "accounts", "--column", "notes"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    for _, record := range logger.records {
        if "encrypt database migration finished" != record.message {
            continue
        }

        if 3 != record.context["rows"] || 2 != record.context["boundFromUnbound"] || 1 != record.context["sealedFromPlaintext"] {
            t.Fatalf("expected rows 3, boundFromUnbound 2, sealedFromPlaintext 1, got %v", record.context)
        }

        if "map[notes:accounts.notes]" != fmt.Sprint(record.context["sealBindings"]) {
            t.Fatalf("expected the record to name the bound column, got %v", record.context["sealBindings"])
        }

        return
    }

    t.Fatalf("expected the closing record, got %v", logger.records)
}

func TestEncryptDatabaseCommand_RefusesASealBindingWithoutARef(t *testing.T) {
    var typedNil *pointerBinding

    for name, ref := range map[string]SealBindingRef{"nil": nil, "typed nil": typedNil} {
        t.Run(name, func(t *testing.T) {
            refusalContext := recoverRefusalContext(t, func() {
                NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase(), SealBindings: []SealBindingConfig{{Ref: accountsNotesBinding{}}, {Ref: ref}}})
            })

            if "EncryptDatabaseCommandConfig.SealBindings" != refusalContext["field"] || 1 != refusalContext["index"] {
                t.Fatalf("expected the refusal to name the field and index 1, got %v", refusalContext)
            }
        })
    }
}

/* a run that bound nothing says so on its closing record, so step two of the roll-out is checkable */
func TestEncryptDatabaseCommand_LogsAnUnboundRunAsBindingNothing(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    command, _ := newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}})

    logger := &recordingLogger{}
    if runErr := runCommandLine(t, command, logger, "--table", "accounts", "--column", "notes"); nil != runErr {
        t.Fatalf("unexpected run error: %v", runErr)
    }

    for _, record := range logger.records {
        if "encrypt database migration finished" == record.message {
            if "map[]" != fmt.Sprint(record.context["sealBindings"]) {
                t.Fatalf("expected no bound column on the record, got %v", record.context["sealBindings"])
            }

            return
        }
    }

    t.Fatalf("expected the closing record, got %v", logger.records)
}

/* MySQL folds the case of a column name and the label does not, so a --column spelled otherwise is refused rather than run unbound */
func TestEncryptDatabaseCommand_RefusesAColumnDifferingFromItsLabelByCase(t *testing.T) {
    cipherInstance := newRotationCipher(t)
    command, stub := newScriptedNotesCommand(t, cipherInstance, [][]driver.Value{{"1", "plain"}}, SealBindingConfig{Ref: accountsNotesBinding{}})

    runErr := runCommandLine(t, command, logging.NewNopLogger(), "--table", "accounts", "--column", "NOTES")
    if nil == runErr || "--table and --column differ only by case from a declared seal binding; spell them as the label does" != runErr.Error() {
        t.Fatalf("expected the case refusal, got %v", runErr)
    }

    if queries := runQueries(stub); 0 != len(queries) {
        t.Fatalf("expected no query before the refusal, saw %v", queries)
    }
}

func TestEncryptDatabaseCommand_RefusesALabelDeclaredTwice(t *testing.T) {
    refusalContext := recoverRefusalContext(t, func() {
        NewEncryptDatabaseCommand(NewFakeCipher(), EncryptDatabaseCommandConfig{Database: newMysqlDatabase(), SealBindings: []SealBindingConfig{{Ref: accountsNotesBinding{}}, {Ref: accountsNotesBinding{}, RefuseUnbound: true}}})
    })

    if "EncryptDatabaseCommandConfig.SealBindings" != refusalContext["field"] || 1 != refusalContext["index"] || "accounts.notes" != refusalContext["sealBinding"] {
        t.Fatalf("expected the refusal to name the field, index 1 and the label, got %v", refusalContext)
    }
}

func TestEncryptDatabaseCommand_RefusesOneLabelForTwoColumns(t *testing.T) {
    command, _ := newScriptedNotesCommand(t, newRotationCipher(t), [][]driver.Value{{"1", "plain"}}, SealBindingConfig{Ref: accountsNotesBinding{}})

    _, resolveErr := command.resolveSealBindings("archive", []string{"notes", "body"}, []string{"notes=accounts.notes", "body=accounts.notes"})
    if nil == resolveErr || "--seal-binding names one label for two columns" != resolveErr.Error() {
        t.Fatalf("expected the label named twice refused, got %v", resolveErr)
    }
}

func TestEncryptDatabaseCommand_RefusesAFlagItsModeDoesNotRead(t *testing.T) {
    for message, arguments := range map[string][]string{
        "--target-key is read by mode reencrypt alone": {"--table", "accounts", "--column", "notes", "--mode", "encrypt", "--target-key", "k2"},
        "--deterministic is not read by mode decrypt":  {"--table", "accounts", "--column", "notes", "--mode", "decrypt", "--deterministic"},
    } {
        command, _ := newScriptedNotesCommand(t, newRotationCipher(t), [][]driver.Value{{"1", "plain"}})

        runErr := runCommandLine(t, command, logging.NewNopLogger(), arguments...)
        if nil == runErr || message != runErr.Error() {
            t.Fatalf("expected %q, got %v", message, runErr)
        }
    }
}
