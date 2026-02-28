package security

import (
    securitycontract "github.com/precision-soft/melody/security/contract"
)

func NewAuthenticatedToken(userIdentifier string, roles []string) *AuthenticatedToken {
    copiedRoles := []string{}
    if nil != roles {
        copiedRoles = append([]string{}, roles...)
    }

    return &AuthenticatedToken{
        userIdentifier: userIdentifier,
        roles:          copiedRoles,
    }
}

type AuthenticatedToken struct {
    userIdentifier string
    roles          []string
}

func (instance *AuthenticatedToken) IsAuthenticated() bool {
    return true
}

func (instance *AuthenticatedToken) UserIdentifier() string {
    return instance.userIdentifier
}

func (instance *AuthenticatedToken) Roles() []string {
    if nil == instance.roles {
        return nil
    }

    return append([]string{}, instance.roles...)
}

var _ securitycontract.Token = (*AuthenticatedToken)(nil)
