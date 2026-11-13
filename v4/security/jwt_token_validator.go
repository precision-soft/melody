package security

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "math"
    "strings"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

const (
    jwtAlgorithmHs256 = "HS256"
    jwtDefaultSubject = "sub"
    jwtDefaultRoles   = "roles"
    jwtMaxNumericDate = 253402300799
)

/* jwtNumericDateResolution is the resolution the future checks grant a NumericDate: issuers write whole seconds, their instant truncated down, so a validator whose clock trails the issuer's by a millisecond across a second boundary reads a fresh token as issued in its future */
const jwtNumericDateResolution = time.Second

var jwtLeewayRule = bound.Rule[time.Duration]{
    Name:    "JwtConfig.Leeway",
    Default: defaultLeeway,
    Maximum: maximumLeeway,
}

/* a negative skew is refused: it would move the revocation boundary backwards, and tokens issued before the revocation would verify again */
var jwtRevocationEpochSkewRule = bound.Rule[time.Duration]{
    Name: "JwtConfig.RevocationEpochSkew",
}

/* NewJwtTokenValidator builds the validator, panicking on an empty secret, a secret shorter than MinimumSecretLength, a typed-nil revocation epoch store and a leeway or skew its rule refuses, each by the field's name. */
func NewJwtTokenValidator(config JwtConfig) *JwtTokenValidator {
    if 0 == len(config.Secret) {
        exception.Panic(exception.NewError("jwt secret is empty", exceptioncontract.Context{"field": "JwtConfig.Secret"}, nil))
    }

    minimumSecretLength := resolveMinimumSecretLength(config.MinimumSecretLength, "JwtConfig.MinimumSecretLength")
    if minimumSecretLength > len(config.Secret) {
        exception.Panic(exception.NewError(
            "jwt secret is shorter than its minimum, the 32 bytes RFC 7518 asks of an HS256 key unless MinimumSecretLength raises it",
            shortSecretContext("JwtConfig.Secret", len(config.Secret), minimumSecretLength),
            nil,
        ))
    }

    if nil != config.RevocationEpochStore && true == internal.IsNilInterface(config.RevocationEpochStore) {
        exception.Panic(exception.NewError(
            "jwt revocation epoch store is a typed nil",
            exceptioncontract.Context{"field": "JwtConfig.RevocationEpochStore"},
            nil,
        ))
    }

    revocationEpochSkew, revocationEpochSkewErr := bound.Resolve(config.RevocationEpochSkew, jwtRevocationEpochSkewRule)
    if nil != revocationEpochSkewErr {
        exception.Panic(exception.FromError(revocationEpochSkewErr))
    }

    leeway, leewayErr := bound.Resolve(config.Leeway, jwtLeewayRule)
    if nil != leewayErr {
        exception.Panic(exception.FromError(leewayErr))
    }

    /* the boundary is compared with iat, so a token claiming a future iat would outlive every boundary drawn before that instant */
    rejectFutureIssuedAt := config.RejectFutureIssuedAt || nil != config.RevocationEpochStore

    /* a token an ahead issuer stamped before a revocation carries an iat past the boundary, so under a store the future check tolerates only what the boundary was widened by */
    futureIssuedAtTolerance := leeway.Value
    if nil != config.RevocationEpochStore {
        futureIssuedAtTolerance = revocationEpochSkew.Value
    }

    subjectClaim := config.SubjectClaim
    if "" == subjectClaim {
        subjectClaim = jwtDefaultSubject
    }

    rolesClaim := config.RolesClaim
    if "" == rolesClaim {
        rolesClaim = jwtDefaultRoles
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    return &JwtTokenValidator{
        /* the secret is copied on the way in, so the caller's slice cannot change it under a later signature check */
        secret:                  append([]byte{}, config.Secret...),
        clock:                   clockInstance,
        subjectClaim:            subjectClaim,
        rolesClaim:              rolesClaim,
        scopeClaim:              config.ScopeClaim,
        deviceClaim:             config.DeviceClaim,
        revocationEpochSkew:     revocationEpochSkew.Value,
        leeway:                  leeway.Value,
        futureIssuedAtTolerance: futureIssuedAtTolerance,
        allowWithoutExpiry:      config.AllowWithoutExpiry,
        rejectFutureIssuedAt:    rejectFutureIssuedAt,
        audience:                config.Audience,
        issuer:                  config.Issuer,
        epochStore:              config.RevocationEpochStore,
    }
}

type JwtConfig struct {
    Secret []byte

    /* MinimumSecretLength is the fewest bytes Secret may hold: 32 by default, the RFC 7518 floor for an HS256 key, at least 32, unlimited refused; a shorter secret is refused naming JwtConfig.Secret. */
    MinimumSecretLength bound.Count

    SubjectClaim string
    RolesClaim   string
    ScopeClaim   string
    DeviceClaim  string

    /* Clock is the clock the time claims are verified against; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock

    /* RevocationEpochStore, when set, reads a revocation boundary per user and device and refuses a token issued before it; it forces RejectFutureIssuedAt, since a token claiming a future iat would outlive every boundary drawn before that instant. nil reads no boundary; a typed nil is refused. */
    RevocationEpochStore securitycontract.RevocationEpochStore

    /* RevocationEpochSkew widens the boundary by the clock skew between instances, and is what the future check of iat tolerates under a RevocationEpochStore, one second more for the claim's resolution: 0 when left out, at least 0, unlimited refused. */
    RevocationEpochSkew bound.Duration

    /* Leeway widens the exp, nbf and iat checks by this much, and the future checks of nbf and iat tolerate one second more, the resolution of a whole-second claim: 5s when left out, bound.Of(time.Duration(0)) for strict, at most 5m, unlimited refused. Neither the revocation boundary nor, under a RevocationEpochStore, the future check of iat is widened by it, since a token an ahead issuer stamped before a revocation would outlive it; RevocationEpochSkew decides those. */
    Leeway               bound.Duration
    AllowWithoutExpiry   bool
    RejectFutureIssuedAt bool
    Issuer               string
    Audience             string
}

type JwtTokenValidator struct {
    secret                  []byte
    clock                   clockcontract.Clock
    subjectClaim            string
    rolesClaim              string
    scopeClaim              string
    deviceClaim             string
    revocationEpochSkew     time.Duration
    leeway                  time.Duration
    futureIssuedAtTolerance time.Duration
    allowWithoutExpiry      bool
    rejectFutureIssuedAt    bool
    audience                string
    issuer                  string
    epochStore              securitycontract.RevocationEpochStore
}

func (instance *JwtTokenValidator) Validate(
    runtimeInstance runtimecontract.Runtime,
    tokenString string,
) (securitycontract.Claims, error) {
    parts := strings.Split(tokenString, ".")
    if 3 != len(parts) {
        return securitycontract.Claims{}, exception.NewError("jwt has an invalid structure", nil, nil)
    }

    headerBytes, headerErr := base64.RawURLEncoding.DecodeString(parts[0])
    if nil != headerErr {
        return securitycontract.Claims{}, exception.NewError("jwt header is not valid base64url", nil, headerErr)
    }

    var header struct {
        Algorithm string `json:"alg"`
        Type      string `json:"typ"`
    }
    if unmarshalErr := json.Unmarshal(headerBytes, &header); nil != unmarshalErr {
        return securitycontract.Claims{}, exception.NewError("jwt header is not valid json", nil, unmarshalErr)
    }

    if jwtAlgorithmHs256 != header.Algorithm {
        return securitycontract.Claims{}, exception.NewError(
            "jwt algorithm is not supported",
            map[string]any{"algorithm": internal.BoundDiagnosticText(header.Algorithm)},
            nil,
        )
    }

    /* domain separation from every other HS256 credential melody mints, the internal-auth envelope above all: an absent typ is accepted, as RFC 7519 makes it optional, and "JWT" is compared case-insensitively; any other typ is refused, so a credential of another type cannot be replayed here under a shared secret */
    if "" != header.Type && false == strings.EqualFold("JWT", header.Type) {
        return securitycontract.Claims{}, exception.NewError(
            "jwt type is not accepted",
            map[string]any{"type": internal.BoundDiagnosticText(header.Type)},
            nil,
        )
    }

    signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[2])
    if nil != signatureErr {
        return securitycontract.Claims{}, exception.NewError("jwt signature is not valid base64url", nil, signatureErr)
    }

    expectedSignature := signHmacSha256(parts[0]+"."+parts[1], instance.secret)
    if false == hmac.Equal(signature, expectedSignature) {
        return securitycontract.Claims{}, exception.NewError("jwt signature mismatch", nil, nil)
    }

    payloadBytes, payloadErr := base64.RawURLEncoding.DecodeString(parts[1])
    if nil != payloadErr {
        return securitycontract.Claims{}, exception.NewError("jwt payload is not valid base64url", nil, payloadErr)
    }

    var rawClaims map[string]any
    if unmarshalErr := json.Unmarshal(payloadBytes, &rawClaims); nil != unmarshalErr {
        return securitycontract.Claims{}, exception.NewError("jwt payload is not valid json", nil, unmarshalErr)
    }

    issuedAt, expiryErr := instance.verifyTimeClaims(rawClaims, instance.clock.Now())
    if nil != expiryErr {
        return securitycontract.Claims{}, expiryErr
    }

    registeredErr := instance.verifyRegisteredClaims(rawClaims)
    if nil != registeredErr {
        return securitycontract.Claims{}, registeredErr
    }

    subject := stringClaim(rawClaims, instance.subjectClaim)
    if "" == subject {
        return securitycontract.Claims{}, exception.NewError(
            "jwt subject claim is absent, not a string or empty",
            map[string]any{"claim": instance.subjectClaim},
            nil,
        )
    }

    claims := securitycontract.Claims{
        UserIdentifier: subject,
        Roles:          stringSliceClaim(rawClaims, instance.rolesClaim),
        IssuedAt:       issuedAt,
    }

    if "" != instance.scopeClaim {
        claims.Scope = mapClaim(rawClaims, instance.scopeClaim)
    }

    if "" != instance.deviceClaim {
        claims.DeviceIdentifier = stringClaim(rawClaims, instance.deviceClaim)
    }

    if revocationErr := instance.verifyRevocationEpoch(runtimeInstance, claims); nil != revocationErr {
        return securitycontract.Claims{}, revocationErr
    }

    return claims, nil
}

