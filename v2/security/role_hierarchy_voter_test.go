package security

import (
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v2/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
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

/* tenantOwnedToken stands in for an integrator's own token: it carries what melody's contract does not, and it answers its own twin so the expansion cannot cost it its type */
type tenantOwnedToken struct {
    userIdentifier string
    roles          []string
    tenantId       string
}

func (instance *tenantOwnedToken) IsAuthenticated() bool {
    return true
}

func (instance *tenantOwnedToken) UserIdentifier() string {
    return instance.userIdentifier
}

func (instance *tenantOwnedToken) Roles() []string {
    return append([]string{}, instance.roles...)
}

func (instance *tenantOwnedToken) TenantId() string {
    return instance.tenantId
}

func (instance *tenantOwnedToken) WithRoles(roles []string) securitycontract.Token {
    twin := *instance
    twin.roles = append([]string{}, roles...)

    return &twin
}

/* tenantDenyVoter is the shape a voter of one's own has: it asserts the concrete token to learn whose data is being asked for, refuses a mismatch, and abstains on a token it cannot recognise at all */
type tenantDenyVoter struct {
    expectedTenantId string
    sawTenantToken   bool
    observedRoles    []string
}

func (instance *tenantDenyVoter) Supports(attribute string, subject any) bool {
    return true
}

func (instance *tenantDenyVoter) Vote(token securitycontract.Token, attribute string, subject any) securitycontract.VoteResult {
    tenantToken, isTenantToken := token.(*tenantOwnedToken)
    if false == isTenantToken {
        return securitycontract.VoteAbstain
    }

    instance.sawTenantToken = true
    instance.observedRoles = token.Roles()

    if instance.expectedTenantId != tenantToken.TenantId() {
        return securitycontract.VoteDenied
    }

    return securitycontract.VoteGranted
}

/*
TestRoleHierarchyVoter_ATokenAnsweringItsOwnTwinKeepsItsType pins what the
widened delegate is FOR. The wrapper used to hand the delegate a rebuilt
AuthenticatedToken, so an integrator's voter — the multi-tenant one the
constructor's own GoDoc invites — asserted its token type against melody's and
failed on every request: a voter that would have REFUSED abstained instead, and
under the affirmative strategy beside a granting role voter the request was
granted. The token answers its own twin now, so the assertion still holds and
the delegate still sees the expanded roles.
*/
func TestRoleHierarchyVoter_ATokenAnsweringItsOwnTwinKeepsItsType(t *testing.T) {
    hierarchy := NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})

    delegate := &tenantDenyVoter{expectedTenantId: "acme"}

    voter := NewRoleHierarchyVoter(hierarchy, delegate)

    result := voter.Vote(
        &tenantOwnedToken{userIdentifier: "u1", roles: []string{"ROLE_ADMIN"}, tenantId: "evil-corp"},
        "ROLE_USER",
        nil,
    )

    if securitycontract.VoteDenied != result {
        t.Fatalf("expected the tenant refusal to survive the expansion, got %v", result)
    }

    if false == delegate.sawTenantToken {
        t.Fatalf("expected the delegate to keep receiving its own token type")
    }

    if 2 != len(delegate.observedRoles) {
        t.Fatalf("expected the delegate to see the expanded roles, got %v", delegate.observedRoles)
    }
}

/*
TestRoleHierarchyVoter_ATenantRefusalStillDecidesBesideAGrantingRoleVoter is
where the rebuild's cost is paid in access rather than in diagnosis. A refusal
turned into an abstention is invisible to every strategy that counts denials:
under unanimous a single denial refuses, and an abstention beside a granting
role voter does not, so the request the tenant rule was written to withhold was
granted. The affirmative strategy is the one place this does NOT show — any
grant wins there whatever else was voted — so the strategy has to be one that
lets a refusal decide for the guard to be proven at all.
*/
func TestRoleHierarchyVoter_ATenantRefusalStillDecidesBesideAGrantingRoleVoter(t *testing.T) {
    hierarchy := NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})

    manager := NewAccessDecisionManager(
        securitycontract.DecisionStrategyUnanimous,
        NewRoleHierarchyVoter(hierarchy, &tenantDenyVoter{expectedTenantId: "acme"}),
        NewRoleHierarchyVoter(hierarchy, NewRoleVoter()),
    )

    decisionErr := manager.DecideAll(
        &tenantOwnedToken{userIdentifier: "u1", roles: []string{"ROLE_ADMIN"}, tenantId: "evil-corp"},
        []string{"ROLE_USER"},
        nil,
    )

    if nil == decisionErr {
        t.Fatalf("expected the tenant refusal to decide the request")
    }
}

/* twinAnsweringNilToken is the token that opts into the capability and answers nothing: the delegate must not be handed a nil token, which would deny every request the hierarchy exists to widen */
type twinAnsweringNilToken struct {
    roles []string
}

func (instance twinAnsweringNilToken) IsAuthenticated() bool {
    return true
}

func (instance twinAnsweringNilToken) UserIdentifier() string {
    return "u1"
}

func (instance twinAnsweringNilToken) Roles() []string {
    return append([]string{}, instance.roles...)
}

func (instance twinAnsweringNilToken) WithRoles(roles []string) securitycontract.Token {
    return nil
}

func TestRoleHierarchyVoter_ATwinAnsweredAsNilFallsBackToTheRebuild(t *testing.T) {
    hierarchy := NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})

    voter := NewRoleHierarchyVoter(hierarchy, NewRoleVoter())

    result := voter.Vote(twinAnsweringNilToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_USER", nil)

    if securitycontract.VoteGranted != result {
        t.Fatalf("expected the rebuild to answer for a token that answers no twin, got %v", result)
    }
}

/* a token that does not carry the capability keeps the rebuild it always got, so nothing the framework ships changes shape */
func TestRoleHierarchyVoter_ATokenWithoutTheCapabilityKeepsTheRebuild(t *testing.T) {
    hierarchy := NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})

    delegate := &tenantProbeVoter{}

    voter := NewRoleHierarchyVoter(hierarchy, delegate)

    if securitycontract.VoteGranted != voter.Vote(plainRoledToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_USER", nil) {
        t.Fatalf("expected the expansion to reach the delegate through the rebuild")
    }
}

/* plainRoledToken carries no capability and no type the delegate reads */
type plainRoledToken struct {
    roles []string
}

func (instance plainRoledToken) IsAuthenticated() bool {
    return true
}

func (instance plainRoledToken) UserIdentifier() string {
    return "u1"
}

func (instance plainRoledToken) Roles() []string {
    return append([]string{}, instance.roles...)
}
