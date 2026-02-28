package http

import (
    nethttp "net/http"
    "testing"

    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

func TestGeneratePath_RouteNotFound(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    urlGenerator := NewUrlGenerator(routeRegistry)

    _, err := urlGenerator.GeneratePath("missing", map[string]string{})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_MissingRequiredParam(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/article/:id",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions("article", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    _, err := urlGenerator.GeneratePath("article", map[string]string{})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_OptionalParamIsSkippedWhenMissingAndNoDefault(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/page/:slug?",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions("page", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    pathValue, err := urlGenerator.GeneratePath("page", map[string]string{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/page" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestGeneratePath_DefaultIsUsedWhenParamMissing(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/page/:slug",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions(
            "page",
            []string{nethttp.MethodGet},
            "",
            nil,
            nil,
            map[string]string{"slug": "home"},
            nil,
            0,
            nil,
        ),
    )

    pathValue, err := urlGenerator.GeneratePath("page", map[string]string{})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/page/home" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestGeneratePath_RequirementFailure(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/article/:id",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions(
            "article",
            []string{nethttp.MethodGet},
            "",
            nil,
            map[string]string{"id": "\\d+"},
            nil,
            nil,
            0,
            nil,
        ),
    )

    _, err := urlGenerator.GeneratePath("article", map[string]string{"id": "abc"})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_WildcardNamed_MissingValue(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/asset/*file/x",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions("asset", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    _, err := urlGenerator.GeneratePath("asset", map[string]string{})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_WildcardNamed_RejectsSlashInValue(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/asset/*file/x",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions("asset", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    _, err := urlGenerator.GeneratePath("asset", map[string]string{"file": "a/b"})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_CatchAllSplitsSegmentsAndTrimsSlashes(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/download/*path...",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions("download", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    pathValue, err := urlGenerator.GeneratePath("download", map[string]string{"path": "/a//b/c/"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/download/a/b/c" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestGenerateUrl_AddsQueryParamsAndIgnoresEmptyKey(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/hello",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        NewRouteOptions("hello", []string{nethttp.MethodGet}, "", nil, nil, nil, nil, 0, nil),
    )

    urlValue, err := urlGenerator.GenerateUrl(
        "hello",
        map[string]string{},
        map[string]string{
            "":  "ignored",
            "a": "b",
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/hello?a=b" != urlValue {
        t.Fatalf("unexpected url")
    }
}
