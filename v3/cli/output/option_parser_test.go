package output

import (
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
    commandContext := &stubCommandContext{
        stringValues: map[string]string{
            FlagNameFormat: string(FormatJson),
            FlagNameOrder:  string(SortOrderDescending),
        },
        intValues: map[string]int{
            FlagNameLimit:         7,
            FlagNameOffset:        3,
            FlagNameVerbosity:     2,
            FlagNameTableMaxWidth: 120,
        },
    }

    parsed := ParseOptionFromCommand(commandContext)

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

/* the parser reads a contract now, so the reading is asserted on a double that answers per name instead of on a command line driven through the parsing engine: what the test then proves is this file's own guard — which flag name feeds which field of the option — rather than the engine's ability to parse an argument. That the declared validators and defaults survive the trip into the engine is the adapter's guard and is proved there. */
type stubCommandContext struct {
    stringValues      map[string]string
    boolValues        map[string]bool
    intValues         map[string]int
    stringSliceValues map[string][]string
    setFlagNames      map[string]bool
    arguments         []string
    writer            io.Writer
    askedFlagNames    []string
}

var _ clicontract.Context = (*stubCommandContext)(nil)

func (instance *stubCommandContext) record(flagName string) {
    instance.askedFlagNames = append(instance.askedFlagNames, flagName)
}

func (instance *stubCommandContext) String(flagName string) string {
    instance.record(flagName)

    return instance.stringValues[flagName]
}

func (instance *stubCommandContext) Bool(flagName string) bool {
    instance.record(flagName)

    return instance.boolValues[flagName]
}

func (instance *stubCommandContext) Int(flagName string) int {
    instance.record(flagName)

    return instance.intValues[flagName]
}

func (instance *stubCommandContext) StringSlice(flagName string) []string {
    instance.record(flagName)

    return instance.stringSliceValues[flagName]
}

func (instance *stubCommandContext) IsSet(flagName string) bool {
    instance.record(flagName)

    return instance.setFlagNames[flagName]
}

func (instance *stubCommandContext) Arguments() []string {
    return instance.arguments
}

func (instance *stubCommandContext) Writer() io.Writer {
    if nil == instance.writer {
        return io.Discard
    }

    return instance.writer
}

/* the two sources have to agree on nine strings and nothing checks that they do: a name read here that no flag declares reads the zero value of a flag that does not exist, on every run, in silence. The double records what was asked for, so the lockstep is asserted in the direction the argument-driven test could only assert by accident. */
func TestParseOptionFromCommand_ReadsOnlyDeclaredFlagNames(t *testing.T) {
    commandContext := &stubCommandContext{}

    ParseOptionFromCommand(commandContext)

    declaredFlagNames := map[string]bool{}
    for _, flag := range StandardFlags() {
        declaredFlagNames[flag.Definition().Name] = true
    }

    if 0 == len(commandContext.askedFlagNames) {
        t.Fatalf("expected the parser to read at least one flag")
    }

    for _, askedFlagName := range commandContext.askedFlagNames {
        if false == declaredFlagNames[askedFlagName] {
            t.Fatalf("expected %q to be declared by StandardFlags, declared: %v", askedFlagName, declaredFlagNames)
        }
    }
}

/* a caller handing back a typed nil of its own context type produces a non-nil interface: read with a plain comparison it passes the guard above and the first flag read dereferences it */
func TestParseOptionFromCommand_ReturnsTheDefaultsForATypedNilCommand(t *testing.T) {
    var typedNilContext *stubCommandContext

    parsed := ParseOptionFromCommand(typedNilContext)

    if FormatTable != parsed.Format {
        t.Fatalf("expected %q, got %q", FormatTable, parsed.Format)
    }
}
