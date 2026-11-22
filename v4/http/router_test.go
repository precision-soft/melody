package http

import (
    "context"
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "reflect"
    "strings"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/routeseam"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

type testEnvironmentSource struct {
    values map[string]string
}

func (instance *testEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func TestRouter_HandleAndServeHttp_HappyPath(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/hello",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 200 != recorder.Code {
        t.Fatalf("unexpected status")
    }
    if "ok" != recorder.Body.String() {
        t.Fatalf("unexpected body")
    }
}

func TestRouter_MethodNotAllowed(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/hello",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodPost, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 405 != recorder.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_NotFound(t *testing.T) {
    router := NewRouter()

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/missing", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 404 != recorder.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_PanicConvertedTo500(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/panic",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            exception.Panic(exception.NewError("boom", nil, nil))
            return nil, nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/panic", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 500 != recorder.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_HandlerErrorConvertedTo500(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/err",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, errors.New("handler error")
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/err", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 500 != recorder.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_ParameterExtraction(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/user/:id",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            value, exists := request.Parameter("id")
            if false == exists {
                return TextResponse(500, "missing id"), nil
            }

            return TextResponse(200, value), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/user/123", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 200 != recorder.Code {
        t.Fatalf("unexpected status")
    }
    if "123" != recorder.Body.String() {
        t.Fatalf("unexpected body")
    }
}

func TestRouter_RequirementWithAlternationMatchesTheWholeSegment(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/shop/:locale/list",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
        RouteOptions{
            Name:         "shop.list",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"locale": "en|de|fr"},
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    accepted := []string{"en", "de", "fr"}
    for _, locale := range accepted {
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/shop/"+locale+"/list", nil))

        if 200 != recorder.Code {
            t.Fatalf("expected the whitelisted locale %q to route, got %d", locale, recorder.Code)
        }
    }

    rejected := []string{"aden", "frfr", "xxde", "enen", "en..%2fetc%2fpasswd"}
    for _, locale := range rejected {
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/shop/"+locale+"/list", nil))

        if 200 == recorder.Code {
            t.Fatalf("expected the non-whitelisted locale %q to be refused, but it routed", locale)
        }
    }
}

