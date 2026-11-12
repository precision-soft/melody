package http

import (
    "io"
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* registration was the single channel with no duplicate handling: two unnamed routes on one method and pattern were both stored and the later one could never be dispatched — the tie falls to the first registered — so the shadowing was invisible everywhere. */
func TestRouteRegistry_RefusesAnExactDispatchDuplicate(t *testing.T) {
    router := NewRouter()

    router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())
    }, "route already registered")
}

/* the name stays out of the dispatch identity on purpose: two differently named routes the matcher cannot tell apart are still one route at dispatch, and the later one is still dead. */
func TestRouteRegistry_RefusesADuplicateThatDiffersOnlyInName(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions("/named", routeRegistryTestHandler(), RouteOptions{Name: "first.name", Methods: []string{nethttp.MethodGet}})

    testhelper.AssertPanicsWithError(t, func() {
        router.HandleWithOptions("/named", routeRegistryTestHandler(), RouteOptions{Name: "second.name", Methods: []string{nethttp.MethodGet}})
    }, "route already registered")
}

/* with a recorder armed, a dispatch duplicate is deferred to the aggregated boot report — the first registration wins — and disarming restores the immediate refusal */
func TestRouteRegistry_ARecorderDefersTheDispatchDuplicateAndTheFirstRegistrationWins(t *testing.T) {
    registry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(registry)

    recorded := make([][2]string, 0, 1)
    registry.SetBootCollisionRecorder(func(kind string, name string) {
        recorded = append(recorded, [2]string{kind, name})
    })

    router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())
    router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())

    if 1 != len(recorded) {
        t.Fatalf("expected exactly one recorded collision, got %d", len(recorded))
    }

    if BootCollisionKindHttpRoute != recorded[0][0] {
        t.Fatalf("expected the dispatch-duplicate kind, got %q", recorded[0][0])
    }

    if "GET /health" != recorded[0][1] {
        t.Fatalf("expected the collision to carry the human spelling of the route, got %q", recorded[0][1])
    }

    if 1 != len(registry.RouteDefinitions()) {
        t.Fatalf("expected the first registration to win, got %d routes", len(registry.RouteDefinitions()))
    }

    registry.SetBootCollisionRecorder(nil)

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())
    }, "route already registered")
}

/* a name claimed by two dispatch-distinct routes is recorded under its own kind: both routes stay dispatchable — only the name collided — and the name keeps pointing at the first claimant */
func TestRouteRegistry_ARecorderDefersTheNameDuplicateAndBothRoutesStayDispatchable(t *testing.T) {
    registry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(registry)

    recorded := make([][2]string, 0, 1)
    registry.SetBootCollisionRecorder(func(kind string, name string) {
        recorded = append(recorded, [2]string{kind, name})
    })

    router.HandleWithOptions("/first", routeRegistryTestHandler(), RouteOptions{Name: "claimed.name", Methods: []string{nethttp.MethodGet}})
    router.HandleWithOptions("/second", routeRegistryTestHandler(), RouteOptions{Name: "claimed.name", Methods: []string{nethttp.MethodGet}})

    if 1 != len(recorded) {
        t.Fatalf("expected exactly one recorded collision, got %d", len(recorded))
    }

    if BootCollisionKindHttpRouteName != recorded[0][0] {
        t.Fatalf("expected the name-duplicate kind, got %q", recorded[0][0])
    }

    if "claimed.name" != recorded[0][1] {
        t.Fatalf("expected the collision to carry the claimed name, got %q", recorded[0][1])
    }

    if 2 != len(registry.RouteDefinitions()) {
        t.Fatalf("expected both dispatch-distinct routes to stay registered, got %d", len(registry.RouteDefinitions()))
    }

    definition, exists := registry.RouteDefinition("claimed.name")
    if false == exists {
        t.Fatal("expected the claimed name to keep resolving")
    }

    if "/first" != definition.Pattern() {
        t.Fatalf("expected the name to keep pointing at the first claimant, got %q", definition.Pattern())
    }
}

/* the word Accepts is the claim: the second registration is kept AND stays reachable. Without an assertion the only failure a test like this can report is a panic, so it reads as green for a registry that silently dropped the second route. */
func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherMethod(t *testing.T) {
    registry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(registry)

    router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())
    router.Handle(nethttp.MethodPost, "/health", routeRegistryTestHandler())

    if 2 != len(registry.RouteDefinitions()) {
        t.Fatalf("expected both methods to stay registered, got %d routes", len(registry.RouteDefinitions()))
    }

    for _, method := range []string{nethttp.MethodGet, nethttp.MethodPost} {
        if _, matched := router.Match(method, "/health", "", ""); false == matched {
            t.Fatalf("expected %s /health to be reachable", method)
        }
    }

    if _, matched := router.Match(nethttp.MethodDelete, "/health", "", ""); true == matched {
        t.Fatalf("expected a method neither registration names to stay unmatched")
    }
}

