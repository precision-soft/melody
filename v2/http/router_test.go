package http

import (
    "errors"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/clock"
    "github.com/precision-soft/melody/v2/config"
    configcontract "github.com/precision-soft/melody/v2/config/contract"
    "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/event"
    eventcontract "github.com/precision-soft/melody/v2/event/contract"
    "github.com/precision-soft/melody/v2/exception"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    "github.com/precision-soft/melody/v2/session"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
)

type testEnvironmentSource struct {
    values map[string]string
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func newHttpTestContainer() containercontract.Container {
    return newHttpTestContainerWithSessionStorage(session.NewInMemoryStorage())
}

func newHttpTestContainerWithSessionStorage(storage sessioncontract.Storage) containercontract.Container {
    return newHttpTestContainerWithSessionStorageAndEnvironmentValues(storage, nil)
}

func newHttpTestContainerWithSessionManager(
    sessionManager sessioncontract.Manager,
) containercontract.Container {
    return newHttpTestContainerWithSessionManagerAndEnvironmentValues(sessionManager, nil)
}

func newHttpTestContainerWithSessionStorageAndEnvironmentValues(
    storage sessioncontract.Storage,
    environmentValues map[string]string,
) containercontract.Container {
    return newHttpTestContainerWithSessionManagerAndEnvironmentValues(
        session.NewManager(storage, 30*time.Minute),
        environmentValues,
    )
}

func newHttpTestContainerWithSessionManagerAndEnvironmentValues(
    sessionManager sessioncontract.Manager,
    environmentValues map[string]string,
) containercontract.Container {
    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        logging.ServiceLogger,
        func(resolver containercontract.Resolver) (loggingcontract.Logger, error) {
            return logging.NewNopLogger(), nil
        },
    )

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            values := map[string]string{
                config.EnvKey: config.EnvDevelopment,
            }
            for key, value := range environmentValues {
                values[key] = value
            }

            environment, err := config.NewEnvironment(
                &testEnvironmentSource{
                    values: values,
                },
            )
            if nil != err {
                return nil, err
            }

            return config.NewConfiguration(environment, "/tmp/melody")
        },
    )

    serviceContainer.MustRegister(
        session.ServiceSessionManager,
        func(resolver containercontract.Resolver) (sessioncontract.Manager, error) {
            return sessionManager, nil
        },
    )

    serviceContainer.MustRegister(
        event.ServiceEventDispatcher,
        func(resolver containercontract.Resolver) (eventcontract.EventDispatcher, error) {
            return event.NewEventDispatcher(clock.NewSystemClock()), nil
        },
    )

    return serviceContainer
}

func TestRouter_HandleAndServeHttp_HappyPath(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

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
        nethttp.MethodGet,
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodPost, "/hello", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 405 != recorder.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_NotFound(t *testing.T) {
    router := NewRouter()

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

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
        nethttp.MethodGet,
        "/panic",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            exception.Panic(exception.NewError("boom", nil, nil))
            return nil, nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

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
        nethttp.MethodGet,
        "/err",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, errors.New("handler error")
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    request := httptest.NewRequest(nethttp.MethodGet, "/err", nil)
    recorder := httptest.NewRecorder()

    handler.ServeHTTP(recorder, request)

    if 500 != recorder.Code {
        t.Fatalf("unexpected status")
    }
}

func TestRouter_ParamExtraction(t *testing.T) {
    router := NewRouter()

    router.Handle(
        nethttp.MethodGet,
        "/user/:id",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            value, exists := request.Param("id")
            if false == exists {
                return TextResponse(500, "missing id"), nil
            }

            return TextResponse(200, value), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

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

    router.HandleWithOptions(
        "/shop/:locale/list",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
        NewRouteOptions(
            "shop.list",
            []string{nethttp.MethodGet},
            "",
            nil,
            map[string]string{"locale": "en|de|fr"},
            nil,
            nil,
            0,
            nil,
        ),
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

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

    router.HandleWithOptions(
        "/files/*path",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "ok"), nil
        },
        NewRouteOptions(
            "files.serve",
            []string{nethttp.MethodGet},
            "",
            nil,
            map[string]string{"path": `[a-z0-9/]+`},
            nil,
            nil,
            0,
            nil,
        ),
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

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
        nethttp.MethodGet,
        "/admin",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "admin"), nil
        },
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
        nethttp.MethodGet,
        "/users/:id/profile",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "profile"), nil
        },
    )

    if _, matched := router.Match(nethttp.MethodGet, "/users//profile", "", ""); true == matched {
        t.Fatalf("expected an empty identifier segment not to match")
    }

    routeMatch, matched := router.Match(nethttp.MethodGet, "/users/7/profile", "", "")
    if false == matched {
        t.Fatalf("expected a supplied identifier to match")
    }

    if "7" != routeMatch.Params["id"] {
        t.Fatalf("expected the identifier to be bound, got %q", routeMatch.Params["id"])
    }
}

