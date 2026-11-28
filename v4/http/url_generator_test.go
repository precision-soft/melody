package http

import (
    nethttp "net/http"
    "net/url"
    "reflect"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestGeneratePath_RouteNotFound(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    _, err := urlGenerator.GeneratePath("missing", httpcontract.PathOptions{Parameters: map[string]string{}})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_MissingRequiredParameter(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/article/:id",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "article", Methods: []string{nethttp.MethodGet}},
    )

    _, err := urlGenerator.GeneratePath("article", httpcontract.PathOptions{Parameters: map[string]string{}})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_OptionalParameterIsSkippedWhenMissingAndNoDefault(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/page/:slug?",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "page", Methods: []string{nethttp.MethodGet}},
    )

    pathValue, err := urlGenerator.GeneratePath("page", httpcontract.PathOptions{Parameters: map[string]string{}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/page" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestGeneratePath_DefaultIsUsedWhenParameterMissing(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/page/:slug",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:     "page",
            Methods:  []string{nethttp.MethodGet},
            Defaults: map[string]string{"slug": "home"},
        },
    )

    pathValue, err := urlGenerator.GeneratePath("page", httpcontract.PathOptions{Parameters: map[string]string{}})
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
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/article/:id",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:         "article",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"id": "\\d+"},
        },
    )

    _, err := urlGenerator.GeneratePath("article", httpcontract.PathOptions{Parameters: map[string]string{"id": "abc"}})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_WildcardNamed_MissingValue(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/asset/*file/x",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "asset", Methods: []string{nethttp.MethodGet}},
    )

    _, err := urlGenerator.GeneratePath("asset", httpcontract.PathOptions{Parameters: map[string]string{}})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_WildcardNamed_RejectsSlashInValue(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/asset/*file/x",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "asset", Methods: []string{nethttp.MethodGet}},
    )

    _, err := urlGenerator.GeneratePath("asset", httpcontract.PathOptions{Parameters: map[string]string{"file": "a/b"}})
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestGeneratePath_CatchAllSplitsSegmentsAndTrimsSlashes(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/download/*path...",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "download", Methods: []string{nethttp.MethodGet}},
    )

    pathValue, err := urlGenerator.GeneratePath("download", httpcontract.PathOptions{Parameters: map[string]string{"path": "/a//b/c/"}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/download/a/b/c" != pathValue {
        t.Fatalf("unexpected path")
    }
}

func TestGeneratePath_EscapesParameterSpecialCharacters(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/article/:slug",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "article", Methods: []string{nethttp.MethodGet}},
    )

    pathValue, err := urlGenerator.GeneratePath("article", httpcontract.PathOptions{Parameters: map[string]string{"slug": "hello world"}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/article/hello%20world" != pathValue {
        t.Fatalf("expected spaces to be escaped, got: %s", pathValue)
    }
}

func TestGeneratePath_RejectsSlashInParameter(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/file/:name",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "file", Methods: []string{nethttp.MethodGet}},
    )

    generated, err := urlGenerator.GeneratePath("file", httpcontract.PathOptions{Parameters: map[string]string{"name": "a/b"}})
    if nil == err {
        t.Fatalf("a single-segment param spanning a slash mints a %%2F url the router decodes to a different path; expected rejection, generated %q", generated)
    }
}

func newHelloUrlGenerator(t *testing.T, generatorConfig UrlGeneratorConfig) *UrlGenerator {
    t.Helper()

    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, generatorConfig)

    handler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return EmptyResponse(200), nil
    }

    router.Handle("/hello", handler, RouteOptions{Name: "hello", Methods: []string{nethttp.MethodGet}})
    router.Handle("/reset/:token", handler, RouteOptions{Name: "user.reset"})
    router.Handle("/", handler, RouteOptions{Name: "home"})

    return urlGenerator
}

func TestGeneratePath_AnswersAStaticSegmentPercentEncoded(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/über/:id",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "over", Methods: []string{nethttp.MethodGet}},
    )

    pathValue, err := urlGenerator.GeneratePath("over", httpcontract.PathOptions{Parameters: map[string]string{"id": "a;b"}})
    if nil != err || "/%C3%BCber/a;b" != pathValue {
        t.Fatalf("expected the static segment percent-encoded and the parameter escaped by url.URL, got %q, %v", pathValue, err)
    }
}

func TestGeneratePath_AppendsTheQueryAndTheFragmentAndSkipsAnEmptyKey(t *testing.T) {
    urlGenerator := newHelloUrlGenerator(t, UrlGeneratorConfig{})

    urlValue, err := urlGenerator.GeneratePath(
        "hello",
        httpcontract.PathOptions{
            Query:    url.Values{"tab": []string{"a", "b"}, "": []string{"x"}, "page": []string{"2"}},
            Fragment: "top",
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if "/hello?page=2&tab=a&tab=b#top" != urlValue {
        t.Fatalf("expected the sorted query, the repeated key in order and the fragment last, got %q", urlValue)
    }
}

func TestGeneratePath_TheZeroConfigIsTheBarePath(t *testing.T) {
    urlGenerator := newHelloUrlGenerator(t, UrlGeneratorConfig{PublicUrl: "https://app.example.com/app"})

    urlValue, err := urlGenerator.GeneratePath("hello", httpcontract.PathOptions{})
    if nil != err || "/hello" != urlValue {
        t.Fatalf("expected the bare path without the public base's path, got %q, %v", urlValue, err)
    }

    escaped, escapedErr := urlGenerator.GeneratePath("hello", httpcontract.PathOptions{Query: url.Values{"q": []string{"a b&c"}}, Fragment: "a b"})
    if nil != escapedErr || "/hello?q=a+b%26c#a%20b" != escaped {
        t.Fatalf("expected the query and the fragment escaped, got %q, %v", escaped, escapedErr)
    }
}

func TestGenerateUrl_AnswersAnAbsoluteUrlBehindThePublicBase(t *testing.T) {
    urlGenerator := newHelloUrlGenerator(t, UrlGeneratorConfig{PublicUrl: "https://app.example.com"})

    urlValue, err := urlGenerator.GenerateUrl("user.reset", httpcontract.PathOptions{Parameters: map[string]string{"token": "t"}})
    if nil != err || "https://app.example.com/reset/t" != urlValue {
        t.Fatalf("expected the absolute url, got %q, %v", urlValue, err)
    }

    withQuery, withQueryErr := urlGenerator.GenerateUrl("hello", httpcontract.PathOptions{Query: url.Values{"a": []string{"b"}}, Fragment: "f"})
    if nil != withQueryErr || "https://app.example.com/hello?a=b#f" != withQuery {
        t.Fatalf("expected the query and the fragment behind the base, got %q, %v", withQuery, withQueryErr)
    }
}

func TestGenerateUrl_JoinsTheBasesPathWithOneSlash(t *testing.T) {
    cases := map[string]string{
        "https://x.ro/app":            "https://x.ro/app/reset/t",
        "https://x.ro/app/":           "https://x.ro/app/reset/t",
        "https://x.ro/":               "https://x.ro/reset/t",
        "http://x.ro:8080":            "http://x.ro:8080/reset/t",
        "https://x.ro/a%2Fb":          "https://x.ro/a%2Fb/reset/t",
        "http://[fe80::1%25eth0]/app": "http://[fe80::1%25eth0]/app/reset/t",
    }

    for publicUrl, expected := range cases {
        urlValue, err := newHelloUrlGenerator(t, UrlGeneratorConfig{PublicUrl: publicUrl}).GenerateUrl("user.reset", httpcontract.PathOptions{Parameters: map[string]string{"token": "t"}})
        if nil != err || expected != urlValue {
            t.Fatalf("expected %q behind %q, got %q, %v", expected, publicUrl, urlValue, err)
        }
    }

    escapedToken, escapedTokenErr := newHelloUrlGenerator(t, UrlGeneratorConfig{PublicUrl: "https://x.ro/my%20app"}).GenerateUrl("user.reset", httpcontract.PathOptions{Parameters: map[string]string{"token": "a b"}})
    if nil != escapedTokenErr || "https://x.ro/my%20app/reset/a%20b" != escapedToken {
        t.Fatalf("expected the escaped token behind the escaped base, got %q, %v", escapedToken, escapedTokenErr)
    }

    root, rootErr := newHelloUrlGenerator(t, UrlGeneratorConfig{PublicUrl: "https://x.ro/app"}).GenerateUrl("home", httpcontract.PathOptions{})
    if nil != rootErr || "https://x.ro/app/" != root {
        t.Fatalf("expected the root route behind the base's path, got %q, %v", root, rootErr)
    }
}

func TestGenerateUrl_IsAnErrorNamingTheKeyWhenNoPublicBaseIsSet(t *testing.T) {
    urlValue, err := newHelloUrlGenerator(t, UrlGeneratorConfig{}).GenerateUrl("hello", httpcontract.PathOptions{})
    if "" != urlValue || nil == err {
        t.Fatalf("expected an empty url and an error, got %q, %v", urlValue, err)
    }

    context := exception.FromError(err).Context()
    if config.HttpPublicUrlKey != context["key"] || "hello" != context["routeName"] {
        t.Fatalf("expected the error to name the key and the route, got %v", context)
    }
}

func TestNewUrlGenerator_RefusesAPublicUrlThatIsNoCleanBase(t *testing.T) {
    for _, publicUrl := range []string{"ftp://x", "https://u:p@x", "https://x?a=1", "https://x#f", "//x", "https://:8443", "https://x//app", "https://x/a/../b"} {
        testhelper.AssertPanicsWithError(t, func() {
            NewUrlGenerator(NewRouteRegistry(), UrlGeneratorConfig{PublicUrl: publicUrl})
        }, "the public url is refused")
    }
}

func TestGenerateUrl_NeverReadsTheRequestsHost(t *testing.T) {
    requestType := reflect.TypeOf((*httpcontract.Request)(nil)).Elem()

    for _, methodName := range []string{"GeneratePath", "GenerateUrl"} {
        method, exists := reflect.TypeOf((*httpcontract.UrlGenerator)(nil)).Elem().MethodByName(methodName)
        if false == exists {
            t.Fatalf("expected %s on the contract", methodName)
        }

        for index := 0; index < method.Type.NumIn(); index++ {
            if requestType == method.Type.In(index) {
                t.Fatalf("expected %s to take no request", methodName)
            }
        }
    }

    constructorType := reflect.TypeOf(NewUrlGenerator)
    for index := 0; index < constructorType.NumIn(); index++ {
        if requestType == constructorType.In(index) {
            t.Fatal("expected the constructor to take no request")
        }
    }

    if 1 != reflect.TypeOf(UrlGeneratorConfig{}).NumField() {
        t.Fatal("expected the generator's config to carry the public url only")
    }
}

func TestGeneratePath_CatchAllRequirementFailure(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/files/*path...",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:         "files",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"path": "[a-z/]+"},
        },
    )

    if _, err := urlGenerator.GeneratePath("files", httpcontract.PathOptions{Parameters: map[string]string{"path": "docs/readme"}}); nil != err {
        t.Fatalf("a catch-all value the router accepts must generate: %v", err)
    }

    generated, err := urlGenerator.GeneratePath("files", httpcontract.PathOptions{Parameters: map[string]string{"path": "../../etc/passwd"}})
    if nil == err {
        t.Fatalf("expected the catch-all requirement to reject the value, generated %q", generated)
    }
}

func TestGeneratePath_CatchAllRequirementMatchesEmittedRemainder(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/files/*path...",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:         "files",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"path": "[a-z]+(?:/[a-z]+)*"},
        },
    )

    pathValue, err := urlGenerator.GeneratePath("files", httpcontract.PathOptions{Parameters: map[string]string{"path": "a//b"}})
    if nil != err {
        t.Fatalf("an interior double slash collapses to the remainder the router accepts; expected generation to succeed: %v", err)
    }

    if "/files/a/b" != pathValue {
        t.Fatalf("expected the emitted url to collapse the empty segment, got: %s", pathValue)
    }
}

func TestGeneratePath_NonTerminalCatchAllDropsTrailingLiterals(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/m/*p.../view",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{
            Name:         "mid",
            Methods:      []string{nethttp.MethodGet},
            Requirements: map[string]string{"p": "\\d+/\\d+"},
        },
    )

    pathValue, err := urlGenerator.GeneratePath("mid", httpcontract.PathOptions{Parameters: map[string]string{"p": "1/2"}})
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

func TestGeneratePath_RejectsAnEmptyRequiredParameter(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    generator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    router.Handle(
        "/users/:id/profile",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return EmptyResponse(200), nil
        },
        RouteOptions{Name: "users.profile", Methods: []string{nethttp.MethodGet}},
    )

    if _, err := generator.GeneratePath("users.profile", httpcontract.PathOptions{Parameters: map[string]string{"id": ""}}); nil == err {
        t.Fatalf("expected an empty required parameter to be refused")
    }

    generatedPath, err := generator.GeneratePath("users.profile", httpcontract.PathOptions{Parameters: map[string]string{"id": "7"}})
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

func TestGeneratePath_ASingleSegmentWildcardSuppliedEmptyTakesTheDefaultOrIsRefused(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    handler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return EmptyResponse(200), nil
    }

    router.Handle(
        "/files/*name/edit",
        handler,
        RouteOptions{Name: "file_edit", Methods: []string{nethttp.MethodGet}},
    )
    router.Handle(
        "/folders/*name/edit",
        handler,
        RouteOptions{Name: "folder_edit", Methods: []string{nethttp.MethodGet}, Defaults: map[string]string{"name": "index"}},
    )

    pathValue, err := urlGenerator.GeneratePath("file_edit", httpcontract.PathOptions{Parameters: map[string]string{"name": ""}})
    if nil == err {
        t.Fatalf("expected an empty single-segment wildcard to be refused, got %q", pathValue)
    }
    if false == strings.Contains(err.Error(), "wildcard parameter may not be empty") {
        t.Fatalf("unexpected error: %v", err)
    }

    pathValue, err = urlGenerator.GeneratePath("folder_edit", httpcontract.PathOptions{Parameters: map[string]string{"name": ""}})
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "/folders/index/edit" != pathValue {
        t.Fatalf("expected the default to fill the empty wildcard, got %q", pathValue)
    }
}

func TestGeneratePath_RefusesAPathTheKernelWouldRefuseAsNonCanonical(t *testing.T) {
    routeRegistry := NewRouteRegistry()
    router := NewRouterWithRouteRegistry(routeRegistry)
    urlGenerator := NewUrlGenerator(routeRegistry, UrlGeneratorConfig{})

    handler := func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return EmptyResponse(200), nil
    }
    router.Handle("/file/:name", handler, RouteOptions{Name: "file", Methods: []string{nethttp.MethodGet}})
    router.Handle("/tree/*rest...", handler, RouteOptions{Name: "tree", Methods: []string{nethttp.MethodGet}})

    for _, testCase := range []struct {
        routeName  string
        parameters map[string]string
    }{
        {"file", map[string]string{"name": ".."}},
        {"file", map[string]string{"name": "."}},
        {"file", map[string]string{"name": "report "}},
        {"tree", map[string]string{"rest": "a/../b"}},
    } {
        generated, err := urlGenerator.GeneratePath(testCase.routeName, httpcontract.PathOptions{Parameters: testCase.parameters})
        if nil == err || false == strings.Contains(err.Error(), "not one the router serves") {
            t.Fatalf("expected %v refused as a path the router does not serve, generated %q, error %v", testCase.parameters, generated, err)
        }
    }

    for _, testCase := range []struct {
        routeName  string
        parameters map[string]string
        path       string
    }{
        {"file", map[string]string{"name": "a b"}, "/file/a%20b"},
        {"file", map[string]string{"name": "..report"}, "/file/..report"},
        {"tree", map[string]string{"rest": "a/b"}, "/tree/a/b"},
    } {
        generated, err := urlGenerator.GeneratePath(testCase.routeName, httpcontract.PathOptions{Parameters: testCase.parameters})
        if nil != err || testCase.path != generated {
            t.Fatalf("expected %q, got %q, error %v", testCase.path, generated, err)
        }
    }
}
