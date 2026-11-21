package contract

import (
    "net/http"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
)

/* PresignExpiryRule is the reading every backend gives PresignConfig.Expiry; its ceiling is the one of an S3 signature, so a link that signs on one backend signs on every other. */
func PresignExpiryRule() bound.Rule[time.Duration] {
    return bound.Rule[time.Duration]{
        Name:    "PresignConfig.Expiry",
        Default: 15 * time.Minute,
        Minimum: bound.TtlMinimum,
        Maximum: 7 * 24 * time.Hour,
    }
}

/* ResolvePresignExpiry decides the expiry against PresignExpiryRule; a refusal names the field. */
func ResolvePresignExpiry(expiry bound.Duration) (time.Duration, error) {
    resolved, err := bound.Resolve(expiry, PresignExpiryRule())
    if nil != err {
        return 0, err
    }

    return resolved.Value, nil
}

/* ResolvePresignMethod answers the method to sign: http.MethodGet for an empty one, http.MethodGet and http.MethodPut as they are, compared exactly, and an error carrying ErrPresignUnsupported and the method for any other. */
func ResolvePresignMethod(method string) (string, error) {
    switch method {
    case "":
        return http.MethodGet, nil
    case http.MethodGet, http.MethodPut:
        return method, nil
    }

    return "", exception.NewError(
        "the presign method is not supported",
        exceptioncontract.Context{
            "field":  "PresignConfig.Method",
            "method": method,
        },
        ErrPresignUnsupported,
    )
}
