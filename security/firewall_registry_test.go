package security

import (
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    securitycontract "github.com/precision-soft/melody/security/contract"
)

type registryTestMatcher struct {
    matches bool
}

func (instance *registryTestMatcher) Matches(request httpcontract.Request) bool {
    return instance.matches
}

var _ securitycontract.Matcher = (*registryTestMatcher)(nil)

func TestFirewallRegistry_Match_ReturnsFirstMatchedFirewallInOrder(t *testing.T) {
    firewallA := NewCompiledFirewall(
        "a",
        &registryTestMatcher{matches: true},
        "a",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    firewallB := NewCompiledFirewall(
        "b",
        &registryTestMatcher{matches: true},
        "b",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    configuration := NewCompiledConfiguration([]*CompiledFirewall{firewallA, firewallB}, nil)
    registry := NewFirewallRegistry(configuration)

    request := newFirewallTestRequest("/admin")

    matchedFirewall, matched := registry.Match(request)
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedFirewall {
        t.Fatalf("expected firewall")
    }
    if "a" != matchedFirewall.Name() {
        t.Fatalf("expected first firewall to win")
    }
}

func TestFirewallRegistry_Match_IgnoresNilFirewallOrNilMatcher(t *testing.T) {
    firewallWithNilMatcher := NewCompiledFirewall(
        "nilMatcher",
        nil,
        "nil",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    firewallGood := NewCompiledFirewall(
        "good",
        &registryTestMatcher{matches: true},
        "good",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "/admin/login",
        "/admin/logout",
        nil,
        nil,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
        SourceNone,
    )

    configuration := NewCompiledConfiguration([]*CompiledFirewall{nil, firewallWithNilMatcher, firewallGood}, nil)
    registry := NewFirewallRegistry(configuration)

    request := newFirewallTestRequest("/admin")

    matchedFirewall, matched := registry.Match(request)
    if false == matched {
        t.Fatalf("expected matched")
    }
    if nil == matchedFirewall {
        t.Fatalf("expected firewall")
    }
    if "good" != matchedFirewall.Name() {
        t.Fatalf("expected good firewall to win")
    }
}

func TestNewFirewallRegistry_NilConfigurationPanics(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            _ = NewFirewallRegistry(nil)
        },
        "compiled security configuration is nil",
    )
}

/* @info a nil request matches no firewall: without this the walk would hand a nil request to every matcher, and a matcher written outside this package is under no obligation to survive one */
func TestFirewallRegistry_MatchRefusesANilRequest(t *testing.T) {
    registry := NewFirewallRegistry(
        NewCompiledConfiguration(
            []*CompiledFirewall{newTestCompiledFirewallWithRoleHierarchy("main", nil)},
            nil,
        ),
    )

    firewall, matched := registry.Match(nil)
    if true == matched {
        t.Fatalf("expected a nil request to match no firewall")
    }

    if nil != firewall {
        t.Fatalf("expected no firewall alongside the refusal")
    }
}

/* @info a request no firewall claims is not an error: the access control listener falls back to the global policy, which is the shape a global-only configuration compiles to */
func TestFirewallRegistry_MatchAnswersNotMatchedWhenNoFirewallClaims(t *testing.T) {
    registry := NewFirewallRegistry(
        NewCompiledConfiguration(
            []*CompiledFirewall{
                NewCompiledFirewall(
                    "admin",
                    NewPathPrefixMatcher("/admin"),
                    "prefix /admin",
                    nil, nil, nil, nil, nil, nil, nil, "", "", nil, nil,
                    SourceNone, SourceNone, SourceNone, SourceNone, SourceNone,
                ),
            },
            nil,
        ),
    )

    firewall, matched := registry.Match(newFirewallTestRequest("/public"))
    if true == matched {
        t.Fatalf("expected no firewall to claim an unmatched path")
    }

    if nil != firewall {
        t.Fatalf("expected no firewall alongside the miss")
    }
}

func TestFirewallRegistry_GlobalAccessControlIsAnswered(t *testing.T) {
    globalAccessControl := NewAccessControl(
        NewAccessControlRule("/", "ROLE_USER"),
    )

    registry := NewFirewallRegistry(
        NewCompiledConfiguration(nil, globalAccessControl),
    )

    if globalAccessControl != registry.GlobalAccessControl() {
        t.Fatalf("expected the global access control to be answered")
    }
}

/* @info the nil-request refusal needs an observable of its OWN: with the matchers this package ships, a nil request is declined by the matcher too, so removing the registry's guard changes nothing. Against a matcher that claims everything — the first matcher an application writes — the guard is the only thing between a nil request and a firewall selected for it. */
func TestFirewallRegistry_MatchRefusesANilRequestEvenAgainstAClaimingMatcher(t *testing.T) {
    registry := NewFirewallRegistry(
        NewCompiledConfiguration(
            []*CompiledFirewall{
                NewCompiledFirewall(
                    "main",
                    &alwaysMatchingRuleMatcher{},
                    "always",
                    nil, nil, nil, nil, nil, nil, nil, "", "", nil, nil,
                    SourceNone, SourceNone, SourceNone, SourceNone, SourceNone,
                ),
            },
            nil,
        ),
    )

    firewall, matched := registry.Match(nil)
    if true == matched {
        t.Fatalf("expected a nil request to select no firewall even when a matcher claims everything")
    }

    if nil != firewall {
        t.Fatalf("expected no firewall alongside the refusal")
    }
}
