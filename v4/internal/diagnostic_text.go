package internal

import (
    "strconv"
    "unicode/utf8"
)

const maxDiagnosticTextLength = 512

/* BoundDiagnosticText cuts a request-supplied value a diagnostic record carries, a path, a query, a header, at 512 bytes on a rune boundary and names the bytes it dropped, so a request line of any length costs each record a bounded field */
func BoundDiagnosticText(value string) string {
    if maxDiagnosticTextLength >= len(value) {
        return value
    }

    cut := maxDiagnosticTextLength
    for 0 < cut && false == utf8.RuneStart(value[cut]) {
        cut--
    }

    return value[:cut] + "...(truncated " + strconv.Itoa(len(value)-cut) + " bytes)"
}
