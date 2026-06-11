package security

import (
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestBearerTokenSource_OpaqueValidTokenAuthenticates(t *testing.T) {
    store := NewInMemoryTokenStore()
    store.Put("opaque-123", securitycontract.Claims{UserIdentifier: "user-9", Roles: []string{"ROLE_ADMIN"}})

    source := NewBearerTokenSource(NewOpaqueTokenValidator(store))

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest("opaque-123"))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatalf("expected authenticated token")
    }

    if "user-9" != token.UserIdentifier() {
        t.Fatalf("unexpected user identifier: %s", token.UserIdentifier())
    }

    if 1 != len(token.Roles()) || "ROLE_ADMIN" != token.Roles()[0] {
        t.Fatalf("unexpected roles: %v", token.Roles())
    }
}

func TestBearerTokenSource_AcceptsCaseInsensitiveScheme(t *testing.T) {
    store := NewInMemoryTokenStore()
    store.Put("opaque-123", securitycontract.Claims{UserIdentifier: "user-9", Roles: []string{"ROLE_ADMIN"}})

    source := NewBearerTokenSource(NewOpaqueTokenValidator(store))

    request := httptest.NewRequest("GET", "/api/resource", nil)
    request.Header.Set("Authorization", "bearer opaque-123")

    token, resolveErr := source.Resolve(testRuntime(), testhelper.NewHttpTestRequestFromHttpRequest(request))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatalf("expected lowercase bearer scheme to authenticate")
    }
}

func TestBearerTokenSource_MissingHeaderIsAnonymous(t *testing.T) {
    source := NewBearerTokenSource(NewOpaqueTokenValidator(NewInMemoryTokenStore()))

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest(""))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token")
    }
}

func TestBearerTokenSource_UnknownOpaqueTokenIsAnonymous(t *testing.T) {
    source := NewBearerTokenSource(NewOpaqueTokenValidator(NewInMemoryTokenStore()))

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest("missing"))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token for unknown opaque token")
    }
}

func TestBearerTokenSource_RejectsEmptySubjectOpaqueToken(t *testing.T) {
    store := NewInMemoryTokenStore()
    store.Put("opaque-empty", securitycontract.Claims{Roles: []string{"ROLE_USER"}})

    source := NewBearerTokenSource(NewOpaqueTokenValidator(store))

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest("opaque-empty"))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected a subjectless opaque token to fall back to anonymous")
    }
}

/** @info enricher */

type scopeRoleEnricher struct{}

func (instance scopeRoleEnricher) Enrich(runtimeInstance runtimecontract.Runtime, claims securitycontract.Claims) (securitycontract.Claims, error) {
    role, hasRole := claims.Scope["role"].(string)
    if false == hasRole {
        return claims, exception.NewError("scope has no role", nil, nil)
    }

    claims.Roles = []string{role}

    return claims, nil
}

func TestBearerTokenSource_EnricherResolvesRolesFromScope(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, ScopeClaim: "scope"})
    source := NewBearerTokenSourceWithEnricher(validator, scopeRoleEnricher{})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub":   "user-1",
        "exp":   time.Now().Add(time.Hour).Unix(),
        "scope": map[string]any{"role": "ROLE_ADMIN"},
    })

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest(tokenString))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if false == token.IsAuthenticated() {
        t.Fatalf("expected an authenticated token")
    }

    roles := token.Roles()
    if 1 != len(roles) || "ROLE_ADMIN" != roles[0] {
        t.Fatalf("expected the enricher to resolve roles from scope, got %v", roles)
    }
}

func TestBearerTokenSource_EnrichmentFailureFallsBackToAnonymous(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, ScopeClaim: "scope"})
    source := NewBearerTokenSourceWithEnricher(validator, scopeRoleEnricher{})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub":   "user-1",
        "exp":   time.Now().Add(time.Hour).Unix(),
        "scope": map[string]any{"tenant": "acme"},
    })

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest(tokenString))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token when enrichment fails")
    }
}
