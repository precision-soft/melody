package security

import (
    "errors"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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
    store := NewInMemoryTokenStore()
    store.Put("opaque-123", securitycontract.Claims{UserIdentifier: "u1", Roles: []string{"ROLE_USER"}})

    claims, validateErr := NewOpaqueTokenValidator(store).Validate(nil, "opaque-123")
    if nil != validateErr {
        t.Fatalf("unexpected error: %v", validateErr)
    }

    if "u1" != claims.UserIdentifier {
        t.Fatalf("expected the stored subject, got %q", claims.UserIdentifier)
    }
}

func TestOpaqueTokenValidator_ValidateRefusesATokenTheStoreDoesNotHold(t *testing.T) {
    _, validateErr := NewOpaqueTokenValidator(NewInMemoryTokenStore()).Validate(nil, "opaque-nobody-stored")
    if nil == validateErr || false == strings.Contains(validateErr.Error(), "opaque token was not found") {
        t.Fatalf("expected the not-found refusal, got %v", validateErr)
    }
}

/* a stored record with no subject would authenticate a principal nobody can name: the roles would still be granted, and every audit line downstream would carry an empty identifier */
func TestOpaqueTokenValidator_ValidateRefusesAStoredRecordWithoutASubject(t *testing.T) {
    store := NewInMemoryTokenStore()
    store.Put("opaque-empty", securitycontract.Claims{UserIdentifier: "", Roles: []string{"ROLE_USER"}})

    _, validateErr := NewOpaqueTokenValidator(store).Validate(nil, "opaque-empty")
    if nil == validateErr || false == strings.Contains(validateErr.Error(), "opaque token has an empty subject") {
        t.Fatalf("expected the empty-subject refusal, got %v", validateErr)
    }
}

/* a store that cannot answer is the platform's failure, not the token's: the mark is what lets the bearer source file it as the incident it is — every opaque-token caller degrading to anonymous at once — instead of the routine Info a bad token earns */
func TestOpaqueTokenValidator_ValidateMarksAStoreFailureAsInfrastructure(t *testing.T) {
    lookupFailure := errors.New("redis is down")

    _, validateErr := NewOpaqueTokenValidator(&failingTokenStore{failure: lookupFailure}).Validate(nil, "opaque-123")
    if nil == validateErr {
        t.Fatal("expected an unanswerable store to refuse")
    }

    if false == errors.Is(validateErr, lookupFailure) {
        t.Fatalf("expected the store's own failure to stay in the cause chain, got %v", validateErr)
    }

    if false == isInfrastructureFailure(validateErr) {
        t.Fatalf("expected the failure to be marked as infrastructure, got %v", validateErr)
    }

    notFoundErr := NewInMemoryTokenStore()
    _, routineErr := NewOpaqueTokenValidator(notFoundErr).Validate(nil, "opaque-absent")
    if true == isInfrastructureFailure(routineErr) {
        t.Fatalf("expected an absent token to stay a routine refusal, got %v", routineErr)
    }
}
