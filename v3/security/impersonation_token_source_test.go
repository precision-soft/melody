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

func TestImpersonation_AnonymousCanNotSwitch(t *testing.T) {
    token, _ := impersonationSource(NewAnonymousToken()).Resolve(testRuntime(), switchRequest("user-7"))

    if true == token.IsAuthenticated() {
        t.Fatal("expected an anonymous principal to remain anonymous")
    }

    if _, present := ImpersonatorFromToken(token); true == present {
        t.Fatal("expected no impersonation for an anonymous principal")
    }
}
