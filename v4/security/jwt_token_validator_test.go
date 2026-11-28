package security

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
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

func TestNumericClaim_ReadsEveryTimeClaimDown(t *testing.T) {
    claims := map[string]any{
        "nbf": 1700000000.4,
        "iat": 1700000000.6,
        "exp": 1700000000.9,
    }

    for _, name := range []string{"nbf", "iat", "exp"} {
        value, _, valid := numericClaim(claims, name)
        if false == valid || 1700000000 != value {
            t.Fatalf("%s must be read rounded down, got %d (valid=%v)", name, value, valid)
        }
    }
}

/* issuers write whole seconds, so a validator a few milliseconds behind the issuer across a second boundary reads a fresh token as issued in its future; the future checks of nbf and iat tolerate that one second on top of a zero leeway, and a fractional claim is accepted during its own first second */
func TestJwtTokenValidator_FutureChecksTolerateTheClaimsOneSecondResolution(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")

    for _, testCase := range []struct {
        name  string
        claim string
        value float64
        now   time.Time
    }{
        {name: "iat 2ms ahead across a boundary", claim: "iat", value: 1000, now: time.Unix(999, 998_000_000)},
        {name: "nbf 2ms ahead across a boundary", claim: "nbf", value: 1000, now: time.Unix(999, 998_000_000)},
        {name: "iat 800ms ahead, inside the second", claim: "iat", value: 1000, now: time.Unix(999, 200_000_000)},
        {name: "nbf 800ms ahead, inside the second", claim: "nbf", value: 1000, now: time.Unix(999, 200_000_000)},
        {name: "fractional iat 500ms in the past", claim: "iat", value: 1000.001, now: time.Unix(1000, 501_000_000)},
        {name: "fractional nbf 500ms in the past", claim: "nbf", value: 1000.001, now: time.Unix(1000, 501_000_000)},
    } {
        validator := newTestJwtValidator(secret, JwtConfig{Leeway: strictLeeway, Clock: clock.NewFrozenClock(testCase.now)})

        token := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": 5000, testCase.claim: testCase.value})
        if _, validateErr := validator.Validate(testRuntime(), token); nil != validateErr {
            t.Fatalf("%s: expected the token accepted, got %v", testCase.name, validateErr)
        }
    }
}

/* the tolerance is exactly one second: a claim 1.5s ahead of the clock is still refused, and the expiry is not widened by it */
func TestJwtTokenValidator_TheResolutionToleranceIsOneSecondAndSparesTheExpiry(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    frozen := clock.NewFrozenClock(time.Unix(998, 500_000_000))
    validator := newTestJwtValidator(secret, JwtConfig{Leeway: strictLeeway, Clock: frozen})

    for claim, expected := range map[string]string{"iat": "jwt is issued in the future", "nbf": "jwt is not yet valid"} {
        token := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": 5000, claim: 1000})

        _, validateErr := validator.Validate(testRuntime(), token)
        if nil == validateErr || expected != validateErr.Error() {
            t.Fatalf("%s 1.5s ahead: expected %q, got %v", claim, expected, validateErr)
        }
    }

    expiredValidator := newTestJwtValidator(secret, JwtConfig{Leeway: strictLeeway, Clock: clock.NewFrozenClock(time.Unix(1000, 1_000_000))})
    expiredToken := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": 1000})

    _, expiredErr := expiredValidator.Validate(testRuntime(), expiredToken)
    if nil == expiredErr || "jwt is expired" != expiredErr.Error() {
        t.Fatalf("expected a token 1ms past its exp refused, got %v", expiredErr)
    }
}

func TestJwtTokenValidator_RejectsOutOfRangeExp(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": 1e19})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected an out-of-range exp to be rejected")
    }
}

func TestJwtTokenValidator_AcceptsValidToken(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

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
    validator := newTestJwtValidator([]byte("correct-secret-padded-to-the-hs256-minimum"), JwtConfig{})

    tokenString := signJwtHs256([]byte("wrong-secret-padded-to-the-hs256-minimum"), map[string]any{
        "sub": "user-1",
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected signature mismatch error")
    }
}

func TestJwtTokenValidator_RejectsTokenWithoutExpByDefault(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1"})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected an exp claim to be required by default")
    }
}

func TestJwtTokenValidator_AllowWithoutExpiryAcceptsTokenWithoutExp(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{AllowWithoutExpiry: true})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1"})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a token without exp to be accepted when explicitly allowed: %v", validateErr)
    }
}

func TestJwtTokenValidator_RejectsMalformedExp(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": "not-a-number"})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatalf("expected rejection for a malformed exp claim")
    }
}

func TestJwtTokenValidator_AcceptsMatchingAudienceAndIssuer(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{
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
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{Issuer: "wms"})

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
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{Audience: "picking"})

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
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

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
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

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
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

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
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

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
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{AllowWithoutExpiry: true})

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

