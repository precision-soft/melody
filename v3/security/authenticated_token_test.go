package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestNewAuthenticatedTokenFromClaims_CarriesScopeAndAttributes(t *testing.T) {
    claims := securitycontract.Claims{
        UserIdentifier: "alice",
        Roles:          []string{"ROLE_USER"},
        Scope:          map[string]any{"tenant": "acme"},
        Attributes:     map[string]any{"department": "wms"},
    }

    token := NewAuthenticatedTokenFromClaims(claims)

    if "acme" != token.Scope()["tenant"] {
        t.Fatalf("expected the scope to be carried onto the token, got %+v", token.Scope())
    }

    if "wms" != token.Attributes()["department"] {
        t.Fatalf("expected the attributes to be carried onto the token, got %+v", token.Attributes())
    }

    token.Attributes()["department"] = "tampered"
    if "wms" != token.Attributes()["department"] {
        t.Fatalf("expected Attributes() to return a defensive copy")
    }
}
