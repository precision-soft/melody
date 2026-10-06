package config

import (
    "context"
    "net"
    "net/netip"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyhttpmiddleware "github.com/precision-soft/melody/v3/http/middleware"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* exampleForwardedHeadersPolicy is the list the kernel believes a forwarded scheme from, which decides whether the session cookie is marked Secure behind a proxy that terminates tls. It is a static list of the private ranges and loopback, read at boot, where the rate limit's key goes through trustedProxyResolver, which trusts the balancer alone by name: a peer that forges X-Forwarded-Proto marks only its own cookie Secure, so the scheme can take the wide list a restarted balancer keeps matching, while a forged client address would choose whose budget a request spends and takes the narrow one. */
func exampleForwardedHeadersPolicy() melodyhttpcontract.ForwardedHeadersPolicy {
    return melodyhttpcontract.ForwardedHeadersPolicy{
        TrustForwardedHeaders: true,
        TrustedProxyList: []string{
            "127.0.0.0/8",
            "::1/128",
            "10.0.0.0/8",
            "172.16.0.0/12",
            "192.168.0.0/16",
        },
    }
}

/* trustedProxyRefreshInterval is how long a resolved list is believed before its names are looked up again: the compose balancer keeps its service name across a restart and loses its address. A minute is the longest a restarted balancer goes untrusted, for one lookup a minute on the request path, off the lock. */
const trustedProxyRefreshInterval = time.Minute

/* trustedProxyFailedLookupRetryInterval is how soon a list holding a name that resolved to nothing is looked up again. While a name cannot be resolved no forwarded header is believed from it, so every client behind that balancer spends ONE budget, the balancer's address: the lookup is retried every five seconds rather than held for the minute a resolved list is. */
const trustedProxyFailedLookupRetryInterval = 5 * time.Second

/* trustedProxyWarningLogger reports an entry of the list that names nothing when the resolving request carries no logger of its own. The entry is skipped rather than refused, so a cli command without the balancer beside it still runs, and the list trusts one hop fewer, which fails closed onto the peer address; a variable so a test can capture it. */
var trustedProxyWarningLogger = melodylogging.EmergencyLogger

/* trustedProxyClientIpAttribute is the request attribute the first answer about a request is kept under. */
const trustedProxyClientIpAttribute = "example.trustedProxy.clientIp"

/* trustedProxyLookupTimeout bounds one resolution of a trusted proxy name: the first lookup and the minute's refresh run in line on the request that finds the list stale, the retry of a failed lookup off it, and the system resolver retries for seconds. A lookup that does not answer in time is skipped and reported for this interval. */
const trustedProxyLookupTimeout = 2 * time.Second

/* trustedProxyLookup is the name resolution the list goes through; a variable so a test can hand it a table. */
var trustedProxyLookup = func(host string) ([]string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), trustedProxyLookupTimeout)
    defer cancel()

    return net.DefaultResolver.LookupHost(ctx, host)
}

/* trustedProxyResolver answers which client a request came from: behind a trusted proxy the X-Forwarded-For client, on a direct hit the peer address, and a header from a peer outside the list is ignored; an empty list trusts no header. Both budgets share one resolver so their trusted lists cannot drift, and the list is the balancer itself, from configuration, because trusting the whole private range would let any container choose its own key. Names are re-resolved once the interval passes, outside the lock on the request that finds the list stale; for that interval a restarted balancer is a stranger and its former address is believed, and a failed lookup leaves the list without that name, which fails closed: every client behind it is charged to the balancer's one budget until a lookup, retried every five seconds, answers. */
type trustedProxyResolver struct {
    entryList []string

    mutex         sync.Mutex
    resolver      melodyhttpmiddleware.ClientIpResolver
    nextRefreshAt time.Time
    refreshing    bool
    /* lookupFailed says the list in hand left out a name that did not resolve, so its next resolution is the five-second retry, run off the request path */
    lookupFailed bool
    /* retries tracks the retries running off the request path, which Close waits for, so the warning of the last one reaches the journal before the container closes it */
    retries sync.WaitGroup
    /* closed stops new retries off the request path once the http server shuts down */
    closed bool
    now    func() time.Time
}

/* Close starts no retry off the request path from here on and waits for the one running, bounded by the lookup's own timeout; the request paths keep the list in hand */
func (instance *trustedProxyResolver) Close() {
    instance.mutex.Lock()
    instance.closed = true
    instance.mutex.Unlock()

    instance.retries.Wait()
}

/* newTrustedProxyResolver reads the comma-separated list and refuses an entry that can be nothing (an address with a port, a bracketed address, a prefix without its length), because skipped on every request it would narrow the list in silence. A name that does not resolve right now is skipped and reported by the resolution. */
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

/* Resolve is the ClientIpResolver both budgets are handed. The first answer about a request is kept on the request, so a re-resolution of the list between the two budgets' asks cannot charge one request to two different keys. */
func (instance *trustedProxyResolver) Resolve(request melodyhttpcontract.Request) string {
    attributes := request.Attributes()
    if nil != attributes {
        if kept, found := attributes.Get(trustedProxyClientIpAttribute); true == found {
            if clientIp, isText := kept.(string); true == isText {
                return clientIp
            }
        }
    }

    /* the logger is resolved by the one request that re-resolves the list, so an entry that names nothing reaches the application's journal rather than standard error without costing every request a resolution */
    clientIp := instance.current(
        func() melodyloggingcontract.Logger {
            return examplejournal.LoggerOr(request.RuntimeInstance(), trustedProxyWarningLogger())
        },
        func() melodyloggingcontract.Logger {
            return applicationLoggerOr(request.RuntimeInstance(), trustedProxyWarningLogger())
        },
    )(request)

    if nil != attributes {
        attributes.Set(trustedProxyClientIpAttribute, clientIp)
    }

    return clientIp
}

