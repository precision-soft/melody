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

func TestRegisterRuntimeAddsValue(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    configuration.RegisterRuntime("runtime.test", "x")

    if "x" != configuration.MustGet("runtime.test").String() {
        t.Fatalf("expected runtime value to be visible")
    }
}

func TestKernelGettersMatchParameterValues(t *testing.T) {
    source := &testEnvironmentSource{values: map[string]string{}}

    environment, err := NewEnvironment(source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if configuration.Kernel().LogsDir() != configuration.MustGet(KernelLogsDir).String() {
        t.Fatalf("expected LogsDir getter to match parameter value")
    }

    if configuration.Kernel().CacheDir() != configuration.MustGet(KernelCacheDir).String() {
        t.Fatalf("expected CacheDir getter to match parameter value")
    }
}
