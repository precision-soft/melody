package security

import (
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

/* unauthenticatedRoledToken carries roles while answering IsAuthenticated false — the shape a "remembered" or half-logged-in token takes. It is what proves the voters refuse a token that reports roles it has not authenticated for; the in-package tokens never combine the two. */
type unauthenticatedRoledToken struct {
    roles []string
}

func (instance *unauthenticatedRoledToken) UserIdentifier() string     { return "u1" }
func (instance *unauthenticatedRoledToken) Roles() []string            { return instance.roles }
func (instance *unauthenticatedRoledToken) IsAuthenticated() bool      { return false }
func (instance *unauthenticatedRoledToken) Scope() map[string]any      { return map[string]any{} }
func (instance *unauthenticatedRoledToken) Attributes() map[string]any { return map[string]any{} }

var _ securitycontract.Token = (*unauthenticatedRoledToken)(nil)

func TestRoleVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    voter := NewRoleVoter()

    result := voter.Vote(&unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_ADMIN", nil)

    if securitycontract.VoteDenied != result {
        t.Fatalf("expected an unauthenticated token carrying the role to be denied, got %v", result)
    }
}

func TestRoleHierarchyVoter_DeniesWhenTokenNotAuthenticated(t *testing.T) {
    hierarchy := NewRoleHierarchy(map[string][]string{"ROLE_ADMIN": {"ROLE_USER"}})
    voter := NewRoleHierarchyVoter(hierarchy, NewRoleVoter())

    result := voter.Vote(&unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}, "ROLE_USER", nil)

    if securitycontract.VoteDenied != result {
        t.Fatalf("expected an unauthenticated token carrying the role to be denied, got %v", result)
    }
}

func TestNewApiKeyHeaderRule_NilMatcherPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(nil, "X-Api-Key", "secret")
    }, "api key header rule matcher is nil")
}

func TestNewApiKeyHeaderRule_TypedNilMatcherPanics(t *testing.T) {
    var typedNilMatcher *PathPrefixMatcher

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewApiKeyHeaderRule(typedNilMatcher, "X-Api-Key", "secret")
    }, "api key header rule matcher is nil")
}

func TestAccessDecisionManager_DecideAllRefusesAnEmptyAttributeList(t *testing.T) {
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter())

    err := manager.DecideAll(NewAuthenticatedToken("u1", []string{"ROLE_ADMIN"}), []string{}, nil)
    if nil == err {
        t.Fatalf("expected DecideAll over an empty attribute list to refuse, as DecideAny does")
    }
}

func TestNewAccessDecisionManager_CopiesTheCallersVoters(t *testing.T) {
    voters := []securitycontract.Voter{NewRoleVoter()}
    manager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, voters...)

    voters[0] = &securityTestVoter{attribute: "ROLE_X", result: securitycontract.VoteGranted}

    if _, isRoleVoter := manager.Voters()[0].(*RoleVoter); false == isRoleVoter {
        t.Fatalf("expected the manager to keep its own copy of the voters, not the caller's mutated slice")
    }
}

func TestNewFirewall_TypedNilRulePanics(t *testing.T) {
    var typedNilRule *ApiKeyHeaderRule

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewFirewall(typedNilRule)
    }, "security firewall rule is nil")
}

func TestNewAuthenticatorManager_TypedNilAuthenticatorPanics(t *testing.T) {
    var typedNilAuthenticator *ApiKeyHeaderAuthenticator

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewAuthenticatorManager(typedNilAuthenticator)
    }, "authenticator at index 0 is nil")
}

func TestNewToken_TypedNilPanics(t *testing.T) {
    var typedNilToken *AuthenticatedToken

    testhelper.AssertPanicsWithError(t, func() {
        _ = NewToken(typedNilToken)
    }, "can not create a security token from nil")
}

func TestSecurityContext_IsGranted_TypedNilTokenAnswersFalse(t *testing.T) {
    firewall := NewCompiledFirewall(
        "main", NewPathPrefixMatcher("/"), "", nil, NewResolverTokenSource(func(request httpcontract.Request) securitycontract.Token { return nil }),
        NewAccessControl(), NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter()), nil, nil, nil,
        "", "", nil, nil,
        SourceNone, SourceFirewall, SourceFirewall, SourceNone, SourceNone,
    )

    var typedNilToken *AuthenticatedToken
    securityContext := NewSecurityContext(firewall, typedNilToken)

    if true == securityContext.IsGranted("ROLE_ADMIN") {
        t.Fatalf("expected IsGranted to answer false for a typed-nil token rather than dereference it")
    }
}
