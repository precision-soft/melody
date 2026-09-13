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

/* trustedProxyRefreshInterval is how long a resolved list is believed before the names on it are looked up again. The compose balancer keeps its service name across a restart and loses its address, so a list resolved once at build — the previous form — was stale after the first restart of the balancer and empty for a process booted before the balancer's name existed, in both cases with every client behind it charged to the balancer's own key and one line on standard error. A minute is the longest a restarted balancer is not trusted, and the price is one lookup a minute on the request path, off the lock. */
const trustedProxyRefreshInterval = time.Minute

/* trustedProxyWarningLogger is where an entry of the trusted proxy list that names nothing is reported: the entry is skipped rather than refused because a name that does not resolve in this process — the balancer not started beside a cli command — must not stop the command; skipped, the list trusts one hop fewer, which fails closed onto the peer address. A variable so the test can capture what would otherwise go to standard error. */
var trustedProxyWarningLogger = melodylogging.EmergencyLogger

/* trustedProxyLookup is the name resolution the list goes through; a variable so a test can hand it a table. */
var trustedProxyLookup = net.LookupHost

/* trustedProxyResolver answers which client a request came from, the way every budget of this example has to read it: behind a trusted proxy the X-Forwarded-For client, on a direct hit the peer address, and a header sent by a peer outside the trusted list ignored rather than believed. An empty list trusts no header at all, so every request is charged to its peer.

   Both budgets share ONE resolver because they are two halves of one policy and the trusted list must not drift apart: the write throttle meters the writes that reach a handler, the request budget meters every request ahead of authentication. Left on the peer address, that one charged the whole world to the proxy, so one client could spend everyone's hour; trusting the whole private address space instead charged the header to whoever sat in it, so any other container of the deployment chose its own key per request — a budget it could refill at will, or a victim's it could spend — and, behind the compose balancer, the docker gateway every host client enters through was read as one more hop, which put the whole host population back on the balancer's key. The list is the balancer itself, from configuration.

   The entries are kept as written and the names among them are resolved when the resolver is first asked and again once the interval has passed, on the request that finds the list stale and outside the lock — every other request keeps the list last resolved, so a lookup that hangs costs one request, not a burst. The interval cuts both ways, and both are the declared posture: a balancer restarted onto a new address is a stranger for up to a minute, and its OLD address is believed for the same minute — a peer docker hands that address to inside it would be trusted; and a lookup that fails at a refresh leaves the list empty for the minute, every client behind the balancer on the balancer's key, which fails closed. */
type trustedProxyResolver struct {
    entryList []string

    mutex      sync.Mutex
    resolver   melodyhttpmiddleware.ClientIpResolver
    resolvedAt time.Time
    refreshing bool
    now        func() time.Time
}

/* newTrustedProxyResolver reads the comma-separated list and REFUSES an entry that can be nothing — neither a prefix, nor an address, nor a host name: an address with a port, a bracketed address, a prefix without its length. The middleware refuses such an entry at construction for the reason this does — skipped on every request, it narrowed the trusted list in silence, which reads at runtime as every client behind that proxy sharing one key. A name that merely does not resolve right now is a different case, and is skipped and reported by the resolution below. */
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

/* current hands back the resolver over the list last resolved, resolving it when there is none and again when the interval has passed. Both resolutions run OUTSIDE the lock, on the one request that found nothing or found the list stale, and land under the lock when they are done: every other request keeps the list last resolved — and, before there is one, a list that trusts NOTHING, so a request served while the first lookup is in flight is charged to its peer, which fails closed. The first form resolved the first list under the lock, and a lookup that hung held every concurrent request of the process for as long as it hung, on the listener that runs ahead of authentication for every request. */
func (instance *trustedProxyResolver) current() melodyhttpmiddleware.ClientIpResolver {
    instance.mutex.Lock()

    resolver := instance.resolver
    stale := nil == resolver || trustedProxyRefreshInterval <= instance.now().Sub(instance.resolvedAt)
    resolveHere := stale && false == instance.refreshing
    if true == resolveHere {
        instance.refreshing = true
    }

    instance.mutex.Unlock()

    if true == resolveHere {
        resolved := forwardedClientIpResolver(instance.resolvedList())

        instance.mutex.Lock()
        instance.resolver = resolved
        instance.resolvedAt = instance.now()
        instance.refreshing = false
        instance.mutex.Unlock()

        return resolved
    }

    if nil == resolver {
        return forwardedClientIpResolver(nil)
    }

    return resolver
}

/* resolvedList is the list as the middleware reads it: an address or a prefix is taken as written, and any other entry is a host name looked up now — the compose balancer is reachable by its service name and nothing else about it is stable, so naming it is the one spelling that survives a restart of the stack. A name that resolves to nothing is skipped and reported, never refused: a cli command boots without the balancer beside it. */
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

/* trustedProxyList is the list as it resolves right now, the door the tests read the resolution through. */
func (instance *Module) trustedProxyList() []string {
    return instance.trustedProxyResolver.resolvedList()
}

/* forwardedClientIpResolver is the middleware's resolver over one resolved list. */
func forwardedClientIpResolver(trustedProxyList []string) melodyhttpmiddleware.ClientIpResolver {
    return melodyhttpmiddleware.NewForwardedClientIpResolver(melodyhttpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: 0 < len(trustedProxyList),
        TrustedProxyList:      trustedProxyList,
    })
}

/* buildTrustedProxyResolver reads the list at build and resolves nothing: the names on it are looked up at the first request, which arrives through the balancer and therefore after the balancer exists. */
func (instance *Module) buildTrustedProxyResolver() {
    instance.trustedProxyResolver = newTrustedProxyResolver(instance.environmentValue(environmentKeyTrustedProxyList), time.Now)
}
