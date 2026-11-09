package config

import (
    "testing"

    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type anonymousTokenSource struct{}

func (instance *anonymousTokenSource) Name() string {
    return "anonymous"
}

func (instance *anonymousTokenSource) Resolve(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (securitycontract.Token, error) {
    _ = runtimeInstance
    _ = request

    return security.NewAnonymousToken(), nil
}

var _ securitycontract.TokenSource = (*anonymousTokenSource)(nil)

func expectRefusalNamingField(t *testing.T, message string, field string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected %q naming %s, got no refusal", message, field)
        }

        if message != refusal.Message() || field != refusal.Context()["field"] {
            t.Fatalf("expected %q naming %s, got %q with %v", message, field, refusal.Message(), refusal.Context())
        }
    }()

    callback()
}

func roleDecisionManager() securitycontract.AccessDecisionManager {
    return security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{security.NewRoleVoter()})
}

func openFirewallConfig() FirewallConfig {
    return FirewallConfig{Override: FirewallOverride{AccessControl: security.NewAccessControl(nil)}}
}

type noopAuthenticator struct{}

func (instance *noopAuthenticator) Supports(request httpcontract.Request) bool { return false }

func (instance *noopAuthenticator) Authenticate(request httpcontract.Request) (securitycontract.Token, error) {
    return nil, nil
}
