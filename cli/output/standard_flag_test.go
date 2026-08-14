package output

import (
    "context"
    "io"
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/cli/contract"
)

func runStandardFlagCommand(arguments []string) error {
    commandContext := &clicontract.CommandContext{
        Name:      "test",
        Flags:     StandardFlags(),
        Writer:    io.Discard,
        ErrWriter: io.Discard,
        ExitErrHandler: func(
            handlerContext context.Context,
            handlerCommandContext *clicontract.CommandContext,
            handlerErr error,
        ) {
        },
        Action: func(
            actionContext context.Context,
            actionCommandContext *clicontract.CommandContext,
        ) error {
            return nil
        },
    }

    commandArguments := make([]string, 0, len(arguments)+1)
    commandArguments = append(commandArguments, "test")
    commandArguments = append(commandArguments, arguments...)

    return commandContext.Run(context.Background(), commandArguments)
}

func TestStandardFlags_RejectAnUnsupportedFormat(t *testing.T) {
    for _, value := range []string{"JSON", "Table", "yaml", "jsonl", "text"} {
        t.Run(value, func(t *testing.T) {
            runErr := runStandardFlagCommand([]string{"--format=" + value})
            if nil == runErr {
                t.Fatalf("expected --format=%s to be rejected", value)
            }
            if false == strings.Contains(runErr.Error(), "format") {
                t.Fatalf("expected the error to name the format flag, got %q", runErr.Error())
            }
        })
    }
}

func TestStandardFlags_RejectAnUnsupportedOrder(t *testing.T) {
    for _, value := range []string{"ASC", "ascending", "descending", "sideways"} {
        t.Run(value, func(t *testing.T) {
            runErr := runStandardFlagCommand([]string{"--order=" + value})
            if nil == runErr {
                t.Fatalf("expected --order=%s to be rejected", value)
            }
            if false == strings.Contains(runErr.Error(), "order") {
                t.Fatalf("expected the error to name the order flag, got %q", runErr.Error())
            }
        })
    }
}

func TestStandardFlags_AcceptTheSupportedFormatAndOrderValues(t *testing.T) {
    accepted := [][]string{
        {},
        {"--format=table"},
        {"--format=json"},
        {"--order=asc"},
        {"--order=desc"},
        {"--format=json", "--order=desc"},
    }

    for _, arguments := range accepted {
        t.Run(strings.Join(arguments, " "), func(t *testing.T) {
            runErr := runStandardFlagCommand(arguments)
            if nil != runErr {
                t.Fatalf("expected %v to be accepted, got %v", arguments, runErr)
            }
        })
    }
}

func TestStandardFlags_RejectTheWithdrawnProjectionFlags(t *testing.T) {
    for _, arguments := range [][]string{
        {"--fields=name"},
        {"--sort=name"},
    } {
        t.Run(strings.Join(arguments, " "), func(t *testing.T) {
            runErr := runStandardFlagCommand(arguments)
            if nil == runErr {
                t.Fatalf("expected %v to be rejected as an unknown flag", arguments)
            }
        })
    }
}

func TestStandardFlags_DeclareOnlyTheHonouredFlags(t *testing.T) {
    declared := map[string]struct{}{}
    for _, flag := range StandardFlags() {
        for _, name := range flag.Names() {
            declared[name] = struct{}{}
        }
    }

    expected := []string{
        FlagNameFormat,
        FlagNameNoColor,
        FlagNameVerbose,
        FlagNameVerbosity,
        FlagNameQuiet,
        FlagNameOrder,
        FlagNameLimit,
        FlagNameOffset,
        FlagNameTableMaxWidth,
    }

    if len(expected) != len(declared) {
        t.Fatalf("expected %d flags, got %v", len(expected), declared)
    }

    for _, name := range expected {
        _, isDeclared := declared[name]
        if false == isDeclared {
            t.Fatalf("expected the %q flag to be declared, got %v", name, declared)
        }
    }
}

func TestDebugFlags_DefaultQuietToFalse(t *testing.T) {
    for _, flag := range DebugFlags() {
        boolFlag, ok := flag.(*clicontract.BoolFlag)
        if false == ok {
            continue
        }

        if FlagNameQuiet != boolFlag.Name {
            continue
        }

        if false != boolFlag.Value {
            t.Fatalf("expected the debug quiet default to be false")
        }

        return
    }

    t.Fatalf("expected a quiet flag among the debug flags")
}

func TestStandardFlags_RefuseNegativeIntegerValues(t *testing.T) {
    for _, flagName := range []string{FlagNameVerbosity, FlagNameLimit, FlagNameOffset, FlagNameTableMaxWidth} {
        validator := findIntFlagValidator(t, flagName)

        validationErr := validator(-1)
        if nil == validationErr {
            t.Fatalf("expected %q to refuse a negative value", flagName)
        }
        if false == strings.Contains(validationErr.Error(), flagName) {
            t.Fatalf("expected the refusal to name the flag %q, got %q", flagName, validationErr.Error())
        }
    }
}

func TestStandardFlags_AcceptZeroAndPositiveIntegerValues(t *testing.T) {
    for _, flagName := range []string{FlagNameVerbosity, FlagNameLimit, FlagNameOffset, FlagNameTableMaxWidth} {
        validator := findIntFlagValidator(t, flagName)

        if nil != validator(0) {
            t.Fatalf("expected %q to accept zero", flagName)
        }
        if nil != validator(7) {
            t.Fatalf("expected %q to accept a positive value", flagName)
        }
    }
}

func findIntFlagValidator(t *testing.T, flagName string) func(value int) error {
    t.Helper()

    for _, flag := range StandardFlags() {
        intFlag, isIntFlag := flag.(*clicontract.IntFlag)
        if false == isIntFlag {
            continue
        }

        if flagName != intFlag.Name {
            continue
        }

        if nil == intFlag.Validator {
            t.Fatalf("expected %q to carry a validator", flagName)
        }

        return intFlag.Validator
    }

    t.Fatalf("expected the standard flags to carry %q", flagName)

    return nil
}
