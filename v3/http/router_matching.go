package http

import (
    "sort"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

func (instance *Router) Match(method string, path string, host string, scheme string) (*httpcontract.MatchResult, bool) {
    handler, params, routeAttributes := instance.match(method, path, host, scheme)

    if nil == params {
        params = map[string]string{}
    }
    if nil == routeAttributes {
        routeAttributes = map[string]any{}
    }

    matchResult := &httpcontract.MatchResult{
        Handler:         handler,
        Params:          params,
        RouteAttributes: routeAttributes,
    }

    if nil == handler {
        return matchResult, false
    }

    return matchResult, true
}

/* AllowedMethods gathers the methods every route matching this path, host and scheme accepts — the four filters the matcher itself applies, locales included: a route restricted to a set of locales does not accept a request whose path carries another one, so announcing its methods advertised a route the matcher refuses to reach. What this answers is the routing table's own set; the kernel adds the synthetic OPTIONS and HEAD its MethodPolicy allows on top of it when it writes an Allow header, so a caller building that header itself gets the routes but not the synthetic entries. */
func (instance *Router) AllowedMethods(path string, host string, scheme string) []string {
    routes := instance.routeRegistry.routesInternal()
    allowedMethodsSet := make(map[string]struct{})

    pathSegments := splitRequestPath(path)

    for _, routeValue := range routes {
        if false == matchesHost(routeValue.host, host) {
            continue
        }

        if false == matchesScheme(routeValue.schemes, scheme) {
            continue
        }

        params, matched := matchPath(routeValue, pathSegments)
        if false == matched {
            continue
        }

        if false == matchesLocale(routeValue.locales, params) {
            continue
        }

        for _, method := range routeValue.methods {
            if "" == method {
                continue
            }

            allowedMethodsSet[method] = struct{}{}
        }
    }

    allowedMethods := make([]string, 0, len(allowedMethodsSet))
    for allowedMethod := range allowedMethodsSet {
        allowedMethods = append(allowedMethods, allowedMethod)
    }

    sort.Strings(allowedMethods)

    return allowedMethods
}
