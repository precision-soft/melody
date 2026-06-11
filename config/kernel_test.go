package config

import (
    "testing"
)

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
