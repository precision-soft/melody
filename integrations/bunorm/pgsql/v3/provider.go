package pgsql

import (
    "context"
    "crypto/tls"
    "database/sql"
    "errors"
    "fmt"
    "net"
    "strings"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

func NewProvider(
    providerOptions ...ProviderOption,
) *Provider {
    provider := &Provider{
        poolConfig:    nil,
        timeoutConfig: nil,
        retryConfig:   nil,
        postBuildHook: nil,
        insecure:      false,
        tlsConfig:     nil,
    }
    for _, providerOption := range providerOptions {
        providerOption(provider)
    }
    return provider
}

type Provider struct {
    poolConfig    *PoolConfig
    timeoutConfig *TimeoutConfig
    retryConfig   *RetryConfig
    postBuildHook PostBuildHook
    insecure      bool
    tlsConfig     *tls.Config
}

func (instance *Provider) Open(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
    if nil == instance.retryConfig {
        return instance.open(params)
    }

    return instance.openWithRetry(params, logger)
}

func (instance *Provider) openWithRetry(params bunorm.ConnectionParams, logger loggingcontract.Logger) (*bun.DB, error) {
    logger = logging.EnsureLogger(logger)

    attempt := uint32(0)
    maxAttempts := instance.retryConfig.MaxAttempts
    if 0 == maxAttempts {
        maxAttempts = DefaultRetryConfig().MaxAttempts
    }

    for {
        attempt = attempt + 1

        database, openErr := instance.open(params)
        if nil == openErr {
            if 1 < attempt {
                logger.Info(
                    "database connection successful after retry",
                    map[string]interface{}{
                        "attempt": attempt,
                    },
                )
            }

            return database, nil
        }

        if false == instance.isTransientError(openErr) {
            logger.Error(
                "database connection failed with non-transient error",
                map[string]interface{}{
                    "attempt": attempt,
                    "error":   openErr.Error(),
                },
            )

            return nil, openErr
        }

        if attempt >= maxAttempts {
            logger.Error(
                "database connection failed after max retry attempts",
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
            "database connection failed and retrying",
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

func (instance *Provider) open(params bunorm.ConnectionParams) (*bun.DB, error) {
    poolConfig := instance.poolConfig
    if nil == poolConfig {
        poolConfig = DefaultPoolConfig()
    }

    timeoutConfig := instance.timeoutConfig
    if nil == timeoutConfig {
        timeoutConfig = DefaultTimeoutConfig()
    }

    address := fmt.Sprintf("%s:%s", params.Host, params.Port)

    connectorOptions := []pgdriver.Option{
        pgdriver.WithAddr(address),
        pgdriver.WithDatabase(params.Database),
        pgdriver.WithUser(params.User),
        pgdriver.WithPassword(params.Password),
    }

    if nil != instance.tlsConfig {
        connectorOptions = append(connectorOptions, pgdriver.WithTLSConfig(instance.tlsConfig))
    } else if true == instance.insecure {
        /* pgdriver.WithInsecure(true) disables TLS entirely */
        connectorOptions = append(connectorOptions, pgdriver.WithInsecure(true))
    } else {
        /* @important do NOT hand this case to pgdriver.WithInsecure(false): despite the name, pgdriver implements it as tls.Config{InsecureSkipVerify: true} — TLS is negotiated but the server certificate is never checked, so the default connection is trivially machine-in-the-middled. Build a verifying config instead: the system roots, and the configured host as the name to verify against. Callers that genuinely want an unverified session pass WithTlsConfig or WithInsecure(true) explicitly. */
        connectorOptions = append(connectorOptions, pgdriver.WithTLSConfig(&tls.Config{
            ServerName: params.Host,
            MinVersion: tls.VersionTLS12,
        }))
    }

    connector := pgdriver.NewConnector(connectorOptions...)

    if nil != instance.postBuildHook {
        hookContext := context.Background()
        hookCancel := func() {}
        if 0 < timeoutConfig.ConnectTimeout {
            hookContext, hookCancel = context.WithTimeout(context.Background(), timeoutConfig.ConnectTimeout)
        }
        defer hookCancel()

        hookErr := instance.postBuildHook(hookContext, connector)
        if nil != hookErr {
            return nil, exception.NewError(
                "pgsql database connector configuration failed",
                params.SafeContext(),
                hookErr,
            )
        }
    }

    sqlDatabase := sql.OpenDB(connector)

    sqlDatabase.SetMaxOpenConns(poolConfig.MaxOpenConnections)
    sqlDatabase.SetMaxIdleConns(poolConfig.MaxIdleConnections)
    sqlDatabase.SetConnMaxLifetime(poolConfig.ConnectionMaxLifetime)
    sqlDatabase.SetConnMaxIdleTime(poolConfig.ConnectionMaxIdleTime)

    database := bun.NewDB(
        sqlDatabase,
        dialectWithDefaultSchema{
            Dialect: pgdialect.New(),
        },
    )

    pingContext := context.Background()
    pingCancel := func() {}
    if 0 < timeoutConfig.ConnectTimeout {
        pingContext, pingCancel = context.WithTimeout(context.Background(), timeoutConfig.ConnectTimeout)
    }
    defer pingCancel()

    pingErr := database.PingContext(pingContext)
    if nil != pingErr {
        _ = database.Close()

        return nil, exception.NewError(
            "database connection failed",
            params.SafeContext(),
            pingErr,
        )
    }

    return database, nil
}

func (instance *Provider) computeBackoffDelay(attempt uint32) time.Duration {
    defaults := DefaultRetryConfig()

    /* non-positive delays and a multiplier below 1 fall back to the defaults: a negative delay makes
       time.Sleep return immediately and a sub-1 multiplier decays the delay toward zero, both collapsing
       the backoff into a re-dial storm; a multiplier of exactly 1 stays a valid constant backoff. */
    initialDelay := instance.retryConfig.InitialDelay
    if 0 >= initialDelay {
        initialDelay = defaults.InitialDelay
    }

    maxDelay := instance.retryConfig.MaxDelay
    if 0 >= maxDelay {
        maxDelay = defaults.MaxDelay
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN
       multiplier through, poison the float-space growth below and collapse the backoff into an immediate
       re-dial storm once the NaN converts to a negative duration. */
    backoffMultiplier := instance.retryConfig.BackoffMultiplier
    if false == (backoffMultiplier >= 1) {
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
        "temporary failure",
        "no such host",
        "server closed the connection",
        "connection closed",
        "use of closed network connection",
        /* the same aborted-connection error under the two spellings its platforms give it */
        "software caused connection abort",
        "established connection was aborted",
        "bad connection",
        "too many connections",
        "network is unreachable",
        "host is down",
        "broken pipe",
        "connection reset",
        "eof",
        "the database system is",
    }

    currentErr := inputErr
    for nil != currentErr {
        message := strings.ToLower(currentErr.Error())

        for _, marker := range transientMarkers {
            if "" == marker {
                continue
            }

            if true == strings.Contains(message, marker) {
                return true
            }
        }

        currentErr = errors.Unwrap(currentErr)
    }

    return false
}

var _ bunorm.Provider = (*Provider)(nil)
