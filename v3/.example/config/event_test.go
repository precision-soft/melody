package config

import (
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/event"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyevent "github.com/precision-soft/melody/v3/event"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
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

/* resolvedBudgetKey reads the key the budget charges, over a resolver whose entries are the given list — addresses and prefixes taken as written, so no name is looked up here; what these tests assert is what the resolver does with a peer and a header, not how a name resolves */
func resolvedBudgetKey(t *testing.T, trustedProxyList []string, peer string, forwardedFor string) string {
    t.Helper()

    resolver := requestBudgetConfig(100, newTrustedProxyResolver(strings.Join(trustedProxyList, ","), time.Now)).ClientIpResolver()
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

/* the release of a second factor with its account is a subscriber the composition root INSTALLS: its own tests pin what it does once installed, and nothing pinned that it is — the wiring line could go and every test stayed green. Read off a real dispatcher: with a store, one more owner on the deletion event, and it is this one. */
func TestRegisterSubscribers_InstallsTheTwoFactorEnrollmentReleaseWhenThereIsAStore(t *testing.T) {
    ownersOnDeletion := func(moduleInstance *Module) []string {
        eventDispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
        moduleInstance.registerSubscribers(eventDispatcher)

        var owners []string
        for _, registered := range eventDispatcher.RegisteredEvents() {
            if event.UserDeletedEventName != registered.EventName {
                continue
            }

            for _, listener := range registered.Listeners {
                owners = append(owners, listener.Owner)
            }
        }

        return owners
    }

    without := ownersOnDeletion(moduleWithEnvironment(t, map[string]string{}))

    withStore := moduleWithEnvironment(t, map[string]string{})
    withStore.twoFactorStore = twofactor.NewStore(newUndialedDatabase())
    with := ownersOnDeletion(withStore)

    if len(without)+1 != len(with) {
        t.Fatalf("expected the store to add one owner on %s, got %v without and %v with", event.UserDeletedEventName, without, with)
    }

    found := false
    for _, owner := range with {
        if true == strings.Contains(owner, "TwoFactorEnrollmentSubscriber") {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected the enrollment subscriber among the owners of %s, got %v", event.UserDeletedEventName, with)
    }
}

/* the composition root registers the cache subscriber before the release, and the dispatcher ends a dispatch at the first listener that fails: registered at equal priority, the release ran behind a cache listener whose backend was gone and never ran at all — the row stayed for the next holder of the identifier. Read off the dispatcher the root fills: on the deletion event the release outranks the cache subscriber's listener, whatever order they were registered in. */
func TestRegisterSubscribers_TheEnrollmentReleaseOutranksTheCacheClearOnUserDeleted(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.twoFactorStore = twofactor.NewStore(newUndialedDatabase())

    eventDispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
    moduleInstance.registerSubscribers(eventDispatcher)

    releasePriority, cachePriority := 0, 0
    releaseFound, cacheFound := false, false
    for _, registered := range eventDispatcher.RegisteredEvents() {
        if event.UserDeletedEventName != registered.EventName {
            continue
        }

        for _, listener := range registered.Listeners {
            if true == strings.Contains(listener.Owner, "TwoFactorEnrollmentSubscriber") {
                releasePriority, releaseFound = listener.Priority, true
            }
            if true == strings.Contains(listener.Owner, "UserEventSubscriber") {
                cachePriority, cacheFound = listener.Priority, true
            }
        }
    }

    if false == releaseFound || false == cacheFound {
        t.Fatalf("expected both the release and the cache subscriber on %s, found release %v and cache %v", event.UserDeletedEventName, releaseFound, cacheFound)
    }
    if releasePriority <= cachePriority {
        t.Fatalf("expected the release (%d) to outrank the cache clear (%d) on %s", releasePriority, cachePriority, event.UserDeletedEventName)
    }
}