func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherHost(t *testing.T) {
    registry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(registry)

    router.HandleWithOptions(
        "/health",
        routeRegistryTestHandler(),
        RouteOptions{Methods: []string{nethttp.MethodGet}, Host: "one.example.test"},
    )

    router.HandleWithOptions(
        "/health",
        routeRegistryTestHandler(),
        RouteOptions{Methods: []string{nethttp.MethodGet}, Host: "two.example.test"},
    )

    if 2 != len(registry.RouteDefinitions()) {
        t.Fatalf("expected both hosts to stay registered, got %d routes", len(registry.RouteDefinitions()))
    }

    for _, host := range []string{"one.example.test", "two.example.test"} {
        if _, matched := router.Match(nethttp.MethodGet, "/health", host, ""); false == matched {
            t.Fatalf("expected /health on %s to be reachable", host)
        }
    }

    if _, matched := router.Match(nethttp.MethodGet, "/health", "three.example.test", ""); true == matched {
        t.Fatalf("expected a host neither registration names to stay unmatched")
    }
}

func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherRequirement(t *testing.T) {
    registry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(registry)

    router.HandleWithOptions(
        "/item/:id",
        routeRegistryTestHandler(),
        RouteOptions{Methods: []string{nethttp.MethodGet}, Requirements: map[string]string{"id": "[0-9]+"}},
    )

    router.HandleWithOptions(
        "/item/:id",
        routeRegistryTestHandler(),
        RouteOptions{Methods: []string{nethttp.MethodGet}, Requirements: map[string]string{"id": "[a-z]+"}},
    )

    if 2 != len(registry.RouteDefinitions()) {
        t.Fatalf("expected both requirements to stay registered, got %d routes", len(registry.RouteDefinitions()))
    }

    /* each registration owns the spelling the other one refuses, so a dropped second route shows up as one of these two going unmatched */
    for _, identifier := range []string{"7", "abc"} {
        routeMatch, matched := router.Match(nethttp.MethodGet, "/item/"+identifier, "", "")
        if false == matched {
            t.Fatalf("expected /item/%s to be reachable", identifier)
        }

        if identifier != routeMatch.Params["id"] {
            t.Fatalf("expected the identifier to be bound to %q, got %q", identifier, routeMatch.Params["id"])
        }
    }

    if _, matched := router.Match(nethttp.MethodGet, "/item/7a", "", ""); true == matched {
        t.Fatalf("expected a spelling neither requirement admits to stay unmatched")
    }
}

func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherPriority(t *testing.T) {
    registry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(registry)

    router.HandleWithOptions(
        "/ranked",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "declared-first"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}},
    )

    router.HandleWithOptions(
        "/ranked",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "higher-priority"), nil
        },
        RouteOptions{Methods: []string{nethttp.MethodGet}, Priority: 10},
    )

    if 2 != len(registry.RouteDefinitions()) {
        t.Fatalf("expected both priorities to stay registered, got %d routes", len(registry.RouteDefinitions()))
    }

    routeMatch, matched := router.Match(nethttp.MethodGet, "/ranked", "", "")
    if false == matched {
        t.Fatalf("expected /ranked to be reachable")
    }

    /* priority outranks registration order, so the route declared second is the one that answers — which is also what tells the two registrations apart */
    response, handlerErr := routeMatch.Handler(nil, nil, nil)
    if nil != handlerErr {
        t.Fatalf("unexpected handler error: %v", handlerErr)
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read body: %v", readErr)
    }

    if "higher-priority" != string(body) {
        t.Fatalf("expected the higher priority route to answer, got %q", string(body))
    }
}

func TestDeclareRouteDefinition_IsPublishedAndNeverMatched(t *testing.T) {
    router := NewRouter()
    router.DeclareRouteDefinition("/login/check/", httpcontract.RouteOptions{Name: "door.login", Methods: []string{nethttp.MethodPost}, Attributes: ExposedRouteAttributes(RouteZonePublic)})

    definition, exists := router.RouteRegistry().RouteDefinition("door.login")
    if false == exists || "/login/check" != definition.Pattern() {
        t.Fatalf("expected the definition published, got %v", definition)
    }

    url, generateErr := NewUrlGenerator(router.RouteRegistry()).GeneratePath("door.login", nil)
    if nil != generateErr || "/login/check" != url {
        t.Fatalf("expected the door's url, got %q and %v", url, generateErr)
    }

    if 1 != len(BuildRouteManifest(router.RouteDefinitions()).Routes) {
        t.Fatalf("expected the exposed door in the manifest, got %+v", BuildRouteManifest(router.RouteDefinitions()))
    }

    matchResult, matched := router.Match(nethttp.MethodPost, "/login/check", "", "http")
    if true == matched || nil != matchResult.Handler {
        t.Fatal("expected no handler for a definition-only route")
    }

    if "door.login" != matchResult.RouteAttributes[RouteAttributeName] || true != matchResult.RouteAttributes[RouteAttributeDefinitionOnly] {
        t.Fatalf("expected the definition named on its method, got %+v", matchResult.RouteAttributes)
    }

    if _, declaresMethods := matchResult.RouteAttributes[RouteAttributeMethods]; true == declaresMethods {
        t.Fatal("expected no allowed methods, which the kernel would answer as a 405")
    }
}

