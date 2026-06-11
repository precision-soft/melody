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
