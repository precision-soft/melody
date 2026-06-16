package exception

import (
    "testing"
)

func TestPanic_WithNilErrorPanics(t *testing.T) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    Panic(nil)
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