func TestJwtTokenValidator_AllowFutureIssuedAtAcceptsAFutureIssuedAt(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{AllowFutureIssuedAt: true})

    future := time.Now().Add(1 * time.Hour).Unix()
    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": float64(future + 3600), "iat": float64(future)})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected a future iat to be accepted through the door: %v", validateErr)
    }
}

func TestJwtTokenValidator_ZeroConfigRefusesAFutureIssuedAt(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    future := time.Now().Add(1 * time.Hour).Unix()
    tokenString := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": float64(future + 3600), "iat": float64(future)})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr || false == strings.Contains(validateErr.Error(), "jwt is issued in the future") {
        t.Fatalf("expected a future iat refused by default, got %v", validateErr)
    }
}

func TestJwtTokenValidator_RejectsNoneAlgorithm(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    tokenString := signJwtWithAlg(secret, "none", map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected alg=none to be rejected")
    }
}

/* the algorithm a refused header names is chosen by the caller before any signature check, so the refusal carries it bounded: a 200 KiB alg does not reach the journal whole */
func TestJwtTokenValidator_ARefusedAlgorithmIsBoundedInTheRefusal(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    tokenString := signJwtWithAlg(secret, strings.Repeat("A", 200*1024), map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatal("expected the algorithm refused")
    }

    algorithm, _ := exception.LogContext(validateErr, exception.LogContextOptions{})["algorithm"].(string)
    if 1024 < len(algorithm) || false == strings.Contains(algorithm, "truncated") {
        t.Fatalf("expected the algorithm bounded in the refusal, got %d bytes", len(algorithm))
    }
}

/* the type a refused header names is bounded as the algorithm is */
func TestJwtTokenValidator_ARefusedTypeIsBoundedInTheRefusal(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    headerJson, _ := json.Marshal(map[string]any{"alg": "HS256", "typ": strings.Repeat("T", 200*1024)})
    claimsJson, _ := json.Marshal(map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})
    tokenString := base64.RawURLEncoding.EncodeToString(headerJson) + "." + base64.RawURLEncoding.EncodeToString(claimsJson) + ".c2lnbmF0dXJl"

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatal("expected the type refused")
    }

    typeValue, _ := exception.LogContext(validateErr, exception.LogContextOptions{})["type"].(string)
    if 1024 < len(typeValue) || false == strings.Contains(typeValue, "truncated") {
        t.Fatalf("expected the type bounded in the refusal, got %d bytes", len(typeValue))
    }
}

func TestJwtTokenValidator_RejectsRsaAlgorithm(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{})

    tokenString := signJwtWithAlg(secret, "RS256", map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()})

    if _, validateErr := validator.Validate(testRuntime(), tokenString); nil == validateErr {
        t.Fatalf("expected alg=RS256 to be rejected")
    }
}

func TestJwtTokenValidator_ADeclaredLeewayAcceptsWhatTheDefaultRefuses(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))

    for name, claims := range map[string]map[string]any{
        "exp": {"sub": "user-1", "exp": 993},
        "nbf": {"sub": "user-1", "exp": 5000, "nbf": 1007},
    } {
        tokenString := signJwtHs256(secret, claims)

        declared := newTestJwtValidator(secret, JwtConfig{Leeway: bound.Of(10 * time.Second), Clock: frozen})
        if _, validateErr := declared.Validate(testRuntime(), tokenString); nil != validateErr {
            t.Fatalf("%s: expected a token 7s off accepted under a 10s leeway: %v", name, validateErr)
        }

        byDefault := newTestJwtValidator(secret, JwtConfig{Clock: frozen})
        if _, validateErr := byDefault.Validate(testRuntime(), tokenString); nil == validateErr {
            t.Fatalf("%s: expected a token 7s off refused under the default leeway", name)
        }
    }
}

func TestJwtTokenValidator_UnderAStoreTheFutureIssuedAtToleratesTheSkewNotTheLeeway(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", time.Unix(1000, 0), securitycontract.RevocationOptions{})

    for _, testCase := range []struct {
        name     string
        config   JwtConfig
        issuedAt int
        expected string
    }{
        {name: "default leeway, the claim's second", config: JwtConfig{}, issuedAt: 1001},
        {name: "default leeway, ahead of the second", config: JwtConfig{}, issuedAt: 1002, expected: "jwt is issued in the future"},
        {name: "wide leeway, ahead of the second", config: JwtConfig{Leeway: bound.Of(time.Minute)}, issuedAt: 1002, expected: "jwt is issued in the future"},
        {name: "skew and the second", config: JwtConfig{RevocationEpochSkew: bound.Of(3 * time.Second)}, issuedAt: 1004},
        {name: "past the skew and the second", config: JwtConfig{RevocationEpochSkew: bound.Of(3 * time.Second)}, issuedAt: 1005, expected: "jwt is issued in the future"},
    } {
        config := testCase.config
        config.Clock = frozen
        config.RevocationEpochStore = store

        token := signJwtHs256(secret, map[string]any{"sub": "alice", "iat": testCase.issuedAt, "exp": 5000})

        _, validateErr := newTestJwtValidator(secret, config).Validate(testRuntime(), token)
        if "" == testCase.expected && nil != validateErr {
            t.Fatalf("%s: expected the token accepted, got %v", testCase.name, validateErr)
        }

        if "" != testCase.expected && (nil == validateErr || testCase.expected != validateErr.Error()) {
            t.Fatalf("%s: expected %q, got %v", testCase.name, testCase.expected, validateErr)
        }
    }
}

