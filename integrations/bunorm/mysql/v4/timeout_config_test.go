package mysql

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestResolveTimeout_AnAbsentFieldTakesItsDefault(t *testing.T) {
    resolved, resolveErr := resolveTimeout(TimeoutConfig{})
    if nil != resolveErr {
        t.Fatalf("expected the zero timeouts to resolve, got %v", resolveErr)
    }

    if 10*time.Second != resolved.connectTimeout || 30*time.Second != driverDuration(resolved.readTimeout) || 30*time.Second != driverDuration(resolved.writeTimeout) {
        t.Fatalf("expected the default deadlines 10s / 30s / 30s, got %+v", resolved)
    }
}

func TestResolveTimeout_KeepsTheConfiguredValuesAndLiftsAnUnlimitedReadOrWrite(t *testing.T) {
    resolved, resolveErr := resolveTimeout(TimeoutConfig{
        ConnectTimeout: bound.Of(7 * time.Second),
        ReadTimeout:    bound.UnlimitedDuration(),
        WriteTimeout:   bound.Of(13 * time.Second),
    })
    if nil != resolveErr {
        t.Fatalf("expected the timeouts to resolve, got %v", resolveErr)
    }

    if 7*time.Second != resolved.connectTimeout || 0 != driverDuration(resolved.readTimeout) || 13*time.Second != driverDuration(resolved.writeTimeout) {
        t.Fatalf("expected the configured deadlines with the read one lifted, got %+v", resolved)
    }
}

func TestResolveTimeout_RefusesAnUnlimitedConnectTimeoutAndAFieldBelowItsMinimum(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config TimeoutConfig
    }{
        {field: "TimeoutConfig.ConnectTimeout", config: TimeoutConfig{ConnectTimeout: bound.UnlimitedDuration()}},
        {field: "TimeoutConfig.ConnectTimeout", config: TimeoutConfig{ConnectTimeout: bound.Of(time.Duration(0))}},
        {field: "TimeoutConfig.ReadTimeout", config: TimeoutConfig{ReadTimeout: bound.Of(time.Microsecond)}},
        {field: "TimeoutConfig.WriteTimeout", config: TimeoutConfig{WriteTimeout: bound.Of(-time.Second)}},
    } {
        _, resolveErr := resolveTimeout(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
    }
}

func TestResolvedTimeout_LiftedKeepsTheConnectTimeoutAndLiftsTheDeadlines(t *testing.T) {
    lifted := resolvedTimeout{
        connectTimeout: 7 * time.Second,
        readTimeout:    bound.Resolved[time.Duration]{Value: 30 * time.Second},
        writeTimeout:   bound.Resolved[time.Duration]{Value: 30 * time.Second},
    }.lifted()

    if 7*time.Second != lifted.connectTimeout {
        t.Fatalf("expected the connect timeout kept, got %v", lifted.connectTimeout)
    }

    if 0 != driverDuration(lifted.readTimeout) || 0 != driverDuration(lifted.writeTimeout) {
        t.Fatalf("expected the read and write deadlines lifted, got %+v", lifted)
    }
}
