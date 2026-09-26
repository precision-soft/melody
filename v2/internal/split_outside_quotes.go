package internal

/* maximumSplitOutsideQuotesMembers bounds the work one header line can name. Past it the remainder stays the final member and the cut is reported, because the remainder can carry a refusal a reader must not miss. */
const maximumSplitOutsideQuotesMembers = 64

/* SplitOutsideQuotes splits value on separator outside double-quoted sections, where a backslash escapes the next character, so a quoted parameter value stays in its member. It is the one splitter for every negotiating reader. It cuts at most maximumSplitOutsideQuotesMembers members, and the second result reports the cut so a reader can refuse a list it only half read. */
func SplitOutsideQuotes(value string, separator byte) ([]string, bool) {
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
            if maximumSplitOutsideQuotesMembers-1 <= len(result) {
                return append(result, value[start:]), true
            }

            result = append(result, value[start:index])
            start = index + 1
        }
    }

    return append(result, value[start:]), false
}
