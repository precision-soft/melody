package openapi_test

import (
    "context"
    "encoding/json"
    "io"
    nethttp "net/http"
    "reflect"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/openapi"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func TestSpecHandler_ServesDocumentFromLiveRoutes(t *testing.T) {
    router := melodyhttp.NewRouter()
    router.HandleNamed(
        "products.create",
        "POST",
        "/products/api/create/",
        func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
    )

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
    )

    registry := openapi.NewRegistry()
    registry.Describe("products.create", openapi.Descriptor{
        Summary:     "Create a product",
        RequestType: openapi.TypeOf[createProductRequest](),
        Responses: map[int]reflect.Type{
            201: openapi.TypeOf[productResponse](),
        },
    })

    runtimeInstance := runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    handler := openapi.SpecHandler(openapi.Info{Title: "Example", Version: "1.0.0"}, registry)

    response, handlerErr := handler(runtimeInstance, nil, nil)
    if nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected status 200, got %d", response.StatusCode())
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read body: %v", readErr)
    }

    document := struct {
        OpenApi string                     `json:"openapi"`
        Paths   map[string]json.RawMessage `json:"paths"`
    }{}
    if unmarshalErr := json.Unmarshal(body, &document); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if "3.0.3" != document.OpenApi {
        t.Fatalf("unexpected openapi version: %s", document.OpenApi)
    }

    if _, exists := document.Paths["/products/api/create"]; false == exists {
        t.Fatalf("expected the live route to appear in the served document, got paths %v", document.Paths)
    }
}
