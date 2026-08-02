package config

import (
    "path/filepath"
    "testing"

    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

/* @info these are the values an application gets by declaring nothing, so every one of them is a decision the framework makes on the caller's behalf — and nothing had ever read them back. The three that matter most are the ones the hardening sweep argued over: a bounded session lifetime rather than an unbounded store, a body limit rather than none, and the development environment rather than production, because the safe default of the last one is the LOUD one. */
func TestRegisterDefaultParameters_DeclaresTheDefaultsAnApplicationInheritsByDeclaringNothing(t *testing.T) {
    configuration := newResolvedConfiguration(t, map[string]string{}, func(configuration *Configuration) {})

    for key, expected := range map[string]any{
        KernelProjectDir:           "/srv/app",
        DefaultModeKey:             ModeHttp,
        ProcessRoleKey:             RoleAll,
        EnvKey:                     EnvDevelopment,
        HttpAddressKey:             ":8080",
        HttpMaxRequestBodyBytesKey: 1048576,
        HttpSessionTtlKey:          DefaultSessionTtl.String(),
        CliNameKey:                 "melody",
        CliDescriptionKey:          "",
        LogLevelKey:                string(loggingcontract.LevelDebug),
        DefaultLocaleKey:           "en",
        PublicDirKey:               "public",
        StaticIndexFileKey:         "index.html",
        StaticEnableCacheKey:       true,
        StaticCacheMaxAgeKey:       3600,
    } {
        parameter := configuration.Get(key)
        if nil == parameter {
            t.Fatalf("expected the default %q to be declared", key)
        }

        if expected != parameter.Value() {
            t.Fatalf("expected %q to default to %#v, got %#v", key, expected, parameter.Value())
        }

        if false == parameter.IsDefault() {
            t.Fatalf("expected %q to report itself as a default", key)
        }
    }
}

/* @info the directory defaults are declared as REFERENCES to the project directory rather than as absolute paths, so an application that moves its project moves them with it; the resolution is what turns them into paths, and a default written absolute would pin them to whatever the first boot saw. */
func TestRegisterDefaultParameters_TheDirectoryDefaultsHangUnderTheProjectDirectory(t *testing.T) {
    configuration := newResolvedConfiguration(t, map[string]string{}, func(configuration *Configuration) {})

    logsDirectory := configuration.Get(KernelLogsDir)
    if nil == logsDirectory {
        t.Fatalf("expected the logs directory to be declared")
    }

    if filepath.Join("/srv/app", "var", "log") != logsDirectory.String() {
        t.Fatalf("expected the logs directory to hang under the project directory, got %q", logsDirectory.String())
    }

    cacheDirectory := configuration.Get(KernelCacheDir)
    if nil == cacheDirectory {
        t.Fatalf("expected the cache directory to be declared")
    }

    if filepath.Join("/srv/app", "var", "cache") != cacheDirectory.String() {
        t.Fatalf("expected the cache directory to hang under the project directory, got %q", cacheDirectory.String())
    }
}

/* @info a value the environment declares must win over the default, or the defaults would be a floor nobody can raise — the parameter names ARE the environment keys, which is what makes the override a single lookup rather than a mapping table, and the parameter stops reporting itself as a default once one arrives. */
func TestRegisterDefaultParameters_AnEnvironmentValueWinsOverTheDefault(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{
            LogLevelKey:    string(loggingcontract.LevelError),
            HttpAddressKey: ":9090",
        },
        func(configuration *Configuration) {},
    )

    logLevel := configuration.Get(LogLevelKey)
    if string(loggingcontract.LevelError) != logLevel.String() {
        t.Fatalf("expected the declared log level to win, got %q", logLevel.String())
    }

    if true == logLevel.IsDefault() {
        t.Fatalf("expected a parameter the environment declared to stop reporting itself as a default")
    }

    httpAddress := configuration.Get(HttpAddressKey)
    if ":9090" != httpAddress.String() {
        t.Fatalf("expected the declared address to win, got %q", httpAddress.String())
    }

    /* a default the environment said nothing about is untouched by the two that were overridden */
    if "melody" != configuration.Get(CliNameKey).String() {
        t.Fatalf("expected the untouched default to survive, got %q", configuration.Get(CliNameKey).String())
    }
}