func TestJwtTokenValidator_WithoutAStoreTheFutureIssuedAtToleratesTheLeeway(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{Clock: clock.NewFrozenClock(time.Unix(1000, 0))})

    admitted := signJwtHs256(secret, map[string]any{"sub": "alice", "iat": 1006, "exp": 5000})
    if _, validateErr := validator.Validate(testRuntime(), admitted); nil != validateErr {
        t.Fatalf("expected an iat inside the leeway and the claim's second accepted, got %v", validateErr)
    }

    refused := signJwtHs256(secret, map[string]any{"sub": "alice", "iat": 1007, "exp": 5000})
    if _, validateErr := validator.Validate(testRuntime(), refused); nil == validateErr || "jwt is issued in the future" != validateErr.Error() {
        t.Fatalf("expected an iat past the leeway and the claim's second refused, got %v", validateErr)
    }
}

func TestJwtTokenValidator_PopulatesScopeClaim(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{ScopeClaim: "scope"})

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

func (instance *revocationEpochStoreStub) RevokeBefore(
    _ containercontract.Runtime,
    userIdentifier string,
    instant time.Time,
    options securitycontract.RevocationOptions,
) error {
    if nil == instance.epochs {
        instance.epochs = map[string]time.Time{}
    }

    instance.epochs[userIdentifier+"|"+options.DeviceIdentifier] = instant

    return nil
}

func (instance *revocationEpochStoreStub) RevocationEpoch(
    _ containercontract.Runtime,
    userIdentifier string,
    options securitycontract.RevocationOptions,
) (time.Time, error) {
    instance.askedUser = userIdentifier
    instance.askedDevice = options.DeviceIdentifier

    if nil != instance.failure {
        return time.Time{}, instance.failure
    }

    if epoch, found := instance.epochs[userIdentifier+"|"+options.DeviceIdentifier]; true == found {
        return epoch, nil
    }

    return instance.epochs[userIdentifier+"|"], nil
}

func TestJwtTokenValidator_CarriesTheIssuedAtClaim(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")
    issued := time.Now().Add(-time.Minute).Truncate(time.Second)

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": issued.Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    claims, err := newTestJwtValidator(secret, JwtConfig{}).Validate(testRuntime(), token)
    if nil != err {
        t.Fatalf("validate: %v", err)
    }

    if false == claims.IssuedAt.Equal(issued) {
        t.Fatalf("expected the issue instant %v to be carried, got %v", issued, claims.IssuedAt)
    }
}

func TestJwtTokenValidator_ReadsTheDeviceClaimOnlyWhenOneIsConfigured(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")
    payload := map[string]any{
        "sub":       "alice",
        "device_id": "phone",
        "iat":       time.Now().Add(-time.Minute).Unix(),
        "exp":       time.Now().Add(time.Hour).Unix(),
    }

    unconfigured, err := newTestJwtValidator(secret, JwtConfig{}).Validate(testRuntime(), signJwtHs256(secret, payload))
    if nil != err {
        t.Fatalf("validate without a device claim: %v", err)
    }

    if "" != unconfigured.DeviceIdentifier {
        t.Fatalf("a device was read although no claim was configured, got %q", unconfigured.DeviceIdentifier)
    }

    configured, err := newTestJwtValidator(secret, JwtConfig{DeviceClaim: "device_id"}).Validate(testRuntime(), signJwtHs256(secret, payload))
    if nil != err {
        t.Fatalf("validate with a device claim: %v", err)
    }

    if "phone" != configured.DeviceIdentifier {
        t.Fatalf("expected the configured claim to name the device, got %q", configured.DeviceIdentifier)
    }
}

func TestJwtTokenValidator_RefusesATokenIssuedBeforeTheBoundary(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")
    boundary := time.Now().Add(-time.Minute)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", boundary, securitycontract.RevocationOptions{})

    validator := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: store})

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
    secret := []byte("secret-padded-to-the-hs256-minimum")
    boundary := time.Now().Add(-time.Minute)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", boundary, securitycontract.RevocationOptions{DeviceIdentifier: "phone"})

    validator := newTestJwtValidator(secret, JwtConfig{DeviceClaim: "device_id", RevocationEpochStore: store})

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
    secret := []byte("secret-padded-to-the-hs256-minimum")

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    if _, err := newTestJwtValidator(secret, JwtConfig{}).Validate(testRuntime(), token); nil != err {
        t.Fatalf("a token without iat was refused although no boundary store is wired: %v", err)
    }

    withStore := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: &revocationEpochStoreStub{}})
    if _, err := withStore.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token without iat was accepted although a boundary store is wired, so it can never be revoked")
    }
}

