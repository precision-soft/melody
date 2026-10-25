package output

import (
    "testing"

    "github.com/precision-soft/melody/v4/bound"
)

func TestOption_TheZeroValueReadsAsTheDefault(t *testing.T) {
    normalized, normalizeErr := NormalizeOption(Option{})
    if nil != normalizeErr {
        t.Fatalf("expected the zero option admitted, got %v", normalizeErr)
    }

    if FormatTable != normalized.Format {
        t.Fatalf("expected a human-readable table by default, got %v", normalized.Format)
    }

    if SortOrderAscending != normalized.Order {
        t.Fatalf("expected ascending order by default, got %v", normalized.Order)
    }

    if true == normalized.NoColor || true == normalized.Verbose || true == normalized.Quiet {
        t.Fatalf("expected no output switch to be turned on by default, got %#v", normalized)
    }

    if 0 != normalized.VerbosityLevel || false == normalized.Limit.IsAbsent() || false == normalized.Offset.IsAbsent() || false == normalized.TableMaxWidth.IsAbsent() {
        t.Fatalf("expected every bound to be left absent by default, got %#v", normalized)
    }
}

func TestOption_TheRulesDefaultToNoLimitNoOffsetAndA120CellTable(t *testing.T) {
    limit, limitErr := bound.Resolve(bound.Count{}, OptionLimitRule)
    if nil != limitErr || false == limit.Unlimited {
        t.Fatalf("expected an absent limit to read as no limit, got %+v (%v)", limit, limitErr)
    }

    offset, offsetErr := bound.Resolve(bound.Count{}, OptionOffsetRule)
    if nil != offsetErr || true == offset.Unlimited || 0 != offset.Value {
        t.Fatalf("expected an absent offset to read as 0, got %+v (%v)", offset, offsetErr)
    }

    tableMaxWidth, tableMaxWidthErr := bound.Resolve(bound.Count{}, OptionTableMaxWidthRule)
    if nil != tableMaxWidthErr || true == tableMaxWidth.Unlimited || 120 != tableMaxWidth.Value {
        t.Fatalf("expected an absent table width to read as 120, got %+v (%v)", tableMaxWidth, tableMaxWidthErr)
    }
}
