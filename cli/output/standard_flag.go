package output

import (
    "fmt"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/exception"
)

/* nonNegativeIntValidator refuses a negative value at parsing, naming the flag: the option normalization clamps a negative to zero, and zero means unlimited for the limit and "from the start" for the offset — an argument asking for less than nothing silently delivered everything. The clamp stays as the defensive floor for an Option assembled in code, exactly like the format coercion. */
func nonNegativeIntValidator(flagName string) func(value int) error {
    return func(value int) error {
        if 0 > value {
            return exception.NewError(
                fmt.Sprintf("%s may not be negative", flagName),
                map[string]any{
                    "flagName": flagName,
                    "value":    value,
                },
                nil,
            )
        }

        return nil
    }
}

/* StandardFlags returns the flag set every melody command accepts. Quiet defaults to true on purpose: a command's essential output — the document, table or summary the command exists to produce — is never gated on quiet, so the flag governs only headers and decoration, and the default keeps a scripted invocation clean without asking for it. */
func StandardFlags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  FlagNameFormat,
            Usage: "output format: table|json|json-pretty (json is one document per line)",
            Value: string(FormatTable),
            Validator: func(value string) error {
                _, parseErr := parseFormat(value)

                return parseErr
            },
        },
        &clicontract.BoolFlag{
            Name:  FlagNameNoColor,
            Usage: "disable ansi colors",
            Value: false,
        },
        &clicontract.BoolFlag{
            Name:  FlagNameVerbose,
            Usage: "include advanced details",
            Value: false,
        },
        &clicontract.IntFlag{
            Name:      FlagNameVerbosity,
            Usage:     "verbosity level (0..n). supports -v/-vv/-vvv via argument normalization",
            Value:     0,
            Validator: nonNegativeIntValidator(FlagNameVerbosity),
        },
        &clicontract.BoolFlag{
            Name:  FlagNameQuiet,
            Usage: "suppress headers and non-essential output",
            Value: true,
        },
        &clicontract.StringFlag{
            Name:  FlagNameOrder,
            Usage: "sort order: asc|desc",
            Value: string(SortOrderAscending),
            Validator: func(value string) error {
                _, parseErr := parseSortOrder(value)

                return parseErr
            },
        },
        &clicontract.IntFlag{
            Name:      FlagNameLimit,
            Usage:     "max number of items (0 = unlimited)",
            Value:     0,
            Validator: nonNegativeIntValidator(FlagNameLimit),
        },
        &clicontract.IntFlag{
            Name:      FlagNameOffset,
            Usage:     "offset for item list pagination",
            Value:     0,
            Validator: nonNegativeIntValidator(FlagNameOffset),
        },
        &clicontract.IntFlag{
            Name:      FlagNameTableMaxWidth,
            Usage:     fmt.Sprintf("max table width in characters (0 = default %d)", defaultTableMaxWidth),
            Value:     0,
            Validator: nonNegativeIntValidator(FlagNameTableMaxWidth),
        },
    }
}

/* DebugFlags returns StandardFlags with quiet defaulting to false: an introspection command exists to be read by a person, so the headers and context quiet suppresses are part of its answer. */
func DebugFlags() []clicontract.Flag {
    flags := StandardFlags()

    for _, flag := range flags {
        boolFlag, ok := flag.(*clicontract.BoolFlag)
        if false == ok {
            continue
        }

        if FlagNameQuiet == boolFlag.Name {
            boolFlag.Value = false
        }
    }

    return flags
}
