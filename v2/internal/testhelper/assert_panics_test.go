package testhelper

import (
    "testing"

    "github.com/precision-soft/melody/v2/exception"
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

func TestAssertPanics_FailsWhenTheCallbackDoesNotPanic(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanics(substituteT, func() {})
    })

    if false == substituteT.Failed() {
        t.Fatalf("expected AssertPanics to fail when the callback does not panic")
    }
}

func TestAssertPanics_PassesWhenTheCallbackPanics(t *testing.T) {
    substituteT := runWithSubstituteT(func(substituteT *testing.T) {
        AssertPanics(substituteT, func() {
            exception.Panic(exception.NewError("boom", nil, nil))
        })
    })

    if true == substituteT.Failed() {
        t.Fatalf("expected AssertPanics to pass when the callback panics")
    }
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
