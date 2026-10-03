package config

import (
    "bytes"
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

func moduleWithTrustedProxyList(t *testing.T, value string) *Module {
    t.Helper()

    moduleInstance := moduleWithEnvironment(t, map[string]string{environmentKeyTrustedProxyList: value})
    moduleInstance.buildTrustedProxyResolver()

    return moduleInstance
}

/* the list is read from configuration, an address or a prefix as written and a host name resolved when it is first asked for: the compose balancer is reachable by its service name and nothing else about it survives a restart of the stack. */
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

/* resolverOver builds a resolver over the given raw list with the given clock, the way buildTrustedProxyResolver does over the configured one */
func resolverOver(t *testing.T, value string, now func() time.Time) *trustedProxyResolver {
    t.Helper()

    return newTrustedProxyResolver(value, now)
}

/* lookupTable replaces name resolution with a table the test controls, and counts the lookups: the property under test is WHEN the names are looked up, which a real resolver cannot show */
func lookupTable(t *testing.T, table map[string][]string) *int {
    t.Helper()

    lookups := 0
    previous := trustedProxyLookup
    trustedProxyLookup = func(host string) ([]string, error) {
        lookups++

        addressList, known := table[host]
        if false == known {
            return nil, errors.New("lookup " + host + ": no such host")
        }

        return addressList, nil
    }
    t.Cleanup(func() {
        trustedProxyLookup = previous
    })

    return &lookups
}

/* the names are resolved at the first request and again once the interval has passed, so a balancer restarted onto a new address is trusted again within the interval, and a process booted before the balancer's name exists trusts it once the name resolves */
func TestTrustedProxyResolver_ResolvesAtTheFirstRequestAndAgainAfterTheInterval(t *testing.T) {
    table := map[string][]string{"load-balancer": {balancerAddress}}
    lookups := lookupTable(t, table)

    now := time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC)
    resolver := resolverOver(t, "load-balancer", func() time.Time { return now })

    if 0 != *lookups {
        t.Fatalf("expected no lookup at build, got %d", *lookups)
    }

    if key := resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7")); "203.0.113.7" != key {
        t.Fatalf("expected the balancer to be trusted at the first request, got %q", key)
    }
    if 1 != *lookups {
        t.Fatalf("expected the first request to resolve the name once, got %d lookups", *lookups)
    }

    /* the balancer restarts onto a new address inside the interval: the list last resolved is still believed, and the new address is a stranger */
    table["load-balancer"] = []string{"172.18.0.13"}
    now = now.Add(30 * time.Second)
    if key := resolver.Resolve(requestForwardedBy(t, "172.18.0.13", "203.0.113.7")); "172.18.0.13" != key {
        t.Fatalf("expected the restarted balancer to be a stranger inside the interval, got %q", key)
    }
    if 1 != *lookups {
        t.Fatalf("expected no lookup inside the interval, got %d", *lookups)
    }

    /* past the interval the name is looked up again and the restarted balancer is trusted */
    now = now.Add(trustedProxyRefreshInterval)
    if key := resolver.Resolve(requestForwardedBy(t, "172.18.0.13", "203.0.113.7")); "203.0.113.7" != key {
        t.Fatalf("expected the restarted balancer to be trusted after the interval, got %q", key)
    }
    if 2 != *lookups {
        t.Fatalf("expected one lookup after the interval, got %d", *lookups)
    }
}

/* a name that resolves to nothing at the first request — the balancer not up yet — is skipped and reported, and looked up again after the interval, when it is there */
func TestTrustedProxyResolver_TrustsANameThatBeginsToResolveAfterTheInterval(t *testing.T) {
    journal := &bytes.Buffer{}
    previous := trustedProxyWarningLogger
    trustedProxyWarningLogger = func() melodyloggingcontract.Logger {
        return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug)
    }
    t.Cleanup(func() {
        trustedProxyWarningLogger = previous
    })

    table := map[string][]string{}
    lookupTable(t, table)

    now := time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC)
    resolver := resolverOver(t, "load-balancer", func() time.Time { return now })

    if key := resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7")); balancerAddress != key {
        t.Fatalf("expected an unresolved name to trust nothing and charge the peer, got %q", key)
    }
    if false == strings.Contains(journal.String(), "load-balancer") {
        t.Fatalf("expected the unresolved name to be reported, got %q", journal.String())
    }

    table["load-balancer"] = []string{balancerAddress}
    now = now.Add(trustedProxyRefreshInterval)
    if key := resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7")); "203.0.113.7" != key {
        t.Fatalf("expected the name to be trusted once it resolves, got %q", key)
    }
}