func TestRouter_RequirementIsEnforcedOnCatchAllWildcard(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/files/*path",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
        RouteOptions{
            Name:         "files.serve",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"path": `[a-z0-9/]+`},
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/files/a/b/c", nil))
    if 200 != recorder.Code {
        t.Fatalf("expected a conforming catch-all value to route, got %d", recorder.Code)
    }

    rejected := httptest.NewRecorder()
    handler.ServeHTTP(rejected, httptest.NewRequest(nethttp.MethodGet, "/files/A..%2fetc", nil))
    if 200 == rejected.Code {
        t.Fatal("expected a catch-all value violating the requirement to be refused")
    }
}

func TestRouter_Match_WhitespacePaddedRequestPathDoesNotAliasTheRoute(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/admin",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "admin"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    for _, requestPath := range []string{"/admin ", "/admin\t", "/admin\n", "/admin\u00a0", " /admin"} {
        if _, matched := router.Match(nethttp.MethodGet, requestPath, "", ""); true == matched {
            t.Fatalf("expected the padded path %q not to match the route", requestPath)
        }
    }

    if _, matched := router.Match(nethttp.MethodGet, "/admin", "", ""); false == matched {
        t.Fatalf("expected the exact path to match the route")
    }
}

func TestRouter_Match_EmptySegmentDoesNotSatisfyNamedParameter(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/users/:id/profile",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "profile"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    if _, matched := router.Match(nethttp.MethodGet, "/users//profile", "", ""); true == matched {
        t.Fatalf("expected an empty identifier segment not to match")
    }

    routeMatch, matched := router.Match(nethttp.MethodGet, "/users/7/profile", "", "")
    if false == matched {
        t.Fatalf("expected a supplied identifier to match")
    }

    if "7" != routeMatch.Parameters["id"] {
        t.Fatalf("expected the identifier to be bound, got %q", routeMatch.Parameters["id"])
    }
}

func TestRouter_AddRoute_RejectsOptionalSegmentBeforeAnotherSegment(t *testing.T) {
    for _, pattern := range []string{"/blog/:locale?/posts", "/:locale?/blog", "/blog/:locale?/:page?"} {
        router := NewRouter()

        assertPanicWithExceptionMessage(
            t,
            func() {
                router.Handle(
                    pattern,
                    func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                        return TextResponse(200, "blog"), nil
                    },
                    RouteOptions{Methods: []string{nethttp.MethodGet}},
                )
            },
            "optional route parameter must be the last pattern segment unless it has a default",
        )
    }
}

func TestRouter_AddRoute_AcceptsANonTrailingOptionalSegmentThatHasADefault(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.Handle(
        "/:locale?/list/:page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "list"), nil
        },
        RouteOptions{
            Name:    "list",
            Methods: []string{nethttp.MethodGet},
            Defaults: map[string]string{
                "locale": "en",
            },
        },
    )

    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    generatedPath, generateErr := urlGenerator.GeneratePath("list", httpcontract.PathConfig{Parameters: map[string]string{"page": "2"}})
    if nil != generateErr {
        t.Fatalf("unexpected generate error: %v", generateErr)
    }
    if "/en/list/2" != generatedPath {
        t.Fatalf("expected the default to be substituted, got %q", generatedPath)
    }

    if _, matched := router.Match(nethttp.MethodGet, generatedPath, "", ""); false == matched {
        t.Fatalf("expected the generated path %q to match the route it came from", generatedPath)
    }
}

func TestRouter_AddRoute_AcceptsOptionalSegmentAtTheTail(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/blog/posts/:locale?",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "blog"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    for _, requestPath := range []string{"/blog/posts", "/blog/posts/en"} {
        if _, matched := router.Match(nethttp.MethodGet, requestPath, "", ""); false == matched {
            t.Fatalf("expected %q to match a route with a trailing optional segment", requestPath)
        }
    }
}

func TestRouter_AddRoute_NonTrailingOptionalWithADefaultSurvivesASuppliedEmptyValue(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.Handle(
        "/:locale?/list/:page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "list"), nil
        },
        RouteOptions{
            Name:    "list",
            Methods: []string{nethttp.MethodGet},
            Defaults: map[string]string{
                "locale": "en",
            },
        },
    )

    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    generatedPath, generateErr := urlGenerator.GeneratePath("list", httpcontract.PathConfig{Parameters: map[string]string{
            "locale": "",
            "page":   "2",
        }})
    if nil != generateErr {
        t.Fatalf("unexpected generate error: %v", generateErr)
    }

    if "/en/list/2" != generatedPath {
        t.Fatalf("expected the default to fill in for the supplied empty value, got %q", generatedPath)
    }

    routeMatch, matched := router.Match(nethttp.MethodGet, generatedPath, "", "")
    if false == matched {
        t.Fatalf("expected the generated path %q to match the route it came from", generatedPath)
    }

    if "en" != routeMatch.Parameters["locale"] {
        t.Fatalf("expected the locale to be bound to the default, got %q", routeMatch.Parameters["locale"])
    }

    if "2" != routeMatch.Parameters["page"] {
        t.Fatalf("expected the page to be bound, got %q", routeMatch.Parameters["page"])
    }
}

func TestRouter_AddRoute_NonTrailingOptionalWithAGroupSuppliedDefaultSurvivesASuppliedEmptyValue(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    group := router.Group(
        GroupConfig{
            PathPrefix: "/shop",
            Defaults: map[string]string{
                "locale": "en",
            },
        },
    )

    group.Handle(
        "/:locale?/list/:page",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "list"), nil
        },
        RouteOptions{Name: "shop.list", Methods: []string{nethttp.MethodGet}},
    )

    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    for _, parameters := range []map[string]string{
        {"page": "2"},
        {"locale": "", "page": "2"},
    } {
        generatedPath, generateErr := urlGenerator.GeneratePath("shop.list", httpcontract.PathConfig{Parameters: parameters})
        if nil != generateErr {
            t.Fatalf("unexpected generate error: %v", generateErr)
        }

        if "/shop/en/list/2" != generatedPath {
            t.Fatalf("expected the group default to be substituted, got %q", generatedPath)
        }

        if _, matched := router.Match(nethttp.MethodGet, generatedPath, "", ""); false == matched {
            t.Fatalf("expected the generated path %q to match the route it came from", generatedPath)
        }
    }
}

func TestRouter_AddRoute_RejectsANonTrailingOptionalWhoseDefaultIsEmpty(t *testing.T) {
    router := NewRouter()

    assertPanicWithExceptionMessage(
        t,
        func() {
            router.Handle(
                "/:locale?/list/:page",
                func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    return TextResponse(200, "list"), nil
                },
                RouteOptions{
                    Name:    "list",
                    Methods: []string{nethttp.MethodGet},
                    Defaults: map[string]string{
                        "locale": "",
                    },
                },
            )
        },
        "optional route parameter must be the last pattern segment unless it has a default",
    )
}
func TestRouter_AddRoute_ServesATrailingOptionalReachedThroughTheRoot(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.Handle(
        "/:page?",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "home"), nil
        },
        RouteOptions{
            Name:    "home",
            Methods: []string{nethttp.MethodGet},
        },
    )

    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    generatedPath, generateErr := urlGenerator.GeneratePath("home", httpcontract.PathConfig{})
    if nil != generateErr {
        t.Fatalf("unexpected generate error: %v", generateErr)
    }

    if "/" != generatedPath {
        t.Fatalf("expected the omitted optional to generate the root path, got %q", generatedPath)
    }

    matchResult, matched := router.Match(nethttp.MethodGet, generatedPath, "", "")
    if false == matched {
        t.Fatalf("expected the generated path %q to match the route it came from", generatedPath)
    }

    if _, bound := matchResult.Parameters["page"]; true == bound {
        t.Fatalf("expected the omitted optional to stay unbound, got %v", matchResult.Parameters)
    }
}

func TestRouter_Match_NeverAdvertisesAnEmptyAllowedMethod(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/empty-method",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "never"), nil
        },
        RouteOptions{Methods: []string{""}},
    )

    matchResult, matched := router.Match(nethttp.MethodGet, "/empty-method", "", "")
    if true == matched {
        t.Fatalf("expected a route declared with an empty method to stay unreachable")
    }

    advertised, carried := matchResult.RouteAttributes[RouteAttributeMethods]
    if false == carried {
        return
    }

    methodList, ok := advertised.([]string)
    if false == ok {
        t.Fatalf("expected the advertised methods to be a string list, got %T", advertised)
    }

    for _, methodName := range methodList {
        if "" == methodName {
            t.Fatalf("expected no empty method token among the advertised methods, got %q", methodList)
        }
    }
}

func TestRouter_AllowHeaderRespectsMethodPolicy(t *testing.T) {
    newGetOnlyKernel := func() *Kernel {
        router := NewRouter()
        router.Handle(
            "/hello",
            func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return TextResponse(200, "ok"), nil
            },
            RouteOptions{Methods: []string{nethttp.MethodGet}},
        )

        return NewKernel(router, KernelConfig{})
    }

    allowFor := func(kernel *Kernel) string {
        handler := kernel.ServeHttp(newHttpTestContainer())
        request := httptest.NewRequest(nethttp.MethodPost, "/hello", nil)
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, request)

        if 405 != recorder.Code {
            t.Fatalf("expected 405, got %d", recorder.Code)
        }

        return recorder.Header().Get("Allow")
    }

    /* default policy advertises the synthetic OPTIONS and HEAD it actually honors */
    defaultAllow := allowFor(newGetOnlyKernel())
    if false == strings.Contains(defaultAllow, nethttp.MethodOptions) || false == strings.Contains(defaultAllow, nethttp.MethodHead) {
        t.Fatalf("default policy Allow must advertise OPTIONS and HEAD, got %q", defaultAllow)
    }

    /* with both policy flags off, OPTIONS and HEAD in fact return 405, so Allow must not promise them */
    restricted := newGetOnlyKernel()
    restricted.policies.methodPolicy.AutomaticOptionsDisabled = true
    restricted.policies.methodPolicy.HeadFallbackDisabled = true
    restrictedAllow := allowFor(restricted)

    if true == strings.Contains(restrictedAllow, nethttp.MethodOptions) {
        t.Fatalf("Allow must not advertise OPTIONS when AutomaticOptionsDisabled is set, got %q", restrictedAllow)
    }
    if true == strings.Contains(restrictedAllow, nethttp.MethodHead) {
        t.Fatalf("Allow must not advertise HEAD when HeadFallbackDisabled is set, got %q", restrictedAllow)
    }
    if false == strings.Contains(restrictedAllow, nethttp.MethodGet) {
        t.Fatalf("Allow must still advertise the real GET method, got %q", restrictedAllow)
    }
}

