package security

import (
    "testing"

    "github.com/precision-soft/melody/v2/internal/testhelper"
)

func TestNewSecurityContext_NilFirewallPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewSecurityContext(nil, &securityTestToken{roles: []string{"ROLE_USER"}})
        },
        "the firewall is required for the security context",
    )
}

/* the typed-nil shape reaches the same refusal: a nil *CompiledFirewall handed over as the parameter is not `nil ==` once it is inside the guard's reflective check */
func TestNewSecurityContext_TypedNilFirewallPanics(t *testing.T) {
    var typedNilFirewall *CompiledFirewall

    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewSecurityContext(typedNilFirewall, &securityTestToken{roles: []string{"ROLE_USER"}})
        },
        "the firewall is required for the security context",
    )
}

/* the context copies the five sources and the matcher description off the firewall at construction, so a debug panel reads what was decided at compile time rather than re-deriving it */
func TestNewSecurityContext_CarriesTheFirewallSources(t *testing.T) {
    firewall := NewCompiledFirewall(
        "main",
        NewPathPrefixMatcher("/admin"),
        "prefix /admin",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        SourceGlobal,
        SourceFirewall,
        SourceMerged,
        SourceNone,
        SourceGlobal,
    )

    securityContext := NewSecurityContext(firewall, nil)

    if SourceGlobal != securityContext.RoleHierarchySource() {
        t.Fatalf("expected the role hierarchy source to be global, got %v", securityContext.RoleHierarchySource())
    }

    if SourceFirewall != securityContext.AccessDecisionManagerSource() {
        t.Fatalf("expected the decision manager source to be firewall, got %v", securityContext.AccessDecisionManagerSource())
    }

    if SourceMerged != securityContext.AccessControlSource() {
        t.Fatalf("expected the access control source to be merged, got %v", securityContext.AccessControlSource())
    }

    if SourceNone != securityContext.EntryPointSource() {
        t.Fatalf("expected the entry point source to be none, got %v", securityContext.EntryPointSource())
    }

    if SourceGlobal != securityContext.AccessDeniedHandlerSource() {
        t.Fatalf("expected the access denied handler source to be global, got %v", securityContext.AccessDeniedHandlerSource())
    }

    if "prefix /admin" != securityContext.MatchedFirewallMatcher() {
        t.Fatalf("expected the matcher description to be carried, got %q", securityContext.MatchedFirewallMatcher())
    }

    if firewall != securityContext.Firewall() {
        t.Fatalf("expected the context to keep the firewall it was built from")
    }
}

func TestSecurityContext_MatchedRuleRoundTrips(t *testing.T) {
    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", nil),
        nil,
    )

    if nil != securityContext.MatchedRule() {
        t.Fatalf("expected no matched rule before one is set")
    }

    matchedRule := NewMatchedAccessControlRule("/admin", []string{"ROLE_ADMIN"}, SourceFirewall, 0, "main")
    securityContext.SetMatchedRule(matchedRule)

    if matchedRule != securityContext.MatchedRule() {
        t.Fatalf("expected the matched rule that was set to be answered")
    }
}

/* without a token there is nobody to grant anything to: the answer is not-granted rather than a dereference of the missing token */
func TestSecurityContext_IsGrantedRefusesWithoutAToken(t *testing.T) {
    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", nil),
        nil,
    )

    if true == securityContext.IsGranted("ROLE_USER") {
        t.Fatalf("expected a context without a token to grant nothing")
    }
}

/* with no hierarchy configured the roles the token carries are the whole answer */
func TestSecurityContext_IsGrantedReadsTheRawRolesWithoutAHierarchy(t *testing.T) {
    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", nil),
        &securityTestToken{roles: []string{"ROLE_EDITOR"}},
    )

    if false == securityContext.IsGranted("ROLE_EDITOR") {
        t.Fatalf("expected the role the token carries to be granted")
    }

    if true == securityContext.IsGranted("ROLE_ADMIN") {
        t.Fatalf("expected a role the token does not carry to be refused")
    }
}

/* with a hierarchy the inherited roles count too: ROLE_ADMIN inheriting ROLE_EDITOR grants ROLE_EDITOR to a token that names only ROLE_ADMIN. Without expansion this reads exactly like the raw-roles case above, which is why both are pinned. */
func TestSecurityContext_IsGrantedExpandsThroughTheHierarchy(t *testing.T) {
    roleHierarchy := NewRoleHierarchy(map[string][]string{
        "ROLE_ADMIN": {"ROLE_EDITOR"},
    })

    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", roleHierarchy),
        &securityTestToken{roles: []string{"ROLE_ADMIN"}},
    )

    if false == securityContext.IsGranted("ROLE_EDITOR") {
        t.Fatalf("expected the inherited role to be granted through the hierarchy")
    }

    if true == securityContext.IsGranted("ROLE_OTHER") {
        t.Fatalf("expected an unrelated role to be refused")
    }
}

/* an unauthenticated token is still read for its roles here: authentication is weighed by the voters, and the context is the cheap read a handler makes. Pinned so the difference from RoleVoter, which refuses exactly this token, stays deliberate. */
func TestSecurityContext_IsGrantedReadsAnUnauthenticatedTokensRoles(t *testing.T) {
    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", nil),
        unauthenticatedRoledToken{roles: []string{"ROLE_EDITOR"}},
    )

    if false == securityContext.IsGranted("ROLE_EDITOR") {
        t.Fatalf("expected the context to answer on the roles the token declares")
    }
}

/* the empty role is nobody's role: without this refusal a caller passing a role name that a configuration value resolved to empty would be granted by any token whose role list happened to contain an empty entry */
func TestSecurityContext_IsGrantedRefusesTheEmptyRole(t *testing.T) {
    securityContext := NewSecurityContext(
        newTestCompiledFirewallWithRoleHierarchy("main", nil),
        &securityTestToken{roles: []string{"", "ROLE_EDITOR"}},
    )

    if true == securityContext.IsGranted("") {
        t.Fatalf("expected the empty role to be refused even when the token carries an empty entry")
    }
}

func TestHasRole_MatchesExactlyAndRefusesTheEmptyRole(t *testing.T) {
    if false == hasRole([]string{"ROLE_A", "ROLE_B"}, "ROLE_B") {
        t.Fatalf("expected a role in the list to match")
    }

    if true == hasRole([]string{"ROLE_A"}, "ROLE_B") {
        t.Fatalf("expected a role outside the list to be refused")
    }

    if true == hasRole([]string{"ROLE_ADMIN"}, "role_admin") {
        t.Fatalf("expected the comparison to be case sensitive")
    }

    if true == hasRole([]string{""}, "") {
        t.Fatalf("expected the empty role to be refused")
    }

    if true == hasRole(nil, "ROLE_A") {
        t.Fatalf("expected an absent role list to grant nothing")
    }
}

/* AuthenticatedToken.IsAuthenticated answers true without touching its receiver and Roles dereferences it,
so a typed nil read as a live token is granted a hearing it cannot survive. */
func TestSecurityContext_IsGrantedReadsATypedNilTokenAsAbsent(t *testing.T) {
    firewall := NewCompiledFirewall(
        "main",
        NewPathPrefixMatcher("/admin"),
        "prefix /admin",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    var unassignedToken *AuthenticatedToken

    securityContext := NewSecurityContext(firewall, unassignedToken)

    if true == securityContext.IsGranted("ROLE_ADMIN") {
        t.Fatalf("expected a typed-nil token to grant nothing")
    }
}
