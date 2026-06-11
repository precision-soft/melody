package security

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "testing"
    "time"
)

func signJwtHs256(secret []byte, claims map[string]any) string {
    headerJson, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": "JWT"})
    payloadJson, _ := json.Marshal(claims)

    signingInput := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(payloadJson)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func signJwtWithAlg(secret []byte, algorithm string, claims map[string]any) string {
    headerJson, _ := json.Marshal(map[string]any{"alg": algorithm, "typ": "JWT"})
    payloadJson, _ := json.Marshal(claims)

    signingInput := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(payloadJson)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestNumericClaim_RoundsNotBeforeUpAndExpiryDown(t *testing.T) {
    claims := map[string]any{
        "nbf": 1700000000.4,
        "iat": 1700000000.6,
        "exp": 1700000000.9,
    }

    notBefore, _, notBeforeValid := numericClaim(claims, "nbf", true)
    if false == notBeforeValid || 1700000001 != notBefore {
        t.Fatalf("nbf must round up (fail-closed: activate no earlier than the issuer stated), got %d (valid=%v)", notBefore, notBeforeValid)
    }

    issuedAt, _, issuedAtValid := numericClaim(claims, "iat", true)
    if false == issuedAtValid || 1700000001 != issuedAt {
        t.Fatalf("iat must round up (fail-closed), got %d (valid=%v)", issuedAt, issuedAtValid)
    }

    expiry, _, expiryValid := numericClaim(claims, "exp", false)
    if false == expiryValid || 1700000000 != expiry {
        t.Fatalf("exp must round down (fail-closed: expire no later than the issuer stated), got %d (valid=%v)", expiry, expiryValid)
    }
}

func TestJwtTokenValidator_RejectsOutOfRangeExp(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": 1e19})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected an out-of-range exp to be rejected")
    }
}

func TestJwtTokenValidator_AcceptsValidToken(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

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
    validator := NewJwtTokenValidator(JwtConfig{Secret: []byte("correct-secret")})

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
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1"})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected an exp claim to be required by default")
    }
}

func TestJwtTokenValidator_AllowWithoutExpiryAcceptsTokenWithoutExp(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, AllowWithoutExpiry: true})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1"})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a token without exp to be accepted when explicitly allowed: %v", validateErr)
    }
}

func TestJwtTokenValidator_RejectsMalformedExp(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": "not-a-number"})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected rejection for a malformed exp claim")
    }
}

func TestJwtTokenValidator_AcceptsMatchingAudienceAndIssuer(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{
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
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, Issuer: "wms"})

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
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, Audience: "picking"})

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
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, RejectFutureIssuedAt: true})

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
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

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
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
        "iat": "not-a-number",
    })

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected rejection for a malformed iat claim")
    }
}

func TestJwtTokenValidator_RejectsExpiredToken(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(-time.Hour).Unix(),
    })

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected expired token error")
    }
}

func TestJwtTokenValidator_RejectsEmptySubject(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, AllowWithoutExpiry: true})

    cases := map[string]map[string]any{
        "absent subject":     {"roles": []any{"ROLE_USER"}},
        "empty subject":      {"sub": "", "roles": []any{"ROLE_USER"}},
        "non-string subject": {"sub": 12345},
    }

    for name, claims := range cases {
        t.Run(name, func(t *testing.T) {
            tokenString := signJwtHs256(secret, claims)

            if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
                t.Fatalf("expected rejection for a token with no usable subject")
            }
        })
    }
}

func TestJwtTokenValidator_FutureIssuedAtAcceptedByDefault(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    future := time.Now().Add(1 * time.Hour).Unix()
    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": float64(future + 3600), "iat": float64(future)})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a future iat to be accepted by default: %v", validateErr)
    }
}

func TestJwtTokenValidator_FutureIssuedAtRejectedWhenConfigured(t *testing.T) {
    secret := []byte("super-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, RejectFutureIssuedAt: true})

    future := time.Now().Add(1 * time.Hour).Unix()
    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": float64(future + 3600), "iat": float64(future)})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected a future iat to be rejected when RejectFutureIssuedAt is set")
    }
}

/** @info algorithm and leeway */

func TestJwtTokenValidator_RejectsNoneAlgorithm(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    tokenString := signJwtWithAlg(secret, "none", map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected alg=none to be rejected")
    }
}

func TestJwtTokenValidator_RejectsRsaAlgorithm(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    tokenString := signJwtWithAlg(secret, "RS256", map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected alg=RS256 to be rejected")
    }
}

func TestJwtTokenValidator_LeewayAcceptsRecentlyExpired(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, Leeway: 5 * time.Second})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": time.Now().Add(-2 * time.Second).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a token expired within leeway to be accepted: %v", validateErr)
    }
}

func TestJwtTokenValidator_LeewayRelaxesNotBefore(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, Leeway: 5 * time.Second})

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
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, ScopeClaim: "scope"})

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
