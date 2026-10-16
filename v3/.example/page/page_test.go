package page

import (
    "context"
    "errors"
    "io"
    nethttp "net/http"
    "strings"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* runtimeServingRoute hands back a runtime whose route registry holds exactly the one public route the caller names, so a test can put an arbitrary spelling into the manifest the page splices into its script; the runtime carries no security context, so the manifest is the public zone alone. */
func runtimeServingRoute(t *testing.T, name string, pattern string) melodyruntimecontract.Runtime {
    t.Helper()

    router := melodyhttp.NewRouter()
    router.HandleWithOptions(
        pattern,
        func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
            return melodyhttp.NewResponse(nethttp.StatusOK, []byte("ok")), nil
        },
        melodyhttp.NewRouteOptions(name, []string{"GET"}, "", nil, nil, nil, nil, 0, melodyhttp.ExposedRouteAttributes(melodyhttp.RouteZonePublic)),
    )

    serviceContainer := melodycontainer.NewContainer()

    registerErr := melodycontainer.Register[melodyhttpcontract.RouteRegistry](
        serviceContainer,
        melodyhttp.ServiceRouteRegistry,
        func(resolver melodycontainercontract.Resolver) (melodyhttpcontract.RouteRegistry, error) {
            return router.RouteRegistry(), nil
        },
    )
    if nil != registerErr {
        t.Fatalf("register the route registry: %v", registerErr)
    }

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
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

/* The manifest is spliced into a single-quoted JavaScript literal and json.Marshal leaves a single quote bare, so a quote in a route name must reach the literal escaped. The negative assertion pins the order of the two replacements: escaping the quote first yields \\', an escaped backslash followed by a live quote. Only an input carrying a quote separates the two orders. */
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

/* json.Marshal writes the one backslash of the name as two, and the escaping doubles each of them, so the literal carries four. */
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
