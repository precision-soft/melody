package config

import (
    "reflect"

    "github.com/precision-soft/melody/v3/.example/route"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
)

type productCreateRequest struct {
    Name  string  `json:"name" validate:"notBlank,min=2"`
    Price float64 `json:"price" validate:"greaterThan=0"`
}

type productView struct {
    Id    string  `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}

type greetingView struct {
    Locale   string `json:"locale"`
    Greeting string `json:"greeting"`
}

func (instance *Module) buildOpenApi() {
    instance.openApiInfo = melodyopenapi.Info{
        Title:   "Melody Example API",
        Version: "1.0.0",
    }

    instance.openApiRegistry = melodyopenapi.NewRegistry()

    instance.openApiRegistry.Describe(route.ProductsApiCreateName, melodyopenapi.Descriptor{
        Summary:     "Create a product",
        Tags:        []string{"products"},
        RequestType: melodyopenapi.TypeOf[productCreateRequest](),
        Responses: map[int]reflect.Type{
            201: melodyopenapi.TypeOf[productView](),
        },
    })

    instance.openApiRegistry.Describe(route.I18nGreetingName, melodyopenapi.Descriptor{
        Summary: "Translated greeting",
        Tags:    []string{"i18n"},
        Responses: map[int]reflect.Type{
            200: melodyopenapi.TypeOf[greetingView](),
        },
    })
}
