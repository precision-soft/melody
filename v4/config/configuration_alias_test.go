package config

import (
    "strings"
    "testing"
)

func TestFrameworkEnvironmentKeys_EachKeyFeedsOneKernelParameter(t *testing.T) {
    keys := frameworkEnvironmentKeys()
    if 33 != len(keys) || len(keys) != len(frameworkEnvironmentKeyIndex) {
        t.Fatalf("expected 33 distinct framework keys, got %d listed and %d indexed", len(keys), len(frameworkEnvironmentKeyIndex))
    }

    parameters := make(map[string]bool, len(keys))
    for _, key := range keys {
        if false == strings.HasPrefix(key.Key, environmentKeyNamespace) || false == strings.HasPrefix(key.Parameter, "kernel.") || key.Key != key.Field || nil != key.Rule {
            t.Fatalf("unexpected framework declaration %+v", key)
        }

        if true == parameters[key.Parameter] {
            t.Fatalf("expected %s fed by one key", key.Parameter)
        }
        parameters[key.Parameter] = true
    }
}

func TestFrameworkEnvironmentKeys_AKeyIsOneParameterUnderBothNames_AnUndeclaredLineUnderItsOwn(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{EnvironmentNameKey: EnvironmentProduction, "APP_UNKNOWN_KEY": "x"})

    byKey, byParameter := configuration.Get(EnvironmentNameKey), configuration.Get(KernelEnvironment)
    if nil == byKey || byKey != byParameter || EnvironmentProduction != byParameter.String() {
        t.Fatalf("expected one parameter under the key and the kernel name, got %v and %v", byKey, byParameter)
    }

    if frameworkEnvironmentKeyOwner != configuration.declaredEnvironmentKeys[EnvironmentNameKey].owner {
        t.Fatalf("expected the framework to own its key in the key table")
    }

    unknown := configuration.Get("APP_UNKNOWN_KEY")
    if nil == unknown || "x" != unknown.String() || "x" != unknown.Template() {
        t.Fatalf("expected an undeclared line to answer under itself, holding its line, got %v", unknown)
    }
}
