package config

import (
    "testing"
    "time"
)

func TestConfigurationHttpSessionTtlDefaultsToNoExpiry(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if 0 != configuration.Http().SessionTtl() {
        t.Fatalf("expected the default session ttl to stay unbounded, got %v", configuration.Http().SessionTtl())
    }
}

func TestConfigurationHttpSessionTtlIsReadFromTheEnvironment(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            HttpSessionTtlKey: "30m",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if 30*time.Minute != configuration.Http().SessionTtl() {
        t.Fatalf("expected the configured session ttl, got %v", configuration.Http().SessionTtl())
    }
}

func TestConfigurationHttpSessionTtlRejectsANegativeValue(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            HttpSessionTtlKey: "-1m",
        },
    }

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    _, err = NewConfiguration(environment, "/tmp/melody")
    if nil == err {
        t.Fatalf("expected a negative session ttl to fail the boot")
    }
}
