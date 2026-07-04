package rueidis

import (
    "context"
    "errors"
    "net"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    "github.com/redis/rueidis"
)

func NewProvider(
    options ...ProviderOption,
) *Provider {
    provider := &Provider{
        clientConfig:  nil,
        timeoutConfig: nil,
        retryConfig:   nil,
    }
    for _, option := range options {
        option(provider)
    }
    return provider
}

type ProviderOption func(*Provider)

func WithClientConfig(clientConfig *ClientConfig) ProviderOption {
    return func(p *Provider) {
        p.clientConfig = clientConfig
    }
}

func WithTimeoutConfig(timeoutConfig *TimeoutConfig) ProviderOption {
    return func(p *Provider) {
        p.timeoutConfig = timeoutConfig
    }
}

func WithRetryConfig(retryConfig *RetryConfig) ProviderOption {
    return func(p *Provider) {
        p.retryConfig = retryConfig
    }
}

type Provider struct {
    clientConfig  *ClientConfig
    timeoutConfig *TimeoutConfig
    retryConfig   *RetryConfig
}

func (instance *Provider) Open(params ConnectionParams) (rueidis.Client, error) {
    if nil == instance.retryConfig {
        return instance.open(params)
    }

    return instance.openWithRetry(params)
}

func (instance *Provider) openWithRetry(params ConnectionParams) (rueidis.Client, error) {
    logger := logging.EnsureLogger(nil)

    attempt := uint32(0)
    maxAttempts := instance.retryConfig.MaxAttempts
    if 0 == maxAttempts {
        maxAttempts = DefaultRetryConfig().MaxAttempts
    }

    for {
        attempt = attempt + 1

        client, openErr := instance.open(params)
        if nil == openErr {
            if 1 < attempt {
                logger.Info(
                    "redis connection successful after retry",
                    map[string]interface{}{
                        "attempt": attempt,
                    },
                )
            }

            return client, nil
        }

        if false == instance.isTransientError(openErr) {
            return nil, openErr
        }

        if attempt >= maxAttempts {
            logger.Error(
                "redis connection failed after max retry attempts",
                map[string]interface{}{
                    "attempt":     attempt,
                    "maxAttempts": maxAttempts,
                    "error":       openErr.Error(),
                },
            )

            return nil, openErr
        }

        delay := instance.computeBackoffDelay(attempt)

        logger.Warning(
            "redis connection failed and retrying",
            map[string]interface{}{
                "attempt":     attempt,
                "maxAttempts": maxAttempts,
                "retryIn":     delay.String(),
                "error":       openErr.Error(),
            },
        )

        time.Sleep(delay)
    }
}

func (instance *Provider) isTransientError(inputErr error) bool {
    if nil == inputErr {
        return false
    }

    var dnsErr *net.DNSError
    if true == errors.As(inputErr, &dnsErr) {
        return true
    }

    var netErr net.Error
    if true == errors.As(inputErr, &netErr) {
        if true == netErr.Timeout() {
            return true
        }
    }

    transientMarkers := []string{
        "connection refused",
        "i/o timeout",
        "timeout",
        "no such host",
        "server closed the connection",
        "connection closed",
        "use of closed network connection",
        "network is unreachable",
        "host is down",
        "broken pipe",
        "connection reset",
        "eof",
    }

    currentErr := inputErr
    for nil != currentErr {
        message := strings.ToLower(currentErr.Error())

        for _, marker := range transientMarkers {
            if true == strings.Contains(message, marker) {
                return true
            }
        }

        currentErr = errors.Unwrap(currentErr)
    }

    return false
}

func (instance *Provider) computeBackoffDelay(attempt uint32) time.Duration {
    defaults := DefaultRetryConfig()

    initialDelay := instance.retryConfig.InitialDelay
    if 0 == initialDelay {
        initialDelay = defaults.InitialDelay
    }

    maxDelay := instance.retryConfig.MaxDelay
    if 0 == maxDelay {
        maxDelay = defaults.MaxDelay
    }

    backoffMultiplier := instance.retryConfig.BackoffMultiplier
    if 0.0 == backoffMultiplier {
        backoffMultiplier = defaults.BackoffMultiplier
    }

    /* grow the delay in float space and cap at maxDelay as soon as it is reached, before converting to
       time.Duration — otherwise a large attempt count overflows the float64->int64 conversion to a negative
       duration, which slips past the `> maxDelay` cap and collapses the backoff to zero (a re-dial storm). */
    maxDelayFloat := float64(maxDelay)
    delay := float64(initialDelay)

    for i := uint32(1); i < attempt; i = i + 1 {
        delay = delay * backoffMultiplier
        if delay >= maxDelayFloat {
            return maxDelay
        }
    }

    if delay >= maxDelayFloat {
        return maxDelay
    }

    return time.Duration(delay)
}

func (instance *Provider) open(params ConnectionParams) (rueidis.Client, error) {
    clientConfig := instance.clientConfig
    if nil == clientConfig {
        clientConfig = DefaultClientConfig()
    }

    timeoutConfig := instance.timeoutConfig
    if nil == timeoutConfig {
        timeoutConfig = DefaultTimeoutConfig()
    }

    addresses := parseAddressList(params.Address)
    if 0 == len(addresses) {
        return nil, exception.NewError(
            "redis address is empty",
            params.SafeContext(),
            nil,
        )
    }

    option := rueidis.ClientOption{
        InitAddress:  addresses,
        Username:     params.User,
        Password:     params.Password,
        ClientName:   clientConfig.ClientName,
        SelectDB:     clientConfig.SelectDb,
        DisableCache: clientConfig.DisableCache,
        TLSConfig:    clientConfig.TlsConfig,
    }

    if 0 < clientConfig.DialTimeout {
        option.Dialer = net.Dialer{
            Timeout: clientConfig.DialTimeout,
        }
    }

    if 0 < clientConfig.ConnWriteTimeout {
        option.ConnWriteTimeout = clientConfig.ConnWriteTimeout
    }

    client, createErr := rueidis.NewClient(option)
    if nil != createErr {
        return nil, exception.NewError(
            "redis client creation failed",
            params.SafeContext(),
            createErr,
        )
    }

    if false == clientConfig.PingOnStart {
        return client, nil
    }

    pingContext := context.Background()
    pingCancel := func() {}
    if 0 < timeoutConfig.ConnectTimeout {
        pingContext, pingCancel = context.WithTimeout(context.Background(), timeoutConfig.ConnectTimeout)
    }
    defer pingCancel()

    pingErr := client.Do(pingContext, client.B().Ping().Build()).Error()
    if nil == pingErr {
        return client, nil
    }

    client.Close()

    return nil, exception.NewError(
        "redis connection failed",
        params.SafeContext(),
        pingErr,
    )
}

func (instance *Provider) Close(client rueidis.Client) error {
    if nil == client {
        return nil
    }

    client.Close()
    return nil
}

func (instance *Provider) Ping(client rueidis.Client) error {
    if nil == client {
        return exception.NewError(
            "redis client is nil",
            nil,
            nil,
        )
    }

    commandTimeout := 3 * time.Second
    if nil != instance.timeoutConfig && 0 < instance.timeoutConfig.CommandTimeout {
        commandTimeout = instance.timeoutConfig.CommandTimeout
    }

    ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
    defer cancel()

    return client.Do(ctx, client.B().Ping().Build()).Error()
}
