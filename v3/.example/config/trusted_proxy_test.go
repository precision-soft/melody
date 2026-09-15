package config

import (
    "bytes"
    "strings"
    "testing"
    "time"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

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

    table["load-balancer"] = []string{"172.18.0.13"}
    now = now.Add(30 * time.Second)
    if key := resolver.Resolve(requestForwardedBy(t, "172.18.0.13", "203.0.113.7")); "172.18.0.13" != key {
        t.Fatalf("expected the restarted balancer to be a stranger inside the interval, got %q", key)
    }
    if 1 != *lookups {
        t.Fatalf("expected no lookup inside the interval, got %d", *lookups)
    }

    now = now.Add(trustedProxyRefreshInterval)
    if key := resolver.Resolve(requestForwardedBy(t, "172.18.0.13", "203.0.113.7")); "203.0.113.7" != key {
        t.Fatalf("expected the restarted balancer to be trusted after the interval, got %q", key)
    }
    if 2 != *lookups {
        t.Fatalf("expected one lookup after the interval, got %d", *lookups)
    }
}

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

func TestNewExampleModule_HandsBothBudgetsTheSameTrustedProxyResolver(t *testing.T) {
    moduleInstance := moduleWithTrustedProxyList(t, "10.1.2.3")
    moduleInstance.buildTrustedProxyResolver()

    if nil == moduleInstance.trustedProxyResolver {
        t.Fatal("expected the module to build its resolver")
    }

    if 1 != len(moduleInstance.trustedProxyResolver.entryList) || "10.1.2.3" != moduleInstance.trustedProxyResolver.entryList[0] {
        t.Fatalf("expected the resolver to hold the configured entries, got %v", moduleInstance.trustedProxyResolver.entryList)
    }
}
