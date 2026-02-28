package output

import (
    "strings"

    clicontract "github.com/precision-soft/melody/cli/contract"
)

func StandardFlags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  FlagNameFormat,
            Usage: "output format: table|json",
            Value: string(FormatTable),
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
            Name:  FlagNameFields,
            Usage: "comma-separated list of fields to include (json only)",
            Value: "",
        },
        &clicontract.StringFlag{
            Name:  FlagNameSortKey,
            Usage: "sort key (command-specific)",
            Value: "",
        },
        &clicontract.StringFlag{
            Name:  FlagNameOrder,
            Usage: "sort order: asc|desc",
            Value: string(SortOrderAscending),
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
    }
}

func SplitFields(fieldsString string) []string {
    trimmed := strings.TrimSpace(fieldsString)
    if "" == trimmed {
        return []string{}
    }

    raw := strings.Split(trimmed, ",")
    result := make([]string, 0, len(raw))

    for _, part := range raw {
        field := strings.TrimSpace(part)
        if "" == field {
            continue
        }

        result = append(result, field)
    }

    return result
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
