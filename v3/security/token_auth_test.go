package security_test

import (
    "context"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "net/http/httptest"
    "testing"
    "time"

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

func TestJwtTokenValidator_RequireExpiryRejectsTokenWithoutExp(t *testing.T) {
    secret := []byte("super-secret")
    validator := security.NewJwtTokenValidator(security.JwtConfig{Secret: secret, RequireExpiry: true})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1"})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected rejection when exp is required and missing")
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