func TestControllerHandler_RegistersTheControllerUnderItsMethod(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/articles",
        ControllerHandler(func(request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "from the controller"), nil
        }),
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/articles", nil))

    if nethttp.StatusOK != recorder.Code {
        t.Fatalf("expected the controller to answer, got: %d", recorder.Code)
    }

    if "from the controller" != recorder.Body.String() {
        t.Fatalf("unexpected body: %q", recorder.Body.String())
    }

    otherMethodRecorder := httptest.NewRecorder()
    handler.ServeHTTP(otherMethodRecorder, httptest.NewRequest(nethttp.MethodPost, "/articles", nil))

    if nethttp.StatusOK == otherMethodRecorder.Code {
        t.Fatalf("expected the controller to be bound to its method alone")
    }
}

func TestControllerHandler_ANamedRouteServesTheControllerUnderTheNameTheGeneratorResolves(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/articles/:id",
        ControllerHandler(func(request httpcontract.Request) (httpcontract.Response, error) {
            identifier, _ := request.Parameter("id")

            return TextResponse(nethttp.StatusOK, identifier), nil
        }),
        RouteOptions{Name: "article.show", Methods: []string{nethttp.MethodGet}},
    )

    definition, found := router.RouteDefinition("article.show")
    if false == found {
        t.Fatalf("expected the named controller route to be registered under its name")
    }

    if "/articles/:id" != definition.Pattern() {
        t.Fatalf("unexpected pattern: %q", definition.Pattern())
    }

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/articles/42", nil))

    if "42" != recorder.Body.String() {
        t.Fatalf("expected the route parameter to reach the controller, got: %q", recorder.Body.String())
    }
}

