package cli

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "go/parser"
    "go/token"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    urfavecli "github.com/urfave/cli/v3"
)

/* the boundary this file exists to hold: the flag parsing engine is named in the cli package and nowhere else in the module. Without the guard a catch-up that reaches for an engine type in a command, a module or a contract rebuilds the coupling this layer was built to remove, and nothing else in the tree would say so. */
func TestAdapter_TheEngineIsNamedOnlyInsideTheCliPackage(t *testing.T) {
    moduleRoot := findModuleRoot(t)
    cliPackageDirectory := filepath.Join(moduleRoot, "cli")

    offenders := []string{}

    walkErr := filepath.Walk(moduleRoot, func(path string, info os.FileInfo, walkErr error) error {
        if nil != walkErr {
            return walkErr
        }

        if true == info.IsDir() {
            return nil
        }

        if false == strings.HasSuffix(path, ".go") || true == strings.HasSuffix(path, "_test.go") {
            return nil
        }

        if true == strings.HasPrefix(path, cliPackageDirectory+string(filepath.Separator)) {
            return nil
        }

        fileSet := token.NewFileSet()
        parsed, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
        if nil != parseErr {
            return parseErr
        }

        for _, importSpec := range parsed.Imports {
            if true == strings.Contains(importSpec.Path.Value, "urfave/cli") {
                offenders = append(offenders, path)
            }
        }

        return nil
    })
    if nil != walkErr {
        t.Fatalf("failed to walk the module: %v", walkErr)
    }

    if 0 != len(offenders) {
        t.Fatalf("expected the engine to be named only inside the cli package, found it in %v", offenders)
    }
}

/* the positive control: the walk above answers nothing only because nothing outside the package imports the engine, not because it never looked — the same walk over the cli package itself must find the sources that do */
func TestAdapter_TheBoundaryWalkFindsTheEngineWhereItIsAllowed(t *testing.T) {
    moduleRoot := findModuleRoot(t)
    cliPackageDirectory := filepath.Join(moduleRoot, "cli")

    found := []string{}

    entries, readErr := os.ReadDir(cliPackageDirectory)
    if nil != readErr {
        t.Fatalf("failed to read the cli package: %v", readErr)
    }

    for _, entry := range entries {
        if true == entry.IsDir() || false == strings.HasSuffix(entry.Name(), ".go") || true == strings.HasSuffix(entry.Name(), "_test.go") {
            continue
        }

        fileSet := token.NewFileSet()
        parsed, parseErr := parser.ParseFile(fileSet, filepath.Join(cliPackageDirectory, entry.Name()), nil, parser.ImportsOnly)
        if nil != parseErr {
            t.Fatalf("failed to parse %q: %v", entry.Name(), parseErr)
        }

        for _, importSpec := range parsed.Imports {
            if true == strings.Contains(importSpec.Path.Value, "urfave/cli") {
                found = append(found, entry.Name())
            }
        }
    }

    if 0 == len(found) {
        t.Fatalf("expected the cli package to name the engine, found none — the walk above proves nothing")
    }
}

func findModuleRoot(t *testing.T) string {
    t.Helper()

    workingDirectory, workingDirectoryErr := os.Getwd()
    if nil != workingDirectoryErr {
        t.Fatalf("failed to read the working directory: %v", workingDirectoryErr)
    }

    /* the test runs in the cli package's own directory, and the module root is its parent */
    return filepath.Dir(workingDirectory)
}

func TestNewEngineFlag_CarriesTheDeclaredDefault(t *testing.T) {
    parsed := runFlagProbe(t, []clicontract.Flag{
        &clicontract.StringFlag{Name: "format", Value: "table"},
        &clicontract.BoolFlag{Name: "quiet", Value: true},
        &clicontract.IntFlag{Name: "limit", Value: 7},
    })

    if "table" != parsed.String("format") {
        t.Fatalf("expected the declared string default, got %q", parsed.String("format"))
    }
    if false == parsed.Bool("quiet") {
        t.Fatalf("expected the declared bool default")
    }
    if 7 != parsed.Int("limit") {
        t.Fatalf("expected the declared int default, got %d", parsed.Int("limit"))
    }
}

