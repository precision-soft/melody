package pgsql

type ProviderOption func(provider *Provider)

func WithPoolConfig(poolConfig *PoolConfig) ProviderOption {
    return func(provider *Provider) {
        provider.poolConfig = poolConfig
    }
}

func WithTimeoutConfig(timeoutConfig *TimeoutConfig) ProviderOption {
    return func(provider *Provider) {
        provider.timeoutConfig = timeoutConfig
    }
}

func WithRetryConfig(retryConfig *RetryConfig) ProviderOption {
    return func(provider *Provider) {
        provider.retryConfig = retryConfig
    }
}

func WithPostBuildHook(hook PostBuildHook) ProviderOption {
    return func(provider *Provider) {
        provider.postBuildHook = hook
    }
}
