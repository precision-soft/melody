package config

import (
    "reflect"

    handleri18n "github.com/precision-soft/melody/v3/.example/handler/i18n"
    handlerproduct "github.com/precision-soft/melody/v3/.example/handler/product"
    "github.com/precision-soft/melody/v3/.example/route"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
)

/* the descriptors below bind the handler types themselves, never a hand-written copy of them: a copy drifts from the validation rules and the published document then documents a body the route rejects. */
func (instance *Module) buildOpenApi() {
    instance.openApiInfo = melodyopenapi.Info{
        Title:   "Melody Example API",
        Version: "1.0.0",
    }

    instance.openApiRegistry = melodyopenapi.NewRegistry()

    melodyopenapi.DescribeTyped[handlerproduct.CreateRequest, handlerproduct.ProductResponse](
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
            200: melodyopenapi.TypeOf[handleri18n.GreetingResponse](),
        },
    })
}
