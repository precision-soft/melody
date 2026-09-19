package output

import (
    "testing"
)

/* this is the one place that decides which printer a command's output goes through: a selector wired to the wrong branch would silently print a table where a script asked for json, which no exit code reports. It normalizes first, so a zero-value option — the shape a command that declares no flags hands it — still reaches a printer rather than nil. Both json spellings have to reach it: they differ in whitespace alone, and a format the selector does not recognise falls back to a table. */
func TestSelectPrinter_TheFormatDecidesThePrinter(t *testing.T) {
    jsonPrinter := SelectPrinter(Option{Format: FormatJson})
    if _, isJson := jsonPrinter.(*JsonPrinter); false == isJson {
        t.Fatalf("expected the json format to select the json printer, got %T", jsonPrinter)
    }

    prettyPrinter := SelectPrinter(Option{Format: FormatJsonPretty})
    if _, isJson := prettyPrinter.(*JsonPrinter); false == isJson {
        t.Fatalf("expected the pretty json format to select the json printer, got %T", prettyPrinter)
    }

    tablePrinter := SelectPrinter(Option{Format: FormatTable})
    if _, isJson := tablePrinter.(*JsonPrinter); true == isJson {
        t.Fatalf("expected the table format not to select the json printer")
    }

    zeroValuePrinter := SelectPrinter(Option{})
    if nil == zeroValuePrinter {
        t.Fatalf("expected a zero-value option to still select a printer")
    }

    if _, isJson := zeroValuePrinter.(*JsonPrinter); true == isJson {
        t.Fatalf("expected the normalized default to be a table rather than json, got %T", zeroValuePrinter)
    }
}

/* a caller that names a table width gets a printer bound to it, and one that names none gets the default — the two are different constructors, and a selector that always took the default would make the width flag inert. */
func TestSelectPrinter_ANamedTableWidthReachesThePrinter(t *testing.T) {
    narrow := SelectPrinter(Option{Format: FormatTable, TableMaxWidth: 40})
    wide := SelectPrinter(Option{Format: FormatTable, TableMaxWidth: 200})
    byDefault := SelectPrinter(Option{Format: FormatTable})

    narrowPrinter, isNarrowTable := narrow.(*TablePrinter)
    widePrinter, isWideTable := wide.(*TablePrinter)
    defaultPrinter, isDefaultTable := byDefault.(*TablePrinter)

    if false == isNarrowTable || false == isWideTable || false == isDefaultTable {
        t.Fatalf("expected every table format to select a table printer")
    }

    if narrowPrinter.tableMaxWidth == widePrinter.tableMaxWidth {
        t.Fatalf("expected the named width to reach the printer, both read %d", narrowPrinter.tableMaxWidth)
    }

    if 40 != narrowPrinter.tableMaxWidth {
        t.Fatalf("expected the caller's own width, got %d", narrowPrinter.tableMaxWidth)
    }

    if defaultPrinter.tableMaxWidth == narrowPrinter.tableMaxWidth {
        t.Fatalf("expected an unnamed width to fall back to the default rather than to the caller's")
    }
}
