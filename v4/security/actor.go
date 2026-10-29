package security

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* NewActor builds the originating principal; impersonator, nil for none, is the accountable actor acting behind it, so the admin and its roles travel with the actor. A typed-nil impersonator is refused. */
func NewActor(
    identifier string,
    actorType string,
    roles []string,
    attributes map[string]string,
    impersonator securitycontract.Actor,
) *Actor {
    refuseTypedNilDependency("actor impersonator is a typed nil", "impersonator", "NewActor", impersonator)

    actor := &Actor{
        identifier: identifier,
        actorType:  actorType,
        roles:      copyRoles(roles),
        attributes: internal.CopyStringMap(attributes),
    }

    if nil != impersonator {
        actor.impersonator = impersonator
    }

    return actor
}

/* NewActorFromData rebuilds a concrete Actor from its serializable ActorData carrier, or returns nil when the carrier is absent. A nested Impersonator is rebuilt too; the chain is truncated at maxActorImpersonationDepth, so a cyclic ActorData cannot overflow the goroutine stack. */
func NewActorFromData(data *securitycontract.ActorData) *Actor {
    return newActorFromDataAtDepth(data, 0)
}

func newActorFromDataAtDepth(data *securitycontract.ActorData, depth int) *Actor {
    if nil == data {
        return nil
    }

    actor := NewActor(data.Identifier, data.Type, data.Roles, data.Attributes, nil)
    if nil != data.Impersonator && depth < maxActorImpersonationDepth {
        actor.impersonator = newActorFromDataAtDepth(data.Impersonator, depth+1)
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

/* ActorToData converts an Actor into its serializable ActorData carrier, or returns nil for a nil actor. An impersonator the actor carries is encoded too; the chain is truncated at maxActorImpersonationDepth, so a cyclic Actor cannot overflow the goroutine stack. */
func ActorToData(actor securitycontract.Actor) *securitycontract.ActorData {
    return actorToDataAtDepth(actor, 0)
}

func actorToDataAtDepth(actor securitycontract.Actor, depth int) *securitycontract.ActorData {
    if true == internal.IsNilInterface(actor) {
        return nil
    }

    data := &securitycontract.ActorData{
        Identifier: actor.Identifier(),
        Type:       actor.Type(),
        Roles:      actor.Roles(),
        Attributes: actor.Attributes(),
    }

    if depth < maxActorImpersonationDepth {
        if impersonating, isImpersonating := actor.(securitycontract.ActorImpersonating); true == isImpersonating {
            if impersonator, present := impersonating.Impersonator(); true == present {
                data.Impersonator = actorToDataAtDepth(impersonator, depth+1)
            }
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

/* refuseTypedNilDependency refuses an optional dependency that holds a typed nil, naming the argument and the door; a plain nil is the ordinary "none". */
func refuseTypedNilDependency(message string, argument string, door string, dependency any) {
    if nil == dependency || false == internal.IsNilInterface(dependency) {
        return
    }

    exception.Panic(
        exception.NewError(
            message,
            exceptioncontract.Context{
                "argument": argument,
                "door":     door,
            },
            nil,
        ),
    )
}
