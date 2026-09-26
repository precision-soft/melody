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

/* AllowedMethods gathers the methods of every route matching this path, host, scheme and locale, the matcher's own four filters. It answers the routing table's set; the kernel adds the synthetic OPTIONS and HEAD its MethodPolicy allows when it writes an Allow header. */
func (instance *Router) AllowedMethods(path string, host string, scheme string) []string {
    routes := instance.routeRegistry.routesInternal()
    allowedMethodsSet := make(map[string]struct{})

    if false == requestPathIsRoutable(path) {
        return make([]string, 0)
    }

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

        /* the defaults are merged before the locale gate reads them, as Match does */
        for key, defaultValue := range routeValue.defaults {
            if _, exists := params[key]; false == exists {
                params[key] = defaultValue
            }
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
