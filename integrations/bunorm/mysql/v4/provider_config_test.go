package mysql

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestResolveProviderConfig_AnswersTheRefusalOfEverySection(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config ProviderConfig
    }{
        {field: "PoolConfig.MaxOpenConnections", config: ProviderConfig{Pool: PoolConfig{MaxOpenConnections: bound.Of(0)}}},
        {field: "TimeoutConfig.ConnectTimeout", config: ProviderConfig{Timeout: TimeoutConfig{ConnectTimeout: bound.UnlimitedDuration()}}},
        {field: "RetryConfig.MaxDelay", config: ProviderConfig{Retry: RetryConfig{MaxDelay: bound.Of(time.Nanosecond)}}},
    } {
        _, resolveErr := resolveProviderConfig(testCase.config)
        expectErrorNamingField(t, resolveErr, testCase.field)
    }
}
