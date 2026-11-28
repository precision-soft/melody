package output

import (
    "strings"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
)

func ParseOptionsFromCommand(commandContext *clicontract.CommandContext) Options {
    option := Options{}

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

    option.Limit = countFlag(commandContext, FlagNameLimit)
    option.Offset = countFlag(commandContext, FlagNameOffset)
    option.TableMaxWidth = countFlag(commandContext, FlagNameTableMaxWidth)
    option.ApplicationVersion = ApplicationVersionOf(commandContext)

    return option
}

func countFlag(commandContext *clicontract.CommandContext, flagName string) bound.Count {
    if false == commandContext.IsSet(flagName) {
        return bound.Count{}
    }

    return bound.Of(commandContext.Int(flagName))
}

/* NormalizeOptions reads an unknown format as table and an unknown order as ascending, turns colour off for json and a negative verbosity to 0, and refuses a Limit, an Offset or a TableMaxWidth its rule does not admit, naming the field. */
func NormalizeOptions(options Options) (Options, error) {
    normalized := normalizeOptionsChoices(options)

    for _, field := range []struct {
        limit bound.Count
        rule  bound.Rule[int]
    }{
        {limit: normalized.Limit, rule: OptionsLimitRule()},
        {limit: normalized.Offset, rule: OptionsOffsetRule()},
        {limit: normalized.TableMaxWidth, rule: OptionsTableMaxWidthRule()},
    } {
        if _, resolveErr := bound.Resolve(field.limit, field.rule); nil != resolveErr {
            return Options{}, resolveErr
        }
    }

    return normalized, nil
}

func normalizeOptionsChoices(options Options) Options {
    normalized := options

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

    return normalized
}
