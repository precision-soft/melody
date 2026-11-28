package handler

import (
    "testing"
)

func TestExampleParametersOf_NamesEveryParameterOfThePattern(t *testing.T) {
    parameters := exampleParametersOf("/products/api/read/:id/history/:reading/")
    if 2 != len(parameters) || "1" != parameters["id"] || "1" != parameters["reading"] {
        t.Fatalf("expected both parameters sampled, got %v", parameters)
    }

    if sampled := exampleParametersOf("/files/:directory?/*path"); 2 != len(sampled) || "1" != sampled["directory"] || "1" != sampled["path"] {
        t.Fatalf("expected an optional and a wildcard parameter sampled under their names, got %v", sampled)
    }

    if 0 != len(exampleParametersOf("/catalogue/")) {
        t.Fatal("expected no parameter for a static pattern")
    }
}
