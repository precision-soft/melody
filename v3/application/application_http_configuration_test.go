package application

import (
    "testing"

    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
)

/* releasedHttpConfiguration is an http configuration of an application's own that implements the released contract and not ExtendedHttpConfiguration */
type releasedHttpConfiguration struct {
    configcontract.HttpConfiguration
}

func TestExtendedHttpConfigurationOf_AnswersTheDefaultsForAConfigurationWithoutTheExtendedDoor(t *testing.T) {
    extendedHttpConfiguration := extendedHttpConfigurationOf(&releasedHttpConfiguration{})

    if 0 != len(extendedHttpConfiguration.StaticExcludedPaths()) {
        t.Fatalf("expected no excluded path, got %v", extendedHttpConfiguration.StaticExcludedPaths())
    }

    if config.DefaultSessionTtl != extendedHttpConfiguration.SessionTtl() {
        t.Fatalf("expected the default session ttl, got %s", extendedHttpConfiguration.SessionTtl())
    }

    if config.DefaultSessionTombstoneRetention != extendedHttpConfiguration.SessionTombstoneRetention() {
        t.Fatalf("expected the default tombstone retention, got %s", extendedHttpConfiguration.SessionTombstoneRetention())
    }

    if config.DefaultHttpShutdownTimeout != extendedHttpConfiguration.ShutdownTimeout() {
        t.Fatalf("expected the default shutdown wait, got %s", extendedHttpConfiguration.ShutdownTimeout())
    }
}

func TestExtendedHttpConfigurationOf_AnswersTheConfigurationThatImplementsIt(t *testing.T) {
    environment, environmentErr := config.NewEnvironment(&mapEnvironmentSource{values: map[string]string{
        config.HttpShutdownTimeoutKey: "7s",
        config.HttpSessionTtlKey:      "1h",
    }})
    if nil != environmentErr {
        t.Fatalf("new environment error: %v", environmentErr)
    }

    configuration, configurationErr := config.NewConfiguration(environment, t.TempDir())
    if nil != configurationErr {
        t.Fatalf("new configuration error: %v", configurationErr)
    }

    extendedHttpConfiguration := extendedHttpConfigurationOf(configuration.Http())

    if "7s" != extendedHttpConfiguration.ShutdownTimeout().String() || "1h0m0s" != extendedHttpConfiguration.SessionTtl().String() {
        t.Fatalf("expected the configured values to pass through, got %s and %s", extendedHttpConfiguration.ShutdownTimeout(), extendedHttpConfiguration.SessionTtl())
    }
}
