package internal

import (
    "net/url"
    "strings"
)

/* RedactQueryValuesForDiagnostics keeps the parameter names of a raw query string and redacts every value. A query string is the one part of a request line that routinely carries a credential — an api key, a one-time token, a signed link — and the journal is read by more people than the request was, so the names are what diagnoses a mismatch and the values are what must never be kept.

   The redaction is done pair by pair, IN PLACE: each pair keeps its name as written and its order, a repeated name keeps every occurrence, and only the text after the first "=" of a pair becomes the marker. The one reader that renders two query strings side by side — the internal-auth refusal that fires only when the signed query and the request query differ byte for byte — used to receive them re-encoded through url.Values, which sorts the names and collapses a repeated one, so a mismatch by reordering, by duplication or by multiplicity rendered as two identical strings under an error saying they differ.

   A pair whose name is not a well-formed query name — a percent escape that does not decode — redacts the query whole, because an unparseable name cannot have its secret half told apart from its diagnosable half; a pair with no "=" becomes the marker whole, because a capability link carries its token as exactly that — "?9f8a7b3c" — and a name that names no value diagnoses nothing, which is also the one price of the redaction: two queries that differ only in a bare name render alike. An empty query stays empty rather than becoming a redaction marker, so a request with no query reads as a request with no query, and an empty segment inside a query stays empty for the same reason. */
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

        /* an empty segment — the trailing "&" of a hand-built url, a doubled "&" — carries nothing to redact and nothing to diagnose; rendered as the marker it read as a value withheld where nothing was sent */
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
