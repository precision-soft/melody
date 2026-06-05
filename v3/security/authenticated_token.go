package security

import (
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewAuthenticatedToken(userIdentifier string, roles []string) *AuthenticatedToken {
    return &AuthenticatedToken{
        userIdentifier: userIdentifier,
        roles:          copyRoles(roles),
    }
}

/**
 * NewAuthenticatedTokenFromClaims builds an authenticated token carrying the full enriched claims —
 * roles plus the generic Scope and Attributes a TokenEnricher resolved — so attribute/tenant-based
 * access control downstream can read more than just the roles.
 */
func NewAuthenticatedTokenFromClaims(claims securitycontract.Claims) *AuthenticatedToken {
    return &AuthenticatedToken{
        userIdentifier: claims.UserIdentifier,
        roles:          copyRoles(claims.Roles),
        scope:          copyAnyMap(claims.Scope),
        attributes:     copyAnyMap(claims.Attributes),
    }
}

type AuthenticatedToken struct {
    userIdentifier string
    roles          []string
    scope          map[string]any
    attributes     map[string]any
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

/** Scope returns a copy of the generic scope claims carried by the token (nil when none). */
func (instance *AuthenticatedToken) Scope() map[string]any {
    return copyAnyMap(instance.scope)
}

/** Attributes returns a copy of the generic attributes an enricher attached (nil when none). */
func (instance *AuthenticatedToken) Attributes() map[string]any {
    return copyAnyMap(instance.attributes)
}

func copyRoles(roles []string) []string {
    copied := []string{}
    if nil != roles {
        copied = append([]string{}, roles...)
    }

    return copied
}

func copyAnyMap(source map[string]any) map[string]any {
    if nil == source {
        return nil
    }

    copied := make(map[string]any, len(source))
    for key, value := range source {
        copied[key] = value
    }

    return copied
}

var _ securitycontract.Token = (*AuthenticatedToken)(nil)
