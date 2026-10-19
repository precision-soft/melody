package page

import (
    "context"
    "errors"
    "io"
    nethttp "net/http"
    "strings"
    "testing"

    melodycontainer "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    melodyhttp "github.com/precision-soft/melody/http"
    httpcontract "github.com/precision-soft/melody/http/contract"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
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

/* runtimeServingRoute hands back a runtime whose container carries a route registry holding exactly the one route the caller names, so a test can put an arbitrary spelling into the manifest the page splices into its script. */
func runtimeServingRoute(t *testing.T, name string, pattern string) melodyruntimecontract.Runtime {
    t.Helper()

    router := melodyhttp.NewRouter()
    router.HandleNamed(name, nethttp.MethodGet, pattern, func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
        return melodyhttp.NewResponse(nethttp.StatusOK, []byte("ok")), nil
    })

    serviceContainer := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[httpcontract.RouteRegistry](
        serviceContainer,
        melodyhttp.ServiceRouteRegistry,
        func(resolver containercontract.Resolver) (httpcontract.RouteRegistry, error) {
            return router.RouteRegistry(), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register the route registry: %v", registerErr)
    }

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func bodyOf(t *testing.T, response httpcontract.Response) string {
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

/* The manifest is spliced into a SINGLE-QUOTED JavaScript literal — window.melodyRoutes = JSON.parse('…') — so a route name or pattern carrying a quote would otherwise close the literal and let whatever follows it be read as script. json.Marshal escapes the angle brackets, the ampersand and the line separators, but it leaves a single quote bare, so this function is the only thing standing between a route spelling and the page's scripting.

   The negative is the assertion that matters, and it is what pins the ORDER of the two replacements: escaping the quote FIRST produces a backslash that the backslash pass then doubles, turning \' into \\' — an escaped backslash followed by a LIVE quote, which closes the literal exactly as the bare one would. A route name carrying only a backslash cannot see that mistake, because both orders answer identically for it; only an input carrying a quote separates them. */
func TestHtmlEscapesAQuoteOutOfTheScriptLiteral(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, "example.it's", "/injected/")

    response := Html(runtimeInstance, nil, nethttp.StatusOK, LoginHtml)
    body := bodyOf(t, response)

    if false == strings.Contains(body, `example.it\'s`) {
        t.Fatalf("the quote did not reach the script literal escaped")
    }

    if true == strings.Contains(body, `example.it\\'s`) {
        t.Fatalf("the quote is preceded by an escaped backslash, so it still closes the literal")
    }
}

/* A backslash of its own is doubled, so it cannot escape the character the manifest puts after it. */
func TestHtmlEscapesABackslash(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, `example.back\slash`, "/back/")

    response := Html(runtimeInstance, nil, nethttp.StatusOK, LoginHtml)
    body := bodyOf(t, response)

    /* json.Marshal already writes the one backslash of the name as two, and the escaping doubles each of them, so the literal carries four. */
    if false == strings.Contains(body, `example.back\\\\slash`) {
        t.Fatalf("the backslash was not escaped")
    }
}

func TestHtmlPutsTheManifestWhereThePageExpectsIt(t *testing.T) {
    runtimeInstance := runtimeServingRoute(t, "example.products.list.page", "/products/")

    response := Html(runtimeInstance, nil, nethttp.StatusOK, LoginHtml)
    body := bodyOf(t, response)

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

/* an anonymous render carries an empty role list, so a page hides every link marked data-requires-role rather than reading an unsubstituted placeholder */
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

/* every page that offers a link carries the viewer, the catalogue among them */
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

/* runtimeServingRouteAs is runtimeServingRoute with the request's security context carrying an authenticated token of the roles */
func runtimeServingRouteAs(t *testing.T, name string, pattern string, roleList []string) melodyruntimecontract.Runtime {
    t.Helper()

    runtimeInstance := runtimeServingRoute(t, name, pattern)

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil, nil, nil, nil, nil, nil, nil,
        "", "",
        nil, nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken("user-2", roleList)),
    )

    return runtimeInstance
}

/* a signed-in render carries the token's roles expanded through the hierarchy, so the page shows the editor the links its role opens; a render that lost them would hide the Users link from an admin and Products from an editor with every other test green */
func TestHtmlPutsTheViewersExpandedRolesInThePage(t *testing.T) {
    runtimeInstance := runtimeServingRouteAs(t, "example.products.list.page", "/products/", []string{"ROLE_EDITOR"})

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, ProductsListHtml))

    if false == strings.Contains(body, `window.melodyViewer = JSON.parse('{"roles":["ROLE_EDITOR","ROLE_USER"]}')`) {
        t.Fatalf("the editor's expanded roles did not reach the page")
    }
}

/* a role reaches the page inside a single-quoted script string, so a quote or a backslash in its spelling — a role the account store accepted as written — must arrive escaped for that string; unescaped, the quote would end the string and the rest of the role would run as script */
func TestHtmlEscapesAViewersRoleForTheScriptString(t *testing.T) {
    runtimeInstance := runtimeServingRouteAs(t, "example.products.list.page", "/products/", []string{`ROLE_O'X\`})

    body := bodyOf(t, Html(runtimeInstance, nil, nethttp.StatusOK, ProductsListHtml))

    if false == strings.Contains(body, `window.melodyViewer = JSON.parse('{"roles":["ROLE_O\'X\\\\"]}')`) {
        t.Fatalf("the viewer's role did not reach the page escaped for its script string")
    }
}

/* every link to a page some roles are refused is marked with the role that opens it, so the bundle hides it from a viewer without that role; a link that lost its marking would lead a viewer to the refusal page with every other test green */
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
