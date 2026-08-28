package security

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

type revocationEpochStoreStub struct {
    epochs      map[string]time.Time
    failure     error
    askedUser   string
    askedDevice string
}

func (instance *revocationEpochStoreStub) RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time) {
    if nil == instance.epochs {
        instance.epochs = map[string]time.Time{}
    }

    instance.epochs[userIdentifier+"|"+deviceIdentifier] = instant
}

func (instance *revocationEpochStoreStub) RevocationEpoch(
    _ runtimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    instance.askedUser = userIdentifier
    instance.askedDevice = deviceIdentifier

    if nil != instance.failure {
        return time.Time{}, instance.failure
    }

    if epoch, found := instance.epochs[userIdentifier+"|"+deviceIdentifier]; true == found {
        return epoch, nil
    }

    return instance.epochs[userIdentifier+"|"], nil
}

func TestJwtTokenValidator_CarriesTheIssuedAtClaim(t *testing.T) {
    secret := []byte("secret")
    issued := time.Now().Add(-time.Minute).Truncate(time.Second)

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": issued.Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    claims, err := NewJwtTokenValidator(JwtConfig{Secret: secret}).Validate(testRuntime(), token)
    if nil != err {
        t.Fatalf("validate: %v", err)
    }

    if false == claims.IssuedAt.Equal(issued) {
        t.Fatalf("expected the issue instant %v to be carried, got %v", issued, claims.IssuedAt)
    }
}

func TestJwtTokenValidator_ReadsTheDeviceClaimOnlyWhenOneIsConfigured(t *testing.T) {
    secret := []byte("secret")
    payload := map[string]any{
        "sub":       "alice",
        "device_id": "phone",
        "iat":       time.Now().Add(-time.Minute).Unix(),
        "exp":       time.Now().Add(time.Hour).Unix(),
    }

    unconfigured, err := NewJwtTokenValidator(JwtConfig{Secret: secret}).Validate(testRuntime(), signJwtHs256(secret, payload))
    if nil != err {
        t.Fatalf("validate without a device claim: %v", err)
    }

    if "" != unconfigured.DeviceIdentifier {
        t.Fatalf("a device was read although no claim was configured, got %q", unconfigured.DeviceIdentifier)
    }

    configured, err := NewJwtTokenValidator(JwtConfig{Secret: secret, DeviceClaim: "device_id"}).Validate(testRuntime(), signJwtHs256(secret, payload))
    if nil != err {
        t.Fatalf("validate with a device claim: %v", err)
    }

    if "phone" != configured.DeviceIdentifier {
        t.Fatalf("expected the configured claim to name the device, got %q", configured.DeviceIdentifier)
    }
}

func TestJwtTokenValidator_RefusesATokenIssuedBeforeTheBoundary(t *testing.T) {
    secret := []byte("secret")
    boundary := time.Now().Add(-time.Minute)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore("alice", "", boundary)

    validator := NewJwtTokenValidatorWithRevocationEpoch(JwtConfig{Secret: secret}, store)

    stale := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": boundary.Add(-time.Minute).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    if _, err := validator.Validate(testRuntime(), stale); nil == err {
        t.Fatalf("a token issued before the boundary was accepted, so the token is unrevocable")
    }

    fresh := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": boundary.Add(time.Minute).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    if _, err := validator.Validate(testRuntime(), fresh); nil != err {
        t.Fatalf("a token issued after the boundary was refused: %v", err)
    }
}

func TestJwtTokenValidator_DeviceBoundarySparesAnotherDevice(t *testing.T) {
    secret := []byte("secret")
    boundary := time.Now().Add(-time.Minute)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore("alice", "phone", boundary)

    validator := NewJwtTokenValidatorWithRevocationEpoch(JwtConfig{Secret: secret, DeviceClaim: "device_id"}, store)

    issuedAt := boundary.Add(-time.Minute).Unix()
    expiry := time.Now().Add(time.Hour).Unix()

    phone := signJwtHs256(secret, map[string]any{"sub": "alice", "device_id": "phone", "iat": issuedAt, "exp": expiry})
    if _, err := validator.Validate(testRuntime(), phone); nil == err {
        t.Fatalf("the revoked device's token was accepted")
    }

    if "phone" != store.askedDevice {
        t.Fatalf("expected the validator to ask the store about the device the token names, it asked about %q", store.askedDevice)
    }

    laptop := signJwtHs256(secret, map[string]any{"sub": "alice", "device_id": "laptop", "iat": issuedAt, "exp": expiry})
    if _, err := validator.Validate(testRuntime(), laptop); nil != err {
        t.Fatalf("revoking one device refused another device's token: %v", err)
    }
}

