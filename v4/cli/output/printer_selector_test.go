package output

import (
    "testing"

    "github.com/precision-soft/melody/v4/bound"
)

/* this is the one place that decides which printer a command's output goes through: a selector wired to the wrong branch would silently print a table where a script asked for json, which no exit code reports. It normalizes first, so a zero-value option — the shape a command that declares no flags hands it — still reaches a printer rather than nil. Both json spellings have to reach it: they differ in whitespace alone, and a format the selector does not recognise falls back to a table. */
func TestSelectPrinter_TheFormatDecidesThePrinter(t *testing.T) {
    jsonPrinter := mustSelectPrinter(t, Option{Format: FormatJson})
    if _, isJson := jsonPrinter.(*JsonPrinter); false == isJson {
        t.Fatalf("expected the json format to select the json printer, got %T", jsonPrinter)
    }

    prettyPrinter := mustSelectPrinter(t, Option{Format: FormatJsonPretty})
    if _, isJson := prettyPrinter.(*JsonPrinter); false == isJson {
        t.Fatalf("expected the pretty json format to select the json printer, got %T", prettyPrinter)
    }

    tablePrinter := mustSelectPrinter(t, Option{Format: FormatTable})
    if _, isJson := tablePrinter.(*JsonPrinter); true == isJson {
        t.Fatalf("expected the table format not to select the json printer")
    }

    zeroValuePrinter := mustSelectPrinter(t, Option{})
    if nil == zeroValuePrinter {
        t.Fatalf("expected a zero-value option to still select a printer")
    }

    if _, isJson := zeroValuePrinter.(*JsonPrinter); true == isJson {
        t.Fatalf("expected the normalized default to be a table rather than json, got %T", zeroValuePrinter)
    }
}

/* a caller that names a table width gets a printer bound to it, and one that names none gets the default — the two are different constructors, and a selector that always took the default would make the width flag inert. */
func TestSelectPrinter_ANamedTableWidthReachesThePrinter(t *testing.T) {
    narrow := mustSelectPrinter(t, Option{Format: FormatTable, TableMaxWidth: bound.Of(40)})
    wide := mustSelectPrinter(t, Option{Format: FormatTable, TableMaxWidth: bound.Of(200)})
    byDefault := mustSelectPrinter(t, Option{Format: FormatTable})

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

func TestSelectPrinter_AnUnlimitedTableWidthLetsTheTableBeAsWideAsItsContent(t *testing.T) {
    printer := mustSelectPrinter(t, Option{Format: FormatTable, TableMaxWidth: bound.UnlimitedCount()})

    tablePrinter, isTable := printer.(*TablePrinter)
    if false == isTable || 0 != tablePrinter.tableMaxWidth {
        t.Fatalf("expected a table printer with no width cap, got %#v", printer)
    }
}

func TestSelectPrinter_RefusesATableWidthBelowOneWhateverTheFormat(t *testing.T) {
    for _, format := range []Format{FormatTable, FormatJson, FormatJsonPretty} {
        _, selectErr := SelectPrinter(Option{Format: format, TableMaxWidth: bound.Of(0)})
        assertRefusedOptionField(t, selectErr, "Option.TableMaxWidth")
    }
}

func mustSelectPrinter(t *testing.T, option Option) Printer {
    t.Helper()

    printer, selectErr := SelectPrinter(option)
    if nil != selectErr {
        t.Fatalf("expected a printer for %#v, got %v", option, selectErr)
    }

    return printer
}
