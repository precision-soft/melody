package pgsql

import (
    "crypto/tls"
)

/* ProviderConfig declares how the provider opens a database; its zero value is the default, a verifying TLS session over the default pool, deadlines and retry. */
type ProviderConfig struct {
    Pool PoolConfig

    Timeout TimeoutConfig

    Retry RetryConfig

    /* PostBuildHook reaches the driver configuration the typed fields do not, under the connect timeout; nil runs none. */
    PostBuildHook PostBuildHook

    /* Insecure dials a plaintext connection where the default dials a verifying TLS session: the deliberate opt-out for a database reached over a trusted network or one that speaks no TLS. */
    Insecure bool

    /* TlsConfig is an explicit TLS session, to pin a server certificate or present a client one; it wins over Insecure. */
    TlsConfig *tls.Config
}

type resolvedProvider struct {
    pool    resolvedPool
    timeout resolvedTimeout
    retry   resolvedRetry
}

func resolveProviderConfig(config ProviderConfig) (resolvedProvider, error) {
    pool, poolErr := resolvePool(config.Pool)
    if nil != poolErr {
        return resolvedProvider{}, poolErr
    }

    timeout, timeoutErr := resolveTimeout(config.Timeout)
    if nil != timeoutErr {
        return resolvedProvider{}, timeoutErr
    }

    retry, retryErr := resolveRetry(config.Retry)
    if nil != retryErr {
        return resolvedProvider{}, retryErr
    }

    return resolvedProvider{
        pool:    pool,
        timeout: timeout,
        retry:   retry,
    }, nil
}
