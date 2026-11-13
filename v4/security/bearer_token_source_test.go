package security

import (
    "errors"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestBearerTokenSource_OpaqueValidTokenAuthenticates(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put("opaque-123", securitycontract.Claims{UserIdentifier: "user-9", Roles: []string{"ROLE_ADMIN"}}, bound.UnlimitedDuration())

    source := NewBearerTokenSource(NewOpaqueTokenValidator(store), nil)

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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put("opaque-123", securitycontract.Claims{UserIdentifier: "user-9", Roles: []string{"ROLE_ADMIN"}}, bound.UnlimitedDuration())

    source := NewBearerTokenSource(NewOpaqueTokenValidator(store), nil)

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
    source := NewBearerTokenSource(NewOpaqueTokenValidator(NewInMemoryTokenStore(InMemoryTokenStoreConfig{})), nil)

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest(""))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token")
    }
}

func TestBearerTokenSource_UnknownOpaqueTokenIsAnonymous(t *testing.T) {
    source := NewBearerTokenSource(NewOpaqueTokenValidator(NewInMemoryTokenStore(InMemoryTokenStoreConfig{})), nil)

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest("missing"))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token for unknown opaque token")
    }
}

func TestBearerTokenSource_RejectsEmptySubjectOpaqueToken(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put("opaque-empty", securitycontract.Claims{Roles: []string{"ROLE_USER"}}, bound.UnlimitedDuration())

    source := NewBearerTokenSource(NewOpaqueTokenValidator(store), nil)

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest("opaque-empty"))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected a subjectless opaque token to fall back to anonymous")
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
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, ScopeClaim: "scope"})
    source := NewBearerTokenSource(validator, scopeRoleEnricher{})

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
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, ScopeClaim: "scope"})
    source := NewBearerTokenSource(validator, scopeRoleEnricher{})

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

/* failingTokenStore models the token store's backend being down. */
type failingTokenStore struct {
    failure error
}

func (instance *failingTokenStore) Lookup(
    _ runtimecontract.Runtime,
    _ string,
) (securitycontract.Claims, bool, error) {
    return securitycontract.Claims{}, false, instance.failure
}

func TestBearerTokenSource_AStoreThatCannotAnswerIsUnavailable(t *testing.T) {
    storeErr := errors.New("redis is down")
    source := NewBearerTokenSource(NewOpaqueTokenValidator(&failingTokenStore{failure: storeErr}), nil)
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, bearerRequest("opaque-123"))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    cause := expectUnavailableToken(t, token)
    if false == errors.Is(cause, storeErr) {
        t.Fatalf("expected the store's error in the cause chain, got %v", cause)
    }

    if 0 != len(logger.records) {
        t.Fatalf("expected no record from the source, the resolution listener files the one record, got %v", logger.records)
    }

    unknown, _ := NewBearerTokenSource(NewOpaqueTokenValidator(NewInMemoryTokenStore(InMemoryTokenStoreConfig{})), nil).Resolve(testRuntime(), bearerRequest("opaque-unknown"))
    expectAnonymousToken(t, unknown)

    missing, _ := source.Resolve(testRuntime(), newSecurityTestRequest("GET", "/", nil, testRuntime()))
    expectAnonymousToken(t, missing)
}

func TestBearerTokenSource_BadCredentialStaysAtInfo(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    source := NewBearerTokenSource(NewOpaqueTokenValidator(store), nil)
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, bearerRequest("token-nobody-stored"))
    if nil != resolveErr || true == token.IsAuthenticated() {
        t.Fatalf("an unknown token must resolve anonymous without error: authenticated=%v err=%v", token.IsAuthenticated(), resolveErr)
    }

    if 0 != len(logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatal("a bad credential is routine noise and must not be filed at Error")
    }

    if 1 != len(logger.recordsAtLevel(loggingcontract.LevelInfo)) {
        t.Fatal("a bad credential must be filed at Info")
    }
}

/* markingEnricher stands in for an enricher built on the framework's stores, whose lookup failure carries the infrastructure mark. */
type markingEnricher struct{}

func (instance *markingEnricher) Enrich(
    _ runtimecontract.Runtime,
    _ securitycontract.Claims,
) (securitycontract.Claims, error) {
    return securitycontract.Claims{}, exception.NewError("roles lookup failed", nil, exception.MarkInfrastructureFailure(errors.New("store is down")))
}

func TestBearerTokenSource_AMarkedEnrichmentFailureIsUnavailable(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put("opaque-1", securitycontract.Claims{UserIdentifier: "user-9"}, bound.UnlimitedDuration())

    source := NewBearerTokenSource(NewOpaqueTokenValidator(store), &markingEnricher{})
    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, bearerRequest("opaque-1"))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    expectUnavailableToken(t, token)

    if 0 != len(logger.records) {
        t.Fatalf("expected no record from the source, got %v", logger.records)
    }
}

func TestExtractBearerToken_TrimsTheSpaceAroundTheCredential(t *testing.T) {
    tokenString, present := extractBearerToken("Bearer   opaque-123  ")
    if false == present || "opaque-123" != tokenString {
        t.Fatalf("expected the credential without its surrounding space, got %q present=%v", tokenString, present)
    }

    if _, present := extractBearerToken("Bearer    "); true == present {
        t.Fatalf("expected a scheme followed by space alone to carry no credential")
    }
}

func TestNewBearerTokenSource_ATypedNilEnricherIsRefused(t *testing.T) {
    var unassignedEnricher *markingEnricher

    expectPanicNamingArgument(t, "NewBearerTokenSource", "enricher", func() {
        NewBearerTokenSource(NewOpaqueTokenValidator(NewInMemoryTokenStore(InMemoryTokenStoreConfig{})), unassignedEnricher)
    })
}
