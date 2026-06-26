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

/* NewActorFromData rebuilds a concrete Actor from its serializable ActorData carrier, or returns nil when the carrier is absent. */
func NewActorFromData(data *securitycontract.ActorData) *Actor {
    if nil == data {
        return nil
    }

    return NewActor(data.Identifier, data.Type, data.Roles, data.Attributes)
}

type Actor struct {
    identifier string
    actorType  string
    roles      []string
    attributes map[string]string
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

/* ActorToData converts an Actor into its serializable ActorData carrier, or returns nil for a nil actor. */
func ActorToData(actor securitycontract.Actor) *securitycontract.ActorData {
    if true == internal.IsNilInterface(actor) {
        return nil
    }

    return &securitycontract.ActorData{
        Identifier: actor.Identifier(),
        Type:       actor.Type(),
        Roles:      actor.Roles(),
        Attributes: actor.Attributes(),
    }
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
