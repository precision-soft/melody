package exception

import (
    "errors"
    "testing"
)

func assertPanicsWithEmergency(t *testing.T, expectedMessage string, callback func()) {
    t.Helper()

    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected a panic")
        }

        recoveredError, ok := recoveredValue.(*Error)
        if false == ok || nil == recoveredError {
            t.Fatalf("expected the panic to carry an *Error, got %v", recoveredValue)
        }

        if expectedMessage != recoveredError.Message() {
            t.Fatalf("expected message %q, got %q", expectedMessage, recoveredError.Message())
        }
    }()

    callback()
}

func TestNewExitError_NilError_Panics(t *testing.T) {
    assertPanicsWithEmergency(t, "exit error called with nil error", func() {
        NewExitError(1, nil)
    })
}

func TestNewExitError_CodeOutOfRange_Panics(t *testing.T) {
    for _, exitCode := range []int{-1, 0, 256, 1000} {
        assertPanicsWithEmergency(t, "exit code out of range", func() {
            NewExitError(exitCode, NewError("boom", nil, nil))
        })
    }
}

func TestNewExitError_AcceptsTheRangeBoundaries(t *testing.T) {
    for _, exitCode := range []int{1, 255} {
        exitError := NewExitError(exitCode, NewError("boom", nil, nil))

        if exitCode != exitError.ExitCode() {
            t.Fatalf("expected exit code %d, got %d", exitCode, exitError.ExitCode())
        }
    }
}

func TestExitError_ZeroValueError_NamesTheAnomaly(t *testing.T) {
    zeroValue := &ExitError{}

    if "exit error carries no error value" != zeroValue.Error() {
        t.Fatalf("unexpected message %q", zeroValue.Error())
    }
}

func TestExitError_ZeroValueUnwrap_ReturnsUntypedNil(t *testing.T) {
    zeroValue := &ExitError{}

    if nil != zeroValue.Unwrap() {
        t.Fatalf("expected an untyped nil")
    }
}

func TestExitError_CarriesItsErrorThroughUnwrapAndErrorValue(t *testing.T) {
    carried := NewError("boom", nil, nil)

    exitError := NewExitError(7, carried)

    if carried != exitError.Unwrap() {
        t.Fatalf("expected Unwrap to return the carried error")
    }

    if carried != exitError.ErrorValue() {
        t.Fatalf("expected ErrorValue to return the carried error")
    }

    if "boom" != exitError.Error() {
        t.Fatalf("expected the carried message, got %q", exitError.Error())
    }

    if false == errors.Is(exitError, carried) {
        t.Fatalf("expected errors.Is to reach the carried error through Unwrap")
    }
}

func TestExitError_NilReceiverAccessorsAnswerInsteadOfDereferencing(t *testing.T) {
    var typedNil *ExitError

    if 0 != typedNil.ExitCode() {
        t.Fatalf("expected a nil receiver to answer the out-of-range zero, got %d", typedNil.ExitCode())
    }

    if nil != typedNil.ErrorValue() {
        t.Fatalf("expected a nil receiver to answer no error value")
    }

    /* the shape that reaches it: a typed-nil wrapper found through the chain by errors.As */
    wrapped := NewError("command failed", nil, typedNil)

    var found *ExitError
    if false == errors.As(wrapped, &found) {
        t.Fatalf("test setup broken: errors.As must match the typed-nil link")
    }

    if nil != found {
        t.Fatalf("test setup broken: the matched pointer must be nil")
    }

    if 0 != found.ExitCode() {
        t.Fatalf("expected the matched typed nil to answer instead of panicking")
    }
}
