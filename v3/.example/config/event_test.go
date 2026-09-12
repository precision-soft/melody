package config

import (
    "bytes"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* the compose balancer as the shipped .env names it, already resolved: the tests below hand the list in resolved form because what they assert is what the resolver does with a peer and a chain, not what the name resolves to. */
const balancerAddress = "172.18.0.9"

/* a request as a proxy delivers it: the peer is the given address and the client it forwarded is named in the header. */
func requestForwardedBy(t *testing.T, peer string, forwardedFor string) melodyhttpcontract.Request {
    t.Helper()

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.RemoteAddr = peer + ":41234"
    if "" != forwardedFor {
        httpRequest.Header.Set("X-Forwarded-For", forwardedFor)
    }

    return melodyhttp.NewRequest(httpRequest, nil, nil, melodyhttp.NewRequestContext("budget-test", time.Now()))
}

func resolvedBudgetKey(t *testing.T, trustedProxyList []string, peer string, forwardedFor string) string {
    t.Helper()

    resolver := requestBudgetConfig(100, trustedProxyList).ClientIpResolver()
    if nil == resolver {
        t.Fatal("expected the request budget to resolve the client address rather than fall back to the peer")
    }

    return resolver(requestForwardedBy(t, peer, forwardedFor))
}

/* the budget is per client, which is what its own comment and the shipped .env both promise. Read from the
   peer address it is per PROXY: measured through the compose stack the key was the balancer's 172.18.0.9,
   so a single script could spend the hour of everyone behind it — and this listener runs ahead of
   authentication, so what it spends is everyone's ability to log in. */
func TestRequestBudgetConfig_ChargesTheForwardedClientRatherThanTheProxy(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "203.0.113.7"); "203.0.113.7" != key {
        t.Fatalf("expected the forwarded client to be charged, got %q", key)
    }
}

/* the sister case: the header is only believed from a peer this example trusts, so a client that sends one
   itself cannot pick which budget it spends. */
func TestRequestBudgetConfig_IgnoresAForwardedHeaderFromAnUntrustedPeer(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, "198.51.100.4", "203.0.113.7"); "198.51.100.4" != key {
        t.Fatalf("expected a header from an untrusted peer to be ignored, got %q", key)
    }
}

/* every host client of the compose stack enters through the docker bridge gateway, which nginx appends to the chain: with the whole private space trusted the gateway read as one more hop, the chain ran out and the key fell back onto the balancer — the very key the repair existed to leave — so the whole host population still shared one budget and a header a client sent picked its key. Trusting the balancer alone, the gateway is the client the balancer attested. */
func TestRequestBudgetConfig_ReadsTheDockerGatewayAsTheClientRatherThanAsAHop(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected the gateway the balancer forwarded to be the client, got %q", key)
    }

    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "203.0.113.7, 172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected a header the client sent through the balancer to stop at the gateway, got %q", key)
    }
}

/* a neighbouring container of the deployment is not a proxy: with the whole private space trusted, any process beside the example named its own key per request — a fresh budget each time, or a victim's — on the listener that refuses at the door. */
func TestRequestBudgetConfig_DoesNotBelieveANeighbouringContainer(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, "172.18.0.11", "203.0.113.7"); "172.18.0.11" != key {
        t.Fatalf("expected a header from a neighbouring container to be ignored, got %q", key)
    }
}

/* an empty list trusts nobody: the budget keys on the peer even when a header is present, which is the closed side of the switch. */
func TestRequestBudgetConfig_AnEmptyListChargesThePeer(t *testing.T) {
    if key := resolvedBudgetKey(t, nil, balancerAddress, "203.0.113.7"); balancerAddress != key {
        t.Fatalf("expected an empty trusted list to charge the peer, got %q", key)
    }
}

func moduleWithTrustedProxyList(t *testing.T, value string) *Module {
    t.Helper()

    return moduleWithEnvironment(t, map[string]string{environmentKeyTrustedProxyList: value})
}

/* the list is read from configuration, an address or a prefix as written and a host name resolved once at build: the compose balancer is reachable by its service name and nothing else about it survives a restart of the stack. */
func TestTrustedProxyList_ResolvesAHostNameAndKeepsAnAddressAsWritten(t *testing.T) {
    trustedProxyList := moduleWithTrustedProxyList(t, " 10.1.2.3 , 192.168.0.0/16,localhost ").trustedProxyList()

    if 3 > len(trustedProxyList) || "10.1.2.3" != trustedProxyList[0] || "192.168.0.0/16" != trustedProxyList[1] {
        t.Fatalf("expected the address and the prefix as written followed by what localhost resolves to, got %v", trustedProxyList)
    }

    resolvedLoopback := false
    for _, entry := range trustedProxyList[2:] {
        if "127.0.0.1" == entry || "::1" == entry {
            resolvedLoopback = true
        }
    }

    if false == resolvedLoopback {
        t.Fatalf("expected localhost to resolve to a loopback address, got %v", trustedProxyList)
    }

    if key := resolvedBudgetKey(t, trustedProxyList, "127.0.0.1", "203.0.113.7"); "203.0.113.7" != key {
        t.Fatalf("expected the resolved loopback to be a trusted proxy, got %q", key)
    }
}

/* an entry that names nothing is skipped and reported, never refused: the balancer is not started beside a cli command, and a list one hop shorter fails closed onto the peer address. */
func TestTrustedProxyList_SkipsAndReportsAnEntryThatNamesNothing(t *testing.T) {
    journal := &bytes.Buffer{}
    previous := trustedProxyWarningLogger
    trustedProxyWarningLogger = func() melodyloggingcontract.Logger {
        return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug)
    }
    t.Cleanup(func() {
        trustedProxyWarningLogger = previous
    })

    trustedProxyList := moduleWithTrustedProxyList(t, "no-such-host.invalid").trustedProxyList()

    if 0 != len(trustedProxyList) {
        t.Fatalf("expected an entry that names nothing to be skipped, got %v", trustedProxyList)
    }

    if false == strings.Contains(journal.String(), "no-such-host.invalid") || false == strings.Contains(journal.String(), environmentKeyTrustedProxyList) {
        t.Fatalf("expected the skipped entry to be reported with its key, got %q", journal.String())
    }

    if key := resolvedBudgetKey(t, trustedProxyList, balancerAddress, "203.0.113.7"); balancerAddress != key {
        t.Fatalf("expected the empty list to charge the peer, got %q", key)
    }
}
