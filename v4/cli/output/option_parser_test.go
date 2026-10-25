package output

import (
    "testing"

    "github.com/precision-soft/melody/v4/bound"
)

func TestNormalizeOption_ImpliesNoColorForTheJsonFormat(t *testing.T) {
    normalized, normalizeErr := NormalizeOption(
        Option{
            Format:  FormatJson,
            NoColor: false,
        },
    )
    if nil != normalizeErr {
        t.Fatalf("expected the option admitted, got %v", normalizeErr)
    }

    if false == normalized.NoColor {
        t.Fatalf("expected the json format to imply no color")
    }
}

func TestNormalizeOption_KeepsColorForTheTableFormat(t *testing.T) {
    normalized, normalizeErr := NormalizeOption(
        Option{
            Format:  FormatTable,
            NoColor: false,
        },
    )
    if nil != normalizeErr {
        t.Fatalf("expected the option admitted, got %v", normalizeErr)
    }

    if true == normalized.NoColor {
        t.Fatalf("expected the table format to keep color enabled")
    }
}

func TestNormalizeOption_DefaultsAnEmptyFormatAndOrder(t *testing.T) {
    normalized, normalizeErr := NormalizeOption(Option{})
    if nil != normalizeErr {
        t.Fatalf("expected the zero option admitted, got %v", normalizeErr)
    }

    if FormatTable != normalized.Format {
        t.Fatalf("expected %q, got %q", FormatTable, normalized.Format)
    }
    if SortOrderAscending != normalized.Order {
        t.Fatalf("expected %q, got %q", SortOrderAscending, normalized.Order)
    }
}

func TestNormalizeOption_RefusesACountItsRuleDoesNotAdmit(t *testing.T) {
    cases := []struct {
        name   string
        option Option
        field  string
    }{
        {"negativeLimit", Option{Limit: bound.Of(-1)}, "Option.Limit"},
        {"negativeOffset", Option{Offset: bound.Of(-1)}, "Option.Offset"},
        {"unlimitedOffset", Option{Offset: bound.UnlimitedCount()}, "Option.Offset"},
        {"zeroTableMaxWidth", Option{TableMaxWidth: bound.Of(0)}, "Option.TableMaxWidth"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            _, normalizeErr := NormalizeOption(testCase.option)
            assertRefusedOptionField(t, normalizeErr, testCase.field)
        })
    }
}

func TestNormalizeOption_AdmitsAZeroLimitAndTheUnlimitedCounts(t *testing.T) {
    for _, option := range []Option{
        {Limit: bound.Of(0)},
        {Limit: bound.UnlimitedCount()},
        {Offset: bound.Of(0)},
        {TableMaxWidth: bound.Of(1)},
        {TableMaxWidth: bound.UnlimitedCount()},
    } {
        if _, normalizeErr := NormalizeOption(option); nil != normalizeErr {
            t.Fatalf("expected %#v admitted, got %v", option, normalizeErr)
        }
    }
}

/* every standard flag is given a value other than its default, so a name the parser reads that StandardFlags does not declare leaves its field at the zero value and fails here: the two sources agree on the names only through this test */
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
        "--table-width=90",
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
    if bound.Of(7) != parsed.Limit {
        t.Fatalf("expected limit 7, got %v", parsed.Limit)
    }
    if bound.Of(3) != parsed.Offset {
        t.Fatalf("expected offset 3, got %v", parsed.Offset)
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
    if bound.Of(90) != parsed.TableMaxWidth {
        t.Fatalf("expected table width 90, got %v", parsed.TableMaxWidth)
    }
}

func TestParseOptionFromCommand_LeavesAnUnsetCountAbsent(t *testing.T) {
    parsedCommand, runErr := runStandardFlags(t)
    if nil != runErr {
        t.Fatalf("expected the command line to parse, got %v", runErr)
    }

    parsed := ParseOptionFromCommand(parsedCommand)

    if false == parsed.Limit.IsAbsent() || false == parsed.Offset.IsAbsent() || false == parsed.TableMaxWidth.IsAbsent() {
        t.Fatalf("expected the counts left out to stay absent, got %#v", parsed)
    }
}

func TestParseOptionFromCommand_ReadsAZeroLimitAsAValue(t *testing.T) {
    parsedCommand, runErr := runStandardFlags(t, "--limit=0")
    if nil != runErr {
        t.Fatalf("expected the command line to parse, got %v", runErr)
    }

    if bound.Of(0) != ParseOptionFromCommand(parsedCommand).Limit {
        t.Fatalf("expected --limit=0 to be the value 0")
    }
}

func TestParseOptionFromCommand_ReturnsTheDefaultsForANilCommand(t *testing.T) {
    parsed := ParseOptionFromCommand(nil)

    if (Option{}) != parsed {
        t.Fatalf("expected the zero option, which reads as the default, got %#v", parsed)
    }
}

func TestNormalizeOption_ClampsANegativeVerbosityLevel(t *testing.T) {
    normalized, normalizeErr := NormalizeOption(Option{Format: FormatTable, Order: SortOrderAscending, VerbosityLevel: -5})
    if nil != normalizeErr {
        t.Fatalf("expected the option admitted, got %v", normalizeErr)
    }

    if 0 != normalized.VerbosityLevel {
        t.Fatalf("expected the negative verbosity level clamped to 0, got %d", normalized.VerbosityLevel)
    }
}
