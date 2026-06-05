package security

import (
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

const bearerPrefix = "Bearer "

func NewBearerTokenSource(validator securitycontract.TokenValidator) *BearerTokenSource {
    if true == internal.IsNilInterface(validator) {
        exception.Panic(exception.NewError("token validator is nil", nil, nil))
    }

    return &BearerTokenSource{
        validator: validator,
    }
}

type BearerTokenSource struct {
    validator securitycontract.TokenValidator
}

func (instance *BearerTokenSource) Name() string {
    return "bearerToken"
}

func (instance *BearerTokenSource) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    tokenString, extracted := extractBearerToken(request.Header("Authorization"))
    if false == extracted {
        return NewAnonymousToken(), nil
    }

    claims, validateErr := instance.validator.Validate(runtimeInstance, tokenString)
    if nil != validateErr {
        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Info("bearer token rejected", exception.LogContext(validateErr))
        }

        return NewAnonymousToken(), nil
    }

    return NewAuthenticatedToken(claims.UserIdentifier, claims.Roles), nil
}

func extractBearerToken(headerValue string) (string, bool) {
    if "" == headerValue {
        return "", false
    }

    if len(headerValue) < len(bearerPrefix) || false == strings.EqualFold(headerValue[:len(bearerPrefix)], bearerPrefix) {
        return "", false
    }

    tokenString := strings.TrimSpace(headerValue[len(bearerPrefix):])
    if "" == tokenString {
        return "", false
    }

    return tokenString, true
}

var _ securitycontract.TokenSource = (*BearerTokenSource)(nil)
