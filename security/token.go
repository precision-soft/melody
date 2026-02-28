package security

import (
    "github.com/precision-soft/melody/exception"
    securitycontract "github.com/precision-soft/melody/security/contract"
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

var _ securitycontract.Token = (*Token)(nil)