func TestRouter_RouteRegistryIsTheOneItRegistersInto(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.Handle("/articles", routeRegistryTestHandler(), RouteOptions{Name: "article.show", Methods: []string{nethttp.MethodGet}})

    if routeRegistry != router.RouteRegistry() {
        t.Fatalf("expected the router to report the registry it was built with")
    }

    if 1 != len(router.RouteRegistry().RouteDefinitions()) {
        t.Fatalf("expected the registration to be visible through the reported registry")
    }
}

func TestRouter_MatchHandsOutACopyOfTheRouteAttributes(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/aliased",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    firstResult, matched := router.Match(nethttp.MethodGet, "/aliased", "", "http")
    if false == matched || nil == firstResult {
        t.Fatalf("expected a match")
    }

    firstMethods, ok := firstResult.RouteAttributes[RouteAttributeMethods].([]string)
    if false == ok || 0 == len(firstMethods) {
        t.Fatalf("expected the methods attribute")
    }

    firstMethods[0] = "HACKED"
    firstResult.RouteAttributes["injected"] = "yes"

    secondResult, _ := router.Match(nethttp.MethodGet, "/aliased", "", "http")

    secondMethods, ok := secondResult.RouteAttributes[RouteAttributeMethods].([]string)
    if false == ok || nethttp.MethodGet != secondMethods[0] {
        t.Fatalf("expected the registry slice to be untouched by the caller's mutation, got %v", secondMethods)
    }

    if _, exists := secondResult.RouteAttributes["injected"]; true == exists {
        t.Fatalf("expected the registry map to be untouched by the caller's mutation")
    }
}

/* a pattern that names one parameter twice is ambiguous by construction: the extraction writes both segments under one map key, so the handler can only ever read one of the two values and cannot tell which, and the openapi document emitted for it is spec-invalid on duplicate path parameters. */
func TestRouterRegistration_RefusesADuplicateParameterName(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().Handle(
                "/orgs/:id/members/:id",
                func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    return nil, nil
                },
                RouteOptions{Methods: []string{nethttp.MethodGet}},
            )
        },
        "route parameter name is declared twice in one pattern",
    )
}

