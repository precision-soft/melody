package config

import (
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

        return nil, nil
    }

    return melodysecurity.NewAuthenticatedToken(identifier, roles), nil
}
