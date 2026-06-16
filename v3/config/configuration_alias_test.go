package config

import (
    "testing"

    "github.com/precision-soft/melody/v3/logging"
)

func TestConfiguration_AddAliasedParameterFromEnvironment_SharesSinglePointerAcrossAliases(t *testing.T) {
    configuration := &Configuration{
        environment: nil,
        parameters:  make(ParameterMap),
        logger:      logging.NewDefaultLogger(),
    }

    err := configuration.addAliasedParameterFromEnvironment(
        []string{
            "primaryKey",
            "aliasKey",
        },
        "ENV_KEY",
        "ENV_VALUE",
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    primary := configuration.parameters["primaryKey"]
    alias := configuration.parameters["aliasKey"]

    if nil == primary || nil == alias {
        t.Fatalf("expected both parameters to exist")
    }

    if primary != alias {
        t.Fatalf("expected alias to point to the same parameter instance")
    }
}
