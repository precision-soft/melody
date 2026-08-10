package internal

/* SplitOutsideQuotes splits value on separator while honouring quoted-string sections: a separator inside double quotes belongs to the parameter value it sits in, and a backslash escapes the character after it inside a quoted section, so a media range such as text/plain;version="1,2";q=0 stays one member instead of losing the refusal it carries. It is the one member and parameter splitter for every negotiating reader in this tree: a bare strings.Split cuts through quoted parameter values, so the same header could keep a refusal for one reader and lose it for another. */
func SplitOutsideQuotes(value string, separator byte) []string {
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
            result = append(result, value[start:index])
            start = index + 1
        }
    }

    return append(result, value[start:])
}
