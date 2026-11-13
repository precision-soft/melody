package exception

import (
    "errors"
    nethttp "net/http"
    "testing"
)

func TestInfrastructureFailure_MarkSurvivesFurtherWrapping(t *testing.T) {
    marked := MarkInfrastructureFailure(NewError("store is down", nil, nil))

    wrapped := NewError("nonce guard failed", nil, marked)

    if false == IsInfrastructureFailure(wrapped) {
        t.Fatal("expected the infrastructure mark to be readable through the wrapping error's cause chain")
    }
}

func TestInfrastructureFailure_UnmarkedErrorIsNotInfrastructure(t *testing.T) {
    if true == IsInfrastructureFailure(NewError("signature mismatch", nil, nil)) {
        t.Fatal("a credential failure must not read as an infrastructure failure")
    }
}

func TestInfrastructureFailure_NilStaysNil(t *testing.T) {
    if nil != MarkInfrastructureFailure(nil) {
        t.Fatal("marking a nil error must answer nil")
    }

    if true == IsInfrastructureFailure(nil) {
        t.Fatal("a nil error must not read as an infrastructure failure")
    }

    if true == AnswersUnavailable(nil) {
        t.Fatal("a nil error must not answer unavailable")
    }
}

func TestInfrastructureFailure_ErrorIsTheCauseText(t *testing.T) {
    marked := MarkInfrastructureFailure(errors.New("store is down"))

    if "store is down" != marked.Error() {
        t.Fatalf("expected the cause text, got %q", marked.Error())
    }

    if "store is down" != errors.Unwrap(marked).Error() {
        t.Fatal("expected the mark to unwrap to its cause")
    }
}

func TestIsInfrastructureFailure_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if true == IsInfrastructureFailure(newCyclicChainError()) {
            t.Error("expected a chain without the mark to read as unmarked")
        }
    })
}

func TestAnswersUnavailable_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if true == AnswersUnavailable(newCyclicChainError()) {
            t.Error("expected a chain without the mark not to answer unavailable")
        }
    })
}

func TestAnswersUnavailable_TheOutermostStatusDecides(t *testing.T) {
    storeDown := errors.New("store is down")

    for name, testCase := range map[string]struct {
        err      error
        expected bool
    }{
        "a plain error":                          {err: storeDown, expected: false},
        "a marked plain error":                   {err: MarkInfrastructureFailure(storeDown), expected: true},
        "a marked error wrapped further":         {err: NewError("the lookup failed", nil, MarkInfrastructureFailure(storeDown)), expected: true},
        "a store's status beneath the mark":      {err: MarkInfrastructureFailure(NotFound("no such row")), expected: true},
        "an http status around the marked cause": {err: NewHttpError(nethttp.StatusBadRequest, HttpErrorConfig{Message: "bad request", Cause: MarkInfrastructureFailure(storeDown)}), expected: false},
        "an unmarked http status":                {err: NotFound("no such row"), expected: false},
        "a join of the mark and a 429":           {err: errors.Join(MarkInfrastructureFailure(storeDown), TooManyRequests("slow down")), expected: false},
        "a join of a 429 and the mark":           {err: errors.Join(TooManyRequests("slow down"), MarkInfrastructureFailure(storeDown)), expected: false},
    } {
        if testCase.expected != AnswersUnavailable(testCase.err) {
            t.Fatalf("%s: expected AnswersUnavailable %t", name, testCase.expected)
        }
    }
}

func TestAnswersUnavailable_ReturnsOnAMarkedCyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if false == AnswersUnavailable(MarkInfrastructureFailure(newCyclicChainError())) {
            t.Error("expected a marked cyclic chain to answer unavailable")
        }
    })
}
