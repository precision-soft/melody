package output

import (
	"strings"

	clicontract "github.com/precision-soft/melody/cli/contract"
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

	option.Fields = SplitFields(commandContext.String(FlagNameFields))

	option.SortKey = strings.TrimSpace(commandContext.String(FlagNameSortKey))

	orderString := strings.TrimSpace(commandContext.String(FlagNameOrder))
	if "" != orderString {
		option.Order = SortOrder(orderString)
	}

	option.Limit = commandContext.Int(FlagNameLimit)
	option.Offset = commandContext.Int(FlagNameOffset)

	return option
}

func NormalizeOption(option Option) Option {
	normalized := option

	if "" == string(normalized.Format) {
		normalized.Format = FormatTable
	}

	if FormatTable != normalized.Format && FormatJson != normalized.Format {
		normalized.Format = FormatTable
	}

	if "" == string(normalized.Order) {
		normalized.Order = SortOrderAscending
	}

	if SortOrderAscending != normalized.Order && SortOrderDescending != normalized.Order {
		normalized.Order = SortOrderAscending
	}

	if 0 > normalized.Limit {
		normalized.Limit = 0
	}

	if 0 > normalized.Offset {
		normalized.Offset = 0
	}

	return normalized
}
