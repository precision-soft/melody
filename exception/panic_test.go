package exception

import (
    "testing"
)

/* @info the fallback is the only thing that keeps a mistaken Panic(nil) describable: without it the panic value is a nil *Error whose first method call inside any recovery dereferences it. The assertion is on the message, not merely on "something panicked" — a value-blind assertion passes over a fallback that says nothing */
func TestPanic_WithNilErrorPanics(t *testing.T) {
    assertPanicsWithEmergency(t, "panic called with nil error", func() {
        Panic(nil)
    })
}

/* @info Exit carries the sibling fallback, and it is the one that runs at the process boundary: the exit handler recovers the value and reads it to decide the exit code */
func TestExit_WithNilErrorPanics(t *testing.T) {
    assertPanicsWithEmergency(t, "exit called with nil error", func() {
        Exit(nil)
    })
}

func TestExit_WithErrorPanicsWithSamePointer(t *testing.T) {
    expected := NewExitError(3, NewError("exit", nil, nil))

    defer func() {
        recoveredValue := recover()
        if expected != recoveredValue {
            t.Fatalf("expected panic value to be the same *ExitError instance")
        }
    }()

    Exit(expected)
}

func TestPanic_WithErrorPanicsWithSamePointer(t *testing.T) {
    expected := NewError("panic", nil, nil)

    defer func() {
        recoveredValue := recover()
        if expected != recoveredValue {
            t.Fatalf("expected panic value to be the same *Error instance")
        }
    }()

    Panic(expected)
}
