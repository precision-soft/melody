package pgsql

import "crypto/tls"

type ProviderOption func(provider *Provider)

func WithPostBuildHook(hook PostBuildHook) ProviderOption {
    return func(provider *Provider) {
        provider.postBuildHook = hook
    }
}

func WithInsecure(insecure bool) ProviderOption {
    return func(provider *Provider) {
        provider.insecure = insecure
    }
}

func WithTlsConfig(tlsConfig *tls.Config) ProviderOption {
    return func(provider *Provider) {
        provider.tlsConfig = tlsConfig
    }
}
