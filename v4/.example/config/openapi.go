package config

import (
    handlercategory "github.com/precision-soft/melody/v4/.example/handler/category"
    handlercurrency "github.com/precision-soft/melody/v4/.example/handler/currency"
    handleri18n "github.com/precision-soft/melody/v4/.example/handler/i18n"
    handlerproduct "github.com/precision-soft/melody/v4/.example/handler/product"
    "github.com/precision-soft/melody/v4/.example/route"
    melodyopenapi "github.com/precision-soft/melody/v4/openapi"
)

/* openApiDocumentInfo is read at registration, after Configure stamped the build's version on the module */
func (instance *Module) openApiDocumentInfo() melodyopenapi.Info {
    return melodyopenapi.Info{
        Title:   "Melody Example API",
        Version: instance.applicationVersion,
    }
}

/* the tags the example's openapi document groups its operations under */
const (
    OpenApiTagProducts   = "products"
    OpenApiTagCategories = "categories"
    OpenApiTagCurrencies = "currencies"
    OpenApiTagI18n       = "i18n"
    OpenApiTagReports    = "reports"
)

/* the descriptors below bind the handler types themselves, never a hand-written copy of them: a copy drifts from the validation rules and the published document then documents a body the route rejects. */
func (instance *Module) buildOpenApi() {
    instance.openApiRegistry = melodyopenapi.NewRegistry()

    instance.openApiRegistry.Describe(route.ProductsApiCreateName, melodyopenapi.Descriptor{
        Summary:     "Create a product",
        Tags:        []string{OpenApiTagProducts},
        RequestType: melodyopenapi.TypeOf[handlerproduct.CreateRequest](),
        Responses:   []melodyopenapi.ResponseDescription{melodyopenapi.Response[handlerproduct.ProductResponse](201)},
    })

    instance.openApiRegistry.Describe(route.CategoriesApiCreateName, melodyopenapi.Descriptor{
        Summary:     "Create a category",
        Tags:        []string{OpenApiTagCategories},
        RequestType: melodyopenapi.TypeOf[handlercategory.CreateRequest](),
        Responses:   []melodyopenapi.ResponseDescription{melodyopenapi.Response[handlercategory.CategoryResponse](201)},
    })

    instance.openApiRegistry.Describe(route.CategoriesApiUpdateName, melodyopenapi.Descriptor{
        Summary:     "Rename a category",
        Tags:        []string{OpenApiTagCategories},
        RequestType: melodyopenapi.TypeOf[handlercategory.UpdateRequest](),
        Responses:   []melodyopenapi.ResponseDescription{melodyopenapi.Response[handlercategory.CategoryResponse](200)},
    })

    instance.openApiRegistry.Describe(route.CategoriesApiDeleteName, melodyopenapi.Descriptor{
        Summary:     "Delete a category",
        Description: "Refused with 409 while a product sits in the category.",
        Tags:        []string{OpenApiTagCategories},
    })

    instance.openApiRegistry.Describe(route.CurrenciesApiCreateName, melodyopenapi.Descriptor{
        Summary:     "Create a currency",
        Tags:        []string{OpenApiTagCurrencies},
        RequestType: melodyopenapi.TypeOf[handlercurrency.CreateRequest](),
        Responses:   []melodyopenapi.ResponseDescription{melodyopenapi.Response[handlercurrency.CurrencyResponse](201)},
    })

    instance.openApiRegistry.Describe(route.I18nGreetingName, melodyopenapi.Descriptor{
        Summary:   "Translated greeting",
        Tags:      []string{OpenApiTagI18n},
        Responses: []melodyopenapi.ResponseDescription{melodyopenapi.Response[handleri18n.GreetingResponse](200)},
    })

    /* the export answers a csv attachment, which no json schema describes, so its descriptor names the representation in words and binds no response type */
    instance.openApiRegistry.Describe(route.ReportsApiExportName, melodyopenapi.Descriptor{
        Summary:     "Export the reading archive",
        Description: "The readings of the history door, newest first under the same limit, as a text/csv attachment named catalog-readings-<date>.csv.",
        Tags:        []string{OpenApiTagReports},
    })
}
