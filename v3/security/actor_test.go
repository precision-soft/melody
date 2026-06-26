package security_test

import (
    "testing"

    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestAuthenticatedTokenWithActorExposesActor(t *testing.T) {
    actor := security.NewActor(
        "user-7",
        securitycontract.ActorTypeUser,
        []string{"ROLE_BUYER"},
        map[string]string{"tenant": "acme"},
    )

    token := security.NewAuthenticatedTokenWithActor("wms-service", []string{"ROLE_SERVICE"}, actor)

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
    token := security.NewAuthenticatedToken("user-1", []string{"ROLE_USER"})

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

    token := security.NewAuthenticatedTokenFromClaims(claims)

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

    token := security.NewAuthenticatedTokenFromClaims(claims)

    if _, present := token.OnBehalfOf(); false != present {
        t.Fatal("expected no actor when claims omit one")
    }
}

func TestActorFromTokenReadsActorAwareTokens(t *testing.T) {
    actor := security.NewActor("user-9", securitycontract.ActorTypeUser, nil, nil)
    token := security.NewAuthenticatedTokenWithActor("svc", nil, actor)

    resolved, present := security.ActorFromToken(security.NewToken(token))
    if false == present {
        t.Fatal("expected ActorFromToken to read through the token wrapper")
    }

    if "user-9" != resolved.Identifier() {
        t.Fatalf("expected user-9, got %q", resolved.Identifier())
    }
}

/* negative control: ActorFromToken on a non-actor-aware / anonymous token. */
func TestActorFromTokenOnAnonymousToken(t *testing.T) {
    if _, present := security.ActorFromToken(security.NewAnonymousToken()); false != present {
        t.Fatal("expected no actor on an anonymous token")
    }

    if _, present := security.ActorFromToken(nil); false != present {
        t.Fatal("expected no actor for a nil token")
    }
}

func TestActorDefensiveCopies(t *testing.T) {
    roles := []string{"ROLE_A"}
    attributes := map[string]string{"k": "v"}

    actor := security.NewActor("a", securitycontract.ActorTypeSystem, roles, attributes)

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