func TestRouter_AddRoute_RejectsOptionalSegmentBeforeAnotherSegment(t *testing.T) {
    for _, pattern := range []string{"/blog/:locale?/posts", "/:locale?/blog", "/blog/:locale?/:page?"} {
        router := NewRouter()

        assertPanicWithExceptionMessage(
            t,
            func() {
                router.Handle(
                    nethttp.MethodGet,
                    pattern,
                    func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                        return TextResponse(200, "blog"), nil
                    },
                )
            },
            "optional route parameter must be the last pattern segment unless it has a default",
        )
    }
}

func TestRouter_AddRoute_AcceptsANonTrailingOptionalSegmentThatHasADefault(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.HandleWithOptions(
        "/:locale?/list/:page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "list"), nil
        },
        NewRouteOptions(
            "list",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            map[string]string{
                "locale": "en",
            },
            nil,
            0,
            nil,
        ),
    )

    urlGenerator := NewUrlGenerator(routeRegistry)

    generatedPath, generateErr := urlGenerator.GeneratePath("list", map[string]string{"page": "2"})
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
        nethttp.MethodGet,
        "/blog/posts/:locale?",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "blog"), nil
        },
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

    router.HandleWithOptions(
        "/:locale?/list/:page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "list"), nil
        },
        NewRouteOptions(
            "list",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            map[string]string{
                "locale": "en",
            },
            nil,
            0,
            nil,
        ),
    )

    urlGenerator := NewUrlGenerator(routeRegistry)

    generatedPath, generateErr := urlGenerator.GeneratePath(
        "list",
        map[string]string{
            "locale": "",
            "page":   "2",
        },
    )
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

    if "en" != routeMatch.Params["locale"] {
        t.Fatalf("expected the locale to be bound to the default, got %q", routeMatch.Params["locale"])
    }

    if "2" != routeMatch.Params["page"] {
        t.Fatalf("expected the page to be bound, got %q", routeMatch.Params["page"])
    }
}

func TestRouter_AddRoute_NonTrailingOptionalWithAGroupSuppliedDefaultSurvivesASuppliedEmptyValue(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    group := router.Group("/shop")
    group.WithDefaults(
        map[string]string{
            "locale": "en",
        },
    )

    group.HandleWithOptions(
        "/:locale?/list/:page",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "list"), nil
        },
        NewRouteOptions("shop.list", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    urlGenerator := NewUrlGenerator(routeRegistry)

    for _, parameters := range []map[string]string{
        {"page": "2"},
        {"locale": "", "page": "2"},
    } {
        generatedPath, generateErr := urlGenerator.GeneratePath("shop.list", parameters)
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
            router.HandleWithOptions(
                "/:locale?/list/:page",
                func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                    return TextResponse(200, "list"), nil
                },
                NewRouteOptions(
                    "list",
                    []string{nethttp.MethodGet},
                    "",
                    nil,
                    nil,
                    map[string]string{
                        "locale": "",
                    },
                    nil,
                    0,
                    nil,
                ),
            )
        },
        "optional route parameter must be the last pattern segment unless it has a default",
    )
}
func TestRouter_AddRoute_ServesATrailingOptionalReachedThroughTheRoot(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.HandleWithOptions(
        "/:page?",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "home"), nil
        },
        NewRouteOptions(
            "home",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            nil,
            nil,
            0,
            nil,
        ),
    )

    urlGenerator := NewUrlGenerator(routeRegistry)

    generatedPath, generateErr := urlGenerator.GeneratePath("home", nil)
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

    if _, bound := matchResult.Params["page"]; true == bound {
        t.Fatalf("expected the omitted optional to stay unbound, got %v", matchResult.Params)
    }
}