func (instance *JwtTokenValidator) verifyRevocationEpoch(
    runtimeInstance runtimecontract.Runtime,
    claims securitycontract.Claims,
) error {
    if nil == instance.epochStore {
        return nil
    }

    if true == claims.IssuedAt.IsZero() {
        return exception.NewError("jwt has no iat claim and a revocation epoch is configured", nil, nil)
    }

    epoch, epochErr := instance.epochStore.RevocationEpoch(runtimeInstance, claims.UserIdentifier, claims.DeviceIdentifier)
    if nil != epochErr {
        /* the store failing to answer is the platform's failure, marked so the bearer source logs it as an incident; the request fails closed either way */
        return exception.NewError(
            "jwt revocation epoch is unavailable",
            map[string]any{"user": claims.UserIdentifier},
            exception.MarkInfrastructureFailure(epochErr),
        )
    }

    if true == epoch.IsZero() {
        return nil
    }

    boundary := epoch.Add(instance.revocationEpochSkew)
    if false == claims.IssuedAt.After(boundary) {
        /* the boundary is the epoch pushed by the skew: a token stamped inside the skew after the epoch is refused as well, as one an issuer running ahead may have stamped before it */
        return exception.NewError(
            "jwt was not issued after the revocation epoch and its skew",
            map[string]any{
                "issuedAt": claims.IssuedAt.UTC().Format(time.RFC3339),
                "epoch":    epoch.UTC().Format(time.RFC3339),
                "skew":     instance.revocationEpochSkew.String(),
            },
            nil,
        )
    }

    return nil
}