func TestJwtTokenValidator_RefusesWhenTheBoundaryStoreCannotAnswer(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")

    store := &revocationEpochStoreStub{failure: exception.NewError("redis is unreachable", nil, nil)}
    validator := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: store})

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": time.Now().Add(-time.Minute).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    if _, err := validator.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token was accepted while the boundary store could not answer whether it had been revoked")
    }
}

func TestJwtTokenValidator_WithRevocationEpochRefusesAFutureIssuedAt(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")

    validator := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: &revocationEpochStoreStub{}})

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": time.Now().Add(time.Hour).Unix(),
        "exp": time.Now().Add(2 * time.Hour).Unix(),
    })

    if _, err := validator.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token stamped in the future was accepted, so it would outlive every revocation")
    }
}

func TestNewJwtTokenValidator_RefusesAllowFutureIssuedAtUnderARevocationEpochStore(t *testing.T) {
    defer func() {
        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal || "JwtConfig.AllowFutureIssuedAt" != refusal.Context()["field"] || "JwtConfig.RevocationEpochStore" != refusal.Context()["conflictsWith"] {
            t.Fatalf("expected the future iat door refused under a store naming both fields, got %v", refusal)
        }
    }()

    newTestJwtValidator([]byte("secret-padded-to-the-hs256-minimum"), JwtConfig{AllowFutureIssuedAt: true, RevocationEpochStore: &revocationEpochStoreStub{}})
}

func TestNewJwtTokenValidator_RefusesATypedNilStoreAndANegativeLeewayOrSkew(t *testing.T) {
    var unassignedStore *revocationEpochStoreStub

    expectPanicNamingField(t, "JwtConfig.RevocationEpochStore", func() {
        newTestJwtValidator([]byte("secret-padded-to-the-hs256-minimum"), JwtConfig{RevocationEpochStore: unassignedStore})
    })

    expectPanicNamingField(t, "JwtConfig.Leeway", func() {
        newTestJwtValidator([]byte("secret-padded-to-the-hs256-minimum"), JwtConfig{Leeway: bound.Of(-time.Second)})
    })

    expectPanicNamingField(t, "JwtConfig.RevocationEpochSkew", func() {
        newTestJwtValidator([]byte("secret-padded-to-the-hs256-minimum"), JwtConfig{RevocationEpochSkew: bound.Of(-time.Second)})
    })
}

func TestJwtTokenValidator_SecondGranularIssuedAtNeedsTheSkewAllowanceToBeRefused(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")

    issuance := time.Now().Add(-time.Minute).Truncate(time.Second)
    boundary := issuance.Add(500 * time.Millisecond)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", boundary, securitycontract.RevocationOptions{})

    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": issuance.Add(time.Second).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    unbounded := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: store})
    if _, err := unbounded.Validate(testRuntime(), token); nil != err {
        t.Fatalf("the second-granular window is already closed without an allowance, so the bounded half below proves nothing: %v", err)
    }

    bounded := newTestJwtValidator(secret, JwtConfig{RevocationEpochSkew: bound.Of(time.Second), RevocationEpochStore: store})
    if _, err := bounded.Validate(testRuntime(), token); nil == err {
        t.Fatalf("a token whose whole-second iat rounds past the boundary survived it although the allowance covers the second")
    }
}

