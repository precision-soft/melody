package config

import (
    "strings"
    "testing"
)

func TestFrameworkEnvironmentKeys_EachKeyFeedsOneKernelParameter(t *testing.T) {
    keys := frameworkEnvironmentKeys()
    if 38 != len(keys) || len(keys) != len(frameworkEnvironmentKeyIndex) {
        t.Fatalf("expected 38 distinct framework keys, got %d listed and %d indexed", len(keys), len(frameworkEnvironmentKeyIndex))
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

/* a keyless kernel parameter answers its own name */
func frameworkParameter(key string) string {
    if declared, isFrameworkKey := frameworkEnvironmentKeyIndex[key]; true == isFrameworkKey {
        return declared.Parameter
    }

    return key
}

func TestFrameworkEnvironmentKeys_AKeyFeedsItsKernelParameter_AnUndeclaredLineIsNoParameter(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{EnvironmentNameKey: EnvironmentProduction, "APP_UNKNOWN_KEY": "x"})

    if nil != configuration.Get(EnvironmentNameKey) || EnvironmentProduction != configuration.Get(KernelEnvironment).String() {
        t.Fatalf("expected the key read through its kernel parameter only")
    }

    if frameworkEnvironmentKeyOwner != configuration.declaredEnvironmentKeys[EnvironmentNameKey].owner {
        t.Fatalf("expected the framework to own its key in the key table")
    }

    if nil != configuration.Get("APP_UNKNOWN_KEY") {
        t.Fatalf("expected an undeclared line to be no parameter")
    }
}
