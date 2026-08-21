package internal

import (
    "net/url"
)

/* RedactQueryValuesForDiagnostics keeps the parameter names of a raw query string and redacts every
   value. A query string is the one part of a request line that routinely carries a credential — an
   api key, a one-time token, a signed link — and the journal is read by more people than the request
   was, so the names are what diagnoses a mismatch and the values are what must never be kept.

   A query that does not parse is redacted whole, because an unparseable query cannot have its secret
   half told apart from its diagnosable half. An empty query stays empty rather than becoming a
   redaction marker, so a request with no query reads as a request with no query. */
func RedactQueryValuesForDiagnostics(rawQuery string) string {
    if "" == rawQuery {
        return ""
    }

    queryValues, parseErr := url.ParseQuery(rawQuery)
    if nil != parseErr {
        return RedactedQueryValue
    }

    for key := range queryValues {
        queryValues.Set(key, RedactedQueryValue)
    }

    return queryValues.Encode()
}

const RedactedQueryValue = "xxxxx"