func TestDeclareRouteDefinition_AnswersOnlyItsMethodAndItsSegments(t *testing.T) {
    router := NewRouter()
    router.DeclareRouteDefinition("/login/check", httpcontract.RouteOptions{Name: "door.login", Methods: []string{nethttp.MethodPost}})

    for _, testCase := range []struct {
        method string
        path   string
    }{
        {nethttp.MethodGet, "/login/check"},
        {nethttp.MethodPost, "/login%2Fcheck"},
        {nethttp.MethodPost, "/login"},
    } {
        matchResult, _ := router.Match(testCase.method, testCase.path, "", "http")
        if _, named := matchResult.RouteAttributes[RouteAttributeDefinitionOnly]; true == named {
            t.Fatalf("expected %s %s not to name the definition", testCase.method, testCase.path)
        }
    }

    matchResult, _ := router.Match(nethttp.MethodPost, "/login/check/", "", "http")
    if _, named := matchResult.RouteAttributes[RouteAttributeDefinitionOnly]; false == named {
        t.Fatal("expected the trailing slash to name the definition, as the router reads it")
    }
}

func TestDeclareRouteDefinition_RefusesAReservedAttributeAndADuplicateName(t *testing.T) {
    router := NewRouter()

    func() {
        defer func() {
            if nil == recover() {
                t.Fatal("expected the reserved attribute refused")
            }
        }()

        router.DeclareRouteDefinition("/door", httpcontract.RouteOptions{Name: "door", Attributes: map[string]any{"_private": true}})
    }()

    router.DeclareRouteDefinition("/door", httpcontract.RouteOptions{Name: "door", Methods: []string{nethttp.MethodPost}})

    func() {
        defer func() {
            if nil == recover() {
                t.Fatal("expected the duplicate name refused")
            }
        }()

        router.Handle(nethttp.MethodGet, "/elsewhere", routeRegistryTestHandler())
        router.HandleWithOptions("/other", routeRegistryTestHandler(), RouteOptions{Name: "door", Methods: []string{nethttp.MethodGet}})
    }()
}

func TestDeclareRouteDefinition_TwoOnOnePathAnswerTheirOwnMethodsAndCarryTheirAttributes(t *testing.T) {
    router := NewRouter()
    router.DeclareRouteDefinition("/auth", httpcontract.RouteOptions{Name: "door.in", Methods: []string{nethttp.MethodPost}, Attributes: map[string]any{"tenant": "private"}})
    router.DeclareRouteDefinition("/auth", httpcontract.RouteOptions{Name: "door.out", Methods: []string{nethttp.MethodDelete}})

    posted, _ := router.Match(nethttp.MethodPost, "/auth", "", "http")
    if "door.in" != posted.RouteAttributes[RouteAttributeName] || "private" != posted.RouteAttributes["tenant"] {
        t.Fatalf("expected the first definition with its attributes, got %+v", posted.RouteAttributes)
    }

    deleted, _ := router.Match(nethttp.MethodDelete, "/auth", "", "http")
    if "door.out" != deleted.RouteAttributes[RouteAttributeName] {
        t.Fatalf("expected the second definition, got %+v", deleted.RouteAttributes)
    }
}

func TestRoutesClaimingPath_AnswersEveryRouteThatMatchesWhateverItsMethodHostOrScheme(t *testing.T) {
    router := NewRouter()
    router.DeclareRouteDefinition("/login/check", httpcontract.RouteOptions{Name: "door.login", Methods: []string{nethttp.MethodPost}})
    router.HandleWithOptions("/login/check/", routeRegistryTestHandler(), RouteOptions{Name: "literal", Methods: []string{nethttp.MethodGet}})
    router.HandleWithOptions("/login/:step", routeRegistryTestHandler(), RouteOptions{Name: "parameter", Methods: []string{nethttp.MethodPut}})
    router.HandleWithOptions("/*rest...", routeRegistryTestHandler(), httpcontract.RouteOptions{Name: "catchAll", Host: "other.example", Methods: []string{nethttp.MethodGet}})
    router.HandleWithOptions("/login", routeRegistryTestHandler(), RouteOptions{Name: "elsewhere", Methods: []string{nethttp.MethodGet}})
    router.HandleWithOptions("/login/:digits", routeRegistryTestHandler(), httpcontract.RouteOptions{Name: "constrained", Requirements: map[string]string{"digits": "[0-9]+"}})
    router.HandleWithOptions("/:_locale/check", routeRegistryTestHandler(), httpcontract.RouteOptions{Name: "localized", Locales: []string{"en"}})

    names := map[string]bool{}
    for _, definition := range router.RoutesClaimingPath("/login/check") {
        names[definition.Name()] = true
    }

    if 3 != len(names) || false == names["literal"] || false == names["parameter"] || false == names["catchAll"] {
        t.Fatalf("expected the literal, the parameter and the catch-all, got %v", names)
    }
}
