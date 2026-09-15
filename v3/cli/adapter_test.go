package cli

import (
    "bytes"
    "context"
    "errors"
    "go/parser"
    "go/token"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    urfavecli "github.com/urfave/cli/v3"
)

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

func TestNewEngineFlag_PanicsOnADefaultOfAnotherType(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        newEngineFlag(&mistypedDefaultFlag{})
    }, "cli flag default value does not match the flag kind")
}

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
