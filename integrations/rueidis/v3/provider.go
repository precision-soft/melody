package rueidis

import (
    "context"
    "net"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/redis/rueidis"
)

func NewProvider(
    options ...ProviderOption,
) *Provider {
    provider := &Provider{
        clientConfig:  nil,
        timeoutConfig: nil,
        retryConfig:   nil,
        logger:        nil,
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

func WithLogger(logger loggingcontract.Logger) ProviderOption {
    return func(p *Provider) {
        p.logger = logger
    }
}

/* Provider is handed values, not configuration keys, so arming credential redaction is the application's call */
type Provider struct {
    clientConfig  *ClientConfig
    timeoutConfig *TimeoutConfig
    retryConfig   *RetryConfig
    logger        loggingcontract.Logger
}

func (instance *Provider) resolvedLogger() loggingcontract.Logger {
    if true == isAbsentLogger(instance.logger) {
        return logging.EmergencyLogger()
    }

    return instance.logger
}

func (instance *Provider) Open(params ConnectionParameters) (rueidis.Client, error) {
    if nil == instance.retryConfig {
        return instance.open(params)
    }

    return instance.openWithRetry(params)
}

func (instance *Provider) openWithRetry(params ConnectionParameters) (rueidis.Client, error) {
    logger := instance.resolvedLogger()

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

const transientErrorLinkLimit = 64

func (instance *Provider) isTransientError(inputErr error) bool {
    if nil == inputErr {
        return false
    }

    var dnsErr *net.DNSError
    if true == exception.ChainAs(inputErr, &dnsErr) {
        return true
    }

    var netErr net.Error
    if true == exception.ChainAs(inputErr, &netErr) {
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
        /* the same aborted-connection error under the two spellings its platforms give it */
        "software caused connection abort",
        "established connection was aborted",
        "network is unreachable",
        "host is down",
        "broken pipe",
        "connection reset",
        "eof",
        "loading",
    }

    /* an open failure can come from the application and its chain may reach itself; each pointer link is read once */
    for _, link := range exception.BuildCauseChain(inputErr, transientErrorLinkLimit) {
        message := strings.ToLower(link)

        for _, marker := range transientMarkers {
            if true == strings.Contains(message, marker) {
                return true
            }
        }
    }

    return false
}

func (instance *Provider) computeBackoffDelay(attempt uint32) time.Duration {
    defaults := DefaultRetryConfig()

    initialDelay := instance.retryConfig.InitialDelay
    if 0 >= initialDelay {
        initialDelay = defaults.InitialDelay
    }

    maxDelay := instance.retryConfig.MaxDelay
    if 0 >= maxDelay {
        maxDelay = defaults.MaxDelay
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN multiplier through */
    backoffMultiplier := instance.retryConfig.BackoffMultiplier
    if false == (backoffMultiplier >= 1) {
        backoffMultiplier = defaults.BackoffMultiplier
    }

    /* the delay grows in float space and is capped before converting: a large attempt count overflows to negative */
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

func (instance *Provider) open(params ConnectionParameters) (rueidis.Client, error) {
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
            instance.connectionContext(params, clientConfig, timeoutConfig),
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
            instance.connectionContext(params, clientConfig, timeoutConfig),
            createErr,
        )
    }

    if false == clientConfig.PingOnStart {
        return client, nil
    }

    pingContext, pingCancel := context.WithTimeout(context.Background(), resolveConnectTimeout(timeoutConfig))
    defer pingCancel()

    pingErr := client.Do(pingContext, client.B().Ping().Build()).Error()
    if nil == pingErr {
        return client, nil
    }

    client.Close()

    return nil, exception.NewError(
        "redis connection failed",
        instance.connectionContext(params, clientConfig, timeoutConfig),
        pingErr,
    )
}

/* connectionContext never carries the password */
func (instance *Provider) connectionContext(
    params ConnectionParameters,
    clientConfig *ClientConfig,
    timeoutConfig *TimeoutConfig,
) exceptioncontract.Context {
    connectionContext := params.SafeContext()

    connectionContext["connectTimeout"] = resolveConnectTimeout(timeoutConfig).String()

    if nil != clientConfig {
        connectionContext["dialTimeout"] = resolveDialTimeoutDescription(clientConfig)
        connectionContext["selectDb"] = clientConfig.SelectDb
    }

    return connectionContext
}

/* libraryDefaultDialTimeout is rueidis's own, applied whenever this provider installs no dialer of its own */
const libraryDefaultDialTimeout = 5 * time.Second

func resolveDialTimeoutDescription(clientConfig *ClientConfig) string {
    if 0 < clientConfig.DialTimeout {
        return clientConfig.DialTimeout.String()
    }

    return libraryDefaultDialTimeout.String() + " (library default)"
}

/* resolveConnectTimeout keeps the bound: an unbounded ping against a store that never answers would hang boot */
func resolveConnectTimeout(timeoutConfig *TimeoutConfig) time.Duration {
    if nil == timeoutConfig || 0 >= timeoutConfig.ConnectTimeout {
        return DefaultTimeoutConfig().ConnectTimeout
    }

    return timeoutConfig.ConnectTimeout
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
