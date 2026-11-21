package session

import (
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func TestUnavailableSession_IsEmptyHasNoIdAndCarriesItsCause(t *testing.T) {
    cause := errors.New("session store is down")
    unavailable := NewUnavailableSession(cause)

    if "" != unavailable.Id() {
        t.Fatalf("expected no id, got %q", unavailable.Id())
    }

    if 0 != len(unavailable.All()) || true == unavailable.IsModified() || true == unavailable.IsCleared() {
        t.Fatalf("expected an empty untouched session, got %v modified=%v cleared=%v", unavailable.All(), unavailable.IsModified(), unavailable.IsCleared())
    }

    carried, isUnavailable := SessionUnavailableCause(unavailable)
    if false == isUnavailable || cause != carried || cause != unavailable.SessionUnavailableCause() {
        t.Fatalf("expected the cause carried, got %v %v", carried, isUnavailable)
    }
}

func TestNewUnavailableSession_RefusesANilCause(t *testing.T) {
    var typedNil *exception.Error

    for name, cause := range map[string]error{"nil": nil, "typed nil": typedNil} {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError || "the cause of an unavailable session is nil" != recoveredErr.Error() {
                    t.Fatalf("expected the nil cause refused, got %v", recoveredErr)
                }

                if "cause" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["argument"] {
                    t.Fatalf("expected the refusal to name the argument, got %v", exception.LogContext(recoveredErr, exception.LogContextConfig{}))
                }
            }()

            NewUnavailableSession(cause)
        })
    }
}

func TestUnavailableSession_KeepsWhatTheRequestWrote(t *testing.T) {
    unavailable := NewUnavailableSession(errors.New("session store is down"))

    unavailable.Set("cart", "one line")
    if "one line" != unavailable.String("cart") || false == unavailable.Has("cart") || false == unavailable.IsModified() {
        t.Fatalf("expected the write kept for the request, got %v", unavailable.All())
    }

    unavailable.Delete("cart")
    if true == unavailable.Has("cart") || nil != unavailable.Get("cart") {
        t.Fatalf("expected the delete kept, got %v", unavailable.All())
    }

    unavailable.Set("consent", true)
    unavailable.Clear()

    values, modified, cleared := unavailable.Snapshot()
    if 0 != len(values) || false == modified || false == cleared || false == unavailable.IsCleared() {
        t.Fatalf("expected the clear kept, got %v modified=%v cleared=%v", values, modified, cleared)
    }
}

func TestSessionUnavailableCause_AnswersFalseForAnyOtherSession(t *testing.T) {
    var typedNil *UnavailableSession

    for name, sessionInstance := range map[string]sessioncontract.Session{
        "stored":    &Session{id: generateSessionId(), values: map[string]any{}},
        "nil":       nil,
        "typed nil": typedNil,
        "foreign":   &foreignSession{id: "foreign", values: map[string]any{}},
        "nil cause": &UnavailableSession{values: &Session{values: map[string]any{}}},
    } {
        if cause, isUnavailable := SessionUnavailableCause(sessionInstance); true == isUnavailable || nil != cause {
            t.Fatalf("%s: expected false, got %v %v", name, cause, isUnavailable)
        }
    }
}
