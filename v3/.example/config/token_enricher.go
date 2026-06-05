package config

import (
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

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
