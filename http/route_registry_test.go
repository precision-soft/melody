package http

import (
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/internal/testhelper"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func routeRegistryTestHandler() httpcontract.Handler {
    return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return TextResponse(200, "ok"), nil
    }
}

/* @info registration was the single channel with no duplicate handling: two unnamed routes on one method and pattern were both stored and the later one could never be dispatched — the tie falls to the first registered — so the shadowing was invisible everywhere. */
func TestRouteRegistry_RefusesAnExactDispatchDuplicate(t *testing.T) {
    router := NewRouter()

    router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())

    testhelper.AssertPanicsWithError(t, func() {
        router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())
    }, "route already registered")
}

func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherMethod(t *testing.T) {
    router := NewRouter()

    router.Handle(nethttp.MethodGet, "/health", routeRegistryTestHandler())
    router.Handle(nethttp.MethodPost, "/health", routeRegistryTestHandler())
}

func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherHost(t *testing.T) {
    router := NewRouter()

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
}

func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherRequirement(t *testing.T) {
    router := NewRouter()

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
}

func TestRouteRegistry_AcceptsTheSamePatternUnderAnotherPriority(t *testing.T) {
    router := NewRouter()

    router.HandleWithOptions(
        "/ranked",
        routeRegistryTestHandler(),
        NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    router.HandleWithOptions(
        "/ranked",
        routeRegistryTestHandler(),
        NewRouteOptions("", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 10, nil),
    )
}

/* @info the name stays out of the dispatch identity on purpose: two differently named routes the matcher cannot tell apart are still one route at dispatch, and the later one is still dead. */
func TestRouteRegistry_RefusesADuplicateThatDiffersOnlyInName(t *testing.T) {
    router := NewRouter()

    router.HandleNamed("first.name", nethttp.MethodGet, "/named", routeRegistryTestHandler())

    testhelper.AssertPanicsWithError(t, func() {
        router.HandleNamed("second.name", nethttp.MethodGet, "/named", routeRegistryTestHandler())
    }, "route already registered")
}
