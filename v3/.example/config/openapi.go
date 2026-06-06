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

    melodyopenapi.DescribeTyped[productCreateRequest, productView](
        instance.openApiRegistry,
        route.ProductsApiCreateName,
        201,
        melodyopenapi.WithSummary("Create a product"),
        melodyopenapi.WithTags("products"),
    )

    instance.openApiRegistry.Describe(route.I18nGreetingName, melodyopenapi.Descriptor{
        Summary: "Translated greeting",
        Tags:    []string{"i18n"},
        Responses: map[int]reflect.Type{
            200: melodyopenapi.TypeOf[greetingView](),
        },
    })
}
