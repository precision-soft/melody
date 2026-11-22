package openapi

import (
    "context"
    "encoding/json"
    "io"
    nethttp "net/http"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

func TestSpecificationHandler_ServesDocumentFromLiveRoutes(t *testing.T) {
    router := melodyhttp.NewRouter()
    router.Handle(
        "/products/api/create/",
        func(runtimeInstance containercontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            return nil, nil
        },
        melodyhttp.RouteOptions{Name: "products.create", Methods: []string{"POST"}},
    )

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return router, nil
        },
        container.RegisterOptions{},
    )

    registry := NewRegistry()
    registry.Describe("products.create", Descriptor{
        Summary:     "Create a product",
        RequestType: TypeOf[createProductRequest](),
        Responses:   []ResponseDescription{Response[productResponse](201)},
    })
    registerSpecificationServices(serviceContainer, registry, &Info{Title: "Example", Version: "1.0.0"})

    runtimeInstance := runtime.NewRequest(context.Background(), serviceContainer.NewScope())

    handler := SpecificationHandler()

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
        Info    Info                       `json:"info"`
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

    if "Example" != document.Info.Title || "1.0.0" != document.Info.Version {
        t.Fatalf("expected the info of the container's info service, got %+v", document.Info)
    }

    if false == jsonContains(document.Paths["/products/api/create"], "201") {
        t.Fatalf("expected the registry's 201 response in the served document, got %s", document.Paths["/products/api/create"])
    }
}

/* a container with the registry and no info service serves the document with an empty info block, as the generate command writes it */
func TestSpecificationHandler_ServesAnEmptyInfoWhenNoInfoServiceIsRegistered(t *testing.T) {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        melodyhttp.ServiceRouter,
        func(resolver containercontract.Resolver) (httpcontract.Router, error) {
            return melodyhttp.NewRouter(), nil
        },
        container.RegisterOptions{},
    )
    registerSpecificationServices(serviceContainer, NewRegistry(), nil)

    response, handlerErr := SpecificationHandler()(runtime.NewRequest(context.Background(), serviceContainer.NewScope()), nil, nil)
    if nil != handlerErr {
        t.Fatalf("handler: %v", handlerErr)
    }

    body, readErr := io.ReadAll(response.BodyReader())
    if nil != readErr {
        t.Fatalf("read body: %v", readErr)
    }

    document := struct {
        Info Info `json:"info"`
    }{}
    if unmarshalErr := json.Unmarshal(body, &document); nil != unmarshalErr {
        t.Fatalf("unmarshal: %v", unmarshalErr)
    }

    if (Info{}) != document.Info {
        t.Fatalf("expected an empty info block, got %+v", document.Info)
    }
}

func registerSpecificationServices(serviceContainer containercontract.Container, registry *Registry, info *Info) {
    serviceContainer.MustRegister(
        ServiceOpenApiRegistry,
        func(resolver containercontract.Resolver) (*Registry, error) {
            return registry, nil
        },
        container.RegisterOptions{},
    )

    if nil == info {
        return
    }

    serviceContainer.MustRegister(
        ServiceOpenApiInfo,
        func(resolver containercontract.Resolver) (Info, error) {
            return *info, nil
        },
        container.RegisterOptions{},
    )
}

func jsonContains(raw json.RawMessage, fragment string) bool {
    return true == strings.Contains(string(raw), "\""+fragment+"\"")
}