func TestJwtTokenValidator_RefusesATokenWithoutIssuedAtOnlyWhenABoundaryStoreIsWired(t *testing.T) {
    secret := []byte("secret")

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    if _, err := NewJwtTokenValidator(JwtConfig{Secret: secret}).Validate(testRuntime(), token); nil != err {
        t.Fatalf("a token without iat was refused although no boundary store is wired: %v", err)
    }

    withStore := NewJwtTokenValidatorWithRevocationEpoch(JwtConfig{Secret: secret}, &revocationEpochStoreStub{})
    if _, err := withStore.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token without iat was accepted although a boundary store is wired, so it can never be revoked")
    }
}

func TestJwtTokenValidator_RefusesWhenTheBoundaryStoreCannotAnswer(t *testing.T) {
    secret := []byte("secret")

    store := &revocationEpochStoreStub{failure: exception.NewError("redis is unreachable", nil, nil)}
    validator := NewJwtTokenValidatorWithRevocationEpoch(JwtConfig{Secret: secret}, store)

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": time.Now().Add(-time.Minute).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    if _, err := validator.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token was accepted while the boundary store could not answer whether it had been revoked")
    }
}

func TestJwtTokenValidator_WithRevocationEpochForcesFutureIssuedAtToBeRejected(t *testing.T) {
    secret := []byte("secret")

    config := JwtConfig{Secret: secret, RejectFutureIssuedAt: false}
    validator := NewJwtTokenValidatorWithRevocationEpoch(config, &revocationEpochStoreStub{})

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": time.Now().Add(time.Hour).Unix(),
        "exp": time.Now().Add(2 * time.Hour).Unix(),
    })

    if _, err := validator.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token stamped in the future was accepted, so it would outlive every revocation")
    }

    if false == NewJwtTokenValidator(config).rejectFutureIssuedAt {
        return
    }

    t.Fatalf("the plain constructor must keep the configuration's answer, or forcing it in the epoch constructor proves nothing")
}

func TestJwtTokenValidator_SecondGranularIssuedAtNeedsTheSkewAllowanceToBeRefused(t *testing.T) {
    secret := []byte("secret")

    issuance := time.Now().Add(-time.Minute).Truncate(time.Second)
    boundary := issuance.Add(500 * time.Millisecond)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore("alice", "", boundary)

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": issuance.Add(time.Second).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    unbounded := NewJwtTokenValidatorWithRevocationEpoch(JwtConfig{Secret: secret}, store)
    if _, err := unbounded.Validate(testRuntime(), token); nil != err {
        t.Fatalf("the second-granular window is already closed without an allowance, so the bounded half below proves nothing: %v", err)
    }

    bounded := NewJwtTokenValidatorWithRevocationEpoch(
        JwtConfig{Secret: secret, RevocationEpochSkew: time.Second},
        store,
    )
    if _, err := bounded.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token whose whole-second iat rounds past the boundary survived it although the allowance covers the second")
    }
}

func TestJwtTokenValidator_RevocationEpochSkewCoversAnAheadIssuer(t *testing.T) {
    secret := []byte("secret")

    boundary := time.Now().Add(-5 * time.Minute)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore("alice", "", boundary)

    aheadIssuance := boundary.Add(20 * time.Second)
    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": aheadIssuance.Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    unbounded := NewJwtTokenValidatorWithRevocationEpoch(JwtConfig{Secret: secret}, store)
    if _, err := unbounded.Validate(testRuntime(), token); nil != err {
        t.Fatalf("without a skew allowance the ahead-stamped token was already refused, so the bounded half below proves nothing: %v", err)
    }

    bounded := NewJwtTokenValidatorWithRevocationEpoch(
        JwtConfig{Secret: secret, RevocationEpochSkew: 30 * time.Second},
        store,
    )
    if _, err := bounded.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token stamped by a node running ahead survived a revocation the skew allowance covers")
    }

    beyond := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": boundary.Add(90 * time.Second).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })
    if _, err := bounded.Validate(testRuntime(), beyond); nil != err {
        t.Fatalf("a token issued well beyond the skew allowance was refused: %v", err)
    }
}

func signJwtWithHeader(secret []byte, header map[string]any, claims map[string]any) string {
    headerJson, _ := json.Marshal(header)
    payloadJson, _ := json.Marshal(claims)

    signingInput := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(payloadJson)

    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))

    return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestNewJwtTokenValidator_RefusesANegativeRevocationEpochSkew(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        NewJwtTokenValidator(JwtConfig{Secret: []byte("secret"), RevocationEpochSkew: -time.Second})
    }, "jwt revocation epoch skew may not be negative")
}

