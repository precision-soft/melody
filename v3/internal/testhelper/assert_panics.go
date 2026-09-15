package testhelper

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
)

/* AssertPanicsWithError requires an error panic whose message contains expectedMessage. The substring match permits wrappers that append cause details. */
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
