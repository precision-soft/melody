package testhelper

import (
    "testing"

    "github.com/precision-soft/melody/exception"
)

func runWithSubstituteT(callback func(substituteT *testing.T)) *testing.T {
    substituteT := &testing.T{}

    finished := make(chan struct{})

    go func() {
        defer close(finished)

        callback(substituteT)
    }()

    <-finished

    return substituteT
}

func TestAssertPanicsWithError_RefusesANilTestingT(t *testing.T) {
    assertRefusal(t, "testing t may not be nil", func() {
        AssertPanicsWithError(nil, func() {}, "boom")
    })
}

func TestAssertPanicsWithError_RefusesANilCallback(t *testing.T) {
    substituteT := &testing.T{}

    assertRefusal(t, "callback may not be nil", func() {
        AssertPanicsWithError(substituteT, nil, "boom")
    })

    if true == substituteT.Failed() {
        t.Fatalf("expected the refusal to be raised rather than reported as a test failure")
    }
}

func TestAssertPanicsWithError_RefusesAnEmptyExpectedMessage(t *testing.T) {
    substituteT := &testing.T{}

    assertRefusal(t, "expected message may not be empty", func() {
        AssertPanicsWithError(substituteT, func() {}, "")
    })

    if true == substituteT.Failed() {
        t.Fatalf("expected the refusal to be raised rather than reported as a test failure")
    }
}

func assertRefusal(t *testing.T, expectedMessage string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredValue := recover()
        if nil == recoveredValue {
            t.Fatalf("expected %q to be refused, got no panic", expectedMessage)
        }

        recoveredErr, isError := recoveredValue.(error)
        if false == isError {
            t.Fatalf("expected an error panic value, got %#v", recoveredValue)
        }

        if expectedMessage != recoveredErr.Error() {
            t.Fatalf("unexpected refusal message: %q", recoveredErr.Error())
        }
    }()

    callback()
}

func TestAssertPanicsWithError_FailsWhenTheCallbackDoesNotPanic(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanicsWithError(substituteT, func() {}, "boom")
    })

    if false == substituteT.Failed() {
        t.Fatalf("expected AssertPanicsWithError to fail when the callback does not panic")
    }
}

func TestAssertPanicsWithError_FailsWhenThePanicCarriesAnotherMessage(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanicsWithError(
            substituteT,
            func() {
                exception.Panic(exception.NewError("some other guard", nil, nil))
            },
            "boom",
        )
    })

    if false == substituteT.Failed() {
        t.Fatalf("expected AssertPanicsWithError to fail when the panic carries another message")
    }
}

func TestAssertPanicsWithError_FailsWhenThePanicValueIsNotAnError(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanicsWithError(
            substituteT,
            func() {
                panic("boom")
            },
            "boom",
        )
    })

    if false == substituteT.Failed() {
        t.Fatalf("expected AssertPanicsWithError to fail when the panic value is not an error")
    }
}

func TestAssertPanicsWithError_FailsWhenThePanicValueIsATypedNilError(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanicsWithError(
            substituteT,
            func() {
                var typedNil *exception.Error
                panic(typedNil)
            },
            "boom",
        )
    })

    if false == substituteT.Failed() {
        t.Fatalf("expected AssertPanicsWithError to fail when the panic value is a typed-nil error")
    }
}

func TestAssertPanicsWithError_PassesWhenThePanicCarriesTheExpectedMessage(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanicsWithError(
            substituteT,
            func() {
                exception.Panic(exception.NewError("boom", nil, nil))
            },
            "boom",
        )
    })

    if true == substituteT.Failed() {
        t.Fatalf("expected AssertPanicsWithError to pass when the panic carries the expected message")
    }
}

func TestAssertPanicsWithError_MatchesAMessageFragment(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanicsWithError(
            substituteT,
            func() {
                exception.Panic(exception.NewError("the guard exploded: boom", nil, nil))
            },
            "the guard exploded",
        )
    })

    if true == substituteT.Failed() {
        t.Fatalf("expected AssertPanicsWithError to match a fragment of the panic message")
    }
}
