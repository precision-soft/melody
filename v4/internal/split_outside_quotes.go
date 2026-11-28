package internal

/* MaximumSplitOutsideQuotesMembers bounds the work one header line can name, the default of a reader that takes a bound of its own. Past it the remainder stays the final member and the cut is reported, because the remainder can carry a refusal a reader must not miss. */
const MaximumSplitOutsideQuotesMembers = 64

/* SplitOutsideQuotes splits value on separator outside double-quoted sections, where a backslash escapes the next character, so a quoted parameter value stays in its member. It is the one splitter for every negotiating reader. It cuts at most MaximumSplitOutsideQuotesMembers members, and the second result reports the cut so a reader can refuse a list it only half read. */
func SplitOutsideQuotes(value string, separator byte) ([]string, bool) {
    return SplitOutsideQuotesWithin(value, separator, MaximumSplitOutsideQuotesMembers)
}

/* SplitOutsideQuotesWithin is SplitOutsideQuotes cutting at most maximumMembers members, for a reader whose bound is configured; a bound below one reads as one. */
func SplitOutsideQuotesWithin(value string, separator byte, maximumMembers int) ([]string, bool) {
    result := make([]string, 0, 4)
    insideQuotes := false
    escaped := false
    start := 0

    for index := 0; index < len(value); index++ {
        character := value[index]

        if true == escaped {
            escaped = false

            continue
        }

        if true == insideQuotes && '\\' == character {
            escaped = true

            continue
        }

        if '"' == character {
            insideQuotes = false == insideQuotes

            continue
        }

        if separator == character && false == insideQuotes {
            if maximumMembers-1 <= len(result) {
                return append(result, value[start:]), true
            }

            result = append(result, value[start:index])
            start = index + 1
        }
    }

    return append(result, value[start:]), false
}
