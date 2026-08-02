package bag

import (
    "testing"
    "time"
)

func TestBagDuration_ConversionsAndErrors(t *testing.T) {
    parameterBag := NewParameterBag()

    parameterBag.Set("d", time.Second)
    value, exists, err := Duration(parameterBag, "d")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if time.Second != value {
        t.Fatalf("expected %v, got %v", time.Second, value)
    }

    parameterBag.Set("d", " 150ms ")
    value, exists, err = Duration(parameterBag, "d")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if false == exists {
        t.Fatalf("expected exists true")
    }
    if 150*time.Millisecond != value {
        t.Fatalf("expected %v, got %v", 150*time.Millisecond, value)
    }

    parameterBag.Set("d", "not-duration")
    _, exists, err = Duration(parameterBag, "d")
    if false == exists {
        t.Fatalf("expected exists true even when conversion fails")
    }
    if nil == err {
        t.Fatalf("expected error")
    }
}

/* @info a name the bag does not hold is unset, not a zero duration: a caller reading a timeout that was never configured must be able to tell "nobody said" from "somebody said zero", which is the difference between applying a default and applying no timeout at all */
func TestBagDuration_AnAbsentNameIsUnsetRatherThanZero(t *testing.T) {
    parameterBag := NewParameterBag()

    value, exists, durationErr := Duration(parameterBag, "missing")
    if true == exists {
        t.Fatalf("expected an absent name to be reported unset")
    }
    if nil != durationErr {
        t.Fatalf("expected no error for an absent name, got %v", durationErr)
    }
    if 0 != value {
        t.Fatalf("expected the zero duration for an absent name, got %v", value)
    }
}
