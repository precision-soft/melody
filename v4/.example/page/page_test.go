package page

import (
    "context"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "os"
    "regexp"
    "strings"
    "testing"
    "time"

    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    melodysession "github.com/precision-soft/melody/v4/session"
)

func TestLoadAnswersAnEmbeddedPage(t *testing.T) {
    content, err := Load(LoginHtml)
    if nil != err {
        t.Fatalf("load %s: %v", LoginHtml, err)
    }

    if "" == content {
        t.Fatalf("%s loaded empty", LoginHtml)
    }
}

func TestLoadRefusesAFileThatIsNotEmbedded(t *testing.T) {
    _, err := Load("no_such_page.html")

    if false == errors.Is(err, ErrPageNotFound) {
        t.Fatalf("expected ErrPageNotFound, got %v", err)
    }
}

func runtimeServingRoute(t *testing.T, name string, pattern string) melodycontainercontract.Runtime {
    t.Helper()

    router := melodyhttp.NewRouter()
    router.Handle(
        pattern,
        func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            return melodyhttp.NewResponse(nethttp.StatusOK, []byte("ok")), nil
        },
        melodyhttp.RouteOptions{Name: name, Methods: []string{"GET"}, Attributes: melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)},
    )

    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    registerErr := melodycontainer.Register[melodyhttpcontract.RouteRegistry](
        serviceContainer,
        melodyhttp.ServiceRouteRegistry,
        func(resolver melodycontainercontract.Resolver) (melodyhttpcontract.RouteRegistry, error) {
            return router.RouteRegistry(), nil
        },
        melodycontainer.RegisterOptions{},
    )
    if nil != registerErr {
        t.Fatalf("register the route registry: %v", registerErr)
    }

    return melodyruntime.NewRun(context.Background(), serviceContainer.NewScope())
}

func bodyOf(t *testing.T, response melodyhttpcontract.Response) string {
    t.Helper()

    reader := response.BodyReader()
    if nil == reader {
        t.Fatalf("the response carries no body")
    }

    content, readErr := io.ReadAll(reader)
    if nil != readErr {
        t.Fatalf("read the response body: %v", readErr)
    }

    return string(content)
}

func TestHtmlEscapesAQuoteOutOfTheScriptLiteral(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, "example.it's", "/injected/")

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, LoginHtml))

    if false == strings.Contains(body, `example.it\'s`) {
        t.Fatalf("the quote did not reach the script literal escaped")
    }

    if true == strings.Contains(body, `example.it\\'s`) {
        t.Fatalf("the quote is preceded by an escaped backslash, so it still closes the literal")
    }
}

func TestHtmlEscapesABackslash(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, `example.back\slash`, "/back/")

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, LoginHtml))

    if false == strings.Contains(body, `example.back\\\\slash`) {
        t.Fatalf("the backslash was not escaped")
    }
}

func TestHtmlPutsTheManifestWhereThePageExpectsIt(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, "example.products.list.page", "/products/")

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, LoginHtml))

    if true == strings.Contains(body, "{{routes_json}}") {
        t.Fatalf("the placeholder survived the substitution")
    }

    if false == strings.Contains(body, `"name":"example.products.list.page"`) {
        t.Fatalf("the route did not reach the manifest")
    }
}

func TestHtmlAnswersAFailureForAPageThatIsNotEmbedded(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, "example.any", "/any/")

    response := Html(runtimeInstance, nil, nethttp.StatusOK, "no_such_page.html")

    if nethttp.StatusInternalServerError != response.StatusCode() {
        t.Fatalf("expected a failure, got %d", response.StatusCode())
    }
}

func TestHtmlPutsAnEmptyViewerForACallerWithoutAToken(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, "example.products.list.page", "/products/")

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, ProductsListHtml))

    if true == strings.Contains(body, "{{viewer_json}}") {
        t.Fatalf("the viewer placeholder survived the substitution")
    }

    if false == strings.Contains(body, `window.melodyViewer = JSON.parse('{"roles":[]}')`) {
        t.Fatalf("the empty viewer did not reach the page")
    }
}

