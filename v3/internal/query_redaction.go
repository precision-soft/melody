package internal

import (
    "net/url"
)

/* RedactQueryValuesForDiagnostics preserves parameter names and redacts every value. Unparseable queries are redacted entirely; an empty query stays empty. */
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
