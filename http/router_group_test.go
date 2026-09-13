package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

/* the guard exists to replace the raw dereference with a refusal that names the pattern, so an unqualified recover is satisfied by the very crash it was written to remove. */
func TestRouteGroup_PanicsWhenRouterIsNil(t *testing.T) {
    group := NewRouteGroup(nil, "/api")

    testhelper.AssertPanicsWithError(t, func() {
        group.HandleWithOptions(
            "/x",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return EmptyResponse(200), nil
            },
            NewRouteOptions("a", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
        )
    }, "router is nil")
}

/* nil options read as the default options, the router's own answer for the same input: the group was the one registration surface that refused what its sibling accepts. The route carries the group's prefix and answers every method, exactly what the defaults mean. */
func TestRouteGroup_NilOptionsReadAsTheDefaultOptions(t *testing.T) {
    router := NewRouter()
    group := router.Group("/api")

    group.HandleWithOptions(
        "/x",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        nil,
    )

    if _, matched := router.Match(nethttp.MethodGet, "/api/x", "", ""); false == matched {
        t.Fatal("expected the grouped route to answer under the group prefix")
    }

    if _, matched := router.Match(nethttp.MethodPost, "/api/x", "", ""); false == matched {
        t.Fatal("expected the default options to answer every method")
    }

    if _, matched := router.Match(nethttp.MethodGet, "/x", "", ""); true == matched {
        t.Fatal("expected the route to exist only under the group prefix")
    }
}

