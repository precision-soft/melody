package config

import (
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/event"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodyevent "github.com/precision-soft/melody/v3/event"
)

/* the budget is per client, as its own comment and the shipped .env promise; read from the peer address it would be per proxy, so one script could spend the hour of everyone behind the balancer, and this listener runs ahead of authentication, so what it spends is everyone's ability to log in. */
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

/* every host client of the compose stack enters through the docker bridge gateway, which nginx appends to the chain: with the whole private space trusted, the gateway would read as one more hop and the key would fall back onto the balancer, so the whole host population would share one budget. Trusting the balancer alone, the gateway is the client the balancer attested. */
func TestRequestBudgetConfig_ReadsTheDockerGatewayAsTheClientRatherThanAsAHop(t *testing.T) {
    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected the gateway the balancer forwarded to be the client, got %q", key)
    }

    if key := resolvedBudgetKey(t, []string{balancerAddress}, balancerAddress, "203.0.113.7, 172.18.0.1"); "172.18.0.1" != key {
        t.Fatalf("expected a header the client sent through the balancer to stop at the gateway, got %q", key)
    }
}

/* a neighbouring container of the deployment is not a proxy: with the whole private space trusted, any process beside the example could name its own key per request, a fresh budget each time or a victim's, on the listener that refuses at the door. */
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

/* the release of a second factor with its account is a subscriber the composition root installs; its own tests pin what it does once installed, and this one pins that it is installed. Read off a real dispatcher: with a database, one more owner on the deletion event, and it is this one. */
func TestRegisterSubscribers_InstallsTheTwoFactorEnrollmentReleaseWhenThereIsADatabase(t *testing.T) {
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
    withStore.database = newUndialedDatabase()
    with := ownersOnDeletion(withStore)

    if len(without)+1 != len(with) {
        t.Fatalf("expected the database to add one owner on %s, got %v without and %v with", event.UserDeletedEventName, without, with)
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

/* the composition root registers the cache subscriber before the release, and the dispatcher ends a dispatch at the first listener that fails: at equal priority the release would run behind a cache listener whose backend is gone and never run, leaving the row for the next holder of the identifier. Read off the dispatcher the root fills: on the deletion event the release outranks the cache subscriber's listener, whatever order they are registered in. */
func TestRegisterSubscribers_TheEnrollmentReleaseOutranksTheCacheClearOnUserDeleted(t *testing.T) {
    moduleInstance := moduleWithEnvironment(t, map[string]string{})
    moduleInstance.database = newUndialedDatabase()

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

/* the budget is the switch's number of requests per HOUR: the limiter admits exactly that many from one
   client inside the window and refuses the next */
func TestRequestBudgetConfig_AdmitsTheBudgetPerHourAndRefusesTheNext(t *testing.T) {
    if time.Hour != requestBudgetWindow {
        t.Fatalf("expected the budget counted per hour, got %s", requestBudgetWindow)
    }

    limiter := requestBudgetConfig(3, newTrustedProxyResolver("", time.Now)).Limiter()
    for request := 1; request <= 3; request++ {
        if false == limiter.Allow("203.0.113.7") {
            t.Fatalf("expected request %d of a budget of 3 admitted", request)
        }
    }

    if true == limiter.Allow("203.0.113.7") {
        t.Fatal("expected the fourth request of a budget of 3 refused")
    }
}

/* the switch arms the door on a positive integer, spaces around it tolerated, and refuses any other value by name rather than reading it as unset, since a swallowed typo would disarm the global budget with no signal anywhere */
func TestRegisterRateLimitRequestListener_ArmsOnAPositiveBudgetAndRefusesAnyOtherValueByName(t *testing.T) {
    kernelRequestListeners := func(moduleInstance *Module) int {
        eventDispatcher := melodyevent.NewEventDispatcher(melodyclock.NewSystemClock())
        moduleInstance.registerRateLimitRequestListener(eventDispatcher)

        count := 0
        for _, registered := range eventDispatcher.RegisteredEvents() {
            count = count + len(registered.Listeners)
        }

        return count
    }

    if 0 != kernelRequestListeners(moduleWithEnvironment(t, map[string]string{})) {
        t.Fatal("expected the door unwired without the switch")
    }

    if 1 != kernelRequestListeners(moduleWithEnvironment(t, map[string]string{environmentKeyRequestBudgetPerHour: " 120 "})) {
        t.Fatal("expected the door armed on a positive budget")
    }

    for _, value := range []string{"abc", "0", "-5", "1.5"} {
        refusal := func() (recovered any) {
            defer func() {
                recovered = recover()
            }()

            kernelRequestListeners(moduleWithEnvironment(t, map[string]string{environmentKeyRequestBudgetPerHour: value}))

            return nil
        }()

        if nil == refusal || false == strings.Contains(fmt.Sprint(refusal), "the request budget switch does not hold a positive integer") {
            t.Fatalf("expected %q refused by name, got %v", value, refusal)
        }
    }
}
