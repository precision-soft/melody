package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

func TestInfrastructureFailure_MarkSurvivesFurtherWrapping(t *testing.T) {
    marked := markInfrastructureFailure(exception.NewError("store is down", nil, nil))

    wrapped := exception.NewError("nonce guard failed", nil, marked)

    if false == isInfrastructureFailure(wrapped) {
        t.Fatal("expected the infrastructure mark to be readable through the wrapping error's cause chain")
    }
}

func TestInfrastructureFailure_UnmarkedErrorIsNotInfrastructure(t *testing.T) {
    if true == isInfrastructureFailure(exception.NewError("signature mismatch", nil, nil)) {
        t.Fatal("a credential failure must not read as an infrastructure failure")
    }
}

func TestInfrastructureFailure_NilStaysNil(t *testing.T) {
    if nil != markInfrastructureFailure(nil) {
        t.Fatal("marking a nil error must answer nil")
    }

    if true == isInfrastructureFailure(nil) {
        t.Fatal("a nil error must not read as an infrastructure failure")
    }
}