func TestNewEngineFlag_ParsesEachKindUnderItsOwnGrammar(t *testing.T) {
    parsed := runFlagProbe(
        t,
        []clicontract.Flag{
            &clicontract.StringFlag{Name: "format"},
            &clicontract.BoolFlag{Name: "quiet"},
            &clicontract.IntFlag{Name: "limit"},
            &clicontract.StringSliceFlag{Name: "role"},
        },
        "--format=json",
        "--quiet",
        "--limit=9",
        "--role=admin",
        "--role=editor",
    )

    if "json" != parsed.String("format") {
        t.Fatalf("expected the parsed string, got %q", parsed.String("format"))
    }
    if false == parsed.Bool("quiet") {
        t.Fatalf("expected the bool flag to be readable without a value, which is the grammar of its kind")
    }
    if 9 != parsed.Int("limit") {
        t.Fatalf("expected the parsed int, got %d", parsed.Int("limit"))
    }

    roles := parsed.StringSlice("role")
    if 2 != len(roles) || "admin" != roles[0] || "editor" != roles[1] {
        t.Fatalf("expected the repeated values in order, got %v", roles)
    }

    if false == parsed.IsSet("format") {
        t.Fatalf("expected a flag given on the command line to report as set")
    }
    if true == parsed.IsSet("nothing") {
        t.Fatalf("expected a flag nobody gave to report as unset")
    }
}

/* the validator has to survive the trip into the engine, installed and consulted on the value the engine parsed: dropped, every declared refusal in the tree becomes decoration */
func TestNewEngineFlag_CarriesTheDeclaredValidator(t *testing.T) {
    refusal := errors.New("refused by the declared validator")

    runErr := runFlagProbeError(
        t,
        []clicontract.Flag{
            &clicontract.IntFlag{
                Name: "limit",
                Validator: func(value int) error {
                    if 0 > value {
                        return refusal
                    }

                    return nil
                },
            },
        },
        "--limit=-1",
    )

    if nil == runErr || false == strings.Contains(runErr.Error(), refusal.Error()) {
        t.Fatalf("expected the declared validator to refuse the value, got %v", runErr)
    }
}

func TestNewEngineFlag_PanicsOnANilFlag(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        newEngineFlag(nil)
    }, "cli flag may not be nil")
}

type unsupportedKindFlag struct{}

func (instance *unsupportedKindFlag) Definition() clicontract.FlagDefinition {
    return clicontract.FlagDefinition{
        Kind: clicontract.FlagKind("duration"),
        Name: "timeout",
    }
}

/* a flag the engine has no parser for is refused where the command is registered: the alternative to a panic is a command whose flag silently does not exist */
func TestNewEngineFlag_PanicsOnAKindItCannotBuild(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        newEngineFlag(&unsupportedKindFlag{})
    }, "cli flag kind is not supported")
}

type mistypedDefaultFlag struct{}

func (instance *mistypedDefaultFlag) Definition() clicontract.FlagDefinition {
    return clicontract.FlagDefinition{
        Kind:  clicontract.FlagKindInt,
        Name:  "limit",
        Value: "seven",
    }
}

/* a default of the wrong type is refused where a wrong kind is: dropped, the flag would quietly default to zero and the declaration would read as honoured */
func TestNewEngineFlag_PanicsOnADefaultOfAnotherType(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        newEngineFlag(&mistypedDefaultFlag{})
    }, "cli flag default value does not match the flag kind")
}

/* a flag type written by hand and leaving its default unset means the zero value of its kind, not a refusal */
func TestNewEngineFlag_ADefinitionWithoutADefaultAnswersTheZeroValue(t *testing.T) {
    engineFlag := newEngineFlag(&noDefaultFlag{})

    stringFlag, isStringFlag := engineFlag.(*urfavecli.StringFlag)
    if false == isStringFlag {
        t.Fatalf("expected a string flag, got %T", engineFlag)
    }
    if "" != stringFlag.Value {
        t.Fatalf("expected the zero value, got %q", stringFlag.Value)
    }
}

type noDefaultFlag struct{}

func (instance *noDefaultFlag) Definition() clicontract.FlagDefinition {
    return clicontract.FlagDefinition{
        Kind: clicontract.FlagKindString,
        Name: "format",
    }
}

