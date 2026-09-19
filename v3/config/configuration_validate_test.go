package config

import (
    "testing"
)

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
