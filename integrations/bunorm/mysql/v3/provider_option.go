package mysql

import "crypto/tls"

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

/* WithInsecure disables TLS entirely when set, leaving a plaintext connection. It is the deliberate opt-out from the secure default the provider dials with — a verifying TLS session — for a database reached over a trusted network or one that speaks no TLS, spelled the same as its pgsql sibling. */
func WithInsecure(insecure bool) ProviderOption {
    return func(provider *Provider) {
        provider.insecure = insecure
    }
}

/* WithTlsConfig hands the connector an explicit TLS configuration, taking precedence over both the secure default and WithInsecure. A caller uses it to pin a server certificate, present a client certificate, or otherwise shape the session beyond the system-roots verification the default performs. */
func WithTlsConfig(tlsConfig *tls.Config) ProviderOption {
    return func(provider *Provider) {
        provider.tlsConfig = tlsConfig
    }
}