func TestRouterRegistration_RefusesAParameterDeclaredTwiceThroughAnOptionalOrACatchAllSegment(t *testing.T) {
    handler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, nil
    }

    for _, pattern := range []string{"/a/:id/b/:id?", "/a/:path/*path..."} {
        testhelper.AssertPanicsWithError(
            t,
            func() {
                NewRouter().Handle(pattern, handler, RouteOptions{Methods: []string{nethttp.MethodGet}})
            },
            "route parameter name is declared twice in one pattern",
        )
    }

    for _, pattern := range []string{"/a/:id/b/:other?", "/a/:id/*rest..."} {
        NewRouter().Handle(pattern, handler, RouteOptions{Methods: []string{nethttp.MethodGet}})
    }
}

/* a bare ":" binds nothing, so the segment it occupies would be matched and then discarded in silence */
func TestRouterRegistration_RefusesAParameterWithNoName(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewRouter().Handle(
                "/orgs/:",
                func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    return nil, nil
                },
                RouteOptions{Methods: []string{nethttp.MethodGet}},
            )
        },
        "route parameter must be named",
    )
}

/* an unnamed catch-all is the deliberate spelling of "swallow the rest and bind nothing" */
func TestRouterRegistration_AcceptsAnUnnamedCatchAll(t *testing.T) {
    router := NewRouter()
    router.Handle(
        "/files/*...",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    if _, matched := router.Match(nethttp.MethodGet, "/files/a/b", "", "http"); false == matched {
        t.Fatalf("expected the unnamed catch-all to match")
    }
}

func TestRouterRegistration_RefusesBraceParameterSyntax(t *testing.T) {
    router := NewRouter()

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle("/users/{id}", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})
    }, "route parameter must be written as :name, not {name}")
}

func TestRouterRegistration_AcceptsALiteralSegmentThatOnlyOpensABrace(t *testing.T) {
    router := NewRouter()

    router.Handle("/users/{id", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    handler, _, _ := router.match(nethttp.MethodGet, "/users/{id", "", "https")
    if nil == handler {
        t.Fatalf("expected a segment that is not a brace pair to stay a literal segment")
    }
}

func TestRouterRegistration_RefusesLocalesWithoutALocaleParameter(t *testing.T) {
    router := NewRouter()

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle(
            "/articles",
            routeRegistryTestHandler(),
            RouteOptions{Name: "article.list", Methods: []string{nethttp.MethodGet}, Locales: []string{"en", "de"}},
        )
    }, "route declares locales but neither its pattern nor its defaults supply _locale")
}

func TestRouterRegistration_AcceptsLocalesSuppliedByADefault(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "/articles",
        routeRegistryTestHandler(),
        RouteOptions{
            Name:     "article.list",
            Methods:  []string{nethttp.MethodGet},
            Defaults: map[string]string{RouteAttributeLocale: "en"},
            Locales:  []string{"en", "de"},
        },
    )

    handler, parameters, _ := router.match(nethttp.MethodGet, "/articles", "", "https")
    if nil == handler {
        t.Fatalf("expected a route whose locale comes from a default to be reachable")
    }

    if "en" != parameters[RouteAttributeLocale] {
        t.Fatalf("expected the default locale to reach the handler, got %q", parameters[RouteAttributeLocale])
    }
}

func TestRouterRegistration_RefusesALocaleParameterWithNoLocaleList(t *testing.T) {
    router := NewRouter()

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle("/:_locale/articles", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})
    }, "route pattern carries _locale but declares no locales to validate it against")
}

