package security

import (
    "testing"
)

func TestAnonymousToken_IsNobody(t *testing.T) {
    token := NewAnonymousToken()

    if true == token.IsAuthenticated() {
        t.Fatalf("expected the anonymous token to be unauthenticated")
    }

    if "" != token.UserIdentifier() {
        t.Fatalf("expected no user identifier, got %q", token.UserIdentifier())
    }

    roles := token.Roles()
    if nil == roles {
        t.Fatalf("expected an empty role list rather than nil, so a caller can range over it")
    }

    if 0 != len(roles) {
        t.Fatalf("expected no roles, got %v", roles)
    }
}

/* Scope and Attributes exist only on v3, where the contract widened; both must answer an empty map rather than nil, for the same reason Roles does — a consumer ranges over the answer without guarding it. */
func TestAnonymousToken_CarriesEmptyScopeAndAttributes(t *testing.T) {
    token := NewAnonymousToken()

    if nil == token.Scope() {
        t.Fatalf("expected an empty scope rather than nil")
    }

    if 0 != len(token.Scope()) {
        t.Fatalf("expected no scope entries, got %v", token.Scope())
    }

    if nil == token.Attributes() {
        t.Fatalf("expected empty attributes rather than nil")
    }

    if 0 != len(token.Attributes()) {
        t.Fatalf("expected no attributes, got %v", token.Attributes())
    }
}
