package url

import (
    "encoding/json"
    nethttp "net/http"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v2/container"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyhttp "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* containerServingRouter hands the route registry of a real router to the resolver RoutesJsonFromContainer reads, so the projection is exercised over the definitions the router itself produces rather than over a hand-built list that could drift from them. */
func containerServingRouter(t *testing.T, router *melodyhttp.Router) containercontract.Container {
    t.Helper()

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

    return serviceContainer
}

func noopHandler(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
    return melodyhttp.NewResponse(nethttp.StatusOK, []byte("ok")), nil
}

/* The frontend RouteGenerator (assets/melody-routes.ts) reads window.melodyRoutes as an OBJECT carrying the routes under a "routes" key. A bare array decodes into a value whose .routes is undefined, so the generator is built over an empty manifest and every route name it is asked for is unknown — which is not a broken URL but a thrown error on the first link the page builds. The envelope is therefore the contract, and it is asserted as a shape, not as a byte string, so the field order json.Marshal chooses cannot make this test lie. */
func TestRoutesJsonFromContainer_WrapsTheRoutesInAnEnvelopeUnderTheRoutesKey(t *testing.T) {
    router := melodyhttp.NewRouter()
    router.HandleNamed("example.products.list.page", nethttp.MethodGet, "/products/", noopHandler)
    router.HandleNamed("example.users.api.read", nethttp.MethodGet, "/users/api/read/:id/", noopHandler)

    document, documentErr := RoutesJsonFromContainer(containerServingRouter(t, router))
    if nil != documentErr {
        t.Fatalf("unexpected error: %v", documentErr)
    }

    var manifest struct {
        Routes []struct {
            Name    string `json:"name"`
            Pattern string `json:"pattern"`
        } `json:"routes"`
    }

    if unmarshalErr := json.Unmarshal([]byte(document), &manifest); nil != unmarshalErr {
        t.Fatalf("the manifest does not decode into the shape the frontend reads: %v (document %s)", unmarshalErr, document)
    }

    if 2 != len(manifest.Routes) {
        t.Fatalf("expected the two registered routes under the routes key, got %d (document %s)", len(manifest.Routes), document)
    }

    patternByName := make(map[string]string, len(manifest.Routes))
    for _, entry := range manifest.Routes {
        patternByName[entry.Name] = entry.Pattern
    }

    /* the router normalizes a registered pattern through splitPath, so the manifest carries the pattern the ROUTER holds rather than the string the registration was spelled with — which is what the frontend has to generate against, since that is what the matcher compares a request path to */
    if "/products" != patternByName["example.products.list.page"] {
        t.Fatalf("expected the listing route to carry the pattern the router holds, got %q", patternByName["example.products.list.page"])
    }

    if "/users/api/read/:id" != patternByName["example.users.api.read"] {
        t.Fatalf("expected the placeholder pattern to reach the frontend, got %q", patternByName["example.users.api.read"])
    }
}

/* A bare array is exactly what this function used to answer, and it is the one wrong shape that still parses: the assertion is on the decoded json type, because a document that happens to start with '[' would pass a substring check on "routes" the moment a route is NAMED routes. */
func TestRoutesJsonFromContainer_DoesNotAnswerABareArray(t *testing.T) {
    router := melodyhttp.NewRouter()
    router.HandleNamed("example.products.list.page", nethttp.MethodGet, "/products/", noopHandler)

    document, documentErr := RoutesJsonFromContainer(containerServingRouter(t, router))
    if nil != documentErr {
        t.Fatalf("unexpected error: %v", documentErr)
    }

    var decoded any
    if unmarshalErr := json.Unmarshal([]byte(document), &decoded); nil != unmarshalErr {
        t.Fatalf("the manifest is not valid json: %v (document %s)", unmarshalErr, document)
    }

    if _, isObject := decoded.(map[string]any); false == isObject {
        t.Fatalf("expected the manifest to be a json object, got %T (document %s)", decoded, document)
    }
}

/* The frontend addresses routes BY name; an unnamed one could never be asked for, so shipping it would only tell the browser about a pattern the server serves and nothing can use. The router records unnamed routes in the same list as named ones — RouteDefinitions walks every route it holds — so the skip is reachable from a router built exactly as the example builds one. */
func TestRoutesJsonFromContainer_SkipsARouteWithoutAName(t *testing.T) {
    router := melodyhttp.NewRouter()
    router.HandleNamed("example.products.list.page", nethttp.MethodGet, "/products/", noopHandler)
    router.Handle(nethttp.MethodGet, "/internal/unnamed/", noopHandler)

    document, documentErr := RoutesJsonFromContainer(containerServingRouter(t, router))
    if nil != documentErr {
        t.Fatalf("unexpected error: %v", documentErr)
    }

    var manifest struct {
        Routes []struct {
            Name    string `json:"name"`
            Pattern string `json:"pattern"`
        } `json:"routes"`
    }

    if unmarshalErr := json.Unmarshal([]byte(document), &manifest); nil != unmarshalErr {
        t.Fatalf("the manifest does not decode: %v (document %s)", unmarshalErr, document)
    }

    if 1 != len(manifest.Routes) {
        t.Fatalf("expected only the named route, got %d entries (document %s)", len(manifest.Routes), document)
    }

    if "example.products.list.page" != manifest.Routes[0].Name {
        t.Fatalf("expected the named route, got %q", manifest.Routes[0].Name)
    }
}

/* A registry holding nothing must still answer the envelope: the generator is constructed unconditionally on every page, and `{}` or `null` would leave it reading .routes off a value that has none. */
func TestRoutesJsonFromContainer_AnswersAnEmptyEnvelopeWhenNoRouteIsNamed(t *testing.T) {
    router := melodyhttp.NewRouter()
    router.Handle(nethttp.MethodGet, "/internal/unnamed/", noopHandler)

    document, documentErr := RoutesJsonFromContainer(containerServingRouter(t, router))
    if nil != documentErr {
        t.Fatalf("unexpected error: %v", documentErr)
    }

    if `{"routes":[]}` != document {
        t.Fatalf("expected an empty envelope, got %s", document)
    }
}
