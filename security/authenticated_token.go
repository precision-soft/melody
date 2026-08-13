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

/* WithRoles answers this token's own twin under another role set, which is what the role hierarchy voter asks of the token it is about to expand rather than rebuilding one of its own. The receiver is not modified: the voter holds the caller's token and hands the twin to a delegate that may keep it. */
func (instance *AuthenticatedToken) WithRoles(roles []string) securitycontract.Token {
    return NewAuthenticatedToken(instance.userIdentifier, roles)
}

var _ securitycontract.Token = (*AuthenticatedToken)(nil)
var _ RolesReplacer = (*AuthenticatedToken)(nil)