/* an entry that can be nothing (an address with a port, a bracketed address, a prefix without its length) is refused at build rather than looked up as a name and skipped, since skipped it would narrow the trusted list in silence, which reads at runtime as every client behind that proxy sharing one key */
func TestNewTrustedProxyResolver_RefusesAnEntryThatCanBeNothing(t *testing.T) {
    for _, entry := range []string{"172.18.0.9:80", "[::1]", "172.18.0.9/", "load balancer"} {
        refused := func() (refused bool) {
            defer func() {
                if nil != recover() {
                    refused = true
                }
            }()

            newTrustedProxyResolver(entry, time.Now)

            return false
        }()

        if false == refused {
            t.Fatalf("expected %q to be refused at build", entry)
        }
    }

    for _, entry := range []string{"10.1.2.3", "192.168.0.0/16", "load-balancer", "::1", "fe80::1/64"} {
        newTrustedProxyResolver(entry, time.Now)
    }
}

/* both budgets read ONE resolver, so the two halves of the policy cannot drift onto different lists */
func TestNewExampleModule_HandsBothBudgetsTheSameTrustedProxyResolver(t *testing.T) {
    moduleInstance := moduleWithTrustedProxyList(t, "10.1.2.3")
    moduleInstance.buildTrustedProxyResolver()

    if nil == moduleInstance.trustedProxyResolver {
        t.Fatal("expected the module to build its resolver")
    }

    if 1 != len(moduleInstance.trustedProxyResolver.entryList) || "10.1.2.3" != moduleInstance.trustedProxyResolver.entryList[0] {
        t.Fatalf("expected the resolver to hold the configured entries, got %v", moduleInstance.trustedProxyResolver.entryList)
    }

    /* the resolver the request budget INSTALLS is the module's one, read back through the configured door:
       a wiring that handed a budget a resolver of its own would keep this list and still drift */
    installed := requestBudgetConfig(1, moduleInstance.trustedProxyResolver).ClientIpResolver()
    if nil == installed {
        t.Fatal("expected the request budget to carry a client ip resolver")
    }

    request := requestForwardedBy(t, "10.1.2.3", "203.0.113.7")
    if "203.0.113.7" != installed(request) || "203.0.113.7" != moduleInstance.trustedProxyResolver.Resolve(request) {
        t.Fatalf("expected the budget's resolver and the module's to agree on the client behind the trusted proxy, got %q and %q", installed(request), moduleInstance.trustedProxyResolver.Resolve(request))
    }
}

/* slowLookupTable is a lookup that takes as long as it is told and counts, under a lock, how many times it is entered, the figure the single-flight and the "not a burst" promise are pinned on */
func slowLookupTable(t *testing.T, delay time.Duration) *atomic.Int64 {
    t.Helper()

    lookups := &atomic.Int64{}
    previous := trustedProxyLookup
    trustedProxyLookup = func(host string) ([]string, error) {
        lookups.Add(1)
        time.Sleep(delay)

        return []string{balancerAddress}, nil
    }
    t.Cleanup(func() {
        trustedProxyLookup = previous
    })

    return lookups
}

/* the first resolution runs outside the lock too: while it is in flight, every other request is charged to its peer, a list that trusts nothing, instead of waiting behind a lookup that may hang, on the listener ahead of authentication */
func TestTrustedProxyResolver_DoesNotHoldConcurrentRequestsOnTheFirstLookup(t *testing.T) {
    lookups := slowLookupTable(t, 300*time.Millisecond)
    resolver := resolverOver(t, "load-balancer", time.Now)

    started := make(chan struct{})
    finished := make(chan struct{})
    go func() {
        close(started)
        resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7"))
        close(finished)
    }()
    <-started
    time.Sleep(20 * time.Millisecond)

    before := time.Now()
    key := resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7"))
    waited := time.Since(before)

    /* the first lookup outlives the request it runs under: the test waits for it, so the cleanup that restores the lookup table does not run under a goroutine still inside the table it replaces */
    <-finished

    /* the lookup takes 300 ms, so a request held behind it would take nearly that long; a bound at two hundred separates the two under a loaded runner where a request served at once can still take tens of milliseconds */
    if 200*time.Millisecond < waited {
        t.Fatalf("a request during the first lookup waited %s for it, wanted it served at once", waited)
    }

    if balancerAddress != key {
        t.Fatalf("a request during the first lookup was charged to %q, wanted its peer (a list that trusts nothing)", key)
    }

    if 1 != lookups.Load() {
        t.Fatalf("two requests during the first resolution ran %d lookups, wanted one", lookups.Load())
    }
}

/* the cost the GoDoc promises, pinned: one lookup a minute — the refresh advances the instant the list was
   resolved at — and no burst: twenty requests finding the list stale at once run ONE lookup between them */
