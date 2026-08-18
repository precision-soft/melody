package config

import (
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* the impersonation wiring lets an admin holding the switch role act as another user by sending an
X-Switch-User header on the token-protected /secure api. The resolver below is the application-supplied
piece that maps a target identifier to that user's token (a real app queries its user store); an unknown
identifier denies the switch. */
func (instance *Module) buildImpersonation() {
    instance.impersonatedUsers = &staticImpersonatedUserResolver{
        rolesByIdentifier: map[string][]string{
            "bob":   {"ROLE_USER"},
            "carol": {"ROLE_EDITOR", "ROLE_USER"},
        },
    }
}

type staticImpersonatedUserResolver struct {
    rolesByIdentifier map[string][]string
}

func (instance *staticImpersonatedUserResolver) ResolveImpersonatedUser(
    _ melodyruntimecontract.Runtime,
    identifier string,
) (melodysecuritycontract.Token, error) {
    roles, known := instance.rolesByIdentifier[identifier]
    if false == known {
        /* an unknown target denies the switch; the source fails closed to an anonymous token */
        return nil, nil
    }

    return melodysecurity.NewAuthenticatedToken(identifier, roles), nil
}
