package config

import (
    "reflect"
    "testing"

    handlercurrency "github.com/precision-soft/melody/v4/.example/handler/currency"
    handleri18n "github.com/precision-soft/melody/v4/.example/handler/i18n"
    handlerproduct "github.com/precision-soft/melody/v4/.example/handler/product"
    "github.com/precision-soft/melody/v4/.example/route"
    melodyopenapi "github.com/precision-soft/melody/v4/openapi"
)

func TestOpenApiDocument_CarriesTheApplicationVersion(t *testing.T) {
    info := (&Module{applicationVersion: "9.9.9"}).openApiDocumentInfo()

    if "9.9.9" != info.Version || "Melody Example API" != info.Title {
        t.Fatalf("expected the document versioned with the application, got %+v", info)
    }
}

/* the descriptors are compared whole: a request type dropped or a response bound under the wrong status changes the published document without failing any route */
func TestOpenApiRegistry_DescribesTheExampleRoutesWithTheirTypes(t *testing.T) {
    module := &Module{applicationVersion: "9.9.9"}
    module.buildOpenApi()

    expected := map[string]melodyopenapi.Descriptor{
        route.ProductsApiCreateName: {
            Summary:     "Create a product",
            Tags:        []string{OpenApiTagProducts},
            RequestType: melodyopenapi.TypeOf[handlerproduct.CreateRequest](),
            Responses:   []melodyopenapi.ResponseDescription{{Status: 201, Type: melodyopenapi.TypeOf[handlerproduct.ProductResponse]()}},
        },
        route.CurrenciesApiCreateName: {
            Summary:     "Create a currency",
            Tags:        []string{OpenApiTagCurrencies},
            RequestType: melodyopenapi.TypeOf[handlercurrency.CreateRequest](),
            Responses:   []melodyopenapi.ResponseDescription{{Status: 201, Type: melodyopenapi.TypeOf[handlercurrency.CurrencyResponse]()}},
        },
        route.I18nGreetingName: {
            Summary:   "Translated greeting",
            Tags:      []string{OpenApiTagI18n},
            Responses: []melodyopenapi.ResponseDescription{{Status: 200, Type: melodyopenapi.TypeOf[handleri18n.GreetingResponse]()}},
        },
        route.ReportsApiExportName: {
            Summary:     "Export the reading archive",
            Description: "The readings of the history door, newest first under the same limit, as a text/csv attachment named catalog-readings-<date>.csv.",
            Tags:        []string{OpenApiTagReports},
        },
    }

    for routeName, expectedDescriptor := range expected {
        descriptor, exists := module.openApiRegistry.Get(routeName)
        if false == exists {
            t.Fatalf("expected %s to be described", routeName)
        }

        if false == reflect.DeepEqual(expectedDescriptor, descriptor) {
            t.Fatalf("unexpected descriptor of %s:\n got %+v\nwant %+v", routeName, descriptor, expectedDescriptor)
        }
    }
}
