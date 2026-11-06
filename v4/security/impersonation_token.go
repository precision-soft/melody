package security

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* ImpersonationRoleMode selects whose roles an impersonation token authorizes and propagates with. The visible principal, scope and attributes are always the impersonated user's (you act in their context); only the effective role set differs. */
type ImpersonationRoleMode int

const (
    /* RoleModeImpersonated (the default, zero value) takes on the impersonated user's roles, so the admin experiences exactly the target's rights and full context. */
    RoleModeImpersonated ImpersonationRoleMode = iota

    /* RoleModeImpersonator keeps the admin's own roles while acting in the impersonated user's context, so the admin retains their own rights when viewing as the target. */
    RoleModeImpersonator
)

/* NewImpersonationToken builds a token whose visible principal is the impersonated user (it drives Identifier/Roles/Scope/Attributes/IsAuthenticated) while the impersonator (the admin who switched) stays readable through the Impersonating interface so both identities can be audited. roleMode selects whose roles it authorizes with: RoleModeImpersonated, the zero value, takes on the target's roles, RoleModeImpersonator keeps the admin's own; the impersonator stays readable (and propagates between services through the originating actor) in either mode. A nil token and an unknown role mode are refused. */
func NewImpersonationToken(
    impersonated securitycontract.Token,
    impersonator securitycontract.Token,
    roleMode ImpersonationRoleMode,
) *ImpersonationToken {
    if true == internal.IsNilInterface(impersonated) {
        exception.Panic(exception.NewError("can not impersonate with a nil impersonated token", nil, nil))
    }

    if true == internal.IsNilInterface(impersonator) {
        exception.Panic(exception.NewError("can not impersonate with a nil impersonator token", nil, nil))
    }

    if RoleModeImpersonated != roleMode && RoleModeImpersonator != roleMode {
        exception.Panic(
            exception.NewError(
                "impersonation role mode is unknown",
                exceptioncontract.Context{
                    "argument": "roleMode",
                    "door":     "NewImpersonationToken",
                    "roleMode": int(roleMode),
                },
                nil,
            ),
        )
    }

    return &ImpersonationToken{
        impersonated: impersonated,
        impersonator: impersonator,
        roleMode:     roleMode,
    }
}

type ImpersonationToken struct {
    impersonated securitycontract.Token
    impersonator securitycontract.Token
    roleMode     ImpersonationRoleMode
}

func (instance *ImpersonationToken) IsAuthenticated() bool {
    return instance.impersonated.IsAuthenticated()
}

func (instance *ImpersonationToken) UserIdentifier() string {
    return instance.impersonated.UserIdentifier()
}

func (instance *ImpersonationToken) Roles() []string {
    roles := instance.effectiveRoles()
    if nil == roles {
        return nil
    }

    return append([]string{}, roles...)
}

/* effectiveRoles is the role set this token authorizes with: the admin's own under RoleModeImpersonator, otherwise the impersonated user's. */
func (instance *ImpersonationToken) effectiveRoles() []string {
    if RoleModeImpersonator == instance.roleMode {
        return instance.impersonator.Roles()
    }

    return instance.impersonated.Roles()
}

func (instance *ImpersonationToken) Scope() map[string]any {
    return internal.CopyAnyMap(instance.impersonated.Scope())
}

func (instance *ImpersonationToken) Attributes() map[string]any {
    return internal.CopyAnyMap(instance.impersonated.Attributes())
}

func (instance *ImpersonationToken) Impersonator() (securitycontract.Token, bool) {
    return instance.impersonator, true
}

/* OnBehalfOf is the originating actor that propagates the impersonation across services: the impersonated user acting behind the accountable impersonator, each with their own identity and roles. An impersonator that is itself acting behind somebody, a nested impersonation, is carried with its own chain, so the root admin stays in the actor. */
func (instance *ImpersonationToken) OnBehalfOf() (securitycontract.Actor, bool) {
    impersonator := instance.impersonatorActor()

    actor := NewActor(
        instance.impersonated.UserIdentifier(),
        securitycontract.ActorTypeUser,
        instance.effectiveRoles(),
        nil,
        impersonator,
    )

    return actor, true
}

/* impersonatorActor answers the impersonator's own originating actor when it names the impersonator, which is how a nested impersonation keeps its chain; an actor naming somebody else, such as the user an upstream service acts for, is not the impersonator's identity, and the flat actor is built instead. */
func (instance *ImpersonationToken) impersonatorActor() securitycontract.Actor {
    if aware, isAware := instance.impersonator.(securitycontract.ActorAware); true == isAware {
        if actor, present := aware.OnBehalfOf(); true == present && false == internal.IsNilInterface(actor) && instance.impersonator.UserIdentifier() == actor.Identifier() {
            return actor
        }
    }

    return NewActor(
        instance.impersonator.UserIdentifier(),
        securitycontract.ActorTypeUser,
        instance.impersonator.Roles(),
        nil,
        nil,
    )
}

/* ImpersonatorFromToken reads the impersonating (admin) principal behind a token, returning (nil, false) for a nil token, a token that is not Impersonating, or one not currently impersonating. */
func ImpersonatorFromToken(token securitycontract.Token) (securitycontract.Token, bool) {
    if true == internal.IsNilInterface(token) {
        return nil, false
    }

    impersonating, isImpersonating := token.(securitycontract.Impersonating)
    if false == isImpersonating {
        return nil, false
    }

    return impersonating.Impersonator()
}

var _ securitycontract.Token = (*ImpersonationToken)(nil)
var _ securitycontract.Impersonating = (*ImpersonationToken)(nil)
var _ securitycontract.ActorAware = (*ImpersonationToken)(nil)
