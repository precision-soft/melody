package rueidis

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestResolveTimeout_AnAbsentFieldTakesItsDefault(t *testing.T) {
    resolved, resolveErr := resolveTimeout(TimeoutConfig{})
    if nil != resolveErr {
        t.Fatalf("expected the zero timeout config to resolve, got %v", resolveErr)
    }

    if 3*time.Second != resolved.connectTimeout || 3*time.Second != resolved.commandTimeout {
        t.Fatalf("expected the default 3s / 3s, got %+v", resolved)
    }
}

func TestResolveTimeout_KeepsTheConfiguredValues(t *testing.T) {
    resolved, resolveErr := resolveTimeout(TimeoutConfig{ConnectTimeout: bound.Of(time.Millisecond), CommandTimeout: bound.Of(7 * time.Second)})
    if nil != resolveErr {
        t.Fatalf("expected the timeout config to resolve, got %v", resolveErr)
    }

    if time.Millisecond != resolved.connectTimeout || 7*time.Second != resolved.commandTimeout {
        t.Fatalf("expected 1ms / 7s, got %+v", resolved)
    }
}

func TestResolveTimeout_RefusesAFieldOutsideItsRuleNamingIt(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config TimeoutConfig
    }{
        {field: "TimeoutConfig.ConnectTimeout", config: TimeoutConfig{ConnectTimeout: bound.Of(time.Microsecond)}},
        {field: "TimeoutConfig.ConnectTimeout", config: TimeoutConfig{ConnectTimeout: bound.UnlimitedDuration()}},
        {field: "TimeoutConfig.CommandTimeout", config: TimeoutConfig{CommandTimeout: bound.Of(0 * time.Second)}},
        {field: "TimeoutConfig.CommandTimeout", config: TimeoutConfig{CommandTimeout: bound.UnlimitedDuration()}},
    } {
        _, resolveErr := resolveTimeout(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
    }
}