/* the validator must keep its own copy of the secret: retained by reference, the caller's slice stays mutable under every later signature check, so zeroing or rotating it in place silently changes what the validator verifies against. */
func TestJwtTokenValidator_CopiesTheSecret(t *testing.T) {
    secret := []byte("copy-me-before-i-change")
    tokenString := signJwtHs256(append([]byte{}, secret...), map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})

    for index := range secret {
        secret[index] = 0
    }

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("mutating the caller's secret slice changed what the validator verifies against: %v", validateErr)
    }
}

/* domain separation: the internal-auth envelope signs under its own typ through the same HS256 primitive, so under a shared or reused secret it must be refused HERE by type, not merely by which claims it happens to carry. */
func TestJwtTokenValidator_RefusesTheInternalAuthEnvelopeType(t *testing.T) {
    secret := []byte("shared-secret")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, SubjectClaim: "app", AllowWithoutExpiry: true})

    envelopeShaped := signJwtWithHeader(
        secret,
        map[string]any{"alg": "HS256", "typ": "melody-internal", "kid": "key-1"},
        map[string]any{"app": "wms-service", "exp": time.Now().Add(time.Hour).Unix()},
    )

    _, validateErr := validator.Validate(testRuntime(), envelopeShaped)
    if nil == validateErr {
        t.Fatal("an internal-auth envelope verified as a jwt: the typ must refuse it whatever the claims spell")
    }
}

func TestJwtTokenValidator_AcceptsAbsentAndLowerCaseJwtType(t *testing.T) {
    secret := []byte("secret")

    withoutType := signJwtWithHeader(
        secret,
        map[string]any{"alg": "HS256"},
        map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()},
    )

    validator := NewJwtTokenValidator(JwtConfig{Secret: secret})
    if _, validateErr := validator.Validate(testRuntime(), withoutType); nil != validateErr {
        t.Fatalf("RFC 7519 makes typ optional, so an absent typ must verify: %v", validateErr)
    }

    lowerCase := signJwtWithHeader(
        secret,
        map[string]any{"alg": "HS256", "typ": "jwt"},
        map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()},
    )

    if _, validateErr := validator.Validate(testRuntime(), lowerCase); nil != validateErr {
        t.Fatalf("typ compares case-insensitively per RFC 7519 §5.1, so \"jwt\" must verify: %v", validateErr)
    }
}

/* the frozen instant sits decades away from the real clock, so a token expiring shortly after it verifies ONLY if the validator reads the injected clock — and stops verifying when that clock alone advances. */
func TestJwtTokenValidator_VerifiesTimeClaimsAgainstTheInjectedClock(t *testing.T) {
    secret := []byte("secret")
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, Clock: frozen})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": int64(2000)})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("a token unexpired on the injected clock was refused, so the validator read some other clock: %v", validateErr)
    }

    frozen.Advance(30 * time.Minute)

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatal("the injected clock passed the expiry and the token still verified, so the validator read some other clock")
    }
}

func TestJwtTokenValidator_MarksAnEpochStoreFailureAsInfrastructure(t *testing.T) {
    secret := []byte("secret")
    store := &revocationEpochStoreStub{failure: errors.New("redis is down")}

    validator := NewJwtTokenValidatorWithRevocationEpoch(JwtConfig{Secret: secret}, store)

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": time.Now().Add(-time.Minute).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatal("an unanswerable epoch store must fail the validation closed")
    }

    if false == isInfrastructureFailure(validateErr) {
        t.Fatal("the epoch store failing to answer is the platform's failure and must carry the infrastructure mark")
    }
}

/* the acceptance above is the only nbf case the suite had, so the whole not-before block could be deleted with it still green — a token that says it is not usable yet would have validated. This is the refusal half: no leeway, activation an hour out. */
func TestJwtTokenValidator_RefusesANotBeforeBeyondTheLeeway(t *testing.T) {
    secret := []byte("super-secret-value")
    validator := NewJwtTokenValidator(JwtConfig{Secret: secret, Leeway: 0})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(2 * time.Hour).Unix(),
        "nbf": time.Now().Add(time.Hour).Unix(),
    })

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected a token whose activation is an hour away to be refused")
    }

    if "jwt is not yet valid" != validateErr.Error() {
        t.Fatalf("expected the not-yet-valid refusal, got %q", validateErr.Error())
    }
}
