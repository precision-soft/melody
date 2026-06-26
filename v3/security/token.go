package security

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewToken(user securitycontract.Token) *Token {
    if nil == user {
        exception.Panic(
            exception.NewError("can not create a security token from nil", nil, nil),
        )
    }

    if alreadyWrapped, ok := user.(*Token); true == ok {
        return alreadyWrapped
    }

    return &Token{
        user: user,
    }
}

type Token struct {
    user securitycontract.Token
}

func (instance *Token) User() securitycontract.Token {
    return instance.user
}

func (instance *Token) IsAuthenticated() bool {
    return instance.user.IsAuthenticated()
}

func (instance *Token) UserIdentifier() string {
    return instance.user.UserIdentifier()
}

func (instance *Token) Roles() []string {
    roles := instance.user.Roles()
    if nil == roles {
        return nil
    }

    return append([]string{}, roles...)
}

func (instance *Token) Scope() map[string]any {
    return internal.CopyAnyMap(instance.user.Scope())
}

func (instance *Token) Attributes() map[string]any {
    return internal.CopyAnyMap(instance.user.Attributes())
}

/* OnBehalfOf delegates to the wrapped token so the originating actor stays readable through the wrapper, returning (nil, false) when the wrapped token does not carry one. */
func (instance *Token) OnBehalfOf() (securitycontract.Actor, bool) {
    aware, isAware := instance.user.(securitycontract.ActorAware)
    if false == isAware {
        return nil, false
    }

    return aware.OnBehalfOf()
}

/* Impersonator delegates to the wrapped token so the impersonating principal stays readable through the wrapper, returning (nil, false) when the wrapped token is not an impersonation. */
func (instance *Token) Impersonator() (securitycontract.Token, bool) {
    impersonating, isImpersonating := instance.user.(securitycontract.Impersonating)
    if false == isImpersonating {
        return nil, false
    }

    return impersonating.Impersonator()
}

var _ securitycontract.Token = (*Token)(nil)
var _ securitycontract.ActorAware = (*Token)(nil)
var _ securitycontract.Impersonating = (*Token)(nil)
