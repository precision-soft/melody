package security

import (
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

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

/* the two doors that answer "may this token do X" have to answer alike. The voters refuse a token that
reports roles while answering IsAuthenticated false — the shape a remembered or half-logged-in token takes —
and IsGranted, which the application calls directly from a handler to branch on privilege, has to refuse it
for the same reason. Without this the firewall denies the route and a handler that branches on IsGranted
renders the privileged content behind it. */
func TestSecurityContext_IsGranted_RefusesAnUnauthenticatedTokenTheVotersRefuse(t *testing.T) {
    token := &unauthenticatedRoledToken{roles: []string{"ROLE_ADMIN"}}

    accessDecisionManager := NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, NewRoleVoter())

    firewall := NewCompiledFirewall(
        "main", NewPathPrefixMatcher("/"), "", nil, NewResolverTokenSource(func(request httpcontract.Request) securitycontract.Token { return token }),
        NewAccessControl(), accessDecisionManager, nil, nil, nil,
        "", "", nil, nil,
        SourceNone, SourceFirewall, SourceFirewall, SourceNone, SourceNone,
    )

    if nil == accessDecisionManager.DecideAll(token, []string{"ROLE_ADMIN"}, nil) {
        t.Fatalf("the premise of this test is gone: the voters now grant ROLE_ADMIN to a token answering IsAuthenticated false")
    }

    securityContext := NewSecurityContext(firewall, token)

    if true == securityContext.IsGranted("ROLE_ADMIN") {
        t.Fatalf("expected IsGranted to refuse ROLE_ADMIN for a token answering IsAuthenticated false, the same answer the voters give it")
    }
}
