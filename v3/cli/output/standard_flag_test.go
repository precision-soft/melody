package output

import (
    "strings"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
)

/* the declared validator is called directly rather than through a driven command line: the flag set is what this source produces, and the validator is the guard it carries. That the validator survives the trip into the parsing engine — installed, and consulted on the value the engine parsed — is the adapter's guard and is proved in its own mirror. */
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

/* the withdrawn flags are refused by not being declared: an argument naming one reaches the parser as an unknown flag, which the parser refuses on its own. What this file owns is the absence, and asserting it here rather than through a driven command line keeps the guard where the declaration is. */
func TestStandardFlags_RejectTheWithdrawnProjectionFlags(t *testing.T) {
    declared := map[string]bool{}
    for _, flag := range StandardFlags() {
        declared[flag.Definition().Name] = true
    }

    for _, withdrawnFlagName := range []string{"fields", "sort"} {
        if true == declared[withdrawnFlagName] {
            t.Fatalf("expected the withdrawn %q flag to be undeclared, declared: %v", withdrawnFlagName, declared)
        }
    }
}

func TestStandardFlags_DeclareOnlyTheHonouredFlags(t *testing.T) {
    declared := map[string]struct{}{}
    for _, flag := range StandardFlags() {
        declared[flag.Definition().Name] = struct{}{}
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