func mapClaim(rawClaims map[string]any, name string) map[string]any {
    value, exists := rawClaims[name]
    if false == exists {
        return nil
    }

    mapValue, isMap := value.(map[string]any)
    if false == isMap {
        return nil
    }

    return mapValue
}

func (instance *JwtTokenValidator) verifyTimeClaims(rawClaims map[string]any, now time.Time) (time.Time, error) {
    expiry, hasExpiry, expiryValid := numericClaim(rawClaims, "exp")
    if true == hasExpiry && false == expiryValid {
        return time.Time{}, exception.NewError("jwt exp claim is malformed", nil, nil)
    }

    if false == hasExpiry && false == instance.allowWithoutExpiry {
        return time.Time{}, exception.NewError("jwt is missing the required exp claim", nil, nil)
    }

    if true == expiryValid {
        deadline := time.Unix(expiry, 0).Add(instance.leeway)
        if true == now.After(deadline) {
            return time.Time{}, exception.NewError("jwt is expired", nil, nil)
        }
    }

    notBefore, hasNotBefore, notBeforeValid := numericClaim(rawClaims, "nbf")
    if true == hasNotBefore && false == notBeforeValid {
        return time.Time{}, exception.NewError("jwt nbf claim is malformed", nil, nil)
    }

    if true == notBeforeValid {
        activation := time.Unix(notBefore, 0).Add(-instance.leeway).Add(-jwtNumericDateResolution)
        if true == now.Before(activation) {
            return time.Time{}, exception.NewError("jwt is not yet valid", nil, nil)
        }
    }

    /* every time claim is read rounded down: the future checks of nbf and iat tolerate the claim's one-second resolution on top of the leeway, the expiry is not widened by it, and the revocation boundary compares the floor, so a token issued inside the revoked second never lands past it */
    issuedAt, hasIssuedAt, issuedAtValid := numericClaim(rawClaims, "iat")
    if true == hasIssuedAt && false == issuedAtValid {
        return time.Time{}, exception.NewError("jwt iat claim is malformed", nil, nil)
    }

    if false == issuedAtValid {
        return time.Time{}, nil
    }

    if true == instance.rejectFutureIssuedAt {
        issued := time.Unix(issuedAt, 0).Add(-instance.futureIssuedAtTolerance).Add(-jwtNumericDateResolution)
        if true == now.Before(issued) {
            return time.Time{}, exception.NewError("jwt is issued in the future", nil, nil)
        }
    }

    return time.Unix(issuedAt, 0).UTC(), nil
}

