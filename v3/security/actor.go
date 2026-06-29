package security

import (
    "github.com/precision-soft/melody/v3/internal"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewActor(
    identifier string,
    actorType string,
    roles []string,
    attributes map[string]string,
) *Actor {
    return &Actor{
        identifier: identifier,
        actorType:  actorType,
        roles:      copyRoles(roles),
        attributes: internal.CopyStringMap(attributes),
    }
}

/* NewActorWithImpersonator builds an Actor that an impersonator is acting behind, so the accountable admin (and its roles) travels with the originating actor. A nil impersonator is equivalent to NewActor. */
func NewActorWithImpersonator(
    identifier string,
    actorType string,
    roles []string,
    attributes map[string]string,
    impersonator securitycontract.Actor,
) *Actor {
    actor := NewActor(identifier, actorType, roles, attributes)
    if false == internal.IsNilInterface(impersonator) {
        actor.impersonator = impersonator
    }

    return actor
}

/* NewActorFromData rebuilds a concrete Actor from its serializable ActorData carrier, or returns nil when the carrier is absent. A nested Impersonator is rebuilt too, so an impersonation propagated across a service boundary stays readable. */
func NewActorFromData(data *securitycontract.ActorData) *Actor {
    if nil == data {
        return nil
    }

    actor := NewActor(data.Identifier, data.Type, data.Roles, data.Attributes)
    if nil != data.Impersonator {
        actor.impersonator = NewActorFromData(data.Impersonator)
    }

    return actor
}

type Actor struct {
    identifier   string
    actorType    string
    roles        []string
    attributes   map[string]string
    impersonator securitycontract.Actor
}

func (instance *Actor) Identifier() string {
    return instance.identifier
}

func (instance *Actor) Type() string {
    return instance.actorType
}

func (instance *Actor) Roles() []string {
    return append([]string{}, instance.roles...)
}

func (instance *Actor) Attributes() map[string]string {
    return internal.CopyStringMap(instance.attributes)
}

/* Impersonator reports the admin acting behind this actor, returning (nil, false) when the actor is not an impersonation. */
func (instance *Actor) Impersonator() (securitycontract.Actor, bool) {
    if true == internal.IsNilInterface(instance.impersonator) {
        return nil, false
    }

    return instance.impersonator, true
}

/* ActorToData converts an Actor into its serializable ActorData carrier, or returns nil for a nil actor. An impersonator carried by the actor (ActorImpersonating) is encoded too, so it round-trips across a service boundary. */
func ActorToData(actor securitycontract.Actor) *securitycontract.ActorData {
    if true == internal.IsNilInterface(actor) {
        return nil
    }

    data := &securitycontract.ActorData{
        Identifier: actor.Identifier(),
        Type:       actor.Type(),
        Roles:      actor.Roles(),
        Attributes: actor.Attributes(),
    }

    if impersonating, isImpersonating := actor.(securitycontract.ActorImpersonating); true == isImpersonating {
        if impersonator, present := impersonating.Impersonator(); true == present {
            data.Impersonator = ActorToData(impersonator)
        }
    }

    return data
}

/* ActorFromToken reads the originating actor from a token when the token carries one, returning (nil, false) for a nil token, a token that is not ActorAware, or an ActorAware token with no actor set. */
func ActorFromToken(token securitycontract.Token) (securitycontract.Actor, bool) {
    if true == internal.IsNilInterface(token) {
        return nil, false
    }

    aware, isAware := token.(securitycontract.ActorAware)
    if false == isAware {
        return nil, false
    }

    return aware.OnBehalfOf()
}

var _ securitycontract.Actor = (*Actor)(nil)
var _ securitycontract.ActorImpersonating = (*Actor)(nil)