func TestTrustedProxyResolver_LooksUpOnceAMinuteAndNeverInABurst(t *testing.T) {
    lookups := slowLookupTable(t, 0)

    now := time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC)
    resolver := resolverOver(t, "load-balancer", func() time.Time { return now })

    resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7"))
    now = now.Add(trustedProxyRefreshInterval)

    for range 100 {
        resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7"))
    }

    if 2 != lookups.Load() {
        t.Fatalf("a hundred requests over a stale list ran %d lookups, wanted two (the first and one refresh)", lookups.Load())
    }

    slow := slowLookupTable(t, 100*time.Millisecond)
    now = now.Add(trustedProxyRefreshInterval)

    var group sync.WaitGroup
    for range 20 {
        group.Add(1)
        go func() {
            defer group.Done()
            resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7"))
        }()
    }
    group.Wait()

    if 1 != slow.Load() {
        t.Fatalf("twenty concurrent requests over a stale list ran %d lookups, wanted one", slow.Load())
    }
}

/* both budgets ask the resolver about the same request, the request budget ahead of authentication and the write throttle once a handler is reached, and a re-resolution landing between the two asks would charge the one request to its forwarded client at one budget and to the balancer at the other. The first answer a request is given is the answer it keeps; the next request reads the list as it now is. */
func TestTrustedProxyResolver_AnswersOneRequestTheSameAcrossARefresh(t *testing.T) {
    table := map[string][]string{"load-balancer": {balancerAddress}}
    lookupTable(t, table)

    now := time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC)
    resolver := resolverOver(t, "load-balancer", func() time.Time { return now })

    request := requestForwardedBy(t, balancerAddress, "203.0.113.7")
    if key := resolver.Resolve(request); "203.0.113.7" != key {
        t.Fatalf("expected the balancer to be trusted at the first ask, got %q", key)
    }

    table["load-balancer"] = []string{"172.18.0.13"}
    now = now.Add(trustedProxyRefreshInterval)

    if key := resolver.Resolve(request); "203.0.113.7" != key {
        t.Errorf("the second ask of the same request answered %q across a refresh, wanted the first answer", key)
    }

    if key := resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7")); balancerAddress != key {
        t.Errorf("a new request after the refresh answered %q, wanted the list as it now is to charge the peer", key)
    }
}

/* an entry that names nothing is reported to the REQUEST's logger when the request has one — the emergency
   journal is standard error, which a process with a configured journal does not read once a minute — and a
   lookup that answered no address without an error does not carry an "error" of nil */
func TestTrustedProxyResolver_ReportsAnEntryThatNamesNothingToTheRequestsLogger(t *testing.T) {
    emergency := &bytes.Buffer{}
    previous := trustedProxyWarningLogger
    trustedProxyWarningLogger = func() melodyloggingcontract.Logger {
        return melodylogging.NewJsonLogger(emergency, melodyloggingcontract.LevelDebug)
    }
    t.Cleanup(func() {
        trustedProxyWarningLogger = previous
    })

    lookupTable(t, map[string][]string{"load-balancer": {}})

    journal := &bytes.Buffer{}
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })
    melodycontainer.MustRegister(
        containerInstance,
        melodylogging.ServiceLogger,
        func(resolver melodycontainercontract.Resolver) (melodyloggingcontract.Logger, error) {
            return melodylogging.NewJsonLogger(journal, melodyloggingcontract.LevelDebug), nil
        },
    )

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/products/", nil)
    httpRequest.RemoteAddr = balancerAddress + ":41234"
    request := melodyhttp.NewRequest(
        httpRequest,
        nil,
        melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance),
        melodyhttp.NewRequestContext("budget-test", time.Now()),
    )

    resolver := resolverOver(t, "load-balancer", time.Now)
    _ = resolver.Resolve(request)

    if false == strings.Contains(journal.String(), "trusted proxy entry names no address") {
        t.Errorf("the request's logger did not receive the report: %q", journal.String())
    }

    if "" != emergency.String() {
        t.Errorf("the report went to the emergency journal as well: %q", emergency.String())
    }

    if true == strings.Contains(journal.String(), `"error":null`) {
        t.Errorf("a lookup that answered no address carried an error of nil: %q", journal.String())
    }
}

/* the logger is resolved by the request that re-resolves the list and by no other, since only a re-resolution can write the record and asking every request would put the emergency logger's lock on each */
func TestTrustedProxyResolver_ResolvesTheLoggerOnlyWhenTheListIsResolved(t *testing.T) {
    previous := trustedProxyWarningLogger
    var asked int
    trustedProxyWarningLogger = func() melodyloggingcontract.Logger {
        asked++

        return previous()
    }
    t.Cleanup(func() {
        trustedProxyWarningLogger = previous
    })

    lookupTable(t, map[string][]string{"load-balancer": {balancerAddress}})

    now := time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC)
    resolver := resolverOver(t, "load-balancer", func() time.Time { return now })

    for index := 0; index < 10; index++ {
        resolver.Resolve(requestForwardedBy(t, balancerAddress, "203.0.113.7"))
    }

    if 1 != asked {
        t.Fatalf("expected the logger asked once, by the request that resolved the list, got %d", asked)
    }
}
