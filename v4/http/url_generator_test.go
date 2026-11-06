package http

import (
    nethttp "net/http"
    "strings"
    "testing"

    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
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
        RouteOptions{Name: "article", Methods: []string{nethttp.MethodGet}},
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
        RouteOptions{Name: "page", Methods: []string{nethttp.MethodGet}},
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
        RouteOptions{
            Name:     "page",
            Methods:  []string{nethttp.MethodGet},
            Defaults: map[string]string{"slug": "home"},
        },
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
        RouteOptions{
            Name:         "article",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"id": "\\d+"},
        },
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
        RouteOptions{Name: "asset", Methods: []string{nethttp.MethodGet}},
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
        RouteOptions{Name: "asset", Methods: []string{nethttp.MethodGet}},
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
        RouteOptions{Name: "download", Methods: []string{nethttp.MethodGet}},
    )

    pathValue, err := urlGenerator.GeneratePath("download", map[string]string{"path": "/a//b/c/"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/download/a/b/c" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestGeneratePath_EscapesParamSpecialCharacters(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/article/:slug",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "article", Methods: []string{nethttp.MethodGet}},
    )

    pathValue, err := urlGenerator.GeneratePath("article", map[string]string{"slug": "hello world"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/article/hello%20world" != pathValue {
        t.Fatalf("expected spaces to be escaped, got: %s", pathValue)
    }
}

/* A ":param" spans one path segment; a slash in its value would be emitted as %2F, which the net/http server decodes back to "/" before the kernel matches on request.URL.Path, so the link would resolve to a different route or 404. The generator must reject the slash rather than mint such a url. */
func TestGeneratePath_RejectsSlashInParam(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/file/:name",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "file", Methods: []string{nethttp.MethodGet}},
    )

    generated, err := urlGenerator.GeneratePath("file", map[string]string{"name": "a/b"})
    if nil == err {
        t.Fatalf("a single-segment param spanning a slash mints a %%2F url the router decodes to a different path; expected rejection, generated %q", generated)
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
        RouteOptions{Name: "hello", Methods: []string{nethttp.MethodGet}},
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

/* A requirement on a catch-all is enforced when matching, so honouring it while generating is what keeps the generator from minting urls its own router answers with a 404. */
func TestGeneratePath_CatchAllRequirementFailure(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/files/*path...",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:         "files",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"path": "[a-z/]+"},
        },
    )

    if _, err := urlGenerator.GeneratePath("files", map[string]string{"path": "docs/readme"}); nil != err {
        t.Fatalf("a catch-all value the router accepts must generate: %v", err)
    }

    generated, err := urlGenerator.GeneratePath("files", map[string]string{"path": "../../etc/passwd"})
    if nil == err {
        t.Fatalf("expected the catch-all requirement to reject the value, generated %q", generated)
    }
}

/* matchPath receives the catch-all remainder as the non-empty segments joined by "/", so the requirement must be tested against that collapsed remainder — an interior double slash the emission drops (here "a//b" -> "a/b") must not fail generation, or a value the router serves cannot be generated. */
func TestGeneratePath_CatchAllRequirementMatchesEmittedRemainder(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/files/*path...",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:         "files",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"path": "[a-z]+(?:/[a-z]+)*"},
        },
    )

    pathValue, err := urlGenerator.GeneratePath("files", map[string]string{"path": "a//b"})
    if nil != err {
        t.Fatalf("an interior double slash collapses to the remainder the router accepts; expected generation to succeed: %v", err)
    }

    if "/files/a/b" != pathValue {
        t.Fatalf("expected the emitted url to collapse the empty segment, got: %s", pathValue)
    }
}

/* registerRouteInTree and matchPath treat "*name..." as terminal wherever it appears, dropping any pattern segments that follow it; the generator must stop there too, or it appends trailing literals onto a url its own router answers with a 404. */
func TestGeneratePath_NonTerminalCatchAllDropsTrailingLiterals(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/m/*p.../view",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:         "mid",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"p": "\\d+/\\d+"},
        },
    )

    pathValue, err := urlGenerator.GeneratePath("mid", map[string]string{"p": "1/2"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/m/1/2" != pathValue {
        t.Fatalf("expected the trailing literal to be dropped as registration drops it, got: %s", pathValue)
    }

    routes := routeRegistry.routesInternal()
    if 0 == len(routes) {
        t.Fatalf("expected the route to be registered")
    }

    if _, matched := matchPath(routes[0], splitPath(pathValue)); false == matched {
        t.Fatalf("the generated catch-all url must be servable by its own router, %q did not match", pathValue)
    }
}

/* the generator and the matcher must agree: matchPath refuses an empty segment for a named parameter, so the generator must refuse to mint one rather than hand back a url that answers 404 */
func TestGeneratePath_RejectsAnEmptyRequiredParameter(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    generator := NewUrlGenerator(routeRegistry)

    router.HandleWithOptions(
        "/users/:id/profile",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "users.profile", Methods: []string{nethttp.MethodGet}},
    )

    if _, err := generator.GeneratePath("users.profile", map[string]string{"id": ""}); nil == err {
        t.Fatalf("expected an empty required parameter to be refused")
    }

    generatedPath, err := generator.GeneratePath("users.profile", map[string]string{"id": "7"})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/users/7/profile" != generatedPath {
        t.Fatalf("unexpected generated path: %s", generatedPath)
    }

    if _, matched := router.Match(nethttp.MethodGet, generatedPath, "", ""); false == matched {
        t.Fatalf("expected the generated path to be routable")
    }
}

/* a single-segment wildcard spans one segment as a named parameter does: a value supplied empty takes a non-empty default, and with none left generation is refused, since the empty segment would mint a "//" url the kernel refuses before it routes */
func TestGeneratePath_ASingleSegmentWildcardSuppliedEmptyTakesTheDefaultOrIsRefused(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry)

    handler := func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return EmptyResponse(200), nil
    }

    router.HandleWithOptions(
        "/files/*name/edit",
        handler,
        RouteOptions{Name: "file_edit", Methods: []string{nethttp.MethodGet}},
    )
    router.HandleWithOptions(
        "/folders/*name/edit",
        handler,
        RouteOptions{Name: "folder_edit", Methods: []string{nethttp.MethodGet}, Defaults: map[string]string{"name": "index"}},
    )

    pathValue, err := urlGenerator.GeneratePath("file_edit", map[string]string{"name": ""})
    if nil == err {
        t.Fatalf("expected an empty single-segment wildcard to be refused, got %q", pathValue)
    }
    if false == strings.Contains(err.Error(), "wildcard parameter may not be empty") {
        t.Fatalf("unexpected error: %v", err)
    }

    pathValue, err = urlGenerator.GeneratePath("folder_edit", map[string]string{"name": ""})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "/folders/index/edit" != pathValue {
        t.Fatalf("expected the default to fill the empty wildcard, got %q", pathValue)
    }
}