func TestJwtTokenValidator_RevocationEpochSkewCoversAnAheadIssuer(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")

    boundary := time.Now().Add(-5 * time.Minute)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", boundary, securitycontract.RevocationOptions{})

    aheadIssuance := boundary.Add(20 * time.Second)
    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": aheadIssuance.Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    unbounded := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: store})
    if _, err := unbounded.Validate(testRuntime(), token); nil != err {
        t.Fatalf("without a skew allowance the ahead-stamped token was already refused, so the bounded half below proves nothing: %v", err)
    }

    bounded := newTestJwtValidator(secret, JwtConfig{RevocationEpochSkew: bound.Of(30 * time.Second), RevocationEpochStore: store})
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

func TestNewJwtTokenValidator_RefusesAnUnlimitedOrTooWideLeewayAndAnUnlimitedSkew(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")

    expectPanicNamingField(t, "JwtConfig.Leeway", func() {
        newTestJwtValidator(secret, JwtConfig{Leeway: bound.UnlimitedDuration()})
    })

    expectPanicNamingField(t, "JwtConfig.Leeway", func() {
        newTestJwtValidator(secret, JwtConfig{Leeway: bound.Of(5*time.Minute + time.Nanosecond)})
    })

    newTestJwtValidator(secret, JwtConfig{Leeway: bound.Of(5 * time.Minute)})

    expectPanicNamingField(t, "JwtConfig.RevocationEpochSkew", func() {
        newTestJwtValidator(secret, JwtConfig{RevocationEpochSkew: bound.UnlimitedDuration()})
    })
}

func TestJwtTokenValidator_TheDefaultLeewayIsFiveSeconds(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")
    token := signJwtHs256(secret, map[string]any{"sub": "user-1", "exp": 1000})

    inside := newTestJwtValidator(secret, JwtConfig{Clock: clock.NewFrozenClock(time.Unix(1005, 0))})
    if _, validateErr := inside.Validate(testRuntime(), token); nil != validateErr {
        t.Fatalf("expected a token 5s past its exp accepted, got %v", validateErr)
    }

    past := newTestJwtValidator(secret, JwtConfig{Clock: clock.NewFrozenClock(time.Unix(1005, 1))})
    if _, validateErr := past.Validate(testRuntime(), token); nil == validateErr || "jwt is expired" != validateErr.Error() {
        t.Fatalf("expected a token just over 5s past its exp refused, got %v", validateErr)
    }

    strict := newTestJwtValidator(secret, JwtConfig{Leeway: strictLeeway, Clock: clock.NewFrozenClock(time.Unix(1000, 1))})
    if _, validateErr := strict.Validate(testRuntime(), token); nil == validateErr || "jwt is expired" != validateErr.Error() {
        t.Fatalf("expected the strict leeway to refuse a token just past its exp, got %v", validateErr)
    }
}

func TestJwtTokenValidator_TheDefaultLeewayDoesNotWidenTheRevocationBoundary(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")
    boundary := time.Unix(1000, 0)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", boundary, securitycontract.RevocationOptions{})

    validator := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: store, Clock: clock.NewFrozenClock(time.Unix(1010, 0))})

    refused := signJwtHs256(secret, map[string]any{"sub": "alice", "iat": 1000, "exp": 5000})
    if _, validateErr := validator.Validate(testRuntime(), refused); nil == validateErr {
        t.Fatalf("expected the token stamped at the boundary refused")
    }

    admitted := signJwtHs256(secret, map[string]any{"sub": "alice", "iat": 1001, "exp": 5000})
    if _, validateErr := validator.Validate(testRuntime(), admitted); nil != validateErr {
        t.Fatalf("expected a token stamped a second past the boundary accepted, got %v", validateErr)
    }
}

/* domain separation: the internal-auth envelope signs under its own typ through the same HS256 primitive, so under a shared or reused secret it must be refused HERE by type, not merely by which claims it happens to carry. */
func TestJwtTokenValidator_RefusesTheInternalAuthEnvelopeType(t *testing.T) {
    secret := []byte("shared-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{SubjectClaim: "app", AllowWithoutExpiry: true})

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
    secret := []byte("secret-padded-to-the-hs256-minimum")

    withoutType := signJwtWithHeader(
        secret,
        map[string]any{"alg": "HS256"},
        map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()},
    )

    validator := newTestJwtValidator(secret, JwtConfig{})
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
    secret := []byte("secret-padded-to-the-hs256-minimum")
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    validator := newTestJwtValidator(secret, JwtConfig{Clock: frozen})

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
    secret := []byte("secret-padded-to-the-hs256-minimum")
    store := &revocationEpochStoreStub{failure: errors.New("redis is down")}

    validator := newTestJwtValidator(secret, JwtConfig{RevocationEpochStore: store})

    tokenString := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": time.Now().Add(-time.Minute).Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    _, validateErr := validator.Validate(testRuntime(), tokenString)
    if nil == validateErr {
        t.Fatal("an unanswerable epoch store must fail the validation closed")
    }

    if false == exception.IsInfrastructureFailure(validateErr) {
        t.Fatal("the epoch store failing to answer is the platform's failure and must carry the infrastructure mark")
    }
}

