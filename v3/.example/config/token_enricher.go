package config

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

/**
 * scopeRoleEnricher demonstrates the generic security.TokenEnricher hook: after the token's
 * signature is validated, it grants any roles listed under the token's `scope.roles` claim. A
 * token without a scope is returned unchanged, so ordinary role-carrying JWTs keep working. A real
 * application would resolve roles from a database keyed by the scope instead of reading them inline.
 */
type scopeRoleEnricher struct{}

func newScopeRoleEnricher() scopeRoleEnricher {
    return scopeRoleEnricher{}
}

func (instance scopeRoleEnricher) Enrich(
    runtimeInstance runtimecontract.Runtime,
    claims melodysecuritycontract.Claims,
) (melodysecuritycontract.Claims, error) {
    rawRoles, hasRoles := claims.Scope["roles"]
    if false == hasRoles {
        return claims, nil
    }

    roleList, isList := rawRoles.([]any)
    if false == isList {
        return claims, nil
    }

    resolved := append([]string{}, claims.Roles...)
    for _, entry := range roleList {
        if role, isString := entry.(string); true == isString {
            resolved = append(resolved, role)
        }
    }

    claims.Roles = resolved

    return claims, nil
}

var _ melodysecuritycontract.TokenEnricher = scopeRoleEnricher{}
