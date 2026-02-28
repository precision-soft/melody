package http

import (
    "sort"

    httpcontract "github.com/precision-soft/melody/v2/http/contract"
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

func (instance *Router) AllowedMethods(path string, host string, scheme string) []string {
    routes := instance.routeRegistry.routesInternal()
    allowedMethodsSet := make(map[string]struct{})

    pathSegments := splitPath(path)

    for _, routeValue := range routes {
        if false == matchesHost(routeValue.host, host) {
            continue
        }

        if false == matchesScheme(routeValue.schemes, scheme) {
            continue
        }

        _, matched := matchPath(routeValue, pathSegments)
        if false == matched {
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
