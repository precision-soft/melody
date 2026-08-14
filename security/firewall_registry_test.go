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

/* the registry's nil-request refusal is SHADOWED by the matchers this package ships, which decline a nil request themselves; the matcher below claims everything, which is what makes the registry's own guard observable. */
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
