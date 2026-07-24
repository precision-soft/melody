package config

import (
    "io"
    "log"
    "os"
    "testing"
)

func TestMain(mainInstance *testing.M) {
    log.SetOutput(io.Discard)
    os.Exit(mainInstance.Run())
}

type testEnvironmentSource struct {
    values map[string]string
    err    error
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    if nil != instance.err {
        return nil, instance.err
    }

    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}

func newResolvedConfiguration(
    t *testing.T,
    environmentValues map[string]string,
    declare func(configuration *Configuration),
) *Configuration {
    t.Helper()

    configuration, newConfigurationErr := NewConfiguration(
        &Environment{values: environmentValues},
        "/srv/app",
    )
    if nil != newConfigurationErr {
        t.Fatalf("expected the configuration to build, got %v", newConfigurationErr)
    }

    declare(configuration)

    resolveErr := configuration.Resolve()
    if nil != resolveErr {
        t.Fatalf("expected the parameters to resolve, got %v", resolveErr)
    }

    return configuration
}
