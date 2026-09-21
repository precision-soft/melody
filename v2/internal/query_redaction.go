package internal

import (
    "net/url"
    "strings"
)

/* RedactQueryValuesForDiagnostics keeps the parameter names of a raw query string and redacts every value. A query string is the one part of a request line that routinely carries a credential — an api key, a one-time token, a signed link — and the journal is read by more people than the request was, so the names are what diagnoses a mismatch and the values are what must never be kept.

   The redaction is done pair by pair, IN PLACE: each pair keeps its name as written and its order, a repeated name keeps every occurrence, and only the text after the first "=" of a pair becomes the marker. The one reader that renders two query strings side by side — the internal-auth refusal that fires only when the signed query and the request query differ byte for byte — used to receive them re-encoded through url.Values, which sorts the names and collapses a repeated one, so a mismatch by reordering, by duplication or by multiplicity rendered as two identical strings under an error saying they differ.

   A pair whose name is not a well-formed query name — a percent escape that does not decode — redacts the query whole, because an unparseable name cannot have its secret half told apart from its diagnosable half; a pair with no "=" is a bare name and is kept as it is. An empty query stays empty rather than becoming a redaction marker, so a request with no query reads as a request with no query. */
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

        if false == hasValue {
            redacted = append(redacted, name)

            continue
        }

        redacted = append(redacted, name+"="+RedactedQueryValue)
    }

    return strings.Join(redacted, "&")
}

const RedactedQueryValue = "xxxxx"
