package output

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/exception"
)

type Format string

const (
    FormatTable Format = "table"
    FormatJson  Format = "json"
    /* FormatJsonPretty is the same document indented for a person reading it by hand. It exists because json is the machine format and a machine reads a stream: one document per line, so a consumer can follow a long-running command live with a line reader and hand each line to a parser whole. Indenting by default made every document many lines, which every line-framed consumer — a log collector's json stage, a `while read line`, a `grep '"failed": true'` — receives as fragments that each fail to parse. */
    FormatJsonPretty Format = "json-pretty"
)

/* IsJsonFormat answers for both json spellings, and every place that used to compare against FormatJson asks through it: the two differ in whitespace alone, so a site that recognises one and not the other would color a banner into a document, or print a table where a document was asked for. */
func IsJsonFormat(format Format) bool {
    return FormatJson == format || FormatJsonPretty == format
}

type SortOrder string

const (
    SortOrderAscending  SortOrder = "asc"
    SortOrderDescending SortOrder = "desc"
)

func isFormatSupported(format Format) bool {
    return FormatTable == format || true == IsJsonFormat(format)
}

func isSortOrderSupported(order SortOrder) bool {
    return SortOrderAscending == order || SortOrderDescending == order
}

func parseFormat(value string) (Format, error) {
    trimmed := strings.TrimSpace(value)
    if "" == trimmed {
        return FormatTable, nil
    }

    format := Format(trimmed)
    if false == isFormatSupported(format) {
        return FormatTable, exception.NewError(
            fmt.Sprintf(
                "unsupported output format %q, expected %q, %q or %q",
                value,
                string(FormatTable),
                string(FormatJson),
                string(FormatJsonPretty),
            ),
            map[string]any{
                "value": value,
            },
            nil,
        )
    }

    return format, nil
}

func parseSortOrder(value string) (SortOrder, error) {
    trimmed := strings.TrimSpace(value)
    if "" == trimmed {
        return SortOrderAscending, nil
    }

    order := SortOrder(trimmed)
    if false == isSortOrderSupported(order) {
        return SortOrderAscending, exception.NewError(
            fmt.Sprintf(
                "unsupported sort order %q, expected %q or %q",
                value,
                string(SortOrderAscending),
                string(SortOrderDescending),
            ),
            map[string]any{
                "value": value,
            },
            nil,
        )
    }

    return order, nil
}