func TestEveryPageCarriesTheViewerPlaceholder(t *testing.T) {
    for _, fileName := range []string{LoginHtml, CatalogueHtml, ProductsListHtml, ProductsDetailsHtml, ProfileHtml, UsersHtml} {
        content, loadErr := Load(fileName)
        if nil != loadErr {
            t.Fatalf("load %s: %v", fileName, loadErr)
        }

        if false == strings.Contains(content, "{{viewer_json}}") {
            t.Fatalf("%s does not carry the viewer placeholder", fileName)
        }
    }
}

func runtimeServingRouteAs(t *testing.T, name string, pattern string, roleList []string) melodycontainercontract.Runtime {
    t.Helper()

    runtimeInstance := runtimeServingRoute(t, name, pattern)

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-2", roleList, nil)),
    )

    return runtimeInstance
}

func TestHtmlPutsTheViewersExpandedRolesInThePage(t *testing.T) {
    runtimeInstance := runtimeServingRouteAs(t, "example.products.list.page", "/products/", []string{"ROLE_EDITOR"})

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, ProductsListHtml))

    if false == strings.Contains(body, `window.melodyViewer = JSON.parse('{"roles":["ROLE_EDITOR","ROLE_USER"]}')`) {
        t.Fatalf("the editor's expanded roles did not reach the page")
    }
}

