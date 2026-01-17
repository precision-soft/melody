package output

import (
	clicontract "github.com/precision-soft/melody/cli/contract"
)

const (
	FlagNameFormat    = "format"
	FlagNameNoColor   = "no-color"
	FlagNameVerbose   = "verbose"
	FlagNameVerbosity = "verbosity"
	FlagNameQuiet     = "quiet"
	FlagNameFields    = "fields"
	FlagNameSortKey   = "sort"
	FlagNameOrder     = "order"
	FlagNameLimit     = "limit"
	FlagNameOffset    = "offset"
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

	return merged
}
