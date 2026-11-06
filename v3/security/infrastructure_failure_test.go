package security

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

func TestInfrastructureFailure_MarkSurvivesFurtherWrapping(t *testing.T) {
    marked := MarkInfrastructureFailure(exception.NewError("store is down", nil, nil))

    wrapped := exception.NewError("nonce guard failed", nil, marked)

    if false == IsInfrastructureFailure(wrapped) {
        t.Fatal("expected the infrastructure mark to be readable through the wrapping error's cause chain")
    }
}

func TestInfrastructureFailure_UnmarkedErrorIsNotInfrastructure(t *testing.T) {
    if true == IsInfrastructureFailure(exception.NewError("signature mismatch", nil, nil)) {
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
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

func TestIsInfrastructureFailure_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if true == IsInfrastructureFailure(newCyclicChainError()) {
            t.Error("expected a chain without the mark to read as unmarked")
        }
    })
}