func TestRouter_Match_NeverAdvertisesAnEmptyAllowedMethod(t *testing.T) {
    router := NewRouter()

    router.Handle(
        "",
        "/empty-method",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "never"), nil
        },
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
            nethttp.MethodGet,
            "/hello",
            func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
                return TextResponse(200, "ok"), nil
            },
        )

        return NewKernel(router)
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
    restricted.options.MethodPolicy.AutomaticOptions = false
    restricted.options.MethodPolicy.HeadFallbackToGet = false
    restrictedAllow := allowFor(restricted)

    if true == strings.Contains(restrictedAllow, nethttp.MethodOptions) {
        t.Fatalf("Allow must not advertise OPTIONS when AutomaticOptions is off, got %q", restrictedAllow)
    }
    if true == strings.Contains(restrictedAllow, nethttp.MethodHead) {
        t.Fatalf("Allow must not advertise HEAD when HeadFallbackToGet is off, got %q", restrictedAllow)
    }
    if false == strings.Contains(restrictedAllow, nethttp.MethodGet) {
        t.Fatalf("Allow must still advertise the real GET method, got %q", restrictedAllow)
    }
}

func TestRouter_HandleControllerRegistersTheControllerUnderItsMethod(t *testing.T) {
    router := NewRouter()

    router.HandleController(
        nethttp.MethodGet,
        "/articles",
        func(request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "from the controller"), nil
        },
    )

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

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

func TestRouter_HandleNamedControllerRegistersTheNameTheGeneratorResolves(t *testing.T) {
    router := NewRouter()

    router.HandleNamedController(
        "article.show",
        nethttp.MethodGet,
        "/articles/:id",
        func(request httpcontract.Request) (httpcontract.Response, error) {
            identifier, _ := request.Param("id")

            return TextResponse(nethttp.StatusOK, identifier), nil
        },
    )

    definition, found := router.RouteDefinition("article.show")
    if false == found {
        t.Fatalf("expected the named controller route to be registered under its name")
    }

    if "/articles/:id" != definition.Pattern() {
        t.Fatalf("unexpected pattern: %q", definition.Pattern())
    }

    handler := NewKernel(router).ServeHttp(newHttpTestContainer())

    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(nethttp.MethodGet, "/articles/42", nil))

    if "42" != recorder.Body.String() {
        t.Fatalf("expected the route parameter to reach the controller, got: %q", recorder.Body.String())
    }
}

func TestRouter_HandleControllerRefusesSomethingThatIsNotAFunction(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatalf("expected a non-function controller to be refused at registration")
        }
    }()

    NewRouter().HandleController(nethttp.MethodGet, "/articles", "not a function")
}

func TestRouter_RouteRegistryIsTheOneItRegistersInto(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)

    router.HandleNamed("article.show", nethttp.MethodGet, "/articles", routeRegistryTestHandler())

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
        nethttp.MethodGet,
        "/aliased",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(nethttp.StatusOK, "ok"), nil
        },
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

/* a route the registry declined is not put in the matching tree. The index the tree receives is the position of the last STORED route, so registering it for a declined duplicate gave the pattern's entry somebody else's route — the invariant every reader of the tree relies on, and the one the priority tie-break reads the index for. */
func TestRouterAddRoute_ADeclinedDuplicateDoesNotEnterTheMatchingTree(t *testing.T) {
    router := NewRouter()

    handler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return nil, nil
    }

    recordedCollisions := []string{}
    router.routeRegistry.SetBootCollisionRecorder(func(kind string, name string) {
        recordedCollisions = append(recordedCollisions, name)
    })

    router.Handle(nethttp.MethodGet, "/alpha", handler)
    router.Handle(nethttp.MethodGet, "/beta", handler)
    router.Handle(nethttp.MethodGet, "/alpha", handler)

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
