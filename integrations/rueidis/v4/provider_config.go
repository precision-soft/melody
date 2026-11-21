package rueidis

import (
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* ProviderConfig declares how the provider opens a client; its zero value is the default, a pinged client with no client-side cache, the default ping deadlines and the default retry. */
type ProviderConfig struct {
    Client ClientConfig

    Timeout TimeoutConfig

    Retry RetryConfig

    /* Logger is the journal the connection retry reports in: a warning per re-dial, an error when the attempts run out and an info record when a later attempt succeeds. The provider is built before the container resolves a logger, so it cannot take the application's on its own; nil or a typed nil reaches the emergency logger, standard error, outside the application's journal, rather than nothing. */
    Logger loggingcontract.Logger
}

type resolvedProvider struct {
    client  resolvedClient
    timeout resolvedTimeout
    retry   resolvedRetry
}

func resolveProviderConfig(config ProviderConfig) (resolvedProvider, error) {
    client, clientErr := resolveClient(config.Client)
    if nil != clientErr {
        return resolvedProvider{}, clientErr
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
        client:  client,
        timeout: timeout,
        retry:   retry,
    }, nil
}
