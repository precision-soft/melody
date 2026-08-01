package http

import (
    nethttp "net/http"
    "strings"
    "testing"
)

/* @info AllowedMethods is the source of the Allow header a 405 carries, and it was never entered by a test. A client that receives a 405 without a correct Allow list is told the path exists and nothing about how to reach it — and a browser preflight reads the same list. */

func TestRouter_AllowedMethodsGathersEveryMethodRegisteredOnThePath(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.Handle(nethttp.MethodGet, "/articles", handler)
    router.Handle(nethttp.MethodPost, "/articles", handler)
    router.Handle(nethttp.MethodDelete, "/articles/:id", handler)

    allowed := router.AllowedMethods("/articles", "", "")

    if 2 != len(allowed) {
        t.Fatalf("expected the two methods registered on the path, got: %v", allowed)
    }

    if "GET" != allowed[0] || "POST" != allowed[1] {
        t.Fatalf("expected the methods sorted, got: %v", allowed)
    }
}

/* @info the list is sorted, so the Allow header a client sees does not depend on map iteration order — two identical deployments answering the same 405 differently is a difference nothing explains. */

func TestRouter_AllowedMethodsAreSorted(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    for _, method := range []string{nethttp.MethodPut, nethttp.MethodDelete, nethttp.MethodGet, nethttp.MethodPatch} {
        router.Handle(method, "/articles", handler)
    }

    allowed := router.AllowedMethods("/articles", "", "")

    if "DELETE, GET, PATCH, PUT" != strings.Join(allowed, ", ") {
        t.Fatalf("expected the methods in sorted order, got: %v", allowed)
    }
}

/* @info a route declared with an empty method contributes no token: an empty element is not a valid method token under RFC 9110, and an Allow header carrying ", GET" is one a strict client rejects outright. */

func TestRouter_AllowedMethodsDropsTheEmptyMethodToken(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.HandleWithOptions("/articles", handler, &RouteOptions{methods: []string{"", nethttp.MethodGet}})

    allowed := router.AllowedMethods("/articles", "", "")

    if 1 != len(allowed) || "GET" != allowed[0] {
        t.Fatalf("expected the empty method token to be dropped, got: %v", allowed)
    }

    for _, method := range allowed {
        if "" == method {
            t.Fatalf("an empty method token reached the Allow header")
        }
    }
}

/* @info a route bound to another host contributes nothing, so a 405 on one virtual host cannot advertise the methods of another — which would tell a client about routes that host does not serve. */

func TestRouter_AllowedMethodsHonoursTheHostBinding(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.HandleWithOptions("/articles", handler, &RouteOptions{
        methods: []string{nethttp.MethodGet},
        host:    "api.example.com",
    })

    router.HandleWithOptions("/articles", handler, &RouteOptions{
        methods: []string{nethttp.MethodPost},
        host:    "admin.example.com",
    })

    apiAllowed := router.AllowedMethods("/articles", "api.example.com", "")

    if 1 != len(apiAllowed) || "GET" != apiAllowed[0] {
        t.Fatalf("expected only the methods of the requested host, got: %v", apiAllowed)
    }

    adminAllowed := router.AllowedMethods("/articles", "admin.example.com", "")

    if 1 != len(adminAllowed) || "POST" != adminAllowed[0] {
        t.Fatalf("expected only the methods of the requested host, got: %v", adminAllowed)
    }
}

/* @info a scheme-restricted route contributes only on the scheme it names. matchesScheme is otherwise reached almost nowhere, and a route pinned to https advertising itself over http would invite a client to retry a request the router refuses. */

func TestRouter_AllowedMethodsHonoursTheSchemeBinding(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.HandleWithOptions("/checkout", handler, &RouteOptions{
        methods: []string{nethttp.MethodPost},
        schemes: []string{"https"},
    })

    router.HandleWithOptions("/checkout", handler, &RouteOptions{
        methods: []string{nethttp.MethodGet},
    })

    secureAllowed := router.AllowedMethods("/checkout", "", "https")

    if 2 != len(secureAllowed) {
        t.Fatalf("expected both the pinned and the unrestricted route over https, got: %v", secureAllowed)
    }

    plainAllowed := router.AllowedMethods("/checkout", "", "http")

    if 1 != len(plainAllowed) || "GET" != plainAllowed[0] {
        t.Fatalf("expected the https-pinned route to contribute nothing over http, got: %v", plainAllowed)
    }
}

/* @info the scheme comparison is case-insensitive, because a scheme written "HTTPS" in a route declaration names the same scheme the request resolved as "https"; a case-sensitive comparison would make the route unreachable with no diagnostic anywhere. */

func TestRouter_AllowedMethodsComparesTheSchemeCaseInsensitively(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions("/checkout", routeRegistryTestHandler(), &RouteOptions{
        methods: []string{nethttp.MethodPost},
        schemes: []string{"HTTPS"},
    })

    allowed := router.AllowedMethods("/checkout", "", "https")

    if 1 != len(allowed) || "POST" != allowed[0] {
        t.Fatalf("expected the scheme comparison to ignore case, got: %v", allowed)
    }
}

/* @info a path nothing is registered on yields an empty list rather than a nil one the caller has to test separately — the 405 path only ever reaches here for a path that matched something, but the accessor is public. */

func TestRouter_AllowedMethodsOnAnUnknownPathIsEmpty(t *testing.T) {
    router := NewRouter()

    router.Handle(nethttp.MethodGet, "/articles", routeRegistryTestHandler())

    allowed := router.AllowedMethods("/nothing-here", "", "")

    if 0 != len(allowed) {
        t.Fatalf("expected no methods for an unregistered path, got: %v", allowed)
    }
}
