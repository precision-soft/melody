package exception

import (
    "testing"
)

func TestPanic_WithNilErrorPanics(t *testing.T) {
    assertPanicsWithEmergency(t, "panic called with nil error", func() {
        Panic(nil)
    })
}

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
