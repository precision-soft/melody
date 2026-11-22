package security

import (
    "strings"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/logging"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

const bearerPrefix = "Bearer "

/* BearerTokenSourceConfig is what a bearer token source is built from beside its validator; the zero value is a source without an enricher. */
type BearerTokenSourceConfig struct {
    /* Enricher rewrites the claims after validation; a typed nil is refused. */
    Enricher securitycontract.TokenEnricher
}

/* NewBearerTokenSource resolves a bearer token through the validator. A nil validator and a typed-nil enricher are refused. */
func NewBearerTokenSource(validator securitycontract.TokenValidator, config BearerTokenSourceConfig) *BearerTokenSource {
    if true == internal.IsNilInterface(validator) {
        exception.Panic(exception.NewError("token validator is nil", nil, nil))
    }

    refuseTypedNilField("token enricher is a typed nil", "BearerTokenSourceConfig.Enricher", "NewBearerTokenSource", config.Enricher)

    return &BearerTokenSource{
        validator: validator,
        enricher:  config.Enricher,
    }
}

type BearerTokenSource struct {
    validator securitycontract.TokenValidator
    enricher  securitycontract.TokenEnricher
}

func (instance *BearerTokenSource) Name() string {
    return "bearerToken"
}

/* Stateless: the bearer token is a header the caller writes on every request. */
func (instance *BearerTokenSource) Stateless() bool {
    return true
}

func (instance *BearerTokenSource) Resolve(
    runtimeInstance containercontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    tokenString, extracted := extractBearerToken(request.Header("Authorization"))
    if false == extracted {
        return NewAnonymousToken(), nil
    }

    claims, validateErr := instance.validator.Validate(runtimeInstance, tokenString)
    if nil != validateErr {
        /* a store that cannot answer is the unavailable token, recorded once by the resolution listener; a credential that failed its checks is anonymous */
        if true == exception.IsInfrastructureFailure(validateErr) {
            return NewUnavailableToken(validateErr), nil
        }

        logger := logging.LoggerFromRuntime(runtimeInstance)
        if nil != logger {
            logger.Info("bearer token rejected", exception.LogContext(validateErr, exception.LogContextConfig{}))
        }

        return NewAnonymousToken(), nil
    }

    if false == internal.IsNilInterface(instance.enricher) {
        enrichedClaims, enrichErr := instance.enricher.Enrich(runtimeInstance, claims)
        if nil != enrichErr {
            if true == exception.IsInfrastructureFailure(enrichErr) {
                return NewUnavailableToken(enrichErr), nil
            }

            logger := logging.LoggerFromRuntime(runtimeInstance)
            if nil != logger {
                logger.Info("bearer token enrichment failed", exception.LogContext(enrichErr, exception.LogContextConfig{}))
            }

            return NewAnonymousToken(), nil
        }

        claims = enrichedClaims
    }

    return NewAuthenticatedTokenFromClaims(claims), nil
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
