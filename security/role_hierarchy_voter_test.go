package security

import (
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

func TestRoleHierarchyVoter_PanicsOnNilRoleHierarchy(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRoleHierarchyVoter(nil, NewRoleVoter())
        },
        "the role hierarchy is nil for role hierarchy voter",
    )
}

/* the second dependency is refused by its own message: a nil delegate is dereferenced by Supports on the request path, and only a message-pinned assertion tells the two refusals apart */
func TestRoleHierarchyVoter_PanicsOnNilDelegate(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewRoleHierarchyVoter(NewRoleHierarchy(nil), nil)
        },
        "the delegate is nil for role hierarchy voter",
    )
}

/* Supports forwards to the delegate rather than answering on its own, so a decision manager sees one consistent answer whether or not a hierarchy was configured */
func TestRoleHierarchyVoter_SupportsForwardsToTheDelegate(t *testing.T) {
    voter := NewRoleHierarchyVoter(NewRoleHierarchy(nil), NewRoleVoter())

    if false == voter.Supports("ROLE_A", nil) {
        t.Fatalf("expected the hierarchy voter to support what its delegate supports")
    }
}

func TestRoleHierarchyVoter_DeniesANilToken(t *testing.T) {
    voter := NewRoleHierarchyVoter(NewRoleHierarchy(nil), NewRoleVoter())

    result := voter.Vote(nil, "ROLE_A", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied, got %v", result)
    }
}

/* the expansion does not invent roles: a token holding only the inherited role is not granted the inheriting one */
func TestRoleHierarchyVoter_DoesNotExpandUpwards(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_ADMIN": {"ROLE_USER"},
        },
    )

    voter := NewRoleHierarchyVoter(hierarchy, NewRoleVoter())

    result := voter.Vote(NewAuthenticatedToken("u1", []string{"ROLE_USER"}), "ROLE_ADMIN", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected the inheritance to run one way only, got %v", result)
    }
}

func TestRoleHierarchyVoter_ExpandsRolesBeforeVoting(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_ADMIN": {"ROLE_USER"},
        },
    )

    delegate := NewRoleVoter()
    voter := NewRoleHierarchyVoter(hierarchy, delegate)

    token := NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"})

    result := voter.Vote(token, "ROLE_USER", nil)
    if securitycontract.VoteGranted != result {
        t.Fatalf("expected granted")
    }
}

/* an unauthenticated token is denied before its roles are expanded: the earlier code rebuilt it with an empty identity but kept the roles, which the delegate reads and never checks, so the neutralisation was a no-op */
func TestRoleHierarchyVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    hierarchy := NewRoleHierarchy(
        map[string][]string{
            "ROLE_ADMIN": {"ROLE_USER"},
        },
    )

    voter := NewRoleHierarchyVoter(hierarchy, NewRoleVoter())

    result := voter.Vote(unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_USER", nil)
    if securitycontract.VoteDenied != result {
        t.Fatalf("expected denied")
    }
}

/*
TestNewRoleHierarchyVoter_TakesAnyVoterAsItsDelegate pins the widened
parameter. The constructor took *RoleVoter, so an integrator's own voter —
multi-tenant, ownership — could not be handed the expanded roles at all and did
not even compile against the door; the only way out was copying the wrapper,
which meant every foreign voter reimplementing the expansion rule. The wrapper
calls nothing but Supports and Vote, so the narrowing bought nothing.
*/
func TestNewRoleHierarchyVoter_TakesAnyVoterAsItsDelegate(t *testing.T) {
    delegate := &tenantProbeVoter{}

    voter := NewRoleHierarchyVoter(
        NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}}),
        delegate,
    )

    token := NewAuthenticatedToken("admin", []string{"ROLE_ADMIN"})

    if securitycontract.VoteGranted != voter.Vote(token, "ROLE_USER", nil) {
        t.Fatal("expected the foreign delegate to see the expanded roles")
    }

    if false == delegate.sawExpandedRoles {
        t.Fatalf("expected the delegate to be handed the expanded roles, saw %v", delegate.observedRoles)
    }
}

/*
TestNewRoleHierarchyVoter_RefusesATypedNilDelegate pins the guard the plain
comparison cannot perform now that the parameter is an interface: a
(*tenantProbeVoter)(nil) reads as non-nil against nil and would dereference its
own nil receiver on the first vote of the request path, which is the wrong
place to learn about a wiring fault.
*/
func TestNewRoleHierarchyVoter_RefusesATypedNilDelegate(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected the typed-nil delegate to be refused at construction")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "the delegate is nil") {
            t.Fatalf("expected the refusal to name the delegate, got %v", recovered)
        }
    }()

    NewRoleHierarchyVoter(
        NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}}),
        (*tenantProbeVoter)(nil),
    )
}

/* tenantProbeVoter stands in for an integrator's own voter: it grants what it is asked for and records the roles it was handed, so the expansion reaching it is observable */
type tenantProbeVoter struct {
    observedRoles     []string
    sawExpandedRoles  bool
}

func (instance *tenantProbeVoter) Supports(attribute string, subject any) bool {
    return true
}

func (instance *tenantProbeVoter) Vote(token securitycontract.Token, attribute string, subject any) securitycontract.VoteResult {
    instance.observedRoles = token.Roles()

    for _, role := range instance.observedRoles {
        if attribute == role {
            instance.sawExpandedRoles = true

            return securitycontract.VoteGranted
        }
    }

    return securitycontract.VoteDenied
}
