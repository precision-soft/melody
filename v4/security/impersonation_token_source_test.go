package security

import (
    "errors"
    "fmt"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
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
            "user-7": NewAuthenticatedToken("user-7", []string{"ROLE_BUYER"}, nil),
        }},
    })
}

func TestImpersonation_AdminWithSwitchRoleImpersonates(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch}, nil)

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
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN"}, nil)

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest("user-7"))

    if "admin-1" != token.UserIdentifier() {
        t.Fatalf("expected the admin token to pass through unchanged, got %q", token.UserIdentifier())
    }

    if _, present := ImpersonatorFromToken(token); true == present {
        t.Fatal("expected no impersonation when the switch role is absent")
    }
}

func TestImpersonation_NoHeaderReturnsInnerToken(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch}, nil)

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest(""))

    if "admin-1" != token.UserIdentifier() {
        t.Fatalf("expected the inner token unchanged without a switch header, got %q", token.UserIdentifier())
    }
}

/* a switch the caller WAS authorized to make (it holds the switch role) but that fails because the target is unknown must fail closed to an anonymous token, not silently keep the admin's own (broader) roles: the request was meant to run narrowed to the target, so continuing as the admin would execute it with privileges the operator believed were constrained to the target. This is distinct from the without-switch-role case above, where no narrowing was ever going to happen and the caller correctly passes through unchanged. */
func TestImpersonation_UnknownTargetFailsClosedToAnonymous(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch}, nil)

    token, _ := impersonationSource(admin).Resolve(testRuntime(), switchRequest("ghost"))

    if true == token.IsAuthenticated() {
        t.Fatalf("expected an unauthenticated token when the authorized switch target is unknown, got authenticated %q", token.UserIdentifier())
    }

    if "admin-1" == token.UserIdentifier() {
        t.Fatal("expected the admin's identity not to pass through on a failed switch")
    }
}

/* under RoleModeImpersonator the admin keeps their own roles while still acting in the target's context (visible principal stays the impersonated user), so they can view as the user without losing their own rights. */
func TestImpersonation_RoleModeImpersonatorKeepsAdminRoles(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch}, nil)

    source := NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: admin},
        Users: &mapUserResolver{usersById: map[string]securitycontract.Token{
            "user-7": NewAuthenticatedToken("user-7", []string{"ROLE_BUYER"}, nil),
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
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch}, nil)

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
    admin := NewAuthenticatedToken("admin-1", []string{"ROLE_ADMIN", securitycontract.RoleAllowedToSwitch}, nil)
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

    downstream, _ := hmacTestSource(NewMemoryNonceGuard(MemoryNonceGuardConfig{})).Resolve(
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

/* the resolver's error travels as the cause into the journal */
func TestImpersonation_ResolverErrorCarriesItsCauseIntoTheJournal(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch}, nil)
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

/* failingUserResolver answers a marked infrastructure failure — the user store being down, not a denial. */
type failingUserResolver struct{}

func (instance *failingUserResolver) ResolveImpersonatedUser(
    _ runtimecontract.Runtime,
    _ string,
) (securitycontract.Token, error) {
    return nil, exception.NewError("user store query failed", nil, exception.MarkInfrastructureFailure(exception.NewError("store is down", nil, nil)))
}

func TestImpersonation_AMarkedResolverFailureIsUnavailable(t *testing.T) {
    admin := NewAuthenticatedToken("admin-1", []string{securitycontract.RoleAllowedToSwitch}, nil)
    source := NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: admin},
        Users: &failingUserResolver{},
    })

    runtimeInstance, logger := runtimeWithRecordingLogger()

    token, resolveErr := source.Resolve(runtimeInstance, switchRequest("anyone"))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    cause := expectUnavailableToken(t, token)
    if "anyone" != exception.LogContext(cause, exception.LogContextConfig{})["target"] {
        t.Fatalf("expected the cause to name the target, got %v", exception.LogContext(cause, exception.LogContextConfig{}))
    }

    if "" != token.UserIdentifier() || 0 != len(token.Roles()) {
        t.Fatalf("expected none of the admin's identity or roles, got %q %v", token.UserIdentifier(), token.Roles())
    }

    if 0 != len(logger.records) {
        t.Fatalf("expected no record from the source, got %v", logger.records)
    }
}

func TestImpersonation_AnUnavailableInnerTokenPassesThroughUntouched(t *testing.T) {
    inner := NewUnavailableToken(exception.MarkInfrastructureFailure(errors.New("redis is down")))
    source := NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: inner},
        Users: &failingUserResolver{},
    })

    for _, request := range []httpcontract.Request{switchRequest("anyone"), switchRequest("")} {
        token, resolveErr := source.Resolve(testRuntime(), request)
        if nil != resolveErr {
            t.Fatalf("resolve: %v", resolveErr)
        }

        if securitycontract.Token(inner) != token {
            t.Fatalf("expected the inner unavailable token returned as it is, got %T", token)
        }
    }
}

