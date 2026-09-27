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

const RedactedQueryValue = "xxxxx"
