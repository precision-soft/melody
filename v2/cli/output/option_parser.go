package output

import (
    "strings"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
)

func ParseOptionFromCommand(commandContext *clicontract.CommandContext) Option {
    option := DefaultOption()

    if nil == commandContext {
        return option
    }

    formatString := strings.TrimSpace(commandContext.String(FlagNameFormat))
    if "" != formatString {
        option.Format = Format(formatString)
    }

    option.NoColor = commandContext.Bool(FlagNameNoColor)
    verboseFlag := commandContext.Bool(FlagNameVerbose)
    verbosityLevel := commandContext.Int(FlagNameVerbosity)

    if true == verboseFlag && 1 > verbosityLevel {
        verbosityLevel = 1
    }

    option.VerbosityLevel = verbosityLevel
    option.Verbose = 0 < verbosityLevel
    option.Quiet = commandContext.Bool(FlagNameQuiet)

    orderString := strings.TrimSpace(commandContext.String(FlagNameOrder))
    if "" != orderString {
        option.Order = SortOrder(orderString)
    }

    option.Limit = commandContext.Int(FlagNameLimit)
    option.Offset = commandContext.Int(FlagNameOffset)

    option.TableMaxWidth = commandContext.Int(FlagNameTableMaxWidth)

    return option
}

func NormalizeOption(option Option) Option {
    normalized := option

    if false == isFormatSupported(normalized.Format) {
        normalized.Format = FormatTable
    }

    if false == isSortOrderSupported(normalized.Order) {
        normalized.Order = SortOrderAscending
    }

    /* the json format carries a single machine-readable document, so nothing around it may be colored */
    if true == IsJsonFormat(normalized.Format) {
        normalized.NoColor = true
    }

    if 0 > normalized.VerbosityLevel {
        normalized.VerbosityLevel = 0
    }

    if 0 > normalized.Limit {
        normalized.Limit = 0
    }

    if 0 > normalized.Offset {
        normalized.Offset = 0
    }

    if 0 > normalized.TableMaxWidth {
        normalized.TableMaxWidth = 0
    }

    return normalized
}
