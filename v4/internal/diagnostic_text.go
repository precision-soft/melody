package internal

import (
    "strconv"
    "unicode/utf8"
)

/* DiagnosticTextMaxLength is the cut of a request-supplied field in a kernel, security or exception diagnostic record, and the default of the access log's own cut */
const DiagnosticTextMaxLength = 512

/* BoundDiagnosticText cuts a request-supplied value a diagnostic record carries, a path, a query, a header, at DiagnosticTextMaxLength bytes on a rune boundary and names the bytes it dropped, so a request line of any length costs each record a bounded field */
func BoundDiagnosticText(value string) string {
    return BoundDiagnosticTextWithin(value, DiagnosticTextMaxLength)
}

/* BoundDiagnosticTextWithin is BoundDiagnosticText under a cut of its caller's; a cut below one keeps nothing of the value but the count */
func BoundDiagnosticTextWithin(value string, maxLength int) string {
    if maxLength >= len(value) {
        return value
    }

    cut := max(maxLength, 0)
    for 0 < cut && false == utf8.RuneStart(value[cut]) {
        cut--
    }

    return value[:cut] + "...(truncated " + strconv.Itoa(len(value)-cut) + " bytes)"
}
