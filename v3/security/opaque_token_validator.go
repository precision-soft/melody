package security

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewOpaqueTokenValidator(store securitycontract.TokenStore) *OpaqueTokenValidator {
    if true == internal.IsNilInterface(store) {
        exception.Panic(exception.NewError("token store is nil", nil, nil))
    }

    return &OpaqueTokenValidator{
        store: store,
    }
}

type OpaqueTokenValidator struct {
    store securitycontract.TokenStore
}

func (instance *OpaqueTokenValidator) Validate(
    runtimeInstance runtimecontract.Runtime,
    tokenString string,
) (securitycontract.Claims, error) {
    claims, found, lookupErr := instance.store.Lookup(runtimeInstance, tokenString)
    if nil != lookupErr {
        return securitycontract.Claims{}, lookupErr
    }

    if false == found {
        return securitycontract.Claims{}, exception.NewError("opaque token was not found", nil, nil)
    }

    if "" == claims.UserIdentifier {
        return securitycontract.Claims{}, exception.NewError("opaque token has an empty subject", nil, nil)
    }

    return claims, nil
}

var _ securitycontract.TokenValidator = (*OpaqueTokenValidator)(nil)
