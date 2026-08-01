package testhelper

import (
    "strings"
    "testing"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
)

/* AssertPanicsWithError pins the identity of the panic: the recovered value must be an error whose message contains expectedMessage. The match is a substring so that a guard reached through a wrapper that appends a cause to its message stays expressible. A value-blind sibling that passed on ANY recovered value used to live beside this one — it could not tell the guard under test firing from the code crashing for an unrelated reason three lines later, so deleting the guard kept its test green; it had no callers and was removed. */
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

        /* a typed-nil error satisfies the assertion above and its Error() dereferences the nil receiver — the helper then crashed the whole test binary blaming this file, while the actual defect, panicking with a typed nil, was never named */
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
