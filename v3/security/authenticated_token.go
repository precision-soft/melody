package security

import (
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewAuthenticatedToken(userIdentifier string, roles []string) *AuthenticatedToken {
    return &AuthenticatedToken{
        userIdentifier: userIdentifier,
        roles:          copyRoles(roles),
    }
}

/* NewAuthenticatedTokenWithActor builds a token whose authenticated principal is userIdentifier/roles and which additionally carries an originating actor readable through OnBehalfOf. A nil actor is equivalent to NewAuthenticatedToken. */
func NewAuthenticatedTokenWithActor(
    userIdentifier string,
    roles []string,
    actor securitycontract.Actor,
) *AuthenticatedToken {
    return &AuthenticatedToken{
        userIdentifier:   userIdentifier,
        roles:            copyRoles(roles),
        originatingActor: actor,
    }
}

func NewAuthenticatedTokenFromClaims(claims securitycontract.Claims) *AuthenticatedToken {
    var actor securitycontract.Actor
    if rebuilt := NewActorFromData(claims.OriginatingActor); nil != rebuilt {
        actor = rebuilt
    }

    return &AuthenticatedToken{
        userIdentifier:   claims.UserIdentifier,
        roles:            copyRoles(claims.Roles),
        scope:            internal.CopyAnyMap(claims.Scope),
        attributes:       internal.CopyAnyMap(claims.Attributes),
        originatingActor: actor,
    }
}

type AuthenticatedToken struct {
    userIdentifier   string
    roles            []string
    scope            map[string]any
    attributes       map[string]any
    originatingActor securitycontract.Actor
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

func (instance *AuthenticatedToken) Scope() map[string]any {
    return internal.CopyAnyMap(instance.scope)
}

func (instance *AuthenticatedToken) Attributes() map[string]any {
    return internal.CopyAnyMap(instance.attributes)
}

func (instance *AuthenticatedToken) OnBehalfOf() (securitycontract.Actor, bool) {
    if true == internal.IsNilInterface(instance.originatingActor) {
        return nil, false
    }

    return instance.originatingActor, true
}

func copyRoles(roles []string) []string {
    copied := []string{}
    if nil != roles {
        copied = append([]string{}, roles...)
    }

    return copied
}

var _ securitycontract.Token = (*AuthenticatedToken)(nil)
var _ securitycontract.ActorAware = (*AuthenticatedToken)(nil)
