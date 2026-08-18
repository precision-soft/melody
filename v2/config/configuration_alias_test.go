package config

import (
    "testing"

    "github.com/precision-soft/melody/v2/logging"
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

func TestConfiguration_AddAliasedParameterFromEnvironment_SkipsWhatCannotBeNamed(t *testing.T) {
    configuration := &Configuration{
        environment: nil,
        parameters:  make(ParameterMap),
        logger:      logging.NewDefaultLogger(),
    }

    if err := configuration.addAliasedParameterFromEnvironment(nil, "ENV_KEY", "ENV_VALUE"); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if err := configuration.addAliasedParameterFromEnvironment([]string{}, "ENV_KEY", "ENV_VALUE"); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 0 != len(configuration.parameters) {
        t.Fatalf("expected no parameter to be registered without a name, got %d", len(configuration.parameters))
    }

    if err := configuration.addAliasedParameterFromEnvironment([]string{"", "named"}, "ENV_KEY", "ENV_VALUE"); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != len(configuration.parameters) {
        t.Fatalf("expected only the spellable name to be registered, got %d", len(configuration.parameters))
    }

    if nil == configuration.parameters["named"] {
        t.Fatalf("expected the named parameter to be registered")
    }
}

func TestConfiguration_MapEnvironmentKeyToParameterNames_CollapsesTheBuiltInAliases(t *testing.T) {
    configuration := &Configuration{
        environment: nil,
        parameters:  make(ParameterMap),
        logger:      logging.NewDefaultLogger(),
    }

    names := configuration.mapEnvironmentKeyToParameterNames(EnvKey)
    if 2 != len(names) {
        t.Fatalf("expected the built-in key to answer under both spellings, got %#v", names)
    }

    foundShort := false
    foundKernel := false
    for _, name := range names {
        if EnvKey == name {
            foundShort = true
        }
        if KernelEnv == name {
            foundKernel = true
        }
    }

    if false == foundShort || false == foundKernel {
        t.Fatalf("expected both the environment key and the kernel name, got %#v", names)
    }

    unknownNames := configuration.mapEnvironmentKeyToParameterNames("APP_UNKNOWN_KEY")
    if 1 != len(unknownNames) || "APP_UNKNOWN_KEY" != unknownNames[0] {
        t.Fatalf("expected an unknown key to answer under itself alone, got %#v", unknownNames)
    }
}
