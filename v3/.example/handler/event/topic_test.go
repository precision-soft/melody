package event

import (
    "testing"

    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestCatalogTopicRequiresEditorOnAnAuthenticatedFirewall(t *testing.T) {
    for _, role := range []string{"ROLE_USER", "ROLE_EDITOR"} {
        t.Run(role, func(t *testing.T) {
            _, runtimeInstance := streamRequest(t, "/events?topic=default", true)
            token := melodysecurity.NewAuthenticatedToken("member", []string{role})
            firewall := melodysecurity.NewCompiledFirewall(
                "main", melodysecurity.NewPathPrefixMatcher("/"), "", nil,
                melodysecurity.NewResolverTokenSource(func(request melodyhttpcontract.Request) securitycontract.Token { return token }),
                melodysecurity.NewAccessControl(), melodysecurity.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, melodysecurity.NewRoleVoter()), nil, nil, nil,
                "", "", nil, nil,
                melodysecurity.SourceNone, melodysecurity.SourceFirewall, melodysecurity.SourceFirewall, melodysecurity.SourceNone, melodysecurity.SourceNone,
            )
            melodysecurity.SecurityContextSetOnRuntime(runtimeInstance, melodysecurity.NewSecurityContext(firewall, token))
            if ("ROLE_EDITOR" == role) != topicIsReadableBy(runtimeInstance, CatalogTopic) {
                t.Fatalf("unexpected catalog topic access for %s", role)
            }
            if false == topicIsReadableBy(runtimeInstance, "custom") {
                t.Fatal("authenticated custom topic refused")
            }
        })
    }
}
