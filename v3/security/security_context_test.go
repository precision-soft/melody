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
