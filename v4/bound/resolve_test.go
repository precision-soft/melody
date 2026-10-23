package bound

import (
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/exception"
)

func timeoutRule(unlimitedAdmitted bool) Rule[time.Duration] {
    return Rule[time.Duration]{
        Name:              "Timeout",
        Default:           30 * time.Second,
        Minimum:           TimeoutMinimum,
        UnlimitedAdmitted: unlimitedAdmitted,
    }
}

func TestResolve_AbsentTakesTheDefault(t *testing.T) {
    resolved, err := Resolve(Duration{}, timeoutRule(false))
    if nil != err || 30*time.Second != resolved.Value || true == resolved.Unlimited {
        t.Fatalf("expected the 30s default, got %+v (%v)", resolved, err)
    }
}

func TestResolve_AValueAtTheMinimumIsAdmitted(t *testing.T) {
    resolved, err := Resolve(Of(time.Millisecond), timeoutRule(false))
    if nil != err || time.Millisecond != resolved.Value {
        t.Fatalf("expected 1ms, got %+v (%v)", resolved, err)
    }
}

func TestResolve_AValueBelowTheMinimumIsRefusedByName(t *testing.T) {
    for _, value := range []time.Duration{0, -time.Second, time.Millisecond - 1} {
        _, err := Resolve(Of(value), timeoutRule(true))
        assertRefusedField(t, err, "Timeout", "the limit is below its minimum")
    }
}

func TestResolve_ZeroIsAValueWhereTheMinimumIsZero(t *testing.T) {
    resolved, err := Resolve(Of(0), Rule[int]{Name: "Retries", Default: 3, Minimum: 0})
    if nil != err || 0 != resolved.Value || true == resolved.Unlimited {
        t.Fatalf("expected the value 0, got %+v (%v)", resolved, err)
    }
}

func TestResolve_UnlimitedFollowsTheRule(t *testing.T) {
    resolved, err := Resolve(UnlimitedDuration(), timeoutRule(true))
    if nil != err || false == resolved.Unlimited || 0 != resolved.Value {
        t.Fatalf("expected unlimited, got %+v (%v)", resolved, err)
    }

    _, err = Resolve(UnlimitedDuration(), timeoutRule(false))
    assertRefusedField(t, err, "Timeout", "the limit does not admit unlimited")
}

func assertRefusedField(t *testing.T, err error, field string, message string) {
    t.Helper()

    var refusal *exception.Error
    if false == errors.As(err, &refusal) {
        t.Fatalf("expected a melody error, got %v", err)
    }

    if message != refusal.Message() {
        t.Fatalf("expected %q, got %q", message, refusal.Message())
    }

    if field != refusal.Context()["field"] {
        t.Fatalf("expected the refusal to name %q, got %+v", field, refusal.Context())
    }
}
