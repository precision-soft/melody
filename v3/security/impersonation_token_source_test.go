package security

import (
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* fixedTokenSource resolves a preset token, standing in for any primary source. */
type fixedTokenSource struct {
    token securitycontract.Token
}

func (instance *fixedTokenSource) Name() string {
    return "fixed"
}

func (instance *fixedTokenSource) Resolve(
    _ runtimecontract.Runtime,
    _ httpcontract.Request,
) (securitycontract.Token, error) {
    return instance.token, nil
}

/* mapUserResolver resolves impersonated users from a fixed table. */
type mapUserResolver struct {
    usersById map[string]securitycontract.Token
}

func (instance *mapUserResolver) ResolveImpersonatedUser(
    _ runtimecontract.Runtime,
    identifier string,
) (securitycontract.Token, error) {
    token, exists := instance.usersById[identifier]
    if false == exists {
        return nil, exception.NewError("user not found", map[string]any{"identifier": identifier}, nil)
    }

    return token, nil
}

func switchRequest(target string) httpcontract.Request {
    request := httptest.NewRequest("GET", "/admin/orders", nil)
    if "" != target {
        request.Header.Set(DefaultSwitchUserHeaderName, target)
    }

    return testhelper.NewHttpTestRequestFromHttpRequest(request)
}

func impersonationSource(adminToken securitycontract.Token) *ImpersonationTokenSource {
    return NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: adminToken},
        Users: &mapUserResolver{usersById: map[string]securitycontract.Token{
            "user-7": NewAuthenticatedToken("user-7", []string{"ROLE_BUYER"}),
        }},
    })
}

func TestImpersonation_AdminWithSwitchRoleImpersonates(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch})

    token, resolveErr := impersonationSource(admin).Resolve(testRuntime(), switchRequest("user-7"))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if "user-7" != token.UserIdentifier() {
        t.Fatalf("expected the visible principal to be the impersonated user, got %q", token.UserIdentifier())
    }

    if 1 != len(token.Roles()) || "ROLE_BUYER" != token.Roles()[0] {
        t.Fatalf("expected impersonated user roles, got %v", token.Roles())
    }

    impersonator, present := ImpersonatorFromToken(token)
    if false == present || "admin-1" != impersonator.UserIdentifier() {
        t.Fatalf("expected the admin to stay readable as impersonator, present=%v", present)
    }
}

/* negative control: an admin lacking the switch role is not allowed to impersonate. */
func TestImpersonation_WithoutSwitchRoleStaysAdmin(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN"})

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest("user-7"))

    if "admin-1" != token.UserIdentifier() {
        t.Fatalf("expected the admin token to pass through unchanged, got %q", token.UserIdentifier())
    }

    if _, present := ImpersonatorFromToken(token); true == present {
        t.Fatal("expected no impersonation when the switch role is absent")
    }
}

func TestImpersonation_NoHeaderReturnsInnerToken(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch})

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest(""))

    if "admin-1" != token.UserIdentifier() {
        t.Fatalf("expected the inner token unchanged without a switch header, got %q", token.UserIdentifier())
    }
}

func TestImpersonation_UnknownTargetStaysAdmin(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch})

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest("ghost"))

    if "admin-1" != token.UserIdentifier() {
        t.Fatalf("expected admin to stay when the target is unknown, got %q", token.UserIdentifier())
    }
}

/* under RoleModeImpersonator the admin keeps their own roles while still acting in the target's context (visible principal stays the impersonated user), so they can view as the user without losing their own rights. */
func TestImpersonation_RoleModeImpersonatorKeepsAdminRoles(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch})

    source := NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: admin},
        Users: &mapUserResolver{usersById: map[string]securitycontract.Token{
            "user-7": NewAuthenticatedToken("user-7", []string{"ROLE_BUYER"}),
        }},
        RoleMode: RoleModeImpersonator,
    })

    token, resolveErr := source.Resolve(testRuntime(), switchRequest("user-7"))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if "user-7" != token.UserIdentifier() {
        t.Fatalf("expected to act in the target's context, got %q", token.UserIdentifier())
    }

    roles := token.Roles()
    if false == hasRole(roles, "ROLE_ADMIN") || true == hasRole(roles, "ROLE_BUYER") {
        t.Fatalf("expected the admin's own roles (not the target's) under RoleModeImpersonator, got %v", roles)
    }
}

/* the impersonation token's originating actor names the impersonated user but carries the accountable admin (and the admin's roles) as its impersonator, so an impersonation stays auditable as it flows on. */
func TestImpersonation_OnBehalfOfPropagatesImpersonator(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch})

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest("user-7"))

    actor, present := ActorFromToken(token)
    if false == present || "user-7" != actor.Identifier() {
        t.Fatalf("expected the originating actor to be the impersonated user, present=%v", present)
    }

    impersonating, isImpersonating := actor.(securitycontract.ActorImpersonating)
    if false == isImpersonating {
        t.Fatal("expected the originating actor to expose its impersonator")
    }

    impersonator, hasImpersonator := impersonating.Impersonator()
    if false == hasImpersonator || "admin-1" != impersonator.Identifier() {
        t.Fatalf("expected the admin to propagate as the impersonator, present=%v", hasImpersonator)
    }

    if false == hasRole(impersonator.Roles(), "ROLE_ADMIN") {
        t.Fatalf("expected the impersonator's roles to propagate, got %v", impersonator.Roles())
    }
}

/* end-to-end: the impersonation's originating actor — impersonated user plus accountable admin — survives serialization into the HMAC envelope and rebuild at the callee, so the admin behind a switch stays auditable across a service boundary. */
func TestImpersonation_PropagatesImpersonatorBetweenServicesOverHmac(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch})
    upstream, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest("user-7"))

    actor, present := ActorFromToken(upstream)
    if false == present {
        t.Fatal("expected the impersonation token to carry an originating actor")
    }

    signer := NewHmacEnvelopeSigner(HmacEnvelopeSignerConfig{App: "wms-service", Secrets: hmacTestSecrets()})
    headerValue, signErr := signer.Sign("POST", "/internal/orders", nil, actor)
    if nil != signErr {
        t.Fatalf("sign: %v", signErr)
    }

    downstream, _ := hmacTestSource(NewMemoryNonceGuard()).Resolve(
        testRuntime(),
        hmacRequest("POST", "/internal/orders", nil, signer.HeaderName(), headerValue),
    )
    if false == downstream.IsAuthenticated() {
        t.Fatal("expected the downstream service to authenticate")
    }

    propagated, hasActor := ActorFromToken(downstream)
    if false == hasActor || "user-7" != propagated.Identifier() {
        t.Fatalf("expected the impersonated user to propagate as the actor, present=%v", hasActor)
    }

    impersonating, isImpersonating := propagated.(securitycontract.ActorImpersonating)
    if false == isImpersonating {
        t.Fatal("expected the propagated actor to expose its impersonator across the boundary")
    }

    impersonator, hasImpersonator := impersonating.Impersonator()
    if false == hasImpersonator || "admin-1" != impersonator.Identifier() || false == hasRole(impersonator.Roles(), "ROLE_ADMIN") {
        t.Fatalf("expected the admin impersonator and roles to survive the HMAC round-trip, present=%v", hasImpersonator)
    }
}

func TestImpersonation_AnonymousCanNotSwitch(t *testing.T) {
    token, _ := impersonationSource(NewAnonymousToken()).Resolve(testRuntime(), switchRequest("user-7"))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an anonymous principal to remain anonymous")
    }

    if _, present := ImpersonatorFromToken(token); true == present {
        t.Fatal("expected no impersonation for an anonymous principal")
    }
}