/* the acceptance above is the only nbf case the suite had, so the whole not-before block could be deleted with it still green — a token that says it is not usable yet would have validated. This is the refusal half: no leeway, activation an hour out. */
func TestJwtTokenValidator_RefusesANotBeforeBeyondTheLeeway(t *testing.T) {
    secret := []byte("super-secret-value-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{Leeway: strictLeeway})

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

/* the revocation boundary reads iat rounded down, so a fractional iat inside the revoked second is refused rather than lifted past the boundary. */
func TestJwtTokenValidator_ReadsAFractionalIssuedAtDownAtTheRevocationBoundary(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", time.Unix(999, 700_000_000), securitycontract.RevocationOptions{})

    validator := newTestJwtValidator(secret, JwtConfig{Clock: clock.NewFrozenClock(time.Unix(1000, 900_000_000)), RevocationEpochStore: store})

    for _, testCase := range []struct {
        issuedAt float64
        accepted bool
    }{
        {999, false},
        {999.5, false},
        {999.7, false},
        {999.9, false},
        {1000, true},
    } {
        token := signJwtHs256(secret, map[string]any{"sub": "alice", "iat": testCase.issuedAt, "exp": 5000})

        _, err := validator.Validate(testRuntime(), token)
        if testCase.accepted != (nil == err) {
            t.Fatalf("iat %v: expected accepted=%v, got %v", testCase.issuedAt, testCase.accepted, err)
        }
    }
}

/* the refusal of a token stamped inside the skew after the epoch names the boundary it applied — the epoch and its skew — and carries the three values a reader needs to see why a token issued after the epoch was still refused. */
func TestJwtTokenValidator_RevocationRefusalInsideTheSkewNamesTheBoundaryAndCarriesItsValues(t *testing.T) {
    secret := []byte("secret-padded-to-the-hs256-minimum")

    boundary := time.Now().Add(-5 * time.Minute).Truncate(time.Second)

    store := &revocationEpochStoreStub{}
    store.RevokeBefore(newTestRuntime(), "alice", boundary, securitycontract.RevocationOptions{})

    insideTheSkew := boundary.Add(20 * time.Second)
    token := signJwtHs256(secret, map[string]any{
        "sub": "alice",
        "iat": insideTheSkew.Unix(),
        "exp": time.Now().Add(time.Hour).Unix(),
    })

    validator := newTestJwtValidator(secret, JwtConfig{RevocationEpochSkew: bound.Of(30 * time.Second), RevocationEpochStore: store})

    _, validateErr := validator.Validate(testRuntime(), token)
    refusal, isRefusal := validateErr.(*exception.Error)
    if false == isRefusal {
        t.Fatalf("expected the token stamped inside the skew to be refused with an exception error, got %v", validateErr)
    }

    if "jwt was not issued after the revocation epoch and its skew" != refusal.Message() {
        t.Fatalf("expected the refusal to name the epoch and its skew, got %q", refusal.Message())
    }

    if insideTheSkew.UTC().Format(time.RFC3339) != refusal.Context()["issuedAt"] {
        t.Fatalf("expected the refusal to carry the issuance, got %v", refusal.Context())
    }

    if boundary.UTC().Format(time.RFC3339) != refusal.Context()["epoch"] {
        t.Fatalf("expected the refusal to carry the epoch, got %v", refusal.Context())
    }

    if "30s" != refusal.Context()["skew"] {
        t.Fatalf("expected the refusal to carry the skew, got %v", refusal.Context())
    }
}

/* the subject refusal answers an absent, a non-string and an empty claim alike, so its sentence says so and names the claim it read. */
func TestJwtTokenValidator_SubjectRefusalNamesTheClaimAndItsThreeShapes(t *testing.T) {
    secret := []byte("super-secret-padded-to-the-hs256-minimum")
    validator := newTestJwtValidator(secret, JwtConfig{AllowWithoutExpiry: true, SubjectClaim: "user"})

    cases := map[string]map[string]any{
        "absent subject":     {"roles": []any{"ROLE_USER"}},
        "empty subject":      {"user": "", "roles": []any{"ROLE_USER"}},
        "non-string subject": {"user": 12345},
    }

    for name, claims := range cases {
        t.Run(name, func(t *testing.T) {
            _, validateErr := validator.Validate(testRuntime(), signJwtHs256(secret, claims))
            refusal, isRefusal := validateErr.(*exception.Error)
            if false == isRefusal {
                t.Fatalf("expected an exception error, got %v", validateErr)
            }

            if "jwt subject claim is absent, not a string or empty" != refusal.Message() {
                t.Fatalf("expected the refusal to name the three shapes, got %q", refusal.Message())
            }

            if "user" != refusal.Context()["claim"] {
                t.Fatalf("expected the refusal to name the subject claim, got %v", refusal.Context())
            }
        })
    }
}

func keySetTestClaims() map[string]any {
    return map[string]any{"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix()}
}

func hs256Key(secret string) securitycontract.JwtKey {
    return securitycontract.JwtKey{Algorithm: JwtAlgorithmHs256, Secret: []byte(secret)}
}

func TestNewJwtTokenValidator_NilKeySet_Refused(t *testing.T) {
    expectPanicNamingTheArgument(t, "keySet", func() {
        NewJwtTokenValidator(nil, JwtConfig{})
    })
}

func TestNewJwtTokenValidator_TypedNilKeySet_Refused(t *testing.T) {
    var unassigned *JwtSecretKeySet

    expectPanicNamingTheArgument(t, "keySet", func() {
        NewJwtTokenValidator(unassigned, JwtConfig{})
    })
}

func TestJwtTokenValidator_HandsTheHeaderKeyIdAndAlgorithmToTheKeySet(t *testing.T) {
    secret := []byte(thirtyTwoByteSecret)
    keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{hs256Key(thirtyTwoByteSecret)}}
    validator := NewJwtTokenValidator(keySet, JwtConfig{})

    withKeyId := signJwtWithHeader(secret, map[string]any{"alg": "HS256", "typ": "JWT", "kid": "key-2026"}, keySetTestClaims())
    if _, validateErr := validator.Validate(testRuntime(), withKeyId); nil != validateErr {
        t.Fatalf("expected the token verified, got %v", validateErr)
    }

    if _, validateErr := validator.Validate(testRuntime(), signJwtHs256(secret, keySetTestClaims())); nil != validateErr {
        t.Fatalf("expected the token without kid verified, got %v", validateErr)
    }

    queries := keySet.queriesSeen()
    expected := []securitycontract.JwtKeyQuery{{KeyId: "key-2026", Algorithm: "HS256"}, {KeyId: "", Algorithm: "HS256"}}
    if 2 != len(queries) || expected[0] != queries[0] || expected[1] != queries[1] {
        t.Fatalf("expected the queries %v, got %v", expected, queries)
    }
}

func TestJwtTokenValidator_HandsTheRequestRuntimeToTheKeySet(t *testing.T) {
    keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{hs256Key(thirtyTwoByteSecret)}}
    runtimeInstance := testRuntime()

    if _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(runtimeInstance, signJwtHs256([]byte(thirtyTwoByteSecret), keySetTestClaims())); nil != validateErr {
        t.Fatalf("expected the token verified, got %v", validateErr)
    }

    if 1 != len(keySet.runtimes) || runtimeInstance != keySet.runtimes[0] {
        t.Fatalf("expected the request's runtime handed to the key set, got %v", keySet.runtimes)
    }
}

func TestJwtTokenValidator_VerifiesAgainstTheSecondAnsweredKey(t *testing.T) {
    keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{hs256Key("the-retired-key-padded-to-the-hs256-minimum"), hs256Key(thirtyTwoByteSecret)}}

    claims, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), signJwtHs256([]byte(thirtyTwoByteSecret), keySetTestClaims()))
    if nil != validateErr || "user-1" != claims.UserIdentifier {
        t.Fatalf("expected the token verified by the second key, got %v %v", claims, validateErr)
    }
}

