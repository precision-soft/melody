package config

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/security"
    "github.com/precision-soft/melody/v4/security/accesscontrol"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type anonymousTokenSource struct{}

func (instance *anonymousTokenSource) Name() string {
    return "anonymous"
}

func (instance *anonymousTokenSource) Stateless() bool {
    return false
}

func (instance *anonymousTokenSource) Resolve(
    runtimeInstance containercontract.Runtime,
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
    return security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, []securitycontract.Voter{security.NewRoleVoter()}, security.AccessDecisionManagerConfig{})
}

func testRuntime() containercontract.Runtime {
    return runtime.NewRequest(context.Background(), container.NewContainer(container.ContainerConfig{}).NewScope())
}

func openFirewallConfig() FirewallConfig {
    return FirewallConfig{Override: FirewallOverride{AccessControl: openAccessControl()}}
}

type noopAuthenticator struct{}

func (instance *noopAuthenticator) Supports(request httpcontract.Request) bool { return false }

func (instance *noopAuthenticator) Authenticate(runtimeInstance containercontract.Runtime, request httpcontract.Request) (securitycontract.Token, error) {
    return nil, nil
}

func unavailableRetryAfterRefusedValues() map[string]bound.Duration {
    return map[string]bound.Duration{
        "0s":        bound.Of(time.Duration(0)),
        "500ms":     bound.Of(500 * time.Millisecond),
        "1500ms":    bound.Of(1500 * time.Millisecond),
        "61m":       bound.Of(61 * time.Minute),
        "2h":        bound.Of(2 * time.Hour),
        "unlimited": bound.UnlimitedDuration(),
    }
}

func expectRefusalField(t *testing.T, field string, firewallName string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        refusal, isRefusal := recover().(*exception.Error)
        if false == isRefusal {
            t.Fatalf("expected a refusal naming %s, got none", field)
        }

        expectErrorField(t, refusal, field, firewallName)
    }()

    callback()
}

func expectErrorField(t *testing.T, err error, field string, firewallName string) {
    t.Helper()

    refusal, isRefusal := err.(*exception.Error)
    if false == isRefusal || field != refusal.Context()["field"] {
        t.Fatalf("expected an error naming %s, got %v", field, err)
    }

    if "" != firewallName && firewallName != refusal.Context()["firewallName"] {
        t.Fatalf("expected the error to name firewall %s, got %v", firewallName, refusal.Context())
    }
}

type statelessTokenSource struct {
    anonymousTokenSource
}

func (instance *statelessTokenSource) Stateless() bool {
    return true
}

func csrfLoginFirewallConfig(csrf FirewallCsrfConfig) FirewallConfig {
    config := openFirewallConfig()
    config.Login = LoginConfig{Path: "/login", Handler: &noopLoginHandler{}, LimiterDisabled: true}
    config.Csrf = csrf

    return config
}

func compileOneCsrfFirewall(t *testing.T, global security.CsrfConfig, tokenSource securitycontract.TokenSource, csrf FirewallCsrfConfig) *security.CompiledFirewall {
    t.Helper()

    builder := NewBuilder()
    builder.SetGlobal(GlobalConfig{Csrf: global})
    builder.AddFirewall("main", security.NewPathPrefixMatcher("/"), tokenSource, csrfLoginFirewallConfig(csrf))

    return builder.BuildAndCompile().Firewalls()[0]
}

type allowingRateLimiter struct{}

func (instance *allowingRateLimiter) Allow(runtimeInstance containercontract.Runtime, key string) (httpcontract.RateLimitDecision, error) {
    return httpcontract.RateLimitDecision{Allowed: true}, nil
}

func (instance *allowingRateLimiter) Reset(runtimeInstance containercontract.Runtime, key string) error {
    return nil
}

/* openAccessControl keeps every path open on purpose: the fallback a compiled access control must hold */
func openAccessControl() *security.AccessControl {
    return security.NewAccessControl([]security.AccessControlRule{accesscontrol.NewSegmentPrefixRule("/", accesscontrol.RuleConfig{Attributes: []string{securitycontract.AttributePublicAccess}})})
}
