package security

import (
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
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

/* WithRoles answers this token's twin under another role set, which the role hierarchy voter asks of the token it expands. The receiver is not modified, since the voter hands the twin to a delegate that may keep it. */
func (instance *AuthenticatedToken) WithRoles(roles []string) securitycontract.Token {
    return NewAuthenticatedToken(instance.userIdentifier, roles)
}

var _ securitycontract.Token = (*AuthenticatedToken)(nil)
var _ RolesReplacer = (*AuthenticatedToken)(nil)
