package output

import (
    "context"
    "io"
    "testing"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
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

func TestParseOptionFromCommand_ReadsTheStandardFlags(t *testing.T) {
    parsed := Option{}

    commandContext := &clicontract.CommandContext{
        Name:      "test",
        Flags:     StandardFlags(),
        Writer:    io.Discard,
        ErrWriter: io.Discard,
        Action: func(
            actionContext context.Context,
            actionCommandContext *clicontract.CommandContext,
        ) error {
            parsed = ParseOptionFromCommand(actionCommandContext)

            return nil
        },
    }

    runErr := commandContext.Run(
        context.Background(),
        []string{
            "test",
            "--format=json",
            "--order=desc",
            "--limit=7",
            "--offset=3",
            "--verbosity=2",
            "--table-width=120",
        },
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if FormatJson != parsed.Format {
        t.Fatalf("expected format %q, got %q", FormatJson, parsed.Format)
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
    if 120 != parsed.TableMaxWidth {
        t.Fatalf("expected table width 120, got %d", parsed.TableMaxWidth)
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
