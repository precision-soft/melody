package security

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "encoding/json"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const (
    jwtAlgorithmHs256 = "HS256"
    jwtDefaultSubject = "sub"
    jwtDefaultRoles   = "roles"
)

func NewJwtTokenValidator(config JwtConfig) *JwtTokenValidator {
    if 0 == len(config.Secret) {
        exception.Panic(exception.NewError("jwt secret is empty", nil, nil))
    }

    subjectClaim := config.SubjectClaim
    if "" == subjectClaim {
        subjectClaim = jwtDefaultSubject
    }

    rolesClaim := config.RolesClaim
    if "" == rolesClaim {
        rolesClaim = jwtDefaultRoles
    }

    return &JwtTokenValidator{
        secret:        config.Secret,
        subjectClaim:  subjectClaim,
        rolesClaim:    rolesClaim,
        leeway:        config.Leeway,
        requireExpiry: config.RequireExpiry,
        audience:      config.Audience,
        issuer:        config.Issuer,
    }
}

type JwtConfig struct {
    Secret        []byte
    SubjectClaim  string
    RolesClaim    string
    Leeway        time.Duration
    RequireExpiry bool
    /** When set, the token must carry a matching iss claim. */
    Issuer string
    /** When set, the token must list this value in its aud claim (string or array). */
    Audience string
}

type JwtTokenValidator struct {
    secret        []byte
    subjectClaim  string
    rolesClaim    string
    leeway        time.Duration
    requireExpiry bool
    audience      string
    issuer        string
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

    expiryErr := instance.verifyTimeClaims(rawClaims, time.Now())
    if nil != expiryErr {
        return securitycontract.Claims{}, expiryErr
    }

    registeredErr := instance.verifyRegisteredClaims(rawClaims)
    if nil != registeredErr {
        return securitycontract.Claims{}, registeredErr
    }

    return securitycontract.Claims{
        UserIdentifier: stringClaim(rawClaims, instance.subjectClaim),
        Roles:          stringSliceClaim(rawClaims, instance.rolesClaim),
    }, nil
}

func (instance *JwtTokenValidator) verifyTimeClaims(rawClaims map[string]any, now time.Time) error {
    expiry, hasExpiry, expiryValid := numericClaim(rawClaims, "exp")
    if true == hasExpiry && false == expiryValid {
        return exception.NewError("jwt exp claim is malformed", nil, nil)
    }

    if false == hasExpiry && true == instance.requireExpiry {
        return exception.NewError("jwt is missing the required exp claim", nil, nil)
    }

    if true == expiryValid {
        deadline := time.Unix(expiry, 0).Add(instance.leeway)
        if true == now.After(deadline) {
            return exception.NewError("jwt is expired", nil, nil)
        }
    }

    notBefore, hasNotBefore, notBeforeValid := numericClaim(rawClaims, "nbf")
    if true == hasNotBefore && false == notBeforeValid {
        return exception.NewError("jwt nbf claim is malformed", nil, nil)
    }

    if true == notBeforeValid {
        activation := time.Unix(notBefore, 0).Add(-instance.leeway)
        if true == now.Before(activation) {
            return exception.NewError("jwt is not yet valid", nil, nil)
        }
    }

    issuedAt, hasIssuedAt, issuedAtValid := numericClaim(rawClaims, "iat")
    if true == hasIssuedAt && false == issuedAtValid {
        return exception.NewError("jwt iat claim is malformed", nil, nil)
    }

    if true == issuedAtValid {
        issued := time.Unix(issuedAt, 0).Add(-instance.leeway)
        if true == now.Before(issued) {
            return exception.NewError("jwt is issued in the future", nil, nil)
        }
    }

    return nil
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

    return int64(floatValue), true, true
}

var _ securitycontract.TokenValidator = (*JwtTokenValidator)(nil)
