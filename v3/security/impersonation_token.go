package security

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* NewImpersonationToken builds a token whose visible principal is the impersonated user (it drives Identifier/Roles/Scope/Attributes/IsAuthenticated) while the impersonator (the admin who switched) stays readable through the Impersonating interface so both identities can be audited. */
func NewImpersonationToken(
    impersonated securitycontract.Token,
    impersonator securitycontract.Token,
) *ImpersonationToken {
    if true == internal.IsNilInterface(impersonated) {
        exception.Panic(exception.NewError("can not impersonate with a nil impersonated token", nil, nil))
    }

    if true == internal.IsNilInterface(impersonator) {
        exception.Panic(exception.NewError("can not impersonate with a nil impersonator token", nil, nil))
    }

    return &ImpersonationToken{
        impersonated: impersonated,
        impersonator: impersonator,
    }
}

type ImpersonationToken struct {
    impersonated securitycontract.Token
    impersonator securitycontract.Token
}

func (instance *ImpersonationToken) IsAuthenticated() bool {
    return instance.impersonated.IsAuthenticated()
}

func (instance *ImpersonationToken) UserIdentifier() string {
    return instance.impersonated.UserIdentifier()
}

func (instance *ImpersonationToken) Roles() []string {
    roles := instance.impersonated.Roles()
    if nil == roles {
        return nil
    }

    return append([]string{}, roles...)
}

func (instance *ImpersonationToken) Scope() map[string]any {
    return internal.CopyAnyMap(instance.impersonated.Scope())
}

func (instance *ImpersonationToken) Attributes() map[string]any {
    return internal.CopyAnyMap(instance.impersonated.Attributes())
}

func (instance *ImpersonationToken) Impersonator() (securitycontract.Token, bool) {
    return instance.impersonator, true
}

/* OnBehalfOf delegates to the impersonated identity so an originating actor carried by that identity stays readable, returning (nil, false) when it carries none. */
func (instance *ImpersonationToken) OnBehalfOf() (securitycontract.Actor, bool) {
    aware, isAware := instance.impersonated.(securitycontract.ActorAware)
    if false == isAware {
        return nil, false
    }

    return aware.OnBehalfOf()
}

/* ImpersonatorFromToken reads the impersonating (admin) principal behind a token, returning (nil, false) for a nil token, a token that is not Impersonating, or one not currently impersonating. */
func ImpersonatorFromToken(token securitycontract.Token) (securitycontract.Token, bool) {
    if true == internal.IsNilInterface(token) {
        return nil, false
    }

    impersonating, isImpersonating := token.(securitycontract.Impersonating)
    if false == isImpersonating {
        return nil, false
    }

    return impersonating.Impersonator()
}

var _ securitycontract.Token = (*ImpersonationToken)(nil)
var _ securitycontract.Impersonating = (*ImpersonationToken)(nil)
var _ securitycontract.ActorAware = (*ImpersonationToken)(nil)
