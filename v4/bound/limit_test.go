package bound

import (
    "testing"
    "time"
)

func TestLimit_ZeroValueIsAbsent(t *testing.T) {
    var duration Duration
    var count Count

    if false == duration.IsAbsent() || true == duration.IsUnlimited() {
        t.Fatalf("expected the zero duration to be absent, got %s", duration)
    }

    if false == count.IsAbsent() || true == count.IsUnlimited() {
        t.Fatalf("expected the zero count to be absent, got %s", count)
    }

    if _, ok := duration.Value(); true == ok {
        t.Fatalf("expected an absent duration to carry no value")
    }
}

func TestOf_SetsTheValueZeroIncluded(t *testing.T) {
    duration := Of(3 * time.Second)

    value, ok := duration.Value()
    if false == ok || 3*time.Second != value || true == duration.IsAbsent() || true == duration.IsUnlimited() {
        t.Fatalf("expected a 3s value, got %s", duration)
    }

    zero := Of(0)

    count, ok := zero.Value()
    if false == ok || 0 != count || true == zero.IsAbsent() {
        t.Fatalf("expected a set zero to be a value, not absent, got %s", zero)
    }
}

func TestUnlimited_IsNeitherAbsentNorAValue(t *testing.T) {
    duration := UnlimitedDuration()
    count := UnlimitedCount()

    if false == duration.IsUnlimited() || true == duration.IsAbsent() {
        t.Fatalf("expected an unlimited duration, got %s", duration)
    }

    if false == count.IsUnlimited() || true == count.IsAbsent() {
        t.Fatalf("expected an unlimited count, got %s", count)
    }

    if _, ok := count.Value(); true == ok {
        t.Fatalf("expected an unlimited count to carry no value")
    }
}

func TestLimit_StringNamesTheState(t *testing.T) {
    cases := map[string]string{
        Duration{}.String():           "absent",
        UnlimitedDuration().String():  "unlimited",
        Of(90 * time.Second).String(): "1m30s",
        Of(42).String():               "42",
    }

    for got, expected := range cases {
        if expected != got {
            t.Fatalf("expected %q, got %q", expected, got)
        }
    }
}