/* a flag declaring no validator must install none: the engine tells a nil validator from one that accepts everything, and the shape a reader of the engine's flag sees has to say the flag has nothing to refuse */
func TestEngineFlagValidator_AFlagWithoutAValidatorInstallsNone(t *testing.T) {
    engineFlag := newEngineFlag(&clicontract.StringFlag{Name: "format"})

    stringFlag, isStringFlag := engineFlag.(*urfavecli.StringFlag)
    if false == isStringFlag {
        t.Fatalf("expected a string flag, got %T", engineFlag)
    }
    if nil != stringFlag.Validator {
        t.Fatalf("expected no validator to be installed")
    }
}

/* the contract promises a plain decimal integer: with the base inferred from the literal, a zero-padded number from a shell loop or a cron entry read as octal or was refused, and a hexadecimal literal was accepted */
func TestNewEngineFlag_ReadsAnIntegerFlagAsDecimal(t *testing.T) {
    parsed := runFlagProbe(t, []clicontract.Flag{&clicontract.IntFlag{Name: "limit"}}, "--limit=010")
    if 10 != parsed.Int("limit") {
        t.Fatalf("expected a zero-padded literal to read as decimal ten, got %d", parsed.Int("limit"))
    }

    parsed = runFlagProbe(t, []clicontract.Flag{&clicontract.IntFlag{Name: "limit"}}, "--limit=08")
    if 8 != parsed.Int("limit") {
        t.Fatalf("expected a zero-padded eight to read as eight, got %d", parsed.Int("limit"))
    }

    runErr := runFlagProbeError(t, []clicontract.Flag{&clicontract.IntFlag{Name: "limit"}}, "--limit=0x10")
    if nil == runErr {
        t.Fatalf("expected a hexadecimal literal to be refused under the decimal base")
    }
}

/* the engine validates only a value it parsed, so a declared default the declared validator refuses was never seen by anyone: every invocation that left the flag out ran on the refused value while the declaration read as honoured — it is refused where the mistyped default is, at registration */
func TestNewEngineFlag_PanicsOnADefaultTheDeclaredValidatorRefuses(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        newEngineFlag(&clicontract.IntFlag{
            Name:  "limit",
            Value: -1,
            Validator: func(value int) error {
                if 0 > value {
                    return errors.New("negative refused")
                }

                return nil
            },
        })
    }, "cli flag default value is refused by the flag's own validator")
}

/* a shipped flag always declares its default — the typed field, zero when left unset — so a validator that refuses that zero is refused at registration like any other refused default; the command would otherwise run on a value its own validator refuses whenever the flag is left out */
func TestNewEngineFlag_PanicsOnAnUnsetDefaultTheDeclaredValidatorRefuses(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        newEngineFlag(&clicontract.IntFlag{
            Name: "limit",
            Validator: func(value int) error {
                if 1 > value {
                    return errors.New("a positive limit is required")
                }

                return nil
            },
        })
    }, "cli flag default value is refused by the flag's own validator")
}

/* a definition written by hand that carries no default at all declares nothing to validate: the zero value it means is nobody's declaration, so it is not refused at registration, and a value given on the command line is validated by the engine as before */
func TestNewEngineFlag_ADefinitionWithoutADefaultIsNotValidatedAtRegistration(t *testing.T) {
    flags := []clicontract.Flag{&positiveLimitFlagWithoutADefault{}}

    parsed := runFlagProbe(t, flags, "--limit=3")
    if 3 != parsed.Int("limit") {
        t.Fatalf("expected the given value, got %d", parsed.Int("limit"))
    }

    runErr := runFlagProbeError(t, flags, "--limit=0")
    if nil == runErr || false == strings.Contains(runErr.Error(), "a positive limit is required") {
        t.Fatalf("expected the engine to refuse the parsed zero, got %v", runErr)
    }
}

type positiveLimitFlagWithoutADefault struct{}

func (instance *positiveLimitFlagWithoutADefault) Definition() clicontract.FlagDefinition {
    return clicontract.FlagDefinition{
        Kind: clicontract.FlagKindInt,
        Name: "limit",
        Validator: func(value any) error {
            if 1 > value.(int) {
                return errors.New("a positive limit is required")
            }

            return nil
        },
    }
}

