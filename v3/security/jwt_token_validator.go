package security

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "math"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const (
    jwtAlgorithmHs256 = "HS256"
    jwtDefaultSubject = "sub"
    jwtDefaultRoles   = "roles"
    jwtMaxNumericDate = 253402300799
)

func NewJwtTokenValidator(config JwtConfig) *JwtTokenValidator {
    return newJwtTokenValidator(config, nil)
}

func NewJwtTokenValidatorWithRevocationEpoch(
    config JwtConfig,
    epochStore securitycontract.RevocationEpochStore,
) *JwtTokenValidator {
    if true == internal.IsNilInterface(epochStore) {
        exception.Panic(exception.NewError("jwt revocation epoch store is nil", nil, nil))
    }

    config.RejectFutureIssuedAt = true

    return newJwtTokenValidator(config, epochStore)
}

func newJwtTokenValidator(config JwtConfig, epochStore securitycontract.RevocationEpochStore) *JwtTokenValidator {
    if 0 == len(config.Secret) {
        exception.Panic(exception.NewError("jwt secret is empty", nil, nil))
    }

    /* @important a negative skew is refused rather than carried: RevocationEpochSkew widens a boundary to absorb clock skew, and a negative value moves the boundary BACKWARDS instead — tokens issued before the revocation verify again, a revocation bypass reachable from a config typo. */
    if 0 > config.RevocationEpochSkew {
        exception.Panic(exception.NewError(
            "jwt revocation epoch skew may not be negative",
            map[string]any{"skew": config.RevocationEpochSkew.String()},
            nil,
        ))
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
        /* the secret is copied on the way in, the way StaticHmacSecretProvider copies on ingest: retained by reference, the caller's slice stayed mutable under every later signature check. */
        secret:               append([]byte{}, config.Secret...),
        clock:                clockInstance,
        subjectClaim:         subjectClaim,
        rolesClaim:           rolesClaim,
        scopeClaim:           config.ScopeClaim,
        deviceClaim:          config.DeviceClaim,
        revocationEpochSkew:  config.RevocationEpochSkew,
        leeway:               config.Leeway,
        allowWithoutExpiry:   config.AllowWithoutExpiry,
        rejectFutureIssuedAt: config.RejectFutureIssuedAt,
        audience:             config.Audience,
        issuer:               config.Issuer,
        epochStore:           epochStore,
    }
}

type JwtConfig struct {
    Secret       []byte
    SubjectClaim string
    RolesClaim   string
    ScopeClaim   string
    DeviceClaim  string

    /* Clock is the clock the time claims are verified against; nil uses the system clock. Inject a frozen clock for deterministic tests. */
    Clock clockcontract.Clock

    RevocationEpochSkew time.Duration

    Leeway               time.Duration
    AllowWithoutExpiry   bool
    RejectFutureIssuedAt bool
    Issuer               string
    Audience             string
}

type JwtTokenValidator struct {
    secret               []byte
    clock                clockcontract.Clock
    subjectClaim         string
    rolesClaim           string
    scopeClaim           string
    deviceClaim          string
    revocationEpochSkew  time.Duration
    leeway               time.Duration
    allowWithoutExpiry   bool
    rejectFutureIssuedAt bool
    audience             string
    issuer               string
    epochStore           securitycontract.RevocationEpochStore
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
            map[string]any{"algorithm": header.Algorithm},
            nil,
        )
    }

    /* @important domain separation from every other HS256 credential melody mints — the internal-auth envelope above all, which is byte-identical in shape and signs through the same primitive under its own "melody-internal" type. An absent typ is accepted (RFC 7519 makes it optional) and "JWT" is compared case-insensitively as §5.1 recommends; anything else is refused, so a credential of another type verifying under a shared or reused secret cannot be replayed here even with SubjectClaim re-pointed at one of its fields. */
    if "" != header.Type && false == strings.EqualFold("JWT", header.Type) {
        return securitycontract.Claims{}, exception.NewError(
            "jwt type is not accepted",
            map[string]any{"type": header.Type},
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
        return securitycontract.Claims{}, exception.NewError("jwt subject is empty", nil, nil)
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
        /* the store failing to answer is the platform's failure, not the credential's: the mark is what lets the bearer source log it as the incident it is instead of the routine Info a bad token earns, while the request still fails closed either way. */
        return exception.NewError(
            "jwt revocation epoch is unavailable",
            map[string]any{"user": claims.UserIdentifier},
            markInfrastructureFailure(epochErr),
        )
    }

    if true == epoch.IsZero() {
        return nil
    }

    if false == claims.IssuedAt.After(epoch.Add(instance.revocationEpochSkew)) {
        return exception.NewError("jwt was issued before the revocation epoch", nil, nil)
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
    expiry, hasExpiry, expiryValid := numericClaim(rawClaims, "exp", false)
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

    notBefore, hasNotBefore, notBeforeValid := numericClaim(rawClaims, "nbf", true)
    if true == hasNotBefore && false == notBeforeValid {
        return time.Time{}, exception.NewError("jwt nbf claim is malformed", nil, nil)
    }

    if true == notBeforeValid {
        activation := time.Unix(notBefore, 0).Add(-instance.leeway)
        if true == now.Before(activation) {
            return time.Time{}, exception.NewError("jwt is not yet valid", nil, nil)
        }
    }

    issuedAt, hasIssuedAt, issuedAtValid := numericClaim(rawClaims, "iat", true)
    if true == hasIssuedAt && false == issuedAtValid {
        return time.Time{}, exception.NewError("jwt iat claim is malformed", nil, nil)
    }

    if false == issuedAtValid {
        return time.Time{}, nil
    }

    if true == instance.rejectFutureIssuedAt {
        issued := time.Unix(issuedAt, 0).Add(-instance.leeway)
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

func numericClaim(rawClaims map[string]any, name string, roundUp bool) (int64, bool, bool) {
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

    if true == roundUp {
        return int64(math.Ceil(floatValue)), true, true
    }

    return int64(math.Floor(floatValue)), true, true
}

var _ securitycontract.TokenValidator = (*JwtTokenValidator)(nil)
