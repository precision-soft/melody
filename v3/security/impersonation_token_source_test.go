package security

import (
    "fmt"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

func TestImpersonation_UnknownTargetFailsClosedToAnonymous(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch})

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest("ghost"))

    if true == token.IsAuthenticated() {
        t.Fatalf("expected an unauthenticated token when the authorized switch target is unknown, got authenticated %q", token.UserIdentifier())
    }

    if "admin-1" == token.UserIdentifier() {
        t.Fatal("expected the admin's identity not to pass through on a failed switch")
    }
}

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

func TestImpersonation_ResolverErrorCarriesItsCauseIntoTheJournal(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch})
    source := NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: admin},
        Users: &mapUserResolver{usersById: map[string]securitycontract.Token{}},
    })

    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, switchRequest("ghost"))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if true == token.IsAuthenticated() {
        t.Fatal("a failed target resolution must fail closed to anonymous")
    }

    infoRecords := logger.recordsAtLevel(loggingcontract.LevelInfo)
    if 1 != len(infoRecords) {
        t.Fatalf("a resolver error is a denial by contract and must be filed once at Info, got %d records", len(infoRecords))
    }

    rendered := fmt.Sprintf("%v", infoRecords[0].context)
    if false == strings.Contains(rendered, "user not found") {
        t.Fatalf("the resolver's own error must travel with the record, got: %s", rendered)
    }
}

type failingUserResolver struct{}

func (instance *failingUserResolver) ResolveImpersonatedUser(
    _ runtimecontract.Runtime,
    _ string,
) (securitycontract.Token, error) {
    return nil, exception.NewError("user store query failed", nil, markInfrastructureFailure(exception.NewError("store is down", nil, nil)))
}

func TestImpersonation_MarkedResolverFailureLogsAtError(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch})
    source := NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: admin},
        Users: &failingUserResolver{},
    })

    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, switchRequest("anyone"))
    if nil != resolveErr || true == token.IsAuthenticated() {
        t.Fatalf("a failed resolution must fail closed to anonymous: authenticated=%v err=%v", token.IsAuthenticated(), resolveErr)
    }

    if 1 != len(logger.recordsAtLevel(loggingcontract.LevelError)) {
        t.Fatal("a resolver failure carrying the infrastructure mark degrades every impersonated request to anonymous and must be filed at Error")
    }
}