func TestRouterRegistration_AnEmptyPatternRegistersTheRootRoute(t *testing.T) {
    router := NewRouter()

    router.Handle("", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    handler, _, _ := router.match(nethttp.MethodGet, "/", "", "https")
    if nil == handler {
        t.Fatalf("expected an empty pattern to register the root route rather than an unreachable one")
    }
}

func TestRouterMatch_AnEmptyPathReachesTheRootRoute(t *testing.T) {
    router := NewRouter()

    router.Handle("/", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    handler, _, _ := router.match(nethttp.MethodGet, "", "", "https")
    if nil == handler {
        t.Fatalf("expected an empty path to be read as the root")
    }
}

func TestRouterMatch_TheAsteriskFormIsNotPathRouted(t *testing.T) {
    router := NewRouter()

    router.Handle("/:anything", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodOptions}})

    handler, parameters, _ := router.match(nethttp.MethodOptions, "*", "", "https")
    if nil != handler {
        t.Fatalf("expected the asterisk-form target not to be offered to a parameter route, bound %v", parameters)
    }

    if 0 != len(router.AllowedMethods("*", "", "https")) {
        t.Fatalf("expected the asterisk-form target to advertise no routed methods")
    }
}

func TestRouterMatch_AnEncodedSlashDoesNotSplitASegment(t *testing.T) {
    router := NewRouter()

    router.Handle("/admin/users", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/:name", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    handler, parameters, _ := router.match(nethttp.MethodGet, "/admin%2Fusers", "", "https")
    if nil == handler {
        t.Fatalf("expected the encoded spelling to match the single-segment route")
    }

    if "admin/users" != parameters["name"] {
        t.Fatalf("expected the encoded separator to stay inside the bound value, got %q", parameters["name"])
    }
}

func TestRouterMatch_AnEncodedSegmentIsUnescapedForTheHandler(t *testing.T) {
    router := NewRouter()

    router.Handle("/article/:slug", routeRegistryTestHandler(), RouteOptions{Methods: []string{nethttp.MethodGet}})

    _, parameters, _ := router.match(nethttp.MethodGet, "/article/hello%20world", "", "https")
    if "hello world" != parameters["slug"] {
        t.Fatalf("expected the segment to be unescaped for the handler, got %q", parameters["slug"])
    }
}

/* a route the registry declined is not put in the matching tree. The index the tree receives is the position of the last STORED route, so registering it for a declined duplicate gave the pattern's entry somebody else's route — the invariant every reader of the tree relies on, and the one the priority tie-break reads the index for. */
func TestRouterAddRoute_ADeclinedDuplicateDoesNotEnterTheMatchingTree(t *testing.T) {
    router := NewRouter()

    handler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, nil
    }

    recordedCollisions := []string{}
    router.routeRegistry.InstallBootCollisionRecorder(routeseam.Token{}, func(kind string, name string) {
        recordedCollisions = append(recordedCollisions, name)
    })

    router.Handle("/alpha", handler, RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/beta", handler, RouteOptions{Methods: []string{nethttp.MethodGet}})
    router.Handle("/alpha", handler, RouteOptions{Methods: []string{nethttp.MethodGet}})

    if 1 != len(recordedCollisions) {
        t.Fatalf("expected the duplicate to be recorded once, got %v", recordedCollisions)
    }

    registeredRoutes := router.routeRegistry.routesInternal()
    alphaNode := router.routeTreeRoot.staticChildren["alpha"]
    if nil == alphaNode {
        t.Fatal("expected a tree node for the alpha segment")
    }

    for _, registeredIndex := range alphaNode.routeIndices {
        if "/alpha" != registeredRoutes[registeredIndex].pattern {
            t.Fatalf("expected every entry of the alpha node to name /alpha, got %q at index %d", registeredRoutes[registeredIndex].pattern, registeredIndex)
        }
    }
}

func TestRouter_CatchAllKeepsAnEncodedSeparatorInsideItsSegment(t *testing.T) {
    router := NewRouter()

    captured := make([]string, 0, 2)

    router.Handle(
        "/files/*path",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            pathValue, _ := request.Parameter("path")
            captured = append(captured, pathValue)

            return TextResponse(200, "ok"), nil
        },
        RouteOptions{
            Name:    "files.serve",
            Methods: []string{nethttp.MethodGet},
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    for _, target := range []string{"/files/a%2Fb/c", "/files/a/b/c"} {
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, target, nil))
        if 200 != recorder.Code {
            t.Fatalf("expected %q to route, got %d", target, recorder.Code)
        }
    }

    if 2 != len(captured) {
        t.Fatalf("expected both requests to reach the handler, got %d", len(captured))
    }

    if captured[0] == captured[1] {
        t.Fatalf("expected the encoded separator to keep the two targets apart, both bound %q", captured[0])
    }
}

