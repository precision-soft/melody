package config

import (
    "strings"
    "testing"

    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func TestBuilder_GlobalAccessControlWithoutFirewallStillCompiles(t *testing.T) {
    builder := NewBuilder()

    globalAccessControl := security.NewAccessControl(
        security.NewAccessControlRule("/admin", "ROLE_ADMIN"),
    )
    builder.SetGlobal(
        globalAccessControl,
        nil,
        security.NewAccessDecisionManager(securitycontract.DecisionStrategyAffirmative, security.NewRoleVoter()),
        nil,
        nil,
    )

    compiled := builder.BuildAndCompile()
    if nil == compiled {
        t.Fatalf("expected a global-only configuration to still compile and enforce, got nil")
    }

    if nil == compiled.GlobalAccessControl() {
        t.Fatalf("expected the compiled configuration to carry the declared global access control")
    }
}

func TestBuilder_NoFirewallAndNoGlobalCompilesToNil(t *testing.T) {
    builder := NewBuilder()

    if nil != builder.BuildAndCompile() {
        t.Fatalf("expected no declared security to compile to nil")
    }
}
func TestCompile_SourceIsNoneWhenDependencyAbsent(t *testing.T) {
    builder := NewBuilder()

    builder.AddStatelessFirewall(
        "api",
        security.NewPathPrefixMatcher("/"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
    )

    firewall := builder.BuildAndCompile().Firewalls()[0]

    _, _, _, entryPointSource, deniedHandlerSource := firewall.Sources()
    if security.SourceNone != entryPointSource {
        t.Fatalf("expected an absent entry point to read SourceNone, got %q", entryPointSource)
    }
    if security.SourceNone != deniedHandlerSource {
        t.Fatalf("expected an absent denied handler to read SourceNone, got %q", deniedHandlerSource)
    }
}

/* the three override dependencies the plain nil comparison cannot judge: a typed nil handed to one of
   these setters reads as declared, so the fallback to the global one is skipped and the first request
   behind the firewall dereferences a nil receiver inside the listener. The refusal has to reach the
   caller from Compile, because the setters take the value without looking at it. */
func TestCompile_RefusesATypedNilOverrideDependency(t *testing.T) {
    var typedNilDecisionManager securitycontract.AccessDecisionManager = (*security.AccessDecisionManager)(nil)
    var typedNilEntryPoint securitycontract.EntryPoint = (*typedNilEntryPointProbe)(nil)
    var typedNilDeniedHandler securitycontract.AccessDeniedHandler = (*typedNilDeniedHandlerProbe)(nil)

    for _, testCase := range []struct {
        name     string
        override FirewallOverrideConfiguration
        expected string
    }{
        {
            "access decision manager",
            NewFirewallOverrideConfiguration().WithAccessDecisionManager(typedNilDecisionManager),
            "security firewall access decision manager is a typed nil",
        },
        {
            "entry point",
            NewFirewallOverrideConfiguration().WithEntryPoint(typedNilEntryPoint),
            "security firewall entry point is a typed nil",
        },
        {
            "access denied handler",
            NewFirewallOverrideConfiguration().WithAccessDeniedHandler(typedNilDeniedHandler),
            "security firewall access denied handler is a typed nil",
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            builder := NewBuilder()

            builder.AddStatelessFirewall(
                "api",
                security.NewPathPrefixMatcher("/"),
                nil,
                &anonymousTokenSource{},
                testCase.override,
            )

            testhelper.AssertPanicsWithError(t, func() {
                builder.BuildAndCompile()
            }, testCase.expected)
        })
    }
}

type typedNilEntryPointProbe struct {
}

func (instance *typedNilEntryPointProbe) Start(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
) (httpcontract.Response, error) {
    return nil, nil
}

type typedNilDeniedHandlerProbe struct {
}

func (instance *typedNilDeniedHandlerProbe) Handle(
    runtimeInstance runtimecontract.Runtime,
    request httpcontract.Request,
    decisionErr error,
) (httpcontract.Response, error) {
    return nil, nil
}

func TestCompile_RefusesANilRule(t *testing.T) {
    _, err := Compile(Configuration{
        firewalls: []FirewallConfiguration{
            {
                name:        "api",
                matcher:     security.NewPathPrefixMatcher("/api"),
                rules:       []securitycontract.Rule{nil},
                tokenSource: &anonymousTokenSource{},
            },
        },
    })
    if nil == err || false == strings.Contains(err.Error(), "security firewall rule is nil") {
        t.Fatalf("expected the nil rule refused, got %v", err)
    }
}

func TestCompile_RefusesANameAlreadyDeclared(t *testing.T) {
    firewall := FirewallConfiguration{
        name:        "api",
        matcher:     security.NewPathPrefixMatcher("/api"),
        tokenSource: &anonymousTokenSource{},
        override:    NewFirewallOverrideConfiguration().WithStateless(true),
    }

    if _, err := Compile(Configuration{firewalls: []FirewallConfiguration{firewall}}); nil != err {
        t.Fatalf("expected one firewall of the name to compile, got %v", err)
    }

    _, err := Compile(Configuration{firewalls: []FirewallConfiguration{firewall, firewall}})
    if nil == err || false == strings.Contains(err.Error(), "security firewall name is already declared") {
        t.Fatalf("expected the duplicate name refused, got %v", err)
    }
}


func TestCompile_DescribesTheFrameworksPathPrefixMatcher(t *testing.T) {
    compiledConfiguration := NewBuilder().AddStatelessFirewall(
        "admin",
        security.NewPathPrefixMatcher("/admin"),
        nil,
        &anonymousTokenSource{},
        NewFirewallOverrideConfiguration(),
    ).BuildAndCompile()

    if `path prefix "/admin"` != compiledConfiguration.Firewalls()[0].MatcherDescription() {
        t.Fatalf("expected the matcher described, got %q", compiledConfiguration.Firewalls()[0].MatcherDescription())
    }
}
