package rueidis

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestCallTimeoutRule_DefaultsAndRefusesUnlimitedAndASubMillisecondBudget(t *testing.T) {
    rule := callTimeoutRule("LockerConfig.CallTimeout", 3*time.Second)

    resolved, resolveErr := bound.Resolve(bound.Duration{}, rule)
    if nil != resolveErr || 3*time.Second != resolved.Value {
        t.Fatalf("expected an absent budget to take the door's default, got %v, %v", resolved, resolveErr)
    }

    resolved, resolveErr = bound.Resolve(bound.Of(time.Millisecond), rule)
    if nil != resolveErr || time.Millisecond != resolved.Value {
        t.Fatalf("expected 1ms to be admitted, got %v, %v", resolved, resolveErr)
    }

    _, resolveErr = bound.Resolve(bound.UnlimitedDuration(), rule)
    expectErrorNamingField(t, resolveErr, "LockerConfig.CallTimeout")

    _, resolveErr = bound.Resolve(bound.Of(time.Microsecond), rule)
    expectErrorNamingField(t, resolveErr, "LockerConfig.CallTimeout")
}
