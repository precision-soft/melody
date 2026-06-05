package translation

import (
    "testing"
)

func TestInterpolate_PoundStaysBoundThroughNestedSelect(t *testing.T) {
    pattern := "{count, plural, other {{gender, select, female {# guests (she)} other {# guests}}}}"

    result := formatMessage(pattern, map[string]any{"count": 3, "gender": "female"}, "en")
    if "3 guests (she)" != result {
        t.Fatalf("expected # to stay bound to the enclosing plural inside a nested select, got %q", result)
    }

    other := formatMessage(pattern, map[string]any{"count": 5, "gender": "robot"}, "en")
    if "5 guests" != other {
        t.Fatalf("expected the nested select other branch to substitute #, got %q", other)
    }
}