/* a literal "%2F" is sent as "%252F"; the catch-all binds it as the encoded separator, the same value "/files/a%2Fb" binds, since only the separator is re-escaped on the way to the binding */
func TestRouter_CatchAllBindsALiteralEncodedSeparatorAsTheEncodedSeparator(t *testing.T) {
    router := NewRouter()

    captured := make([]string, 0, 2)

    router.Handle(
        "/files/*path",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            pathValue, _ := request.Parameter("path")
            captured = append(captured, pathValue)

            return TextResponse(200, "ok"), nil
        },
        RouteOptions{
            Name:    "files.serve",
            Methods: []string{nethttp.MethodGet},
        },
    )

    handler := NewKernel(router, KernelConfig{}).ServeHttp(newHttpTestContainer())

    for _, target := range []string{"/files/a%252Fb", "/files/a%2Fb"} {
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, target, nil))
        if 200 != recorder.Code {
            t.Fatalf("expected %q to route, got %d", target, recorder.Code)
        }
    }

    if 2 != len(captured) {
        t.Fatalf("expected both requests to reach the handler, got %d", len(captured))
    }

    if "a%2Fb" != captured[0] || captured[0] != captured[1] {
        t.Fatalf("expected both targets to bind the encoded separator a%%2Fb, got %q and %q", captured[0], captured[1])
    }
}

/* the readers spin on the registration guard while the kernel raises the flag from another goroutine, the way a late registration meets a kernel that starts serving: each reader must see the raised flag and refuse. */
func TestRouter_RegistrationGuardReadsTheServingFlagRaisedByAnotherGoroutine(t *testing.T) {
    const readerCount = 8

    router := NewRouter()
    release := make(chan struct{})

    var ready sync.WaitGroup
    var done sync.WaitGroup

    ready.Add(readerCount)
    done.Add(readerCount)

    for index := 0; index < readerCount; index++ {
        go func() {
            defer done.Done()

            ready.Done()
            <-release

            for {
                refused := false

                func() {
                    defer func() {
                        if nil != recover() {
                            refused = true
                        }
                    }()

                    router.refuseRegistrationWhileServing("/late")
                }()

                if true == refused {
                    return
                }
            }
        }()
    }

    ready.Wait()
    close(release)
    router.freezeForServing()
    done.Wait()
}

func TestRouter_RefusesAReservedAttributeKeyAtRegistration(t *testing.T) {
    for _, key := range []string{RouteAttributeName, RouteAttributePattern, RouteAttributeMethods, RouteAttributeHost, RouteAttributeSchemes, RouteAttributeLocales, RouteAttributeLocale, requestAttributeSession, RequestAttributeScheme, "_anything"} {
        func() {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError || "route attribute key is reserved by the framework" != recoveredErr.Error() {
                    t.Fatalf("%s: expected the key refused, got %v", key, recoveredErr)
                }

                if key != exception.LogContext(recoveredErr, exception.LogContextConfig{})["key"] {
                    t.Fatalf("%s: expected the refusal to name the key, got %v", key, exception.LogContext(recoveredErr, exception.LogContextConfig{}))
                }
            }()

            NewRouter().Handle("/attributes", func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return nil, nil
            }, httpcontract.RouteOptions{Attributes: map[string]any{key: "value"}})
        }()
    }
}

func TestRouteHandler_DeclaresOneRegistrationDoor(t *testing.T) {
    routeHandlerType := reflect.TypeFor[httpcontract.RouteHandler]()
    if 1 != routeHandlerType.NumMethod() {
        t.Fatalf("expected one registration door, got %d methods", routeHandlerType.NumMethod())
    }

    method := routeHandlerType.Method(0)
    if "Handle" != method.Name || 3 != method.Type.NumIn() {
        t.Fatalf("expected Handle(pattern, handler, options), got %s %s", method.Name, method.Type)
    }

    if reflect.TypeFor[string]() != method.Type.In(0) || reflect.TypeFor[httpcontract.Handler]() != method.Type.In(1) || reflect.TypeFor[httpcontract.RouteOptions]() != method.Type.In(2) {
        t.Fatalf("expected Handle(string, Handler, RouteOptions), got %s", method.Type)
    }
}
