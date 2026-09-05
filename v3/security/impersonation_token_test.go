package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func impersonationProbeTokens() (securitycontract.Token, securitycontract.Token) {
    impersonated := NewAuthenticatedTokenFromClaims(securitycontract.Claims{
        UserIdentifier: "target",
        Roles:          []string{"ROLE_USER"},
        Scope:          map[string]any{"tenant": "acme"},
    })
    impersonator := NewAuthenticatedToken("admin", []string{"ROLE_ADMIN", "ROLE_ALLOWED_TO_SWITCH"})

    return impersonated, impersonator
}

func TestNewImpersonationToken_RefusesEitherSideMissing(t *testing.T) {
    impersonated, impersonator := impersonationProbeTokens()

    var typedNilToken securitycontract.Token = (*AuthenticatedToken)(nil)

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewImpersonationToken(nil, impersonator)
    }, "can not impersonate with a nil impersonated token")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewImpersonationToken(typedNilToken, impersonator)
    }, "can not impersonate with a nil impersonated token")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewImpersonationToken(impersonated, nil)
    }, "can not impersonate with a nil impersonator token")

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewImpersonationToken(impersonated, typedNilToken)
    }, "can not impersonate with a nil impersonator token")
}

/* the visible principal is always the impersonated user — the admin acts in the target's context — and the default role mode takes on the target's rights, which is what makes the session an honest reproduction of what the target sees */
func TestImpersonationToken_ShowsTheImpersonatedPrincipal(t *testing.T) {
    token := NewImpersonationToken(impersonationProbeTokens())

    if "target" != token.UserIdentifier() {
        t.Fatalf("expected the impersonated identifier, got %q", token.UserIdentifier())
    }

    if false == token.IsAuthenticated() {
        t.Fatal("expected the impersonation token to read as authenticated")
    }

    if 1 != len(token.Roles()) || "ROLE_USER" != token.Roles()[0] {
        t.Fatalf("expected the impersonated user's roles by default, got %v", token.Roles())
    }

    if "acme" != token.Scope()["tenant"] {
        t.Fatalf("expected the impersonated user's scope, got %v", token.Scope())
    }
}

/* the explicit mode keeps the admin's own rights while the context stays the target's: identity and scope must NOT follow the roles, or the mode would be an ordinary admin session with the target's name on it */
func TestImpersonationToken_ImpersonatorRoleModeChangesOnlyTheRoles(t *testing.T) {
    impersonated, impersonator := impersonationProbeTokens()
    token := NewImpersonationTokenWithRoleMode(impersonated, impersonator, RoleModeImpersonator)

    if 2 != len(token.Roles()) || "ROLE_ADMIN" != token.Roles()[0] {
        t.Fatalf("expected the impersonator's own roles, got %v", token.Roles())
    }

    if "target" != token.UserIdentifier() {
        t.Fatalf("expected the visible principal to stay the impersonated one, got %q", token.UserIdentifier())
    }

    if "acme" != token.Scope()["tenant"] {
        t.Fatalf("expected the scope to stay the impersonated one, got %v", token.Scope())
    }
}

/* sharingToken hands back its OWN backing slice and map, which is what an application's token is free to do. It is the only shape that separates this copy from the one AuthenticatedToken already makes on the way out: measured, a probe built from the framework's own token leaves this guard shadowed and the mutant that removes it alive. */
type sharingToken struct {
    roles []string
    scope map[string]any
}

func (instance *sharingToken) UserIdentifier() string     { return "target" }
func (instance *sharingToken) Roles() []string            { return instance.roles }
func (instance *sharingToken) Scope() map[string]any      { return instance.scope }
func (instance *sharingToken) Attributes() map[string]any { return map[string]any{} }
func (instance *sharingToken) IsAuthenticated() bool      { return true }

var _ securitycontract.Token = (*sharingToken)(nil)

/* the roles and the scope are handed out as copies: a caller mutating what it read would be rewriting the rights of a live impersonation session */
func TestImpersonationToken_HandsOutCopies(t *testing.T) {
    shared := &sharingToken{roles: []string{"ROLE_USER"}, scope: map[string]any{"tenant": "acme"}}
    _, impersonator := impersonationProbeTokens()

    token := NewImpersonationToken(shared, impersonator)

    roles := token.Roles()
    roles[0] = "ROLE_ADMIN"
    if "ROLE_USER" != token.Roles()[0] {
        t.Fatalf("expected the roles to be copied out, got %v", token.Roles())
    }

    if "ROLE_USER" != shared.roles[0] {
        t.Fatalf("expected the impersonated token's own slice to stay untouched, got %v", shared.roles)
    }

    scope := token.Scope()
    scope["tenant"] = "other"
    if "acme" != token.Scope()["tenant"] {
        t.Fatalf("expected the scope to be copied out, got %v", token.Scope())
    }

    if "acme" != shared.scope["tenant"] {
        t.Fatalf("expected the impersonated token's own map to stay untouched, got %v", shared.scope)
    }
}

/* both identities travel downstream: the impersonated user carries the effective roles of the active mode, and the accountable admin rides behind as the impersonator, so an audit two services away can still name who really acted */
func TestImpersonationToken_OnBehalfOfCarriesBothIdentities(t *testing.T) {
    impersonated, impersonator := impersonationProbeTokens()
    token := NewImpersonationTokenWithRoleMode(impersonated, impersonator, RoleModeImpersonator)

    actor, exists := token.OnBehalfOf()
    if false == exists {
        t.Fatal("expected an originating actor")
    }

    if "target" != actor.Identifier() {
        t.Fatalf("expected the impersonated user as the actor, got %q", actor.Identifier())
    }

    if 2 != len(actor.Roles()) || "ROLE_ADMIN" != actor.Roles()[0] {
        t.Fatalf("expected the effective roles of the active mode on the actor, got %v", actor.Roles())
    }

    /* the impersonator rides on the optional ActorImpersonating capability rather than on Actor itself, so the read is the type assertion a consumer downstream makes */
    impersonating, isImpersonating := actor.(securitycontract.ActorImpersonating)
    if false == isImpersonating {
        t.Fatal("expected the actor to carry the impersonating capability")
    }

    behind, hasImpersonator := impersonating.Impersonator()
    if false == hasImpersonator {
        t.Fatal("expected the accountable admin to ride behind the actor")
    }

    if "admin" != behind.Identifier() {
        t.Fatalf("expected the admin identity behind the actor, got %q", behind.Identifier())
    }
}

func TestImpersonatorFromToken_AnswersFalseForAnythingElse(t *testing.T) {
    var typedNilToken securitycontract.Token = (*AuthenticatedToken)(nil)

    for _, probe := range []securitycontract.Token{nil, typedNilToken, NewAuthenticatedToken("u1", nil), NewAnonymousToken()} {
        impersonator, isImpersonating := ImpersonatorFromToken(probe)
        if true == isImpersonating || nil != impersonator {
            t.Fatalf("expected %T to carry no impersonator, got %v", probe, impersonator)
        }
    }

    token := NewImpersonationToken(impersonationProbeTokens())
    impersonator, isImpersonating := ImpersonatorFromToken(token)
    if false == isImpersonating || "admin" != impersonator.UserIdentifier() {
        t.Fatalf("expected the admin behind the impersonation, got %v", impersonator)
    }
}