/* the switch target is the caller's header, logged even for an anonymous caller, so the denial record carries it bounded: a 200 KiB header does not reach the journal whole */
func TestImpersonation_TheDeniedTargetIsBoundedInTheRecord(t *testing.T) {
    runtimeInstance, logger := runtimeWithRecordingLogger()

    source := impersonationSource(NewAnonymousToken())
    if _, resolveErr := source.Resolve(runtimeInstance, switchRequest(strings.Repeat("u", 200*1024))); nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    if 0 == len(logger.records) {
        t.Fatal("expected the denied switch recorded")
    }

    for _, record := range logger.records {
        target, _ := record.context["target"].(string)
        if 1024 < len(target) {
            t.Fatalf("expected the target bounded in the record, got %d bytes", len(target))
        }
    }
}

func hierarchyGrantingSwitch() *RoleHierarchy {
    return NewRoleHierarchy(map[string][]string{"ROLE_MANAGER": {securitycontract.RoleAllowedToSwitch}})
}

func managerSwitchesThrough(t *testing.T, source *ImpersonationTokenSource) bool {
    t.Helper()

    token, resolveErr := source.Resolve(testRuntime(), switchRequest("user-7"))
    if nil != resolveErr {
        t.Fatalf("resolve: %v", resolveErr)
    }

    return "user-7" == token.UserIdentifier()
}

func managerSource(hierarchy *RoleHierarchy) *ImpersonationTokenSource {
    return NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner: &fixedTokenSource{token: NewAuthenticatedToken("manager-1", []string{"ROLE_MANAGER"}, nil)},
        Users: &mapUserResolver{usersById: map[string]securitycontract.Token{
            "user-7": NewAuthenticatedToken("user-7", []string{"ROLE_BUYER"}, nil),
        }},
        RoleHierarchy: hierarchy,
    })
}

func TestImpersonation_NothingBoundReadsTheRawRoles(t *testing.T) {
    if true == managerSwitchesThrough(t, managerSource(nil)) {
        t.Fatalf("expected the raw roles to refuse a switch role held only through a hierarchy")
    }
}

func TestImpersonation_TheBoundHierarchyGrantsTheSwitch(t *testing.T) {
    source := managerSource(nil)

    if bindErr := source.BindRoleHierarchy("api", hierarchyGrantingSwitch()); nil != bindErr {
        t.Fatalf("bind: %v", bindErr)
    }

    if false == managerSwitchesThrough(t, source) {
        t.Fatalf("expected the bound hierarchy to grant the switch role")
    }
}

func TestImpersonation_TheExplicitHierarchyWinsOverTheBind(t *testing.T) {
    source := managerSource(NewRoleHierarchy(map[string][]string{"ROLE_OTHER": {"ROLE_ELSE"}}))

    if bindErr := source.BindRoleHierarchy("api", hierarchyGrantingSwitch()); nil != bindErr {
        t.Fatalf("bind: %v", bindErr)
    }

    if true == managerSwitchesThrough(t, source) {
        t.Fatalf("expected the explicit hierarchy to decide over the bound one")
    }

    if bindErr := source.BindRoleHierarchy("admin", NewRoleHierarchy(nil)); nil != bindErr {
        t.Fatalf("expected an explicit source to record no bind, got %v", bindErr)
    }
}

func TestImpersonation_TheSameHierarchyBindsTwice(t *testing.T) {
    source := managerSource(nil)
    hierarchy := hierarchyGrantingSwitch()

    for _, firewallName := range []string{"api", "admin"} {
        if bindErr := source.BindRoleHierarchy(firewallName, hierarchy); nil != bindErr {
            t.Fatalf("bind %s: %v", firewallName, bindErr)
        }
    }

    withoutHierarchy := managerSource(nil)
    for _, firewallName := range []string{"api", "admin"} {
        if bindErr := withoutHierarchy.BindRoleHierarchy(firewallName, nil); nil != bindErr {
            t.Fatalf("bind nil %s: %v", firewallName, bindErr)
        }
    }
}

func TestImpersonation_ADifferentHierarchyIsRefusedNamingBothFirewalls(t *testing.T) {
    first := hierarchyGrantingSwitch()
    second := hierarchyGrantingSwitch()

    for _, testCase := range []struct {
        name   string
        bound  securitycontract.RoleHierarchy
        second securitycontract.RoleHierarchy
    }{
        {name: "two hierarchies", bound: first, second: second},
        {name: "nil, then a hierarchy", bound: nil, second: second},
        {name: "a hierarchy, then nil", bound: first, second: nil},
    } {
        source := managerSource(nil)

        if bindErr := source.BindRoleHierarchy("api", testCase.bound); nil != bindErr {
            t.Fatalf("%s: first bind: %v", testCase.name, bindErr)
        }

        bindErr := source.BindRoleHierarchy("admin", testCase.second)
        expectErrorNamingField(t, bindErr, "ImpersonationTokenSourceConfig.RoleHierarchy")

        logContext := exception.LogContext(bindErr, exception.LogContextConfig{})
        if "admin" != logContext["firewallName"] || "api" != logContext["boundFirewallName"] {
            t.Fatalf("%s: expected both firewalls named, got %v", testCase.name, logContext)
        }
    }
}

