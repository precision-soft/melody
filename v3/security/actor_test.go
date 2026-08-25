package security

import (
    "testing"

    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestAuthenticatedTokenWithActorExposesActor(t *testing.T) {
    actor := NewActor(
        "user-7",
        securitycontract.ActorTypeUser,
        []string{"ROLE_BUYER"},
        map[string]string{"tenant": "acme"},
    )

    token := NewAuthenticatedTokenWithActor("wms-service", []string{"ROLE_SERVICE"}, actor)

    if "wms-service" != token.UserIdentifier() {
        t.Fatalf("expected principal wms-service, got %q", token.UserIdentifier())
    }

    resolved, present := token.OnBehalfOf()
    if false == present {
        t.Fatal("expected an originating actor to be present")
    }

    if "user-7" != resolved.Identifier() {
        t.Fatalf("expected actor user-7, got %q", resolved.Identifier())
    }

    if securitycontract.ActorTypeUser != resolved.Type() {
        t.Fatalf("expected actor type user, got %q", resolved.Type())
    }
}

/* negative control: a plain authenticated token reports no actor. */
func TestAuthenticatedTokenWithoutActorReportsAbsent(t *testing.T) {
    token := NewAuthenticatedToken("user-1", []string{"ROLE_USER"})

    resolved, present := token.OnBehalfOf()
    if false != present {
        t.Fatal("expected no originating actor on a plain token")
    }

    if nil != resolved {
        t.Fatalf("expected nil actor, got %#v", resolved)
    }
}

func TestNewAuthenticatedTokenFromClaimsRebuildsActor(t *testing.T) {
    claims := securitycontract.Claims{
        UserIdentifier: "billing-service",
        Roles:          []string{"ROLE_SERVICE"},
        OriginatingActor: &securitycontract.ActorData{
            Identifier: "client-42",
            Type:       securitycontract.ActorTypeApiClient,
            Roles:      []string{"ROLE_CLIENT"},
            Attributes: map[string]string{"region": "eu"},
        },
    }

    token := NewAuthenticatedTokenFromClaims(claims)

    actor, present := token.OnBehalfOf()
    if false == present {
        t.Fatal("expected actor rebuilt from claims")
    }

    if "client-42" != actor.Identifier() || securitycontract.ActorTypeApiClient != actor.Type() {
        t.Fatalf("unexpected actor %q/%q", actor.Identifier(), actor.Type())
    }

    if "eu" != actor.Attributes()["region"] {
        t.Fatalf("expected actor attribute region=eu, got %q", actor.Attributes()["region"])
    }
}

func TestNewAuthenticatedTokenFromClaimsWithoutActor(t *testing.T) {
    claims := securitycontract.Claims{UserIdentifier: "u", Roles: []string{"ROLE_USER"}}

    token := NewAuthenticatedTokenFromClaims(claims)

    if _, present := token.OnBehalfOf(); false != present {
        t.Fatal("expected no actor when claims omit one")
    }
}

func TestActorFromTokenReadsActorAwareTokens(t *testing.T) {
    actor := NewActor("user-9", securitycontract.ActorTypeUser, nil, nil)
    token := NewAuthenticatedTokenWithActor("svc", nil, actor)

    resolved, present := ActorFromToken(NewToken(token))
    if false == present {
        t.Fatal("expected ActorFromToken to read through the token wrapper")
    }

    if "user-9" != resolved.Identifier() {
        t.Fatalf("expected user-9, got %q", resolved.Identifier())
    }
}

/* negative control: ActorFromToken on a non-actor-aware / anonymous token. */
func TestActorFromTokenOnAnonymousToken(t *testing.T) {
    if _, present := ActorFromToken(NewAnonymousToken()); false != present {
        t.Fatal("expected no actor on an anonymous token")
    }

    if _, present := ActorFromToken(nil); false != present {
        t.Fatal("expected no actor for a nil token")
    }
}

/* a cyclic ActorData — an in-process caller can point Impersonator back into the chain through the exported field — must not recurse until the goroutine stack overflows; the depth bound truncates it. If unbounded this crashes the test binary rather than failing an assertion. */
func TestNewActorFromDataBoundsCyclicImpersonatorChain(t *testing.T) {
    data := &securitycontract.ActorData{
        Identifier: "loop",
        Type:       securitycontract.ActorTypeUser,
    }
    data.Impersonator = data

    actor := NewActorFromData(data)
    if nil == actor {
        t.Fatal("expected a rebuilt actor")
    }

    if "loop" != actor.Identifier() {
        t.Fatalf("expected the rebuilt actor identity, got %q", actor.Identifier())
    }
}

type selfImpersonatingActor struct{}

func (instance *selfImpersonatingActor) Identifier() string { return "loop" }

func (instance *selfImpersonatingActor) Type() string { return securitycontract.ActorTypeSystem }

func (instance *selfImpersonatingActor) Roles() []string { return nil }

func (instance *selfImpersonatingActor) Attributes() map[string]string { return nil }

func (instance *selfImpersonatingActor) Impersonator() (securitycontract.Actor, bool) {
    return instance, true
}

/* a cyclic Actor whose Impersonator() returns itself must not recurse until the stack overflows when serialized; the depth bound truncates the chain. */
func TestActorToDataBoundsCyclicImpersonatorChain(t *testing.T) {
    data := ActorToData(&selfImpersonatingActor{})
    if nil == data {
        t.Fatal("expected serialized actor data")
    }

    if "loop" != data.Identifier {
        t.Fatalf("expected the top-level identity, got %q", data.Identifier)
    }
}

func TestActorDefensiveCopies(t *testing.T) {
    roles := []string{"ROLE_A"}
    attributes := map[string]string{"k": "v"}

    actor := NewActor("a", securitycontract.ActorTypeSystem, roles, attributes)

    roles[0] = "ROLE_MUTATED"
    attributes["k"] = "mutated"

    if "ROLE_A" != actor.Roles()[0] {
        t.Fatal("actor roles were not defensively copied on construction")
    }

    if "v" != actor.Attributes()["k"] {
        t.Fatal("actor attributes were not defensively copied on construction")
    }

    actor.Roles()[0] = "ROLE_X"
    actor.Attributes()["k"] = "x"

    if "ROLE_A" != actor.Roles()[0] || "v" != actor.Attributes()["k"] {
        t.Fatal("actor accessors leaked internal state")
    }
}
