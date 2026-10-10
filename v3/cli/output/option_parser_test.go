package output

import (
    "testing"
)

func TestNormalizeOption_ImpliesNoColorForTheJsonFormat(t *testing.T) {
    normalized := NormalizeOption(
        Option{
            Format:  FormatJson,
            NoColor: false,
        },
    )

    if false == normalized.NoColor {
        t.Fatalf("expected the json format to imply no color")
    }
}

func TestNormalizeOption_KeepsColorForTheTableFormat(t *testing.T) {
    normalized := NormalizeOption(
        Option{
            Format:  FormatTable,
            NoColor: false,
        },
    )

    if true == normalized.NoColor {
        t.Fatalf("expected the table format to keep color enabled")
    }
}

func TestNormalizeOption_DefaultsAnEmptyFormatAndOrder(t *testing.T) {
    normalized := NormalizeOption(Option{})

    if FormatTable != normalized.Format {
        t.Fatalf("expected %q, got %q", FormatTable, normalized.Format)
    }
    if SortOrderAscending != normalized.Order {
        t.Fatalf("expected %q, got %q", SortOrderAscending, normalized.Order)
    }
}

func TestNormalizeOption_ClampsNegativeNumericValues(t *testing.T) {
    normalized := NormalizeOption(
        Option{
            Limit:         -5,
            Offset:        -5,
            TableMaxWidth: -5,
        },
    )

    if 0 != normalized.Limit {
        t.Fatalf("expected limit 0, got %d", normalized.Limit)
    }
    if 0 != normalized.Offset {
        t.Fatalf("expected offset 0, got %d", normalized.Offset)
    }
    if 0 != normalized.TableMaxWidth {
        t.Fatalf("expected table width 0, got %d", normalized.TableMaxWidth)
    }
}

/* every standard flag but the two deprecated ones, which the next test reads, is given a value other than its default, so a name the parser reads that StandardFlags does not declare leaves its field at the zero value and fails here: the two sources agree on the names only through this test */
func TestParseOptionFromCommand_ReadsTheStandardFlags(t *testing.T) {
    parsedCommand, runErr := runStandardFlags(
        t,
        "--format=json",
        "--no-color",
        "--verbosity=2",
        "--quiet=false",
        "--order=desc",
        "--limit=7",
        "--offset=3",
        "--table-width=120",
    )
    if nil != runErr {
        t.Fatalf("expected the command line to parse, got %v", runErr)
    }

    parsed := ParseOptionFromCommand(parsedCommand)

    if FormatJson != parsed.Format {
        t.Fatalf("expected format %q, got %q", FormatJson, parsed.Format)
    }
    if false == parsed.NoColor {
        t.Fatalf("expected no-color to be read")
    }
    if SortOrderDescending != parsed.Order {
        t.Fatalf("expected order %q, got %q", SortOrderDescending, parsed.Order)
    }
    if 7 != parsed.Limit {
        t.Fatalf("expected limit 7, got %d", parsed.Limit)
    }
    if 3 != parsed.Offset {
        t.Fatalf("expected offset 3, got %d", parsed.Offset)
    }
    if 2 != parsed.VerbosityLevel {
        t.Fatalf("expected verbosity 2, got %d", parsed.VerbosityLevel)
    }
    if false == parsed.Verbose {
        t.Fatalf("expected a verbosity level to imply verbose")
    }
    if true == parsed.Quiet {
        t.Fatalf("expected quiet=false to be read")
    }
    if 120 != parsed.TableMaxWidth {
        t.Fatalf("expected table width 120, got %d", parsed.TableMaxWidth)
    }
}

/* the deprecated projection flags are still declared and parsed, as at v3.13.0: a deployment script passing them keeps running */
func TestParseOptionFromCommand_ReadsTheDeprecatedFieldsAndSortFlags(t *testing.T) {
    parsedCommand, runErr := runStandardFlags(t, "--fields=name, id,,", "--sort= name ")
    if nil != runErr {
        t.Fatalf("expected the command line to parse, got %v", runErr)
    }

    parsed := ParseOptionFromCommand(parsedCommand)

    if 2 != len(parsed.Fields) || "name" != parsed.Fields[0] || "id" != parsed.Fields[1] {
        t.Fatalf("expected the fields [name id], got %v", parsed.Fields)
    }
    if "name" != parsed.SortKey {
        t.Fatalf("expected the sort key %q, got %q", "name", parsed.SortKey)
    }
}

func TestParseOptionFromCommand_ReturnsTheDefaultsForANilCommand(t *testing.T) {
    parsed := ParseOptionFromCommand(nil)

    if FormatTable != parsed.Format {
        t.Fatalf("expected %q, got %q", FormatTable, parsed.Format)
    }
}

func TestNormalizeOption_ClampsANegativeVerbosityLevel(t *testing.T) {
    normalized := NormalizeOption(Option{Format: FormatTable, Order: SortOrderAscending, VerbosityLevel: -5})

    if 0 != normalized.VerbosityLevel {
        t.Fatalf("expected the negative verbosity level clamped to 0, got %d", normalized.VerbosityLevel)
    }
}
