package security

import (
    "testing"

    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

type registryTestMatcher struct {
    matches bool
}

func (instance *registryTestMatcher) Matches(request httpcontract.Request) bool {
    return instance.matches
}

var _ securitycontract.Matcher = (*registryTestMatcher)(nil)

func TestFirewallRegistry_Match_ReturnsFirstMatchedFirewallInOrder(t *testing.T) {
    firewallA := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "a",
        Matcher:            &registryTestMatcher{matches: true},
        MatcherDescription: "a",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

    firewallB := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "b",
        Matcher:            &registryTestMatcher{matches: true},
        MatcherDescription: "b",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

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
    firewallWithNilMatcher := &CompiledFirewall{name: "nilMatcher"}

    firewallGood := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "good",
        Matcher:            &registryTestMatcher{matches: true},
        MatcherDescription: "good",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

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

/* The registry's nil guard exists so that a request it cannot read selects no firewall. A nil pointer of
a request type is a non-nil interface, so a bare comparison would carry it into the firewall walk. The
matcher here answers yes for everything, the shape an application matcher may take: the framework's own
PathPrefixMatcher refuses such a request itself, so with it the registry's guard cannot be observed at all. */
func TestFirewallRegistry_Match_ATypedNilRequestSelectsNoFirewall(t *testing.T) {
    firewall := NewCompiledFirewall(CompiledFirewallConfig{
        Name:               "a",
        Matcher:            &registryTestMatcher{matches: true},
        MatcherDescription: "always",
        LoginPath:          "/admin/login",
        LogoutPath:         "/admin/logout",
    })

    configuration := NewCompiledConfiguration([]*CompiledFirewall{firewall}, nil)
    registry := NewFirewallRegistry(configuration)

    var unassignedRequest *http.Request

    matchedFirewall, matched := registry.Match(unassignedRequest)
    if true == matched {
        t.Fatalf("expected a typed nil request to select no firewall")
    }
    if nil != matchedFirewall {
        t.Fatalf("expected no firewall, got %v", matchedFirewall)
    }
}