/* the parsed values are the caller's own: a command that sorts or truncates what it was handed would otherwise rewrite the parsed command line under every later reader of the same flag */
func TestEngineContext_AnswersACopyOfWhatTheEngineHolds(t *testing.T) {
    parsed := runFlagProbe(
        t,
        []clicontract.Flag{&clicontract.StringSliceFlag{Name: "role"}},
        "--role=admin",
        "--role=editor",
        "alpha",
        "beta",
    )

    handedRoles := parsed.StringSlice("role")
    handedRoles[0] = "rewritten"

    handedArguments := parsed.Arguments()
    handedArguments[0] = "rewritten"

    if "admin" != parsed.StringSlice("role")[0] {
        t.Fatalf("expected the parsed values to survive the caller's write, got %v", parsed.StringSlice("role"))
    }
    if "alpha" != parsed.Arguments()[0] {
        t.Fatalf("expected the arguments to survive the caller's write, got %v", parsed.Arguments())
    }
}

/* the engine leaves the stream nil on a command that was never given one, and every caller downstream would otherwise repeat the same guard on its first written line */
func TestNewEngineContext_AnswersADiscardingWriterForACommandWithoutOne(t *testing.T) {
    commandContext := newEngineContext(&urfavecli.Command{Name: "probe"})

    if io.Discard != commandContext.Writer() {
        t.Fatalf("expected the discarding writer")
    }
}

func TestNewEngineContext_AnswersTheCommandsOwnWriter(t *testing.T) {
    buffer := &bytes.Buffer{}
    commandContext := newEngineContext(&urfavecli.Command{Name: "probe", Writer: buffer})

    if buffer != commandContext.Writer() {
        t.Fatalf("expected the command's own writer")
    }
}

/* runFlagProbe parses one command line against the melody flags handed to it and answers the context a command would have read */
func runFlagProbe(t *testing.T, flags []clicontract.Flag, arguments ...string) clicontract.Context {
    t.Helper()

    var parsed clicontract.Context

    engineCommand := &urfavecli.Command{
        Name:      "probe",
        Flags:     newEngineFlags(flags),
        Writer:    io.Discard,
        ErrWriter: io.Discard,
        Action: func(actionContext context.Context, actionCommand *urfavecli.Command) error {
            parsed = newEngineContext(actionCommand)

            return nil
        },
        ExitErrHandler: func(handlerContext context.Context, handlerCommand *urfavecli.Command, handlerErr error) {
        },
    }

    runErr := engineCommand.Run(context.Background(), append([]string{"probe"}, arguments...))
    if nil != runErr {
        t.Fatalf("expected the probe command line to parse, got %v", runErr)
    }

    if nil == parsed {
        t.Fatalf("expected the probe action to run")
    }

    return parsed
}

func runFlagProbeError(t *testing.T, flags []clicontract.Flag, arguments ...string) error {
    t.Helper()

    engineCommand := &urfavecli.Command{
        Name:      "probe",
        Flags:     newEngineFlags(flags),
        Writer:    io.Discard,
        ErrWriter: io.Discard,
        Action: func(actionContext context.Context, actionCommand *urfavecli.Command) error {
            return nil
        },
        ExitErrHandler: func(handlerContext context.Context, handlerCommand *urfavecli.Command, handlerErr error) {
        },
    }

    return engineCommand.Run(context.Background(), append([]string{"probe"}, arguments...))
}

/* a validator that panics on its default is the developer's own bug, but raw it named neither the flag nor the kind: it is refused at registration as the other three wiring mistakes are, naming the flag and what exploded */
func TestNewEngineFlag_ADefaultWhoseValidatorPanicsIsRefusedByFlagName(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the panicking validator to refuse the registration")
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic, got %#v", recoveredValue)
        }

        if false == strings.Contains(recoveredErr.Error(), "refused by the flag's own validator") {
            t.Fatalf("expected the registration refusal, got %v", recoveredErr)
        }

        typedError, isTyped := recoveredErr.(*exception.Error)
        if false == isTyped {
            t.Fatalf("expected *exception.Error, got %T", recoveredErr)
        }

        if "limit" != typedError.Context()["flagName"] {
            t.Fatalf("expected the flag named, got %+v", typedError.Context())
        }

        if false == strings.Contains(fmt.Sprint(typedError.Context()["refusal"]), "validator exploded") {
            t.Fatalf("expected the panic value carried in the refusal, got %+v", typedError.Context())
        }
    }()

    newEngineFlag(&clicontract.IntFlag{
        Name:  "limit",
        Value: 3,
        Validator: func(value int) error {
            panic("validator exploded")
        },
    })
}

