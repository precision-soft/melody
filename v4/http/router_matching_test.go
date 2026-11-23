package http

import (
    nethttp "net/http"
    "strings"
    "testing"
)

func TestRouter_AllowedMethodsGathersEveryMethodRegisteredOnThePath(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.Handle("/articles", handler, RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/articles", handler, RouteOptions{Methods: []string{nethttp.MethodPost}})
    router.Handle("/articles/:id", handler, RouteOptions{Methods: []string{nethttp.MethodDelete}})

    allowed := router.AllowedMethods("/articles", "", "")

    if 2 != len(allowed) {
        t.Fatalf("expected the two methods registered on the path, got: %v", allowed)
    }

    if "GET" != allowed[0] || "POST" != allowed[1] {
        t.Fatalf("expected the methods sorted, got: %v", allowed)
    }
}

func TestRouter_AllowedMethodsAreSorted(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    for _, method := range []string{nethttp.MethodPut, nethttp.MethodDelete, nethttp.MethodGet, nethttp.MethodPatch} {
        router.Handle("/articles", handler, RouteOptions{Methods: []string{method}})
    }

    allowed := router.AllowedMethods("/articles", "", "")

    if "DELETE, GET, PATCH, PUT" != strings.Join(allowed, ", ") {
        t.Fatalf("expected the methods in sorted order, got: %v", allowed)
    }
}

func TestRouter_AllowedMethodsDropsTheEmptyMethodToken(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.Handle("/articles", handler, RouteOptions{Methods: []string{"", nethttp.MethodGet}})

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

func TestRouter_AllowedMethodsHonoursTheHostBinding(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.Handle("/articles", handler, RouteOptions{
        Methods: []string{nethttp.MethodGet},
        Host:    "api.example.com",
    })

    router.Handle("/articles", handler, RouteOptions{
        Methods: []string{nethttp.MethodPost},
        Host:    "admin.example.com",
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

func TestRouter_AllowedMethodsHonoursTheSchemeBinding(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.Handle("/checkout", handler, RouteOptions{
        Methods: []string{nethttp.MethodPost},
        Schemes: []string{"https"},
    })

    router.Handle("/checkout", handler, RouteOptions{
        Methods: []string{nethttp.MethodGet},
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

func TestRouter_AllowedMethodsComparesTheSchemeCaseInsensitively(t *testing.T) {
    router := NewRouter()

    router.Handle("/checkout", routeRegistryTestHandler(), RouteOptions{
        Methods: []string{nethttp.MethodPost},
        Schemes: []string{"HTTPS"},
    })

    allowed := router.AllowedMethods("/checkout", "", "https")

    if 1 != len(allowed) || "POST" != allowed[0] {
        t.Fatalf("expected the scheme comparison to ignore case, got: %v", allowed)
    }
}

func TestRouter_AllowedMethodsOnAnUnknownPathIsEmpty(t *testing.T) {
    router := NewRouter()

    router.Handle("/articles", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    allowed := router.AllowedMethods("/nothing-here", "", "")

    if 0 != len(allowed) {
        t.Fatalf("expected no methods for an unregistered path, got: %v", allowed)
    }
}

func TestRouter_AllowedMethodsSkipsARouteThePathsLocaleExcludes(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.Handle(
        "/:_locale/articles",
        handler,
        RouteOptions{Name: "articles", Methods: []string{nethttp.MethodGet}, Locales: []string{"en"}},
    )

    if 0 != len(router.AllowedMethods("/de/articles", "", "")) {
        t.Fatalf("expected no method for a locale the route excludes, got %v", router.AllowedMethods("/de/articles", "", ""))
    }

    allowed := router.AllowedMethods("/en/articles", "", "")
    if 1 != len(allowed) || nethttp.MethodGet != allowed[0] {
        t.Fatalf("expected the route's method for a locale it accepts, got %v", allowed)
    }
}

func TestRouter_AllowedMethodsAndTheMatcherAgreeOnTheSameLocalePath(t *testing.T) {
    router := NewRouter()

    handler := routeRegistryTestHandler()

    router.Handle(
        "/:_locale/articles",
        handler,
        RouteOptions{Name: "articles", Methods: []string{nethttp.MethodGet}, Locales: []string{"en"}},
    )

    for _, path := range []string{"/en/articles", "/de/articles"} {
        _, matched := router.Match(nethttp.MethodGet, path, "", "")
        announced := 0 != len(router.AllowedMethods(path, "", ""))

        if matched != announced {
            t.Fatalf("expected the announced methods and the matcher to agree on %q: matched=%t announced=%t", path, matched, announced)
        }
    }
}

func TestRouterMatch_AnEditOfTheMatchedProtection_NeverReachesTheNextMatch(t *testing.T) {
    router := NewRouter()
    router.Handle("/page", csrfProtectionTestHandler, RouteOptions{
        Methods:    []string{nethttp.MethodGet},
        Protection: RouteProtectionConfig{SecurityHeadersDisabled: []string{HeaderFrameOptions}},
    })
    router.DeclareRouteDefinition("/form", RouteOptions{
        Methods:    []string{nethttp.MethodPost},
        Protection: RouteProtectionConfig{SecurityHeadersDisabled: []string{HeaderFrameOptions}},
    })

    first, firstOk := router.Match(nethttp.MethodGet, "/page", "example.test", "https")
    if false == firstOk {
        t.Fatalf("expected the route matched")
    }
    first.RouteAttributes[RouteAttributeProtection].(RouteProtectionConfig).SecurityHeadersDisabled[0] = HeaderStrictTransportSecurity

    second, _ := router.Match(nethttp.MethodGet, "/page", "example.test", "https")
    if disabled := second.RouteAttributes[RouteAttributeProtection].(RouteProtectionConfig).SecurityHeadersDisabled; HeaderFrameOptions != disabled[0] {
        t.Fatalf("expected a fresh match untouched by an edit of an earlier match's protection, got %v", disabled)
    }

    definitionOnly, _ := router.Match(nethttp.MethodPost, "/form", "example.test", "https")
    definitionOnly.RouteAttributes[RouteAttributeProtection].(RouteProtectionConfig).SecurityHeadersDisabled[0] = HeaderStrictTransportSecurity

    again, _ := router.Match(nethttp.MethodPost, "/form", "example.test", "https")
    if disabled := again.RouteAttributes[RouteAttributeProtection].(RouteProtectionConfig).SecurityHeadersDisabled; HeaderFrameOptions != disabled[0] {
        t.Fatalf("expected the definition-only match's protection copied too, got %v", disabled)
    }
}
