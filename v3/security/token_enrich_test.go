package security_test

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func signJwtWithAlg(secret []byte, algorithm string, claims map[string]any) string {
    headerJson, _ := json.Marshal(map[string]any{"alg": algorithm, "typ": "JWT"})
    payloadJson, _ := json.Marshal(claims)

    signingInput := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(payloadJson)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestJwtTokenValidator_RejectsNoneAlgorithm(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtWithAlg(secret, "none", map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected alg=none to be rejected")
    }
}

func TestJwtTokenValidator_RejectsRsaAlgorithm(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtWithAlg(secret, "RS256", map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected alg=RS256 to be rejected")
    }
}

func TestJwtTokenValidator_LeewayAcceptsRecentlyExpired(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, Leeway: 5 * time.Second})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": time.Now().Add(-2 * time.Second).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a token expired within leeway to be accepted: %v", validateErr)
    }
}

func TestJwtTokenValidator_LeewayRelaxesNotBefore(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, Leeway: 5 * time.Second})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "nbf": time.Now().Add(2 * time.Second).Unix(),
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a not-yet-valid token within leeway to be accepted: %v", validateErr)
    }
}

func TestJwtTokenValidator_PopulatesScopeClaim(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, ScopeClaim: "scope"})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub":   "user-1",
        "exp":   time.Now().Add(time.Hour).Unix(),
        "scope": map[string]any{"tenant": "acme"},
    })

    claims, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil != validateErr {
        t.Fatalf("validate: %v", validateErr)
    }

    if "acme" != claims.Scope["tenant"] {
        t.Fatalf("expected scope claim to be populated, got %v", claims.Scope)
    }
}

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
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, ScopeClaim: "scope"})
    source := security.NewBearerTokenSourceWithEnricher(validator, scopeRoleEnricher{})

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
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, ScopeClaim: "scope"})
    source := security.NewBearerTokenSourceWithEnricher(validator, scopeRoleEnricher{})

    /** no role in scope → enricher errors → request continues anonymously */
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

func TestNewAuthenticatedTokenFromClaims_CarriesScopeAndAttributes(t *testing.T) {
    claims := securitycontract.Claims{
        UserIdentifier: "alice",
        Roles:          []string{"ROLE_USER"},
        Scope:          map[string]any{"tenant": "acme"},
        Attributes:     map[string]any{"department": "wms"},
    }

    token := security.NewAuthenticatedTokenFromClaims(claims)

    if "acme" != token.Scope()["tenant"] {
        t.Fatalf("expected the scope to be carried onto the token, got %+v", token.Scope())
    }

    if "wms" != token.Attributes()["department"] {
        t.Fatalf("expected the attributes to be carried onto the token, got %+v", token.Attributes())
    }

    /** the accessor must return a copy so callers cannot mutate the token's internal state */
    token.Attributes()["department"] = "tampered"
    if "wms" != token.Attributes()["department"] {
        t.Fatalf("expected Attributes() to return a defensive copy")
    }
}
