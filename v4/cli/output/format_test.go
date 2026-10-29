package output

import (
    "strings"
    "testing"
)

func TestParseFormat_AcceptsTheSupportedValuesAndTheUnsetCase(t *testing.T) {
    cases := map[string]Format{
        "":      FormatTable,
        " ":     FormatTable,
        "table": FormatTable,
        "json":  FormatJson,
        " json": FormatJson,
        /* the pretty spelling is accepted by the same gate, through IsJsonFormat: without this case a door that stopped answering for it would be refused at parsing and no test would say so */
        "json-pretty":  FormatJsonPretty,
        " json-pretty": FormatJsonPretty,
    }

    for value, expected := range cases {
        t.Run(value, func(t *testing.T) {
            format, parseErr := parseFormat(value)
            if nil != parseErr {
                t.Fatalf("expected %q to be accepted, got %v", value, parseErr)
            }
            if expected != format {
                t.Fatalf("expected %q, got %q", expected, format)
            }
        })
    }
}

func TestParseFormat_RejectsAnUnsupportedValueInsteadOfSubstituting(t *testing.T) {
    for _, value := range []string{"JSON", "Table", "yaml", "jsonl"} {
        t.Run(value, func(t *testing.T) {
            _, parseErr := parseFormat(value)
            if nil == parseErr {
                t.Fatalf("expected %q to be rejected", value)
            }
            if false == strings.Contains(parseErr.Error(), value) {
                t.Fatalf("expected the error to quote the offending value, got %q", parseErr.Error())
            }
        })
    }
}

func TestParseSortOrder_AcceptsTheSupportedValuesAndTheUnsetCase(t *testing.T) {
    cases := map[string]SortOrder{
        "":     SortOrderAscending,
        " ":    SortOrderAscending,
        "asc":  SortOrderAscending,
        "desc": SortOrderDescending,
    }

    for value, expected := range cases {
        t.Run(value, func(t *testing.T) {
            order, parseErr := parseSortOrder(value)
            if nil != parseErr {
                t.Fatalf("expected %q to be accepted, got %v", value, parseErr)
            }
            if expected != order {
                t.Fatalf("expected %q, got %q", expected, order)
            }
        })
    }
}

func TestParseSortOrder_RejectsAnUnsupportedValueInsteadOfSubstituting(t *testing.T) {
    for _, value := range []string{"ASC", "ascending", "descending", "sideways"} {
        t.Run(value, func(t *testing.T) {
            _, parseErr := parseSortOrder(value)
            if nil == parseErr {
                t.Fatalf("expected %q to be rejected", value)
            }
            if false == strings.Contains(parseErr.Error(), value) {
                t.Fatalf("expected the error to quote the offending value, got %q", parseErr.Error())
            }
        })
    }
}
