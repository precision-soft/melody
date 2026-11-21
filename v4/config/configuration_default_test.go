package config

import (
    "path/filepath"
    "testing"

    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

func TestRegisterDefaultParameters_DeclaresTheDefaultsAnApplicationInheritsByDeclaringNothing(t *testing.T) {
    configuration := newResolvedConfiguration(t, map[string]string{}, func(configuration *Configuration) {})

    for key, expected := range map[string]any{
        KernelProjectDirectory:       "/srv/app",
        DefaultModeKey:               ModeHttp,
        ProcessRoleKey:               RoleAll,
        EnvironmentNameKey:           EnvironmentDevelopment,
        HttpAddressKey:               ":8080",
        HttpMaxRequestBodyBytesKey:   "1048576",
        HttpSessionTtlKey:            DefaultSessionTtl.String(),
        HttpShutdownTimeoutKey:       DefaultHttpShutdownTimeout.String(),
        TeardownTimeoutKey:           "",
        HttpReadTimeoutKey:           "",
        HttpReadHeaderTimeoutKey:     "",
        HttpWriteTimeoutKey:          "",
        HttpIdleTimeoutKey:           "",
        HttpMaxHeaderBytesKey:        "",
        HttpUnavailableRetryAfterKey: "",
        CliNameKey:                   "melody",
        CliDescriptionKey:            "",
        LogLevelKey:                  string(loggingcontract.LevelDebug),
        DefaultLocaleKey:             "en",
        PublicDirectoryKey:           "public",
        StaticIndexFileKey:           "index.html",
        StaticCacheDisabledKey:       "false",
        StaticCacheMaxAgeKey:         "3600",
    } {
        parameter := configuration.Get(frameworkParameter(key))
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

func TestRegisterDefaultParameters_TheDirectoryDefaultsHangUnderTheProjectDirectory(t *testing.T) {
    configuration := newResolvedConfiguration(t, map[string]string{}, func(configuration *Configuration) {})

    logsDirectory := configuration.Get(KernelLogsDirectory)
    if nil == logsDirectory {
        t.Fatalf("expected the logs directory to be declared")
    }

    if filepath.Join("/srv/app", "var", "log") != logsDirectory.String() {
        t.Fatalf("expected the logs directory to hang under the project directory, got %q", logsDirectory.String())
    }

    cacheDirectory := configuration.Get(KernelCacheDirectory)
    if nil == cacheDirectory {
        t.Fatalf("expected the cache directory to be declared")
    }

    if filepath.Join("/srv/app", "var", "cache") != cacheDirectory.String() {
        t.Fatalf("expected the cache directory to hang under the project directory, got %q", cacheDirectory.String())
    }
}

func TestRegisterDefaultParameters_AnEnvironmentValueWinsOverTheDefault(t *testing.T) {
    configuration := newResolvedConfiguration(
        t,
        map[string]string{
            LogLevelKey:    string(loggingcontract.LevelError),
            HttpAddressKey: ":9090",
        },
        func(configuration *Configuration) {},
    )

    logLevel := configuration.Get(frameworkParameter(LogLevelKey))
    if string(loggingcontract.LevelError) != logLevel.String() {
        t.Fatalf("expected the declared log level to win, got %q", logLevel.String())
    }

    if true == logLevel.IsDefault() {
        t.Fatalf("expected a parameter the environment declared to stop reporting itself as a default")
    }

    httpAddress := configuration.Get(frameworkParameter(HttpAddressKey))
    if ":9090" != httpAddress.String() {
        t.Fatalf("expected the declared address to win, got %q", httpAddress.String())
    }

    /* a default the environment said nothing about is untouched by the two that were overridden */
    if "melody" != configuration.Get(frameworkParameter(CliNameKey)).String() {
        t.Fatalf("expected the untouched default to survive, got %q", configuration.Get(frameworkParameter(CliNameKey)).String())
    }
}

func TestFrameworkDefault_IsTextAndReadsLikeASetKey(t *testing.T) {
    configuration := mustConfigurationFromValues(t, map[string]string{})

    for name, expected := range map[string]int{
        KernelHttpMaxRequestBodyBytes: 1048576,
        KernelHttpSessionMaxSessions:  100000,
        KernelStaticCacheMaxAge:       3600,
    } {
        parameter := configuration.Get(name)
        if _, isText := parameter.Value().(string); false == isText {
            t.Fatalf("%s: expected the default as text, got %T", name, parameter.Value())
        }

        intValue, intErr := parameter.Int()
        if nil != intErr || expected != intValue {
            t.Fatalf("%s: expected %d, got %d (%v)", name, expected, intValue, intErr)
        }
    }

    cacheDisabled := configuration.Get(KernelStaticCacheDisabled)
    if _, isText := cacheDisabled.Value().(string); false == isText {
        t.Fatalf("expected the static cache switch default as text, got %T", cacheDisabled.Value())
    }

    if boolValue, boolErr := cacheDisabled.Bool(); nil != boolErr || false != boolValue {
        t.Fatalf("expected false, got %v (%v)", boolValue, boolErr)
    }
}