/* an exit error raised by the validator is re-raised as itself, the way every recovery boundary of the framework re-raises it: read as a refusal carrying it, the process boundary — which reads the exit by type assertion — would have exited as for a plain failure */
func TestNewEngineFlag_AValidatorThatPanicsWithAnExitErrorReRaisesIt(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected the exit error to be re-raised")
        }

        exitErr, isExit := recoveredValue.(*exception.ExitError)
        if false == isExit {
            t.Fatalf("expected the exit error itself, got %T: %v", recoveredValue, recoveredValue)
        }

        if 7 != exitErr.ExitCode() {
            t.Fatalf("expected exit code 7, got %d", exitErr.ExitCode())
        }
    }()

    newEngineFlag(&clicontract.IntFlag{
        Name:  "limit",
        Value: 3,
        Validator: func(value int) error {
            panic(exception.NewExitError(7, exception.NewError("the validator decided an exit", nil, nil)))
        },
    })
}

/* one flag of each kind under the capability named, so a mapping dropped on one kind fails its own case */
func eachFlagKind(required bool, aliases []string, hidden bool) map[string]clicontract.Flag {
    return map[string]clicontract.Flag{
        "text":   &clicontract.StringFlag{Name: "text", Required: required, Aliases: aliases, Hidden: hidden},
        "switch": &clicontract.BoolFlag{Name: "switch", Required: required, Aliases: aliases, Hidden: hidden},
        "count":  &clicontract.IntFlag{Name: "count", Required: required, Aliases: aliases, Hidden: hidden},
        "role":   &clicontract.StringSliceFlag{Name: "role", Required: required, Aliases: aliases, Hidden: hidden},
    }
}

/* a required flag left out refuses the invocation before the command runs, naming the flag */
func TestNewEngineFlag_ARequiredFlagLeftOutIsRefusedByName(t *testing.T) {
    for name, flag := range eachFlagKind(true, nil, false) {
        runErr := runFlagProbeError(t, []clicontract.Flag{flag})
        if nil == runErr || false == strings.Contains(runErr.Error(), `"`+name+`"`) {
            t.Fatalf("%s: expected the invocation refused naming the required flag, got %v", name, runErr)
        }
    }
}

/* an alias is a further spelling the flag parses under, read back under the flag's name */
func TestNewEngineFlag_AnAliasParsesUnderTheFlagsName(t *testing.T) {
    arguments := map[string][]string{
        "text":   {"--t=value"},
        "switch": {"--t"},
        "count":  {"--t=7"},
        "role":   {"--t=admin"},
    }

    for name, flag := range eachFlagKind(false, []string{"t"}, false) {
        parsed := runFlagProbe(t, []clicontract.Flag{flag}, arguments[name]...)
        if false == parsed.IsSet(name) {
            t.Fatalf("%s: expected the value given under the alias read under the flag's name", name)
        }
    }
}

/* a hidden flag stays out of the help output and still parses */
func TestNewEngineFlag_AHiddenFlagIsNotVisibleAndStillParses(t *testing.T) {
    for name, flag := range eachFlagKind(false, nil, true) {
        visible, isVisibleFlag := newEngineFlag(flag).(urfavecli.VisibleFlag)
        if false == isVisibleFlag || true == visible.IsVisible() {
            t.Fatalf("%s: expected the engine flag hidden from the help output", name)
        }
    }

    parsed := runFlagProbe(t, []clicontract.Flag{&clicontract.StringFlag{Name: "secret-mode", Hidden: true}}, "--secret-mode=on")
    if "on" != parsed.String("secret-mode") {
        t.Fatalf("expected the hidden flag parsed, got %q", parsed.String("secret-mode"))
    }
}