func TestImpersonation_ARefusedBindKeepsTheFirstHierarchy(t *testing.T) {
    source := managerSource(nil)

    if bindErr := source.BindRoleHierarchy("api", hierarchyGrantingSwitch()); nil != bindErr {
        t.Fatalf("bind: %v", bindErr)
    }

    if bindErr := source.BindRoleHierarchy("admin", nil); nil == bindErr {
        t.Fatalf("expected the second bind refused")
    }

    if false == managerSwitchesThrough(t, source) {
        t.Fatalf("expected the first hierarchy to keep deciding")
    }
}

func TestImpersonation_ATypedNilHierarchyBindsAsNil(t *testing.T) {
    var typedNil *RoleHierarchy

    source := managerSource(nil)

    if bindErr := source.BindRoleHierarchy("api", typedNil); nil != bindErr {
        t.Fatalf("bind: %v", bindErr)
    }

    if true == managerSwitchesThrough(t, source) {
        t.Fatalf("expected a typed nil to read the raw roles")
    }

    if bindErr := source.BindRoleHierarchy("admin", nil); nil != bindErr {
        t.Fatalf("expected a typed nil and a nil to be the same hierarchy, got %v", bindErr)
    }
}

type uncomparableRoleHierarchy struct {
    inherited []string
}

func (instance uncomparableRoleHierarchy) ExpandRoles(roles []string) []string {
    return append(append([]string{}, roles...), instance.inherited...)
}

func TestImpersonation_AnUncomparableHierarchyIsDifferentAndNeverPanics(t *testing.T) {
    source := managerSource(nil)
    hierarchy := uncomparableRoleHierarchy{inherited: []string{securitycontract.RoleAllowedToSwitch}}

    if bindErr := source.BindRoleHierarchy("api", hierarchy); nil != bindErr {
        t.Fatalf("bind: %v", bindErr)
    }

    if false == managerSwitchesThrough(t, source) {
        t.Fatalf("expected the custom hierarchy to grant the switch role")
    }

    expectErrorNamingField(t, source.BindRoleHierarchy("admin", hierarchy), "ImpersonationTokenSourceConfig.RoleHierarchy")
}

type bindingTokenSource struct {
    fixedTokenSource
    firewallName string
    hierarchy    securitycontract.RoleHierarchy
    failure      error
}

func (instance *bindingTokenSource) BindRoleHierarchy(firewallName string, hierarchy securitycontract.RoleHierarchy) error {
    instance.firewallName = firewallName
    instance.hierarchy = hierarchy

    return instance.failure
}

func TestImpersonation_TheBindReachesABinderInner(t *testing.T) {
    inner := &bindingTokenSource{}
    hierarchy := hierarchyGrantingSwitch()

    explicit := NewImpersonationTokenSource(ImpersonationTokenSourceConfig{
        Inner:         inner,
        Users:         &mapUserResolver{},
        RoleHierarchy: NewRoleHierarchy(nil),
    })

    if bindErr := explicit.BindRoleHierarchy("api", hierarchy); nil != bindErr {
        t.Fatalf("bind: %v", bindErr)
    }

    if "api" != inner.firewallName || securitycontract.RoleHierarchy(hierarchy) != inner.hierarchy {
        t.Fatalf("expected the bind forwarded to the inner source, got %q", inner.firewallName)
    }

    inner.failure = errors.New("the inner source refuses")

    if bindErr := explicit.BindRoleHierarchy("api", hierarchy); false == errors.Is(bindErr, inner.failure) {
        t.Fatalf("expected the inner refusal answered, got %v", bindErr)
    }
}

func TestImpersonation_ConcurrentBindsAgreeOnOneWinner(t *testing.T) {
    source := managerSource(nil)

    results := make([]error, 16)
    var waitGroup sync.WaitGroup
    for index := range results {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            results[index] = source.BindRoleHierarchy(fmt.Sprintf("firewall-%d", index), hierarchyGrantingSwitch())
        }()
    }
    waitGroup.Wait()

    admitted := 0
    for _, result := range results {
        if nil == result {
            admitted++
        }
    }

    if 1 != admitted {
        t.Fatalf("expected exactly one of the different hierarchies bound, got %d", admitted)
    }
}
