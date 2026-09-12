package output

import (
    "testing"
)

func TestDefaultOption_DeclaresTheOutputACommandInheritsByDeclaringNothing(t *testing.T) {
    option := DefaultOption()

    if FormatTable != option.Format {
        t.Fatalf("expected a human-readable table by default, got %v", option.Format)
    }

    if SortOrderAscending != option.Order {
        t.Fatalf("expected ascending order by default, got %v", option.Order)
    }

    if true == option.NoColor || true == option.Verbose || true == option.Quiet {
        t.Fatalf("expected no output switch to be turned on by default, got %#v", option)
    }

    if 0 != option.VerbosityLevel || 0 != option.Limit || 0 != option.Offset || 0 != option.TableMaxWidth {
        t.Fatalf("expected every bound to be left unset by default, got %#v", option)
    }
}

func TestDefaultOption_EachCallerGetsItsOwnCopy(t *testing.T) {
    first := DefaultOption()
    first.Limit = 50
    first.Format = FormatJson

    second := DefaultOption()

    if 0 != second.Limit {
        t.Fatalf("expected a fresh default rather than the one the first caller adjusted, got %d", second.Limit)
    }

    if FormatTable != second.Format {
        t.Fatalf("expected the format default to survive another caller's change, got %v", second.Format)
    }
}
