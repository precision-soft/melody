package i18n

import (
    "context"
    "encoding/json"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodytranslation "github.com/precision-soft/melody/v3/translation"
    melodytranslationcontract "github.com/precision-soft/melody/v3/translation/contract"
)

func greetingFor(t *testing.T, routeLocale string, query string) GreetingResponse {
    t.Helper()

    english := melodytranslation.NewMapCatalog("en")
    english.Add("messages", "greeting", "Hello, {name}!")
    romanian := melodytranslation.NewMapCatalog("ro")
    romanian.Add("messages", "greeting", "Salut, {name}!")

    containerInstance := melodycontainer.NewContainer()
    melodycontainer.MustRegister(
        containerInstance,
        melodytranslation.ServiceTranslator,
        func(resolver melodycontainercontract.Resolver) (melodytranslationcontract.Translator, error) {
            return melodytranslation.NewManager("en", []string{"en"}, english, romanian), nil
        },
    )

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/"+routeLocale+"/i18n/greeting/?"+query, nil)
    request := melodyhttp.NewRequest(httpRequest, nil, runtimeInstance, melodyhttp.NewRequestContext("greeting-test", time.Now()))
    request.Attributes().Set(melodyhttp.RouteAttributeLocale, routeLocale)

    response, handlerErr := GreetingHandler()(runtimeInstance, httptest.NewRecorder(), request)
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
