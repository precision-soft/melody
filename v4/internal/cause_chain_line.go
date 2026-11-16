package internal

import (
    "strconv"
    "strings"
)

/* AppendCauseChain appends at most maximumLinks links of a cause chain to a one-line message as ": cause N: <link>", every link escaped to one line and then cut at 512 bytes; a link the text before it ends with, the shape a %w wrapper repeats, is left out and not counted. */
func AppendCauseChain(line string, causeChain []string, maximumLinks int) string {
    var builder strings.Builder
    builder.WriteString(line)

    previous := line
    written := 0
    for _, link := range causeChain {
        if written >= maximumLinks {
            break
        }

        rendered := BoundDiagnosticText(EscapeControlCharacters(link))
        repeated := "" == rendered || previous == rendered || true == strings.HasSuffix(previous, ": "+rendered)
        previous = rendered
        if true == repeated {
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
