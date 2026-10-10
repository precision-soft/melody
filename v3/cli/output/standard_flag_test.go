package output

import (
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
)

/* the declared validator is called directly: the flag set is what this source produces, and the validator is the guard it carries. That the engine consults it on the value it parsed is proved through a driven command line below. */
func findStringFlagValidator(t *testing.T, flagName string) func(value string) error {
    t.Helper()

    for _, flag := range StandardFlags() {
        stringFlag, isStringFlag := flag.(*clicontract.StringFlag)
        if false == isStringFlag {
            continue
        }

        if flagName != stringFlag.Name {
            continue
        }

        if nil == stringFlag.Validator {
            t.Fatalf("expected %q to carry a validator", flagName)
        }

        return stringFlag.Validator
    }

    t.Fatalf("expected a string flag named %q", flagName)

    return nil
}

func TestStandardFlags_RejectAnUnsupportedFormat(t *testing.T) {
    validator := findStringFlagValidator(t, FlagNameFormat)

    for _, value := range []string{"JSON", "Table", "yaml", "jsonl", "text"} {
        t.Run(value, func(t *testing.T) {
            validationErr := validator(value)
            if nil == validationErr {
                t.Fatalf("expected --format=%s to be rejected", value)
            }
            if false == strings.Contains(validationErr.Error(), "format") {
                t.Fatalf("expected the error to name the format flag, got %q", validationErr.Error())
            }
        })
    }
}

func TestStandardFlags_RejectAnUnsupportedOrder(t *testing.T) {
    validator := findStringFlagValidator(t, FlagNameOrder)

    for _, value := range []string{"ASC", "ascending", "descending", "sideways"} {
        t.Run(value, func(t *testing.T) {
            validationErr := validator(value)
            if nil == validationErr {
                t.Fatalf("expected --order=%s to be rejected", value)
            }
            if false == strings.Contains(validationErr.Error(), "order") {
                t.Fatalf("expected the error to name the order flag, got %q", validationErr.Error())
            }
        })
    }
}

func TestStandardFlags_AcceptTheSupportedFormatAndOrderValues(t *testing.T) {
    formatValidator := findStringFlagValidator(t, FlagNameFormat)
    orderValidator := findStringFlagValidator(t, FlagNameOrder)

    for _, value := range []string{string(FormatTable), string(FormatJson), string(FormatJsonPretty)} {
        t.Run("format="+value, func(t *testing.T) {
            if validationErr := formatValidator(value); nil != validationErr {
                t.Fatalf("expected %q to be accepted, got %v", value, validationErr)
            }
        })
    }

    for _, value := range []string{string(SortOrderAscending), string(SortOrderDescending)} {
        t.Run("order="+value, func(t *testing.T) {
            if validationErr := orderValidator(value); nil != validationErr {
                t.Fatalf("expected %q to be accepted, got %v", value, validationErr)
            }
        })
    }
}

/* the flag set and its order are those v3.13.0 released, the deprecated --fields and --sort included: a flag set is help output as well as a parser */
func TestStandardFlags_DeclareTheReleasedFlagSetInOrder(t *testing.T) {
    declared := make([]string, 0)
    for _, flag := range StandardFlags() {
        declared = append(declared, flag.Names()[0])
    }

    expected := []string{
        FlagNameFormat,
        FlagNameNoColor,
        FlagNameVerbose,
        FlagNameVerbosity,
        FlagNameQuiet,
        FlagNameFields,
        FlagNameSortKey,
        FlagNameOrder,
        FlagNameLimit,
        FlagNameOffset,
        FlagNameTableMaxWidth,
    }

    if strings.Join(expected, ",") != strings.Join(declared, ",") {
        t.Fatalf("expected the flags %v, got %v", expected, declared)
    }
}

/* the engine consults the declared validator on the value it parsed: a negative limit on the command line is refused naming the flag */
func TestStandardFlags_RefuseANegativeIntegerOnTheCommandLine(t *testing.T) {
    _, runErr := runStandardFlags(t, "--limit=-1")
    if nil == runErr {
        t.Fatalf("expected a negative limit to be refused")
    }
    if false == strings.Contains(runErr.Error(), FlagNameLimit) {
        t.Fatalf("expected the refusal to name the flag %q, got %q", FlagNameLimit, runErr.Error())
    }
}

/* the four standard integer flags parse in base ten: with the engine's inferred base a zero-padded value would read as octal */
func TestStandardFlags_ReadAZeroPaddedIntegerAsDecimal(t *testing.T) {
    for _, flagName := range []string{FlagNameVerbosity, FlagNameLimit, FlagNameOffset, FlagNameTableMaxWidth} {
        parsedCommand, runErr := runStandardFlags(t, "--"+flagName+"=010")
        if nil != runErr {
            t.Fatalf("expected --%s=010 to parse, got %v", flagName, runErr)
        }
        if 10 != parsedCommand.Int(flagName) {
            t.Fatalf("expected --%s=010 to read as 10, got %d", flagName, parsedCommand.Int(flagName))
        }

        parsedCommand, runErr = runStandardFlags(t, "--"+flagName+"=08")
        if nil != runErr {
            t.Fatalf("expected --%s=08 to parse, got %v", flagName, runErr)
        }
        if 8 != parsedCommand.Int(flagName) {
            t.Fatalf("expected --%s=08 to read as 8, got %d", flagName, parsedCommand.Int(flagName))
        }
    }
}

/* a hexadecimal spelling is not a decimal integer, so base ten refuses it rather than reading sixteen */
func TestStandardFlags_RefuseAHexadecimalInteger(t *testing.T) {
    _, runErr := runStandardFlags(t, "--limit=0x10")
    if nil == runErr {
        t.Fatalf("expected --limit=0x10 to be refused under base ten")
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