func (instance *JwtTokenValidator) verifyRegisteredClaims(rawClaims map[string]any) error {
    if "" != instance.issuer && instance.issuer != stringClaim(rawClaims, "iss") {
        return exception.NewError("jwt issuer is not accepted", nil, nil)
    }

    if "" != instance.audience && false == audienceContains(rawClaims, instance.audience) {
        return exception.NewError("jwt audience is not accepted", nil, nil)
    }

    return nil
}

func audienceContains(rawClaims map[string]any, expected string) bool {
    value, exists := rawClaims["aud"]
    if false == exists {
        return false
    }

    switch typed := value.(type) {
    case string:
        return expected == typed
    case []any:
        for _, entry := range typed {
            stringEntry, isString := entry.(string)
            if true == isString && expected == stringEntry {
                return true
            }
        }
        return false
    default:
        return false
    }
}

func signHmacSha256(signingInput string, secret []byte) []byte {
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(signingInput))
    return mac.Sum(nil)
}

func stringClaim(rawClaims map[string]any, name string) string {
    value, exists := rawClaims[name]
    if false == exists {
        return ""
    }

    stringValue, isString := value.(string)
    if false == isString {
        return ""
    }

    return stringValue
}

func stringSliceClaim(rawClaims map[string]any, name string) []string {
    value, exists := rawClaims[name]
    if false == exists {
        return []string{}
    }

    switch typed := value.(type) {
    case []any:
        roles := make([]string, 0, len(typed))
        for _, entry := range typed {
            stringEntry, isString := entry.(string)
            if true == isString {
                roles = append(roles, stringEntry)
            }
        }
        return roles
    case string:
        return strings.Fields(typed)
    default:
        return []string{}
    }
}

func numericClaim(rawClaims map[string]any, name string) (int64, bool, bool) {
    value, exists := rawClaims[name]
    if false == exists {
        return 0, false, false
    }

    floatValue, isFloat := value.(float64)
    if false == isFloat {
        return 0, true, false
    }

    if true == math.IsNaN(floatValue) || true == math.IsInf(floatValue, 0) {
        return 0, true, false
    }

    if floatValue < 0 || floatValue > jwtMaxNumericDate {
        return 0, true, false
    }

    return int64(math.Floor(floatValue)), true, true
}

var _ securitycontract.TokenValidator = (*JwtTokenValidator)(nil)