func TestJwtTokenValidator_SkipsAKeyOfAnotherAlgorithm(t *testing.T) {
    keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{{Algorithm: "HS512", Secret: []byte(thirtyTwoByteSecret)}}}

    _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), signJwtHs256([]byte(thirtyTwoByteSecret), keySetTestClaims()))
    if nil == validateErr || "jwt key is not known" != validateErr.Error() {
        t.Fatalf("expected a key of another algorithm never tried, got %v", validateErr)
    }
}

func TestJwtTokenValidator_UnknownKey_RefusedNotAnInfrastructureFailure(t *testing.T) {
    keySet := &stubJwtKeySet{}
    tokenString := signJwtWithHeader([]byte(thirtyTwoByteSecret), map[string]any{"alg": "HS256", "kid": "key-gone"}, keySetTestClaims())

    _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), tokenString)
    if nil == validateErr || "jwt key is not known" != validateErr.Error() {
        t.Fatalf("expected the key not known, got %v", validateErr)
    }

    if true == exception.IsInfrastructureFailure(validateErr) {
        t.Fatalf("expected an unknown key refused as the caller's, not marked, got %v", validateErr)
    }

    if context := exception.LogContext(validateErr, exception.LogContextOptions{}); "key-gone" != context["keyId"] {
        t.Fatalf("expected the key id named, got %v", context)
    }

    mismatch := &stubJwtKeySet{keys: []securitycontract.JwtKey{hs256Key("another-key-padded-to-the-hs256-minimum")}}
    _, mismatchErr := NewJwtTokenValidator(mismatch, JwtConfig{}).Validate(testRuntime(), tokenString)
    if nil == mismatchErr || "jwt signature mismatch" != mismatchErr.Error() || true == exception.IsInfrastructureFailure(mismatchErr) {
        t.Fatalf("expected a candidate that does not match answered as a mismatch, got %v", mismatchErr)
    }
}

func TestJwtTokenValidator_KeySetOutage_IsAnInfrastructureFailure(t *testing.T) {
    outage := errors.New("the key service is down")
    keySet := &stubJwtKeySet{err: outage, keys: []securitycontract.JwtKey{hs256Key(thirtyTwoByteSecret)}}

    _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), signJwtHs256([]byte(thirtyTwoByteSecret), keySetTestClaims()))
    if nil == validateErr || "jwt verification keys are unavailable" != validateErr.Error() {
        t.Fatalf("expected the key set's failure, got %v", validateErr)
    }

    if false == exception.IsInfrastructureFailure(validateErr) || false == errors.Is(validateErr, outage) {
        t.Fatalf("expected the outage marked as an infrastructure failure carrying its cause, got %v", validateErr)
    }
}