/* current hands back the resolver over the list last resolved, resolving it when there is none or when its interval has passed: the refresh interval for a list whose names all resolved, the retry interval for one where a name did not. The resolution runs outside the lock on the one request that found the list missing or stale; every other request keeps the last list, or before the first one a list that trusts nothing, which fails closed. The retry of a failed lookup runs off the request path, the request keeping the list in hand: a resolver that does not answer would otherwise hold a request for the lookup's timeout every five seconds for as long as it is down. */
func (instance *trustedProxyResolver) current(loggerOf func() melodyloggingcontract.Logger, retryLoggerOf func() melodyloggingcontract.Logger) melodyhttpmiddleware.ClientIpResolver {
    instance.mutex.Lock()

    resolver := instance.resolver
    stale := nil == resolver || false == instance.now().Before(instance.nextRefreshAt)
    resolveHere := stale && false == instance.refreshing
    if true == resolveHere {
        instance.refreshing = true
    }

    retryOffThePath := resolveHere && nil != resolver && true == instance.lookupFailed

    if true == retryOffThePath && true == instance.closed {
        instance.refreshing = false
        instance.mutex.Unlock()

        return resolver
    }

    if true == retryOffThePath {
        instance.retries.Add(1)
    }

    instance.mutex.Unlock()

    if true == retryOffThePath {
        logger := retryLoggerOf()

        go func() {
            defer instance.retries.Done()

            instance.refresh(logger)
        }()

        return resolver
    }

    if true == resolveHere {
        return instance.refresh(loggerOf())
    }

    if nil == resolver {
        return forwardedClientIpResolver(nil)
    }

    return resolver
}

/* refresh resolves the list, installs it with its next instant and answers it; the caller set refreshing */
func (instance *trustedProxyResolver) refresh(logger melodyloggingcontract.Logger) melodyhttpmiddleware.ClientIpResolver {
    trustedProxyList, lookupFailed := instance.resolvedList(logger)
    resolved := forwardedClientIpResolver(trustedProxyList)

    refreshInterval := trustedProxyRefreshInterval
    if true == lookupFailed {
        refreshInterval = trustedProxyFailedLookupRetryInterval
    }

    instance.mutex.Lock()
    instance.resolver = resolved
    instance.lookupFailed = lookupFailed
    instance.nextRefreshAt = instance.now().Add(refreshInterval)
    instance.refreshing = false
    instance.mutex.Unlock()

    return resolved
}

/* resolvedList is the list as the middleware reads it: an address or a prefix is taken as written, and any other entry is a host name looked up now — the compose balancer is reachable by its service name and nothing else about it is stable, so naming it is the one spelling that survives a restart of the stack. A name that resolves to nothing is skipped and reported, never refused: a cli command boots without the balancer beside it; lookupFailed says so, for the retry. */
func (instance *trustedProxyResolver) resolvedList(logger melodyloggingcontract.Logger) (trustedProxyList []string, lookupFailed bool) {

    for _, entry := range instance.entryList {
        if true == isPrefixOrAddress(entry) {
            trustedProxyList = append(trustedProxyList, entry)

            continue
        }

        addressList, lookupErr := trustedProxyLookup(entry)
        if nil != lookupErr || 0 == len(addressList) {
            /* the lookup's error travels only when there is one: a name that resolved to an empty list failed
               nothing, and an "error" of nil beside it read as a failure nobody could name */
            reportContext := melodyloggingcontract.Context{"key": environmentKeyTrustedProxyList, "entry": entry}
            if nil != lookupErr {
                reportContext["error"] = lookupErr.Error()
            }

            logger.Warning("trusted proxy entry names no address; skipped, its header is not believed", reportContext)

            lookupFailed = true

            continue
        }

        trustedProxyList = append(trustedProxyList, addressList...)
    }

    return trustedProxyList, lookupFailed
}

/* applicationLoggerOr answers the application's logger, resolved from the container rather than the request's scope, for a record written after the request is answered; the fallback when there is none */
func applicationLoggerOr(runtimeInstance melodyruntimecontract.Runtime, fallback melodyloggingcontract.Logger) melodyloggingcontract.Logger {
    if nil == runtimeInstance || nil == runtimeInstance.Container() {
        return fallback
    }

    logger, resolveErr := melodycontainer.FromResolver[melodyloggingcontract.Logger](runtimeInstance.Container(), melodylogging.ServiceLogger)
    if nil != resolveErr || nil == logger {
        return fallback
    }

    return logger
}

/* trustedProxyList is the list as it resolves right now, the door the tests read the resolution through. */
func (instance *Module) trustedProxyList() []string {
    trustedProxyList, _ := instance.trustedProxyResolver.resolvedList(trustedProxyWarningLogger())

    return trustedProxyList
}

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