func TestRouteGroup_JoinsPathAndPrefixesName(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)
    group := router.Group("/api")

    group.WithNamePrefix("api.")

    defer func() {
        if nil != recover() {
            t.Fatalf("unexpected panic")
        }
    }()

    group.HandleWithOptions(
        "/user/:id",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions("user_show", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    pathValue, err := urlGenerator.GeneratePath("api.user_show", map[string]string{"id": "1"})
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
    urlGenerator := NewUrlGenerator(routeRegistry)
    group := router.Group("/api")

    group.WithDefaults(
        map[string]string{
            "page":   "1",
            "locale": "en",
        },
    )

    defer func() {
        if nil != recover() {
            t.Fatalf("unexpected panic")
        }
    }()

    group.HandleWithOptions(
        "/:locale?/list/:page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions(
            "list",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            map[string]string{
                "page": "2",
            },
            nil,
            0,
            nil,
        ),
    )

    pathValue, err := urlGenerator.GeneratePath("list", map[string]string{})
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
    urlGenerator := NewUrlGenerator(routeRegistry)
    group := router.Group("/api")

    group.WithRequirements(
        map[string]string{
            "id": "\\d+",
        },
    )

    defer func() {
        if nil != recover() {
            t.Fatalf("unexpected panic")
        }
    }()

    group.HandleWithOptions(
        "/user/:id",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions(
            "user",
            []string{nethttp.MethodGet},
            "",
            nil,
            map[string]string{
                "id": "[a-z]+",
            },
            nil,
            nil,
            0,
            nil,
        ),
    )

    _, err := urlGenerator.GeneratePath("user", map[string]string{"id": "abc"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    _, err = urlGenerator.GeneratePath("user", map[string]string{"id": "123"})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestRouteGroup_RejectsAnOptionalSegmentInThePrefix(t *testing.T) {
    router := NewRouter()
    group := router.Group("/:locale?")

    assertPanicWithExceptionMessage(
        t,
        func() {
            group.HandleWithOptions(
                "/list",
                func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    return EmptyResponse(200), nil
                },
                NewRouteOptions("list", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
            )
        },
        "optional route parameter must be the last pattern segment unless it has a default",
    )
}
func TestRouteGroup_HandleWithOptions_LeavesTheCallersOptionsUntouched(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    group := router.Group("/api")
    group.WithNamePrefix("api.")
    group.WithRequirements(map[string]string{"id": "[0-9]+"})

    options := NewRouteOptions(
        "users.show",
        []string{nethttp.MethodGet},
        "",
        nil,
        nil,
        nil,
        nil,
        0,
        nil,
    )

    handler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(200, "users"), nil
    }

    group.HandleWithOptions("/users/:id", handler, options)

    if "users.show" != options.Name() {
        t.Fatalf("expected the caller's options to keep its own name, got %q", options.Name())
    }

    if _, carried := options.Requirements()["id"]; true == carried {
        t.Fatalf("expected the group requirement to stay out of the caller's options, got %v", options.Requirements())
    }

    urlGenerator := NewUrlGenerator(routeRegistry)

    generatedPath, generateErr := urlGenerator.GeneratePath("api.users.show", map[string]string{"id": "7"})
    if nil != generateErr {
        t.Fatalf("unexpected generate error: %v", generateErr)
    }

    if "/api/users/7" != generatedPath {
        t.Fatalf("expected the group prefix to be applied once, got %q", generatedPath)
    }

    assertPanicWithExceptionMessage(
        t,
        func() {
            group.HandleWithOptions("/users/:id/edit", handler, options)
        },
        "route name already exists",
    )
}

func TestRouteGroup_HandleCarriesTheGroupPrefix(t *testing.T) {
    router := NewRouter()

    group := router.Group("/api")
    group.Handle(nethttp.MethodGet, "/articles", routeRegistryTestHandler())

    if 1 != len(router.RouteDefinitions()) {
        t.Fatalf("expected one route to be registered")
    }

    if "/api/articles" != router.RouteDefinitions()[0].Pattern() {
        t.Fatalf("expected the group prefix to be carried, got: %q", router.RouteDefinitions()[0].Pattern())
    }
}

func TestRouteGroup_HandleNamedCarriesBothPrefixes(t *testing.T) {
    router := NewRouter()

    group := router.Group("/api")
    group.WithNamePrefix("api.")
    group.HandleNamed("article.show", nethttp.MethodGet, "/articles/:id", routeRegistryTestHandler())

    definition, found := router.RouteDefinition("api.article.show")
    if false == found {
        t.Fatalf("expected the group name prefix to be carried onto the route name")
    }

    if "/api/articles/:id" != definition.Pattern() {
        t.Fatalf("expected the group path prefix to be carried, got: %q", definition.Pattern())
    }
}

func TestRouteGroup_HandleControllerCarriesTheGroupPrefix(t *testing.T) {
    router := NewRouter()

    group := router.Group("/api")
    group.HandleController(
        nethttp.MethodGet,
        "/articles",
        func(request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "from the grouped controller"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/articles", nil))

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the grouped controller to answer under the prefix, got: %d", recorder.Code)
    }

    if "from the grouped controller" != recorder.Body.String() {
        t.Fatalf("unexpected body: %q", recorder.Body.String())
    }

    ungroupedRecorder := httptest.NewRecorder()
    handler.ServeHTTP(ungroupedRecorder, httptest.NewRequest(nethttp.MethodGet, "/articles", nil))

    if nethttp.StatusOK == ungroupedRecorder.Code {
        t.Fatalf("expected the controller not to be reachable outside the group prefix")
    }
}

func TestRouteGroup_HandleNamedControllerCarriesBothPrefixes(t *testing.T) {
    router := NewRouter()

    group := router.Group("/api")
    group.WithNamePrefix("api.")
    group.HandleNamedController(
        "article.show",
        nethttp.MethodGet,
        "/articles/:id",
        func(request httpcontract.Request) (httpcontract.Response, error) {
            identifier, _ := request.Param("id")

            return TextResponse(nethttp.StatusOK, identifier), nil
        },
    )

    definition, found := router.RouteDefinition("api.article.show")
    if false == found {
        t.Fatalf("expected the group name prefix to be carried onto the route name")
    }

    if "/api/articles/:id" != definition.Pattern() {
        t.Fatalf("expected the group path prefix to be carried, got: %q", definition.Pattern())
    }

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/api/articles/42", nil))

    if "42" != recorder.Body.String() {
        t.Fatalf("expected the route parameter to reach the grouped controller, got: %q", recorder.Body.String())
    }
}
