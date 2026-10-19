package internal

import (
    "net/url"
    "strings"
)

/* RedactQueryValuesForDiagnostics keeps the parameter names of a raw query string and redacts every value, pair by pair and in place: names keep their order and repetitions, and only the text after the first "=" becomes the marker. A name that does not decode redacts the whole query, a pair with no "=" becomes the marker whole, and an empty query or segment stays empty. */
func RedactQueryValuesForDiagnostics(rawQuery string) string {
    if "" == rawQuery {
        return ""
    }

    pairs := strings.Split(rawQuery, "&")
    redacted := make([]string, 0, len(pairs))

    for _, pair := range pairs {
        name, _, hasValue := strings.Cut(pair, "=")

        if _, unescapeErr := url.QueryUnescape(name); nil != unescapeErr {
            return RedactedQueryValue
        }

        if "" == pair {
            redacted = append(redacted, "")

            continue
        }

        if false == hasValue {
            redacted = append(redacted, RedactedQueryValue)

            continue
        }

        redacted = append(redacted, name+"="+RedactedQueryValue)
    }

    return strings.Join(redacted, "&")
}

/* RedactRefererForDiagnostics redacts a Referer the way RedactQueryValuesForDiagnostics redacts the request's own query: the url keeps its scheme, host and path, its query keeps the parameter names with every value redacted, and its fragment and any user information are dropped, since a page that carries a credential in its address hands it on in the Referer of every same-origin request that follows. A Referer that does not parse is cut at its first "?" or "#". */
func RedactRefererForDiagnostics(referer string) string {
    if "" == referer {
        return ""
    }

    parsedReferer, parseErr := url.Parse(referer)
    if nil != parseErr {
        if cutIndex := strings.IndexAny(referer, "?#"); 0 <= cutIndex {
            return referer[:cutIndex]
        }

        return referer
    }

    parsedReferer.User = nil
    parsedReferer.Fragment = ""
    parsedReferer.RawFragment = ""
    parsedReferer.RawQuery = RedactQueryValuesForDiagnostics(parsedReferer.RawQuery)

    return parsedReferer.String()
}

const RedactedQueryValue = "xxxxx"
