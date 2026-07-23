package config

import (
    "testing"
)

/* @info the resolver fails on anything placeholder-shaped it cannot resolve, so the post-resolution hook has nothing left to check and must never fail a boot on a value whose percents are data */
func TestValidate_PassesAfterResolution(t *testing.T) {
    configuration := &Configuration{
        environment: &Environment{values: map[string]string{}},
        parameters: ParameterMap{
            "app.progress": NewParameter("", "50%% done", "", false),
        },
    }

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the resolution to succeed, got %v", resolveErr)
    }

    if "50% done" != configuration.getInternalParameter("app.progress").String() {
        t.Fatalf("unexpected resolved value %q", configuration.getInternalParameter("app.progress").String())
    }

    if validateErr := configuration.validate(); nil != validateErr {
        t.Fatalf("expected the validation to pass, got %v", validateErr)
    }
}
