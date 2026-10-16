package output

import (
    "fmt"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

type Format string

const (
    FormatTable Format = "table"
    FormatJson  Format = "json"
    /* FormatJsonPretty is the same document indented for a person reading it by hand; FormatJson writes one document per line, so a line reader can follow a long-running command live. */
    FormatJsonPretty Format = "json-pretty"
)

/* IsJsonFormat answers for both json spellings, which differ in whitespace alone; every site that decides on json asks through it. */
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
