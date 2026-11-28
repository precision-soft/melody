package http

import (
    nethttp "net/http"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

/* the guard exists to replace the raw dereference with a refusal that names the pattern, so an unqualified recover is satisfied by the very crash it was written to remove. */
func TestRouteGroup_PanicsWhenRouterIsNil(t *testing.T) {
    group := NewRouteGroup(nil, GroupConfig{PathPrefix: "/api"})

    testhelper.AssertPanicsWithError(t, func() {
        group.Handle(
            "/x",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return EmptyResponse(200), nil
            },
            RouteOptions{Name: "a", Methods: []string{nethttp.MethodGet}},
        )
    }, "router is nil")
}

func TestRouteGroup_JoinsPathAndPrefixesName(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})
    group := router.Group(GroupConfig{PathPrefix: "/api", NamePrefix: "api."})

    defer func() {
        if nil != recover() {
            t.Fatalf("unexpected panic")
        }
    }()

    group.Handle(
        "/user/:id",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "user_show", Methods: []string{nethttp.MethodGet}},
    )

    pathValue, err := urlGenerator.GeneratePath("api.user_show", httpcontract.PathOptions{Parameters: map[string]string{"id": "1"}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/api/user/1" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestRouteGroup_MergesDefaultsAndDoesNotOverrideRouteDefault(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})
    group := router.Group(
        GroupConfig{
            PathPrefix: "/api",
            Defaults: map[string]string{
                "page":   "1",
                "locale": "en",
            },
        },
    )

    defer func() {
        if nil != recover() {
            t.Fatalf("unexpected panic")
        }
    }()

    group.Handle(
        "/:locale?/list/:page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:    "list",
            Methods: []string{nethttp.MethodGet},
            Defaults: map[string]string{
                "page": "2",
            },
        },
    )

    pathValue, err := urlGenerator.GeneratePath("list", httpcontract.PathOptions{Parameters: map[string]string{}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/api/en/list/2" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestRouteGroup_MergesRequirementsAndDoesNotOverrideRouteRequirement(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})
    group := router.Group(
        GroupConfig{
            PathPrefix: "/api",
            Requirements: map[string]string{
                "id": "\\d+",
            },
        },
    )

    defer func() {
        if nil != recover() {
            t.Fatalf("unexpected panic")
        }
    }()

    group.Handle(
        "/user/:id",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:    "user",
            Methods: []string{nethttp.MethodGet},
            Requirements: map[string]string{
                "id": "[a-z]+",
            },
        },
    )

    _, err := urlGenerator.GeneratePath("user", httpcontract.PathOptions{Parameters: map[string]string{"id": "abc"}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    _, err = urlGenerator.GeneratePath("user", httpcontract.PathOptions{Parameters: map[string]string{"id": "123"}})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestRouteGroup_RejectsAnOptionalSegmentInThePrefix(t *testing.T) {
    router := NewRouter()
    group := router.Group(GroupConfig{PathPrefix: "/:locale?"})

    assertPanicWithExceptionMessage(
        t,
        func() {
            group.Handle(
                "/list",
                func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    return EmptyResponse(200), nil
                },
                RouteOptions{Name: "list", Methods: []string{nethttp.MethodGet}},
            )
        },
        "optional route parameter must be the last pattern segment unless it has a default",
    )
}

func TestRouteGroup_Handle_LeavesTheCallersOptionsUntouched(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    group := router.Group(
        GroupConfig{
            PathPrefix:   "/api",
            NamePrefix:   "api.",
            Requirements: map[string]string{"id": "[0-9]+"},
            Defaults:     map[string]string{"format": "json"},
        },
    )

    options := RouteOptions{
        Name:         "users.show",
        Methods:      []string{nethttp.MethodGet},
        Requirements: map[string]string{},
        Defaults:     map[string]string{},
    }

    handler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(200, "users"), nil
    }

    group.Handle("/users/:id", handler, options)

    if "users.show" != options.Name {
        t.Fatalf("expected the caller's options to keep its own name, got %q", options.Name)
    }

    if _, carried := options.Requirements["id"]; true == carried {
        t.Fatalf("expected the group requirement to stay out of the caller's map, got %v", options.Requirements)
    }

    if _, carried := options.Defaults["format"]; true == carried {
        t.Fatalf("expected the group default to stay out of the caller's map, got %v", options.Defaults)
    }

    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    generatedPath, generateErr := urlGenerator.GeneratePath("api.users.show", httpcontract.PathOptions{Parameters: map[string]string{"id": "7"}})
    if nil != generateErr {
        t.Fatalf("unexpected generate error: %v", generateErr)
    }

    if "/api/users/7" != generatedPath {
        t.Fatalf("expected the group prefix to be applied once, got %q", generatedPath)
    }

    assertPanicWithExceptionMessage(
        t,
        func() {
            group.Handle("/users/:id/edit", handler, options)
        },
        "route name already exists",
    )
}

func newRouteGroupTestHandler() httpcontract.Handler {
    return func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return EmptyResponse(200), nil
    }
}

func TestRouteGroup_TheZeroConfigRegistersThePatternAndTheNameAsGiven(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.Group(GroupConfig{}).Handle("/plain/:id", newRouteGroupTestHandler(), RouteOptions{Name: "plain", Methods: []string{nethttp.MethodGet}})

    pathValue, err := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{}).GeneratePath("plain", httpcontract.PathOptions{Parameters: map[string]string{"id": "1"}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/plain/1" != pathValue {
        t.Fatalf("expected the pattern unprefixed, got %q", pathValue)
    }
}

func TestRouteGroup_AnUnnamedRouteStaysUnnamedUnderANamePrefix(t *testing.T) {
    router := NewRouter()

    router.Group(GroupConfig{PathPrefix: "/api", NamePrefix: "api."}).Handle("/ping", newRouteGroupTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    for _, routeDefinition := range router.RouteDefinitions() {
        if "" != routeDefinition.Name() {
            t.Fatalf("expected the unnamed route to stay unnamed, got %q", routeDefinition.Name())
        }
    }
}

func TestRouteGroup_KeepsItsOwnCopyOfTheMaps(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    requirements := map[string]string{"id": "\\d+"}
    defaults := map[string]string{"page": "1"}

    group := router.Group(GroupConfig{Requirements: requirements, Defaults: defaults})

    requirements["id"] = "[a-z]+"
    defaults["page"] = "9"

    group.Handle("/user/:id/:page?", newRouteGroupTestHandler(), RouteOptions{Name: "user", Methods: []string{nethttp.MethodGet}})

    _, err := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{}).GeneratePath("user", httpcontract.PathOptions{Parameters: map[string]string{"id": "abc"}})
    if nil == err {
        t.Fatalf("expected the group's requirement as built, a mutation of the caller's map reached the group")
    }

    routeDefinition, found := router.RouteDefinition("user")
    if false == found || "1" != routeDefinition.Defaults()["page"] {
        t.Fatalf("expected the group's default as built, got %+v", routeDefinition)
    }
}
