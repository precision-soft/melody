package security_test

import (
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func bearerRequest(tokenString string) httpcontract.Request {
    request := httptest.NewRequest("GET", "/api/resource", nil)
    if "" != tokenString {
        request.Header.Set("Authorization", "Bearer "+tokenString)
    }

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

func signJwtHs256(secret []byte, claims map[string]any) string {
    headerJson, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
    payloadJson, _ := json.Marshal(claims)

    signingInput := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(payloadJson)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestBearerTokenSource_OpaqueValidTokenAuthenticates(t *testing.T) {
    store := security.NewInMemoryTokenStore()
    store.Put("opaque-123", securitycontract.Claims{UserIdentifier: "user-9", Roles: []string{"ROLE_ADMIN"}})

    source := security.NewBearerTokenSource(security.NewOpaqueTokenValidator(store))

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
    store := security.NewInMemoryTokenStore()
    store.Put("opaque-123", securitycontract.Claims{UserIdentifier: "user-9", Roles: []string{"ROLE_ADMIN"}})

    source := security.NewBearerTokenSource(security.NewOpaqueTokenValidator(store))

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
    source := security.NewBearerTokenSource(security.NewOpaqueTokenValidator(security.NewInMemoryTokenStore()))

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest(""))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token")
    }
}

func TestBearerTokenSource_UnknownOpaqueTokenIsAnonymous(t *testing.T) {
    source := security.NewBearerTokenSource(security.NewOpaqueTokenValidator(security.NewInMemoryTokenStore()))

    token, resolveErr := source.Resolve(testRuntime(), bearerRequest("missing"))
    if nil != resolveErr {
        t.Fatalf("unexpected resolve error: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatalf("expected anonymous token for unknown opaque token")
    }
}

func TestInMemoryTokenStore_TtlExpiresToken(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := security.NewInMemoryTokenStoreWithClock(frozen)
    store.PutWithTtl("short-lived", securitycontract.Claims{UserIdentifier: "user-1"}, 30*time.Second)

    runtimeInstance := testRuntime()

    if _, found, _ := store.Lookup(runtimeInstance, "short-lived"); false == found {
        t.Fatalf("expected token to resolve before expiry")
    }

    frozen.Advance(time.Minute)

    if _, found, _ := store.Lookup(runtimeInstance, "short-lived"); true == found {
        t.Fatalf("expected token to stop resolving after the ttl elapses")
    }
}

func TestInMemoryTokenStore_DeleteByUserRevokesEveryToken(t *testing.T) {
    store := security.NewInMemoryTokenStore()
    store.Put("token-a", securitycontract.Claims{UserIdentifier: "user-1"})
    store.Put("token-b", securitycontract.Claims{UserIdentifier: "user-1"})
    store.Put("token-c", securitycontract.Claims{UserIdentifier: "user-2"})

    runtimeInstance := testRuntime()

    if removed := store.DeleteByUser("user-1"); 2 != removed {
        t.Fatalf("expected two tokens revoked, got %d", removed)
    }

    if _, found, _ := store.Lookup(runtimeInstance, "token-a"); true == found {
        t.Fatalf("expected token-a to be revoked")
    }
    if _, found, _ := store.Lookup(runtimeInstance, "token-c"); false == found {
        t.Fatalf("expected the other user's token to survive")
    }
}

func TestInMemoryTokenStore_PurgeExpiredDropsElapsedEntries(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := security.NewInMemoryTokenStoreWithClock(frozen)
    store.PutWithTtl("short", securitycontract.Claims{UserIdentifier: "user-1"}, 30*time.Second)
    store.Put("forever", securitycontract.Claims{UserIdentifier: "user-2"})

    frozen.Advance(time.Minute)

    if purged := store.PurgeExpired(); 1 != purged {
        t.Fatalf("expected exactly one expired entry purged, got %d", purged)
    }

    if _, found, _ := store.Lookup(testRuntime(), "forever"); false == found {
        t.Fatalf("expected the non-expiring token to survive the purge")
    }
}

func TestJwtTokenValidator_RejectsOutOfRangeExp(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": 1e19})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected an out-of-range exp to be rejected")
    }
}

func TestJwtTokenValidator_AcceptsValidToken(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub":   "user-1",
        "roles": []string{"ROLE_USER", "ROLE_PICKER"},
        "exp":   time.Now().Add(time.Hour).Unix(),
    })

    claims, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil != validateErr {
        t.Fatalf("unexpected validate error: %v", validateErr)
    }

    if "user-1" != claims.UserIdentifier {
        t.Fatalf("unexpected subject: %s", claims.UserIdentifier)
    }

    if 2 != len(claims.Roles) {
        t.Fatalf("unexpected roles: %v", claims.Roles)
    }
}

func TestJwtTokenValidator_RejectsBadSignature(t *testing.T) {
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: []byte("correct-secret")})

    tokenString := signJwtHs256([]byte("wrong-secret"), map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected signature mismatch error")
    }
}

func TestJwtTokenValidator_RejectsTokenWithoutExpByDefault(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1"})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected an exp claim to be required by default")
    }
}

func TestJwtTokenValidator_AllowWithoutExpiryAcceptsTokenWithoutExp(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, AllowWithoutExpiry: true})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1"})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a token without exp to be accepted when explicitly allowed: %v", validateErr)
    }
}

func TestJwtTokenValidator_RejectsMalformedExp(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": "not-a-number"})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected rejection for a malformed exp claim")
    }
}

func TestJwtTokenValidator_AcceptsMatchingAudienceAndIssuer(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{
        Secret:   secret,
        Issuer:   "wms",
        Audience: "picking",
    })

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "iss": "wms",
        "aud": []string{"reporting", "picking"},
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected token with matching aud/iss to be accepted: %v", validateErr)
    }
}

func TestJwtTokenValidator_RejectsWrongIssuer(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, Issuer: "wms"})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "iss": "other-service",
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected rejection for a mismatched issuer")
    }
}

func TestJwtTokenValidator_RejectsMissingAudience(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, Audience: "picking"})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "aud": "reporting",
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected rejection when the required audience is absent")
    }
}

func TestJwtTokenValidator_RejectsTokenIssuedInTheFuture(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "iat": time.Now().Add(time.Hour).Unix(),
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected rejection for a token issued in the future")
    }
}

func TestJwtTokenValidator_AcceptsPastIssuedAt(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "iat": time.Now().Add(-time.Hour).Unix(),
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a past iat to be accepted: %v", validateErr)
    }
}

func TestJwtTokenValidator_RejectsMalformedIssuedAt(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "iat": "not-a-number",
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected rejection for a malformed iat claim")
    }
}

func TestJsonEntryPoint_SetsWwwAuthenticateHeader(t *testing.T) {
    entryPoint := security.NewJsonEntryPoint()

    response, startErr := entryPoint.Start(testRuntime(), bearerRequest(""))
    if nil != startErr {
        t.Fatalf("unexpected start error: %v", startErr)
    }

    if nethttp.StatusUnauthorized != response.StatusCode() {
        t.Fatalf("expected a 401 status, got %d", response.StatusCode())
    }

    if "Bearer" != response.Headers().Get("WWW-Authenticate") {
        t.Fatalf("expected a WWW-Authenticate: Bearer header, got %q", response.Headers().Get("WWW-Authenticate"))
    }
}

func TestJwtTokenValidator_RejectsExpiredToken(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(-time.Hour).Unix(),
    })

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected expired token error")
    }
}
