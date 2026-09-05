package security

import (
    "testing"

    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
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

/* The registry's nil guard exists so that a request it cannot read selects no firewall. A nil pointer of
a request type is a non-nil interface, so the bare comparison this replaces carried it into the firewall
walk. The matcher here answers yes for everything, the shape an application matcher may take: the
framework's own PathPrefixMatcher refuses such a request itself, so with it the registry's guard cannot be
observed at all. */
func TestFirewallRegistry_Match_ATypedNilRequestSelectsNoFirewall(t *testing.T) {
    firewall := NewCompiledFirewall(
        "a",
        &registryTestMatcher{matches: true},
        "always",
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

/* the matcher comes through NewCompiledFirewall, which is public and validates nothing, so a nil pointer of an
application's own matcher type arrives here as a non-nil interface: `nil ==` reads it as a live matcher and the
Matches call below dereferences it. This is the walk that decides which firewall claims a request, so the crash
lands on EVERY request rather than on a rare path. Written before the repair, it panics with SIGSEGV. */
func TestFirewallRegistry_Match_SkipsAFirewallCarryingATypedNilMatcher(t *testing.T) {
    var typedNilMatcher *PathPrefixMatcher

    firewall := NewCompiledFirewall(
        "main", typedNilMatcher, "", nil, nil,
        NewAccessControl(), nil, nil, nil, nil,
        "", "", nil, nil,
        SourceNone, SourceNone, SourceFirewall, SourceNone, SourceNone,
    )

    registry := NewFirewallRegistry(NewCompiledConfiguration([]*CompiledFirewall{firewall}, NewAccessControl()))

    matched, found := registry.Match(newFirewallTestRequest("/anything"))

    if true == found || nil != matched {
        t.Fatalf("expected a firewall whose matcher is a typed nil to claim nothing, got %v", matched)
    }
}
