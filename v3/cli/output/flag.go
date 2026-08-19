package output

import (
    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
)

const (
    FlagNameFormat        = "format"
    FlagNameNoColor       = "no-color"
    FlagNameVerbose       = "verbose"
    FlagNameVerbosity     = "verbosity"
    FlagNameQuiet         = "quiet"
    FlagNameOrder         = "order"
    FlagNameLimit         = "limit"
    FlagNameOffset        = "offset"
    FlagNameTableMaxWidth = "table-width"
)

func MergeFlags(
    standard []clicontract.Flag,
    commandSpecific []clicontract.Flag,
) []clicontract.Flag {
    if 0 == len(commandSpecific) {
        return standard
    }

    merged := make([]clicontract.Flag, 0, len(standard)+len(commandSpecific))
    merged = append(merged, standard...)
    merged = append(merged, commandSpecific...)

    /* a duplicated name is refused at the line that declares it: the flag parser resolves a name to the FIRST declaration, so a command-specific flag reusing a standard name — its default, its validator — would be silently inert, with the help output listing the name twice as the only trace */
    seenFlagNames := map[string]bool{}
    for _, flag := range merged {
        if nil == flag {
            exception.Panic(
                exception.NewError("cli flag may not be nil in merge", nil, nil),
            )
        }

        for _, flagName := range flag.Names() {
            if true == seenFlagNames[flagName] {
                exception.Panic(
                    exception.NewError(
                        "cli flag name declared twice",
                        map[string]any{
                            "flagName": flagName,
                        },
                        nil,
                    ),
                )
            }

            seenFlagNames[flagName] = true
        }
    }

    return merged
}
