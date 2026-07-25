package output

import (
    "fmt"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
)

func StandardFlags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  FlagNameFormat,
            Usage: "output format: table|json",
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
            Name:  FlagNameVerbosity,
            Usage: "verbosity level (0..n). supports -v/-vv/-vvv via argument normalization",
            Value: 0,
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
            Name:  FlagNameLimit,
            Usage: "max number of items (0 = unlimited)",
            Value: 0,
        },
        &clicontract.IntFlag{
            Name:  FlagNameOffset,
            Usage: "offset for item list pagination",
            Value: 0,
        },
        &clicontract.IntFlag{
            Name:  FlagNameTableMaxWidth,
            Usage: fmt.Sprintf("max table width in characters (0 = default %d)", defaultTableMaxWidth),
            Value: 0,
        },
    }
}

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
