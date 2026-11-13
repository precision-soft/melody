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
