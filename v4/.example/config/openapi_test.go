package config

import (
    "testing"
)

func TestOpenApiDocument_CarriesTheApplicationVersion(t *testing.T) {
    info := (&Module{applicationVersion: "9.9.9"}).openApiDocumentInfo()

    if "9.9.9" != info.Version || "Melody Example API" != info.Title {
        t.Fatalf("expected the document versioned with the application, got %+v", info)
    }
}
