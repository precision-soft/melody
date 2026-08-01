package security

import (
    "testing"
)

/* @info the anonymous token is the fallback every resolution path falls back to; its three answers are what "nobody is signed in" means to the voters and to the access control listener */
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
