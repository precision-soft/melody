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

/* @info a typed-nil error passes the error assertion and its Error() dereferences the nil receiver; the helper must fail the test naming the shape instead of crashing the binary at its own line */
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
