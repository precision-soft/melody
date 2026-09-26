package config

import (
    "errors"
    "testing"
)

func TestNewEnvironmentPropagatesSourceError(t *testing.T) {
    source := &testEnvironmentSource{
        values: nil,
        err:    errors.New("load failed"),
    }

    _, err := NewEnvironment(source)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestEmptyStringIsPresentValue(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            LogPathKey: "",
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

    if "" != configuration.MustGet(KernelLogPath).String() {
        t.Fatalf("expected empty string to be present value")
    }
}
