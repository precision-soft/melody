package openapi

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestDocument_TheGoNamesKeepTheOpenApiKeys(t *testing.T) {
    encoded, marshalErr := json.Marshal(Document{
        ExternalDocumentation: &ExternalDocumentation{Url: "https://example.com/docs"},
        Components:            &Components{Schemas: map[string]*Schema{"Order": {Reference: "#/components/schemas/Line"}}},
    })
    if nil != marshalErr {
        t.Fatalf("expected the document to encode, got %v", marshalErr)
    }

    for _, expected := range []string{`"externalDocs":{"url":"https://example.com/docs"}`, `"$ref":"#/components/schemas/Line"`} {
        if false == strings.Contains(string(encoded), expected) {
            t.Fatalf("expected %s in %s", expected, encoded)
        }
    }
}
