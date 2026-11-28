package i18n

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodytranslation "github.com/precision-soft/melody/v4/translation"
    melodytranslationcontract "github.com/precision-soft/melody/v4/translation/contract"
)

type failingTranslator struct {
    err error
}

func (instance failingTranslator) Translate(messageId string, options melodytranslationcontract.TranslateOptions) (string, error) {
    return "", instance.err
}

func (instance failingTranslator) HasMessage(messageId string, options melodytranslationcontract.TranslateOptions) bool {
    return true
}

func serveGreeting(t *testing.T, translator melodytranslationcontract.Translator, routeLocale string, query string) (melodyhttpcontract.Response, error) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister(
        containerInstance,
        melodytranslation.ServiceTranslator,
        func(resolver melodycontainercontract.Resolver) (melodytranslationcontract.Translator, error) {
            return translator, nil
        },
        melodycontainer.RegisterOptions{},
    )

    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/"+routeLocale+"/i18n/greeting/?"+query, nil)
    request := melodyhttp.NewRequest(httpRequest, runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("greeting-test", time.Now())})
    request.Attributes().Set(melodyhttp.RouteAttributeLocale, routeLocale)

    return GreetingHandler()(runtimeInstance, httptest.NewRecorder(), request)
}

func greetingFor(t *testing.T, routeLocale string, query string) GreetingResponse {
    t.Helper()

    english := melodytranslation.NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")
    romanian := melodytranslation.NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut, {name}!")

    translator := melodytranslation.NewManager(melodytranslation.ManagerConfig{Catalogs: []melodytranslationcontract.Catalog{english, romanian}, FallbackLocales: []string{"en"}})

    response, handlerErr := serveGreeting(t, translator, routeLocale, query)
    if nil != handlerErr {
        t.Fatalf("expected the greeting, got %v", handlerErr)
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read the greeting: %v", readErr)
    }

    envelope := struct {
        Payload GreetingResponse `json:"payload"`
    }{}
    if decodeErr := json.Unmarshal(body, &envelope); nil != decodeErr {
        t.Fatalf("decode the greeting %s: %v", body, decodeErr)
    }

    return envelope.Payload
}

func TestGreetingHandler_ServesTheLocaleTheRouteMatched(t *testing.T) {
    greeting := greetingFor(t, "ro", "name=Ana&locale=en")

    if "ro" != greeting.Locale || "Salut, Ana!" != greeting.Greeting {
        t.Fatalf("expected the route's ro locale served whatever the query says, got %+v", greeting)
    }
}

func TestGreetingHandler_ShowsTheIdOfAMessageNoCatalogCarries(t *testing.T) {
    greeting := greetingFor(t, "en", "count=2")

    if "cart.items" != greeting.Cart {
        t.Fatalf("expected the id of the missing cart message shown, got %+v", greeting)
    }
}

func TestGreetingHandler_AnswersTheTranslatorsErrorAs500(t *testing.T) {
    renderErr := errors.New("translation message pattern is malformed")

    response, handlerErr := serveGreeting(t, failingTranslator{err: renderErr}, "en", "name=Ana")
    if false == errors.Is(handlerErr, renderErr) || nil != response {
        t.Fatalf("expected the translator's error returned and no payload, got %v, %v", response, handlerErr)
    }
}
