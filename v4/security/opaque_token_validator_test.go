package security

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestNewOpaqueTokenValidator_RefusesAStoreItCanNotAsk(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewOpaqueTokenValidator(nil)
    }, "token store is nil")

    var typedNilStore securitycontract.TokenStore = (*InMemoryTokenStore)(nil)
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewOpaqueTokenValidator(typedNilStore)
    }, "token store is nil")
}

func TestOpaqueTokenValidator_ValidateAnswersTheStoredClaims(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put(newTestRuntime(), "opaque-123", securitycontract.Claims{UserIdentifier: "u1", Roles: []string{"ROLE_USER"}}, bound.UnlimitedDuration())

    claims, validateErr := NewOpaqueTokenValidator(store).Validate(newTestRuntime(), "opaque-123")
    if nil != validateErr {
        t.Fatalf("unexpected error: %v", validateErr)
    }

    if "u1" != claims.UserIdentifier {
        t.Fatalf("expected the stored subject, got %q", claims.UserIdentifier)
    }
}

func TestOpaqueTokenValidator_ValidateRefusesATokenTheStoreDoesNotHold(t *testing.T) {
    _, validateErr := NewOpaqueTokenValidator(NewInMemoryTokenStore(InMemoryTokenStoreConfig{})).Validate(newTestRuntime(), "opaque-nobody-stored")
    if nil == validateErr || false == strings.Contains(validateErr.Error(), "opaque token was not found") {
        t.Fatalf("expected the not-found refusal, got %v", validateErr)
    }
}

func TestOpaqueTokenValidator_ValidateRefusesAStoredRecordWithoutASubject(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put(newTestRuntime(), "opaque-empty", securitycontract.Claims{UserIdentifier: "", Roles: []string{"ROLE_USER"}}, bound.UnlimitedDuration())

    _, validateErr := NewOpaqueTokenValidator(store).Validate(newTestRuntime(), "opaque-empty")
    if nil == validateErr || false == strings.Contains(validateErr.Error(), "opaque token has an empty subject") {
        t.Fatalf("expected the empty-subject refusal, got %v", validateErr)
    }
}

func TestOpaqueTokenValidator_ValidateMarksAStoreFailureAsInfrastructure(t *testing.T) {
    lookupFailure := errors.New("redis is down")

    _, validateErr := NewOpaqueTokenValidator(&failingTokenStore{failure: lookupFailure}).Validate(newTestRuntime(), "opaque-123")
    if nil == validateErr {
        t.Fatal("expected an unanswerable store to refuse")
    }

    if false == errors.Is(validateErr, lookupFailure) {
        t.Fatalf("expected the store's own failure to stay in the cause chain, got %v", validateErr)
    }

    if false == exception.IsInfrastructureFailure(validateErr) {
        t.Fatalf("expected the failure to be marked as infrastructure, got %v", validateErr)
    }

    notFoundErr := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    _, routineErr := NewOpaqueTokenValidator(notFoundErr).Validate(newTestRuntime(), "opaque-absent")
    if true == exception.IsInfrastructureFailure(routineErr) {
        t.Fatalf("expected an absent token to stay a routine refusal, got %v", routineErr)
    }
}
