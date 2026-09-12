package http

import (
    "io"
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

func routeRegistryTestHandler() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(200, "ok"), nil
    }
}

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

    router.HandleNamed("first.name", nethttp.MethodGet, "/named", routeRegistryTestHandler())

    testhelper.AssertPanicsWithError(t, func() {
        router.HandleNamed("second.name", nethttp.MethodGet, "/named", routeRegistryTestHandler())
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

    router.HandleNamed("claimed.name", nethttp.MethodGet, "/first", routeRegistryTestHandler())
    router.HandleNamed("claimed.name", nethttp.MethodGet, "/second", routeRegistryTestHandler())

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
        NewRouteOptions("", []string{nethttp.MethodGet}, "one.example.test", nil, nil, nil, nil, 0, nil),
    )

    router.HandleWithOptions(
        "/health",
        routeRegistryTestHandler(),
        NewRouteOptions("", []string{nethttp.MethodGet}, "two.example.test", nil, nil, nil, nil, 0, nil),
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
        NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, map[string]string{"id": "[0-9]+"}, nil, nil, 0, nil),
    )

    router.HandleWithOptions(
        "/item/:id",
        routeRegistryTestHandler(),
        NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, map[string]string{"id": "[a-z]+"}, nil, nil, 0, nil),
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
        NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    router.HandleWithOptions(
        "/ranked",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return TextResponse(200, "higher-priority"), nil
        },
        NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 10, nil),
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
