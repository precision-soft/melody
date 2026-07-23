package config

import (
    "testing"
)

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

func TestRegisterRuntimeSecret_MarksOnlyTheDeclaredParameter(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{
            "CLIENT_ID":     "public-identifier",
            "CLIENT_SECRET": "P4ssPhrase",
        },
        func(configuration *Configuration) {
            configuration.RegisterRuntime("app.client_id", "%env(CLIENT_ID)%")
            configuration.RegisterRuntimeSecret("app.client_secret", "%env(CLIENT_SECRET)%")
        },
    )

    expectations := map[string]bool{
        "app.client_id":     false,
        "app.client_secret": true,
    }

    for name, expectedSecret := range expectations {
        parameter := configuration.Get(name)
        if nil == parameter {
            t.Fatalf("expected parameter %q to exist", name)
        }

        if expectedSecret != parameter.IsSecret() {
            t.Fatalf("expected %q secret marking to be %t", name, expectedSecret)
        }
    }
}

/* @info the marking governs display only: a service that consumes the parameter still receives the credential in full */
func TestRegisterRuntimeSecret_LeavesTheValueIntact(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"CLIENT_SECRET": "P4ssPhrase"},
        func(configuration *Configuration) {
            configuration.RegisterRuntimeSecret("app.client_secret", "%env(CLIENT_SECRET)%")
        },
    )

    if "P4ssPhrase" != configuration.Get("app.client_secret").String() {
        t.Fatalf("expected the secret value to resolve in full for its consumers")
    }
}

/* @info a dsn assembled from a declared password holds the credential in full; without propagation the password would be redacted while the dsn beside it printed in clear */
func TestRegisterRuntimeSecret_PropagatesToParametersThatReadIt(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"DATABASE_PASSWORD": "P4ssPhrase"},
        func(configuration *Configuration) {
            configuration.RegisterRuntimeSecret("database.password", "%env(DATABASE_PASSWORD)%")
            configuration.RegisterRuntime("database.dsn", "postgres://app:%database.password%@db:5432/app")
        },
    )

    dsnParameter := configuration.Get("database.dsn")
    if nil == dsnParameter {
        t.Fatalf("expected the dsn parameter to exist")
    }

    if "postgres://app:P4ssPhrase@db:5432/app" != dsnParameter.String() {
        t.Fatalf("expected the dsn to resolve in full, got %q", dsnParameter.String())
    }

    if false == dsnParameter.IsSecret() {
        t.Fatalf("expected the dsn to inherit the secret marking of the password it reads")
    }
}

func TestRegisterRuntime_LeavesAnOrdinaryParameterUnmarked(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{"API_URL": "https://api.example.test"},
        func(configuration *Configuration) {
            configuration.RegisterRuntime("app.api_url", "%env(API_URL)%")
        },
    )

    if true == configuration.Get("app.api_url").IsSecret() {
        t.Fatalf("expected an ordinary parameter to stay unmarked")
    }
}
