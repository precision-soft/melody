package security

import (
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestSecurityContext_IsGranted_TypedNilTokenAnswersFalse(t *testing.T) {
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        TokenSource:                 NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) { return nil, nil }, ResolverTokenSourceConfig{}),
        AccessControl:               NewAccessControl(nil),
        AccessDecisionManager:       NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{NewRoleVoter()}, AccessDecisionManagerConfig{}),
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    var typedNilToken *AuthenticatedToken
    securityContext := NewSecurityContext(firewall, typedNilToken)

    if true == securityContext.IsGranted("ROLE_ADMIN") {
        t.Fatalf("expected IsGranted to answer false for a typed-nil token rather than dereference it")
    }
}

/* the two doors that answer "may this token do X" have to answer alike. The voters refuse a token that
reports roles while answering IsAuthenticated false — the shape a remembered or half-logged-in token takes —
and IsGranted, which the application calls directly from a handler to branch on privilege, has to refuse it
for the same reason. Without this the firewall denies the route and a handler that branches on IsGranted
renders the privileged content behind it. */
func TestSecurityContext_IsGranted_RefusesAnUnauthenticatedTokenTheVotersRefuse(t *testing.T) {
    token := &unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}

    accessDecisionManager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{NewRoleVoter()}, AccessDecisionManagerConfig{})

    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:                        "main",
        Matcher:                     NewPathPrefixMatcher("/"),
        TokenSource:                 NewResolverTokenSource(func(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) { return token, nil }, ResolverTokenSourceConfig{}),
        AccessControl:               NewAccessControl(nil),
        AccessDecisionManager:       accessDecisionManager,
        AccessDecisionManagerSource: SourceFirewall,
        AccessControlSource:         SourceFirewall,
    })

    if nil == accessDecisionManager.DecideAll(testRuntime(), token, []string{"ROLE_ADMIN"}, nil) {
        t.Fatalf("the premise of this test is gone: the voters now grant ROLE_ADMIN to a token answering IsAuthenticated false")
    }

    securityContext := NewSecurityContext(firewall, token)

    if true == securityContext.IsGranted("ROLE_ADMIN") {
        t.Fatalf("expected IsGranted to refuse ROLE_ADMIN for a token answering IsAuthenticated false, the same answer the voters give it")
    }
}
