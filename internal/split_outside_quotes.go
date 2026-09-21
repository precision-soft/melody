package internal

/* the most members SplitOutsideQuotes will cut a value into. A negotiating header is a short list — a browser sends a handful of media types, a handful of encodings — so a cap far above any real header bounds the work an unauthenticated client can name with one line. The header byte budget (Server.MaxHeaderBytes, a megabyte by default) otherwise converts straight into member slices: a megabyte of separators became a million-element slice plus a four-element array per member, tens of megabytes of live heap for one request, on the response-compression and error-negotiation paths that need no credentials to reach. At the cap the remainder is left as the final member and the cut is reported, because that remainder is not garbage every reader drops: it can carry a refusal — gzip;q=0 as the sixty-fifth member — and a reader that scored the members it did see served what the client had refused. A negotiating reader therefore reads a cut header as one it cannot parse; a legitimate header never reaches the cap. */
const maximumSplitOutsideQuotesMembers = 64

/* SplitOutsideQuotes splits value on separator while honouring quoted-string sections: a separator inside double quotes belongs to the parameter value it sits in, and a backslash escapes the character after it inside a quoted section, so a media range such as text/plain;version="1,2";q=0 stays one member instead of losing the refusal it carries. It is the one member and parameter splitter for every negotiating reader in this tree: a bare strings.Split cuts through quoted parameter values, so the same header could keep a refusal for one reader and lose it for another. It cuts at most maximumSplitOutsideQuotesMembers members; the remainder past the cap stays the final member, and the second result says that the cut happened, so a reader can refuse to score a list it has only half read. */
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
