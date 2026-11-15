package internal

import (
    "strconv"
    "strings"
)

/* AppendCauseChain appends each link of a cause chain to a one-line message as ": cause N: <link>", every link bounded and escaped to one line; a link the line already carries, the text a %w wrapper repeats, is left out. */
func AppendCauseChain(line string, causeChain []string) string {
    var builder strings.Builder
    builder.WriteString(line)

    written := 0
    for _, link := range causeChain {
        rendered := EscapeControlCharacters(BoundDiagnosticText(link))
        if "" == rendered || true == strings.Contains(builder.String(), rendered) {
            continue
        }

        written = written + 1
        builder.WriteString(": cause ")
        builder.WriteString(strconv.Itoa(written))
        builder.WriteString(": ")
        builder.WriteString(rendered)
    }

    return builder.String()
}
