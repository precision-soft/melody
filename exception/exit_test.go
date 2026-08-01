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

/* @info os.Exit hands the code to the operating system, which keeps its low 8 bits: 256 reports success from a dying process and a negative reads as 255, while 0 contradicts the error the constructor requires */
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

/* @info the zero value is constructible outside the constructor that refuses a nil error; the methods name the anomaly instead of dereferencing it at the process boundary */
func TestExitError_ZeroValueError_NamesTheAnomaly(t *testing.T) {
    zeroValue := &ExitError{}

    if "exit error carries no error value" != zeroValue.Error() {
        t.Fatalf("unexpected message %q", zeroValue.Error())
    }
}

/* @info returning the nil field through the interface would box a typed nil that passes every nil comparison downstream */
func TestExitError_ZeroValueUnwrap_ReturnsUntypedNil(t *testing.T) {
    zeroValue := &ExitError{}

    if nil != zeroValue.Unwrap() {
        t.Fatalf("expected an untyped nil")
    }
}

/* @info the carried error must travel out of both doors: Unwrap is what errors.Is and errors.As walk, ErrorValue is what the exit handler reads to decide whether the record still needs writing */
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
