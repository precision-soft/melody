package config

import (
    "net"
    "net/netip"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const trustedProxyRefreshInterval = time.Minute

var trustedProxyWarningLogger = melodylogging.EmergencyLogger

var trustedProxyLookup = net.LookupHost

type trustedProxyResolver struct {
    entryList []string

    mutex      sync.Mutex
    resolver   melodyhttpmiddleware.ClientIpResolver
    resolvedAt time.Time
    refreshing bool
    now        func() time.Time
}

func newTrustedProxyResolver(rawList string, now func() time.Time) *trustedProxyResolver {
    var entryList []string

    for _, rawEntry := range strings.Split(rawList, ",") {
        entry := strings.TrimSpace(rawEntry)
        if "" == entry {
            continue
        }

        if false == isPrefixOrAddress(entry) && true == strings.ContainsAny(entry, "/:[] ") {
            exception.Panic(exception.NewError(
                "a trusted proxy entry is neither a prefix, nor an address, nor a host name",
                exceptioncontract.Context{"key": environmentKeyTrustedProxyList, "entry": entry},
                nil,
            ))
        }

        entryList = append(entryList, entry)
    }

    return &trustedProxyResolver{entryList: entryList, now: now}
}

func isPrefixOrAddress(entry string) bool {
    if _, prefixErr := netip.ParsePrefix(entry); nil == prefixErr {
        return true
    }

    _, addressErr := netip.ParseAddr(entry)

    return nil == addressErr
}

/* Resolve is the ClientIpResolver both budgets are handed. */
func (instance *trustedProxyResolver) Resolve(request melodyhttpcontract.Request) string {
    return instance.current()(request)
}

func (instance *trustedProxyResolver) current() melodyhttpmiddleware.ClientIpResolver {
    instance.mutex.Lock()

    if nil == instance.resolver {
        instance.resolver = forwardedClientIpResolver(instance.resolvedList())
        instance.resolvedAt = instance.now()
        resolver := instance.resolver
        instance.mutex.Unlock()

        return resolver
    }

    resolver := instance.resolver
    stale := trustedProxyRefreshInterval <= instance.now().Sub(instance.resolvedAt)
    refreshHere := stale && false == instance.refreshing
    if true == refreshHere {
        instance.refreshing = true
    }

    instance.mutex.Unlock()

    if true == refreshHere {
        refreshed := forwardedClientIpResolver(instance.resolvedList())

        instance.mutex.Lock()
        instance.resolver = refreshed
        instance.resolvedAt = instance.now()
        instance.refreshing = false
        instance.mutex.Unlock()

        return refreshed
    }

    return resolver
}

func (instance *trustedProxyResolver) resolvedList() []string {
    var trustedProxyList []string

    for _, entry := range instance.entryList {
        if true == isPrefixOrAddress(entry) {
            trustedProxyList = append(trustedProxyList, entry)

            continue
        }

        addressList, lookupErr := trustedProxyLookup(entry)
        if nil != lookupErr || 0 == len(addressList) {
            trustedProxyWarningLogger().Warning(
                "trusted proxy entry names no address; skipped, its header is not believed",
                melodyloggingcontract.Context{"key": environmentKeyTrustedProxyList, "entry": entry, "error": lookupErr},
            )

            continue
        }

        trustedProxyList = append(trustedProxyList, addressList...)
    }

    return trustedProxyList
}

func (instance *Module) trustedProxyList() []string {
    return instance.trustedProxyResolver.resolvedList()
}

func forwardedClientIpResolver(trustedProxyList []string) melodyhttpmiddleware.ClientIpResolver {
    return melodyhttpmiddleware.NewForwardedClientIpResolver(melodyhttpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: 0 < len(trustedProxyList),
        TrustedProxyList:      trustedProxyList,
    })
}

func (instance *Module) buildTrustedProxyResolver() {
    instance.trustedProxyResolver = newTrustedProxyResolver(instance.environmentValue(environmentKeyTrustedProxyList), time.Now)
}
