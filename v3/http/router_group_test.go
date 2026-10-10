package http

import (
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* the nil options refusal sits below the nil router one and reads the same to an unqualified recover; the message is what says which of the two answered. */
func TestRouteGroup_PanicsWhenOptionsIsNil(t *testing.T) {
    router := NewRouter()
    group := router.Group("/api")

    testhelper.AssertPanicsWithError(t, func() {
        group.HandleWithOptions(
            "/x",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return EmptyResponse(200), nil
            },
            nil,
        )
    }, "the route options is nil")
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