func TestJwtTokenValidator_NineKeysRefused_EightAdmitted(t *testing.T) {
    keys := make([]securitycontract.JwtKey, 0, 9)
    for index := 0; index < 7; index++ {
        keys = append(keys, hs256Key(strings.Repeat(string(rune('a'+index)), 32)))
    }
    keys = append(keys, hs256Key(thirtyTwoByteSecret))
    tokenString := signJwtHs256([]byte(thirtyTwoByteSecret), keySetTestClaims())

    if _, validateErr := NewJwtTokenValidator(&stubJwtKeySet{keys: keys}, JwtConfig{}).Validate(testRuntime(), tokenString); nil != validateErr {
        t.Fatalf("expected eight keys tried, got %v", validateErr)
    }

    nine := append([]securitycontract.JwtKey{hs256Key(strings.Repeat("z", 32))}, keys...)
    _, nineErr := NewJwtTokenValidator(&stubJwtKeySet{keys: nine}, JwtConfig{}).Validate(testRuntime(), tokenString)
    if nil == nineErr || false == exception.IsInfrastructureFailure(nineErr) {
        t.Fatalf("expected nine keys refused as a failure of the set, got %v", nineErr)
    }

    if context := exception.LogContext(nineErr, exception.LogContextOptions{}); 9 != context["keys"] || 8 != context["maximum"] {
        t.Fatalf("expected the count and the cap named, got %v", context)
    }

    if _, raisedErr := NewJwtTokenValidator(&stubJwtKeySet{keys: nine}, JwtConfig{MaxCandidateKeys: bound.Of(9)}).Validate(testRuntime(), tokenString); nil != raisedErr {
        t.Fatalf("expected a raised cap to try nine keys, got %v", raisedErr)
    }
}

func TestJwtTokenValidator_MaxCandidateKeys_UnlimitedRefused(t *testing.T) {
    keySet := &stubJwtKeySet{}

    expectPanicNamingField(t, "JwtConfig.MaxCandidateKeys", func() {
        NewJwtTokenValidator(keySet, JwtConfig{MaxCandidateKeys: bound.UnlimitedCount()})
    })

    expectPanicNamingField(t, "JwtConfig.MaxCandidateKeys", func() {
        NewJwtTokenValidator(keySet, JwtConfig{MaxCandidateKeys: bound.Of(0)})
    })

    NewJwtTokenValidator(keySet, JwtConfig{MaxCandidateKeys: bound.Of(1)})
}

func TestJwtTokenValidator_UnsupportedAlgorithm_KeySetNotAsked(t *testing.T) {
    keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{{Algorithm: "none"}, {Algorithm: "RS256", Secret: []byte(thirtyTwoByteSecret)}}}
    validator := NewJwtTokenValidator(keySet, JwtConfig{})

    for _, algorithm := range []string{"none", "RS256"} {
        _, validateErr := validator.Validate(testRuntime(), signJwtWithAlg([]byte(thirtyTwoByteSecret), algorithm, keySetTestClaims()))
        if nil == validateErr || "jwt algorithm is not supported" != validateErr.Error() {
            t.Fatalf("%s: expected the algorithm refused, got %v", algorithm, validateErr)
        }
    }

    if 0 != len(keySet.queriesSeen()) {
        t.Fatalf("expected the key set never asked for an algorithm the validator does not implement, got %v", keySet.queriesSeen())
    }
}

func TestJwtTokenValidator_RefusedType_KeySetNotAsked(t *testing.T) {
    keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{hs256Key(thirtyTwoByteSecret)}}
    tokenString := signJwtWithHeader([]byte(thirtyTwoByteSecret), map[string]any{"alg": "HS256", "typ": "melody-hmac"}, keySetTestClaims())

    _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), tokenString)
    if nil == validateErr || "jwt type is not accepted" != validateErr.Error() {
        t.Fatalf("expected the type refused, got %v", validateErr)
    }

    if 0 != len(keySet.queriesSeen()) {
        t.Fatalf("expected the key set never asked for a refused type, got %v", keySet.queriesSeen())
    }
}

func TestJwtTokenValidator_AShortKeyOfACustomSetVerifiesNothing(t *testing.T) {
    for _, secret := range []string{"", "short-custom-secret"} {
        keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{{KeyId: "key-custom", Algorithm: JwtAlgorithmHs256, Secret: []byte(secret)}}}

        _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), signJwtHs256([]byte(secret), keySetTestClaims()))
        if nil == validateErr || false == exception.IsInfrastructureFailure(validateErr) {
            t.Fatalf("secret %q: expected the short key refused as a failure of the set, got %v", secret, validateErr)
        }

        context := exception.LogContext(validateErr, exception.LogContextOptions{})
        if "keySet" != context["argument"] || "key-custom" != context["keyId"] || len(secret) != context["secretLength"] || 32 != context["minimumLength"] {
            t.Fatalf("secret %q: expected the short key named by argument, key id and length, got %v", secret, context)
        }
    }

    keySet := &stubJwtKeySet{keys: []securitycontract.JwtKey{hs256Key(thirtyTwoByteSecret)}}
    if _, validateErr := NewJwtTokenValidator(keySet, JwtConfig{}).Validate(testRuntime(), signJwtHs256([]byte(thirtyTwoByteSecret), keySetTestClaims())); nil != validateErr {
        t.Fatalf("expected a 32-byte key to verify, got %v", validateErr)
    }
}
