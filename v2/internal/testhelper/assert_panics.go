package testhelper

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/exception"
)

func AssertPanics(t *testing.T, callback func()) {
    if nil == t {
        exception.Panic(
            exception.NewError("testing t may not be nil", nil, nil),
        )
    }
    if nil == callback {
        exception.Panic(
            exception.NewError("callback may not be nil", nil, nil),
        )
    }

    t.Helper()

    defer func() {
        /* the deferred closure is a frame of its own, so it must register as a helper here for a failure to be reported at the caller instead of inside this file */
        t.Helper()

        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic")
        }
    }()

    callback()
}

/* AssertPanicsWithError also pins the identity of the panic: the recovered value must be an error whose message contains expectedMessage. The match is a substring so that a guard reached through a wrapper that appends a cause to its message stays expressible. */
func AssertPanicsWithError(t *testing.T, callback func(), expectedMessage string) {
    if nil == t {
        exception.Panic(
            exception.NewError("testing t may not be nil", nil, nil),
        )
    }
    if nil == callback {
        exception.Panic(
            exception.NewError("callback may not be nil", nil, nil),
        )
    }
    if "" == expectedMessage {
        exception.Panic(
            exception.NewError("expected message may not be empty", nil, nil),
        )
    }

    t.Helper()

    defer func() {
        t.Helper()

        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected panic with error message %q, got no panic", expectedMessage)
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf(
                "expected panic with error message %q, got a non-error panic value %#v",
                expectedMessage,
                recoveredValue,
            )
        }

        actualMessage := recoveredErr.Error()
        if false == strings.Contains(actualMessage, expectedMessage) {
            t.Fatalf(
                "expected panic with error message %q, got %q",
                expectedMessage,
                actualMessage,
            )
        }
    }()

    callback()
}
