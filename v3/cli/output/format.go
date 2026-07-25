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
)

type SortOrder string

const (
    SortOrderAscending  SortOrder = "asc"
    SortOrderDescending SortOrder = "desc"
)

func isFormatSupported(format Format) bool {
    return FormatTable == format || FormatJson == format
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
                "unsupported output format %q, expected %q or %q",
                value,
                string(FormatTable),
                string(FormatJson),
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
