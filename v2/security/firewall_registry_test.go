package security

import (
    "testing"

    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    securitycontract "github.com/precision-soft/melody/v2/security/contract"
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
