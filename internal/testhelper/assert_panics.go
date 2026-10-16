package testhelper

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

/* AssertPanicsWithError requires the recovered value to be an error whose message contains expectedMessage, a substring so that a wrapper appending a cause stays expressible. */
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

        /* a typed-nil error would dereference its nil receiver in Error() and crash the test binary instead of naming the defect */
        if true == internal.IsNilInterface(recoveredErr) {
            t.Fatalf(
                "expected panic with error message %q, got a typed-nil error panic value of type %T",
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