func TestHtmlEscapesAViewersRoleForTheScriptString(t *testing.T) {
    runtimeInstance := runtimeServingRouteAs(t, "example.products.list.page", "/products/", []string{`ROLE_O'X\`})

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, ProductsListHtml))

    if false == strings.Contains(body, `window.melodyViewer = JSON.parse('{"roles":["ROLE_O\'X\\\\"]}')`) {
        t.Fatalf("the viewer's role did not reach the page escaped for its script string")
    }
}

func TestEveryLinkToARoleGatedPageCarriesItsRole(t *testing.T) {
    expectations := map[string][]string{
        ProductsListHtml: {`data-requires-role="ROLE_ADMIN" data-route="example.users.list.page"`},
        UsersHtml:        {`data-requires-role="ROLE_EDITOR" data-route="example.products.list.page"`},
        ProfileHtml:      {`data-requires-role="ROLE_ADMIN" data-route="example.users.list.page"`},
        CatalogueHtml: {
            `data-requires-role="ROLE_EDITOR" data-route="example.products.list.page"`,
            `data-requires-role="ROLE_ADMIN" data-route="example.users.list.page"`,
        },
    }

    for fileName, markingList := range expectations {
        content, loadErr := Load(fileName)
        if nil != loadErr {
            t.Fatalf("load %s: %v", fileName, loadErr)
        }

        for _, marking := range markingList {
            if false == strings.Contains(content, marking) {
                t.Fatalf("%s does not carry %s", fileName, marking)
            }
        }
    }
}

var externalAssetTagPattern = regexp.MustCompile(`<(?:link|script)\b[^>]*>`)
var externalAssetUrlPattern = regexp.MustCompile(`(?:src|href)="(https?://[^"]+)"`)
var integrityPattern = regexp.MustCompile(`integrity="([^"]*)"`)
var sha384DigestPattern = regexp.MustCompile(`^sha384-[A-Za-z0-9+/]{64}$`)

func externalAssetSources(t *testing.T) map[string]string {
    t.Helper()

    sourceMap := map[string]string{}

    entryList, readDirErr := pages.ReadDir(".")
    if nil != readDirErr {
        t.Fatalf("read the embedded pages: %v", readDirErr)
    }

    for _, entry := range entryList {
        content, loadErr := Load(entry.Name())
        if nil != loadErr {
            t.Fatalf("load %s: %v", entry.Name(), loadErr)
        }

        sourceMap[entry.Name()] = content
    }

    for _, path := range []string{"../public/index.html", "../security/default_access_denied_handler.go"} {
        content, readErr := os.ReadFile(path)
        if nil != readErr {
            t.Fatalf("read %s: %v", path, readErr)
        }

        sourceMap[path] = string(content)
    }

    return sourceMap
}

func TestPages_EveryExternalAssetCarriesIntegrityAndCrossorigin(t *testing.T) {
    externalTagCount := 0

    for sourceName, content := range externalAssetSources(t) {
        for _, tag := range externalAssetTagPattern.FindAllString(content, -1) {
            if nil == externalAssetUrlPattern.FindStringSubmatch(tag) {
                continue
            }

            externalTagCount++

            if nil == integrityPattern.FindStringSubmatch(tag) {
                t.Fatalf("%s: %s carries no integrity", sourceName, tag)
            }

            if false == strings.Contains(tag, `crossorigin="anonymous"`) {
                t.Fatalf("%s: %s carries no crossorigin", sourceName, tag)
            }
        }
    }

    if 31 != externalTagCount {
        t.Fatalf("expected 31 external asset tags, found %d", externalTagCount)
    }
}

func TestPages_OneDigestPerExternalAsset(t *testing.T) {
    digestByUrl := map[string]string{}

    for sourceName, content := range externalAssetSources(t) {
        for _, tag := range externalAssetTagPattern.FindAllString(content, -1) {
            urlMatch := externalAssetUrlPattern.FindStringSubmatch(tag)
            integrityMatch := integrityPattern.FindStringSubmatch(tag)
            if nil == urlMatch || nil == integrityMatch {
                continue
            }

            if false == sha384DigestPattern.MatchString(integrityMatch[1]) {
                t.Fatalf("%s: %s is not a sha384 digest", sourceName, integrityMatch[1])
            }

            known, seen := digestByUrl[urlMatch[1]]
            if true == seen && known != integrityMatch[1] {
                t.Fatalf("%s: %s carries %s where another page carries %s", sourceName, urlMatch[1], integrityMatch[1], known)
            }

            digestByUrl[urlMatch[1]] = integrityMatch[1]
        }
    }

    if 4 != len(digestByUrl) {
        t.Fatalf("expected four external assets, found %d", len(digestByUrl))
    }
}

func TestEverySessionPageCarriesTheCsrfPlaceholderAndTheLoginPageNone(t *testing.T) {
    for _, fileName := range []string{CatalogueHtml, ProductsListHtml, ProductsDetailsHtml, ProfileHtml, UsersHtml, TwoFactorHtml} {
        content, _ := Load(fileName)
        if false == strings.Contains(content, `<meta content="{{csrf_token}}" name="csrf-token"/>`) {
            t.Fatalf("%s does not carry the csrf meta", fileName)
        }

        if true == strings.Contains(content, `action="/logout/"`) && false == strings.Contains(content, `<input name="_csrf_token" type="hidden" value="{{csrf_token}}"/>`) {
            t.Fatalf("%s signs out without the csrf field", fileName)
        }
    }

    if login, _ := Load(LoginHtml); true == strings.Contains(login, csrfTokenPlaceholder) {
        t.Fatalf("expected the login page to issue no csrf secret")
    }
}

func TestHtmlPutsATokenOfTheSessionInThePage(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, "example.catalogue.page", "/catalogue/")
    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/catalogue/", nil), runtimeInstance, melodyhttp.RequestConfig{})
    manager := melodysession.NewManager(melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{}), melodysession.ManagerConfig{Ttl: melodybound.Of(time.Minute)})
    sessionInstance, sessionErr := manager.NewSession(runtimeInstance)
    if nil != sessionErr {
        t.Fatalf("unexpected error: %v", sessionErr)
    }
    _ = melodyhttp.SetRequestSession(request, sessionInstance)

    tokenPattern := regexp.MustCompile(`<meta content="([A-Za-z0-9_-]{86})" name="csrf-token"/>`)
    first := tokenPattern.FindStringSubmatch(bodyOf(t, Html(runtimeInstance, request, nethttp.StatusOK, CatalogueHtml)))
    second := tokenPattern.FindStringSubmatch(bodyOf(t, Html(runtimeInstance, request, nethttp.StatusOK, CatalogueHtml)))
    if 2 != len(first) || 2 != len(second) || first[1] == second[1] {
        t.Fatalf("expected two different tokens rendered, got %v and %v", first, second)
    }

    for _, token := range []string{first[1], second[1]} {
        if verified, verifyErr := melodyhttp.VerifyCsrfToken(request, token); false == verified || nil != verifyErr {
            t.Fatalf("expected the rendered token to verify, got %v %v", verified, verifyErr)
        }
    }
}

func TestHtmlWithoutASessionRendersAnEmptyToken(t *testing.T) {
    body := bodyOf(t, Html(runtimeServingRoute(t, "example.catalogue.page", "/catalogue/"), nil, nethttp.StatusOK, CatalogueHtml))

    if false == strings.Contains(body, `<meta content="" name="csrf-token"/>`) {
        t.Fatalf("expected the page rendered with an empty token")
    }
}
