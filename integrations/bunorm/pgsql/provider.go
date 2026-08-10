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

    "github.com/precision-soft/melody/config"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/integrations/bunorm"
    "github.com/precision-soft/melody/logging"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

func NewProvider(
    hostParameterName string,
    portParameterName string,
    databaseParameterName string,
    userParameterName string,
    passwordParameterName string,
    providerOptions ...ProviderOption,
) *Provider {
    provider := &Provider{
        hostParameterName:     hostParameterName,
        portParameterName:     portParameterName,
        databaseParameterName: databaseParameterName,
        userParameterName:     userParameterName,
        passwordParameterName: passwordParameterName,
        poolConfig:            nil,
        timeoutConfig:         nil,
        retryConfig:           nil,
        postBuildHook:         nil,
        insecure:              false,
        tlsConfig:             nil,
    }
    for _, providerOption := range providerOptions {
        providerOption(provider)
    }
    return provider
}

func NewProviderWithConfig(
    hostParameterName string,
    portParameterName string,
    databaseParameterName string,
    userParameterName string,
    passwordParameterName string,
    poolConfig *PoolConfig,
    timeoutConfig *TimeoutConfig,
    retryConfig *RetryConfig,
    providerOptions ...ProviderOption,
) *Provider {
    provider := &Provider{
        hostParameterName:     hostParameterName,
        portParameterName:     portParameterName,
        databaseParameterName: databaseParameterName,
        userParameterName:     userParameterName,
        passwordParameterName: passwordParameterName,
        poolConfig:            poolConfig,
        timeoutConfig:         timeoutConfig,
        retryConfig:           retryConfig,
        postBuildHook:         nil,
        insecure:              false,
        tlsConfig:             nil,
    }
    for _, providerOption := range providerOptions {
        providerOption(provider)
    }
    return provider
}

type Provider struct {
    hostParameterName     string
    portParameterName     string
    databaseParameterName string
    userParameterName     string
    passwordParameterName string

    poolConfig    *PoolConfig
    timeoutConfig *TimeoutConfig
    retryConfig   *RetryConfig
    postBuildHook PostBuildHook
    insecure      bool
    tlsConfig     *tls.Config

    /* tunedForMigration marks the derived provider OpenForMigration dials with: its deliberate zero read and write deadlines mean "lifted", and the normalization that protects every other caller from an unset environment key must not re-arm them. */
    tunedForMigration bool
}

func (instance *Provider) WithPoolConfig(poolConfig *PoolConfig) *Provider {
    instance.poolConfig = poolConfig

    return instance
}

func (instance *Provider) WithTimeoutConfig(timeoutConfig *TimeoutConfig) *Provider {
    instance.timeoutConfig = timeoutConfig

    return instance
}

func (instance *Provider) WithRetryConfig(retryConfig *RetryConfig) *Provider {
    instance.retryConfig = retryConfig

    return instance
}

func (instance *Provider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.OpenContext(context.Background(), resolver)
}

/* OpenContext opens under the caller's context: an already-cancelled context is refused before the attempt, the retry sleeps watch it alongside the clock, and the configuration hook and the boot ping derive their budgets from it. The one step outside its reach is the dialect handshake bun performs at construction, which queries the server under no caller context and is bounded by the connect timeout alone — so a cancellation arriving mid-attempt is honoured at the next cancellable step rather than instantly. A nil context reads as context.Background(), which is exactly Open. */
func (instance *Provider) OpenContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    if nil == ctx {
        ctx = context.Background()
    }

    if nil == instance.retryConfig {
        return instance.open(ctx, resolver)
    }

    return instance.openWithRetry(ctx, resolver)
}

/* OpenForMigration opens the same database with the driver deadlines lifted: ReadTimeout and WriteTimeout are per-operation socket deadlines baked into the connector, sized for request traffic, and a DDL statement that legitimately runs past them — an ALTER TABLE adding constraints on a large table, a long CREATE INDEX — is cut mid-statement with an i/o timeout. The connect timeout stays armed (a down database must still fail fast), the pool is kept to the two connections a sequential migration run needs, and no connection is recycled mid-run — a lifetime rotation under a running statement is the same cut by another name. */
func (instance *Provider) OpenForMigration(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.migrationProvider().Open(resolver)
}

/* migrationProvider derives the provider OpenForMigration dials with: the same parameters, hook and retry policy, over the migration pool and the lifted deadlines. */
func (instance *Provider) migrationProvider() *Provider {
    return &Provider{
        hostParameterName:     instance.hostParameterName,
        portParameterName:     instance.portParameterName,
        databaseParameterName: instance.databaseParameterName,
        userParameterName:     instance.userParameterName,
        passwordParameterName: instance.passwordParameterName,
        poolConfig:            migrationPoolConfig(),
        timeoutConfig:         migrationTimeoutConfig(instance.timeoutConfig),
        retryConfig:           instance.retryConfig,
        postBuildHook:         instance.postBuildHook,
        insecure:              instance.insecure,
        tlsConfig:             instance.tlsConfig,
        tunedForMigration:     true,
    }
}

/* migrationTimeoutConfig lifts the read and write deadlines and keeps the connect timeout of the configuration it derives from. */
func migrationTimeoutConfig(baseConfig *TimeoutConfig) *TimeoutConfig {
    connectTimeout := DefaultTimeoutConfig().ConnectTimeout
    if nil != baseConfig {
        connectTimeout = baseConfig.ConnectTimeout
    }

    return &TimeoutConfig{
        ConnectTimeout: connectTimeout,
        ReadTimeout:    0,
        WriteTimeout:   0,
    }
}

func migrationPoolConfig() *PoolConfig {
    return &PoolConfig{
        MaxOpenConnections:    2,
        MaxIdleConnections:    1,
        ConnectionMaxLifetime: 0,
        ConnectionMaxIdleTime: 0,
    }
}

/* resolvedTimeoutConfig answers the configuration the connector is built from, with every non-positive field replaced by the constructor default. A zero reaches here far more often from an environment key nobody set than from a caller who means "no deadline", and the guards below read a non-positive connect timeout as no deadline at all — so the unset key would disarm the very protection the nil configuration arms, and a negative one would put the deadline in the past. */
func (instance *Provider) resolvedTimeoutConfig() *TimeoutConfig {
    defaultConfig := DefaultTimeoutConfig()

    if nil == instance.timeoutConfig {
        return defaultConfig
    }

    resolved := &TimeoutConfig{
        ConnectTimeout: instance.timeoutConfig.ConnectTimeout,
        ReadTimeout:    instance.timeoutConfig.ReadTimeout,
        WriteTimeout:   instance.timeoutConfig.WriteTimeout,
    }

    if 0 >= resolved.ConnectTimeout {
        resolved.ConnectTimeout = defaultConfig.ConnectTimeout
    }

    if true == instance.tunedForMigration {
        return resolved
    }

    if 0 >= resolved.ReadTimeout {
        resolved.ReadTimeout = defaultConfig.ReadTimeout
    }

    if 0 >= resolved.WriteTimeout {
        resolved.WriteTimeout = defaultConfig.WriteTimeout
    }

    return resolved
}

/* resolvedPoolConfig answers the pool sizing the database is built with, with every non-positive field replaced by the constructor default: on database/sql a zero maximum means an UNLIMITED pool and a zero lifetime means connections that are never recycled, so a configuration assembled from unset environment keys would remove the bounds the nil configuration installs. */
func (instance *Provider) resolvedPoolConfig() *PoolConfig {
    defaultConfig := DefaultPoolConfig()

    if nil == instance.poolConfig {
        return defaultConfig
    }

    resolved := &PoolConfig{
        MaxOpenConnections:    instance.poolConfig.MaxOpenConnections,
        MaxIdleConnections:    instance.poolConfig.MaxIdleConnections,
        ConnectionMaxLifetime: instance.poolConfig.ConnectionMaxLifetime,
        ConnectionMaxIdleTime: instance.poolConfig.ConnectionMaxIdleTime,
    }

    if 0 >= resolved.MaxOpenConnections {
        resolved.MaxOpenConnections = defaultConfig.MaxOpenConnections
    }

    if 0 >= resolved.MaxIdleConnections {
        resolved.MaxIdleConnections = defaultConfig.MaxIdleConnections
    }

    if true == instance.tunedForMigration {
        return resolved
    }

    if 0 >= resolved.ConnectionMaxLifetime {
        resolved.ConnectionMaxLifetime = defaultConfig.ConnectionMaxLifetime
    }

    if 0 >= resolved.ConnectionMaxIdleTime {
        resolved.ConnectionMaxIdleTime = defaultConfig.ConnectionMaxIdleTime
    }

    return resolved
}

func (instance *Provider) openWithRetry(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    logger, loggerErr := logging.LoggerFromResolver(resolver)
    if nil != loggerErr {
        logger = logging.EmergencyLogger()
    }

    attempt := uint32(0)
    maxAttempts := instance.retryConfig.MaxAttempts
    if 0 == maxAttempts {
        maxAttempts = 3
    }

    for {
        attempt = attempt + 1

        database, openErr := instance.open(ctx, resolver)
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
            /* the terminal record is the log of this failure: it is written in full and the returned error carries the mark, so the exit handler and the http exception path do not write the same outage a second time */
            terminalErr := exception.FromError(openErr)
            logger.Error(
                "database connection failed with non-transient error",
                exception.LogContext(
                    terminalErr,
                    map[string]any{"attempt": attempt},
                ),
            )

            return nil, exception.MarkLogged(terminalErr)
        }

        if attempt >= maxAttempts {
            terminalErr := exception.FromError(openErr)
            logger.Error(
                "database connection failed after max retry attempts",
                exception.LogContext(
                    terminalErr,
                    map[string]any{"attempt": attempt, "maxAttempts": maxAttempts},
                ),
            )

            return nil, exception.MarkLogged(terminalErr)
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

        /* the sleep watches the caller's context alongside the clock: a shutdown signal arriving mid-retry would otherwise sleep through the whole remaining budget, and the second signal exits with no teardown at all */
        delayTimer := time.NewTimer(delay)
        select {
        case <-ctx.Done():
            delayTimer.Stop()

            return nil, exception.NewError(
                "database connection retry cancelled by the caller's context",
                map[string]any{"attempt": attempt, "error": openErr.Error()},
                ctx.Err(),
            )
        case <-delayTimer.C:
        }
    }
}

func (instance *Provider) open(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    /* an already-cancelled context is refused before the attempt: the dialect handshake bun performs at construction queries the server outside any caller context, bounded by the connect timeout alone, so without this refusal a shutdown-cancelled lazy open still paid one full dial against a database that no longer matters. */
    if ctxErr := ctx.Err(); nil != ctxErr {
        return nil, exception.NewError(
            "database open cancelled before the attempt",
            nil,
            ctxErr,
        )
    }

    configuration := config.ConfigMustFromResolver(resolver)

    /* the provider is the component told authoritatively which parameter holds the credential, so it arms the framework's own redaction for it — the introspection output masks the password and every template derived from it, without the application repeating the knowledge */
    configuration.MarkSecret(instance.passwordParameterName)

    host := configuration.MustGet(instance.hostParameterName).MustString()
    port := configuration.MustGet(instance.portParameterName).MustString()
    databaseName := configuration.MustGet(instance.databaseParameterName).MustString()
    user := configuration.MustGet(instance.userParameterName).MustString()
    password := configuration.MustGet(instance.passwordParameterName).MustString()

    connectionConfig := NewConnectionConfig(host, port, databaseName, user, password)

    poolConfig := instance.resolvedPoolConfig()
    timeoutConfig := instance.resolvedTimeoutConfig()

    address := fmt.Sprintf("%s:%s", host, port)

    /* every deadline the driver applies is named here, none governs invisibly: without these three, pgdriver's own defaults — 5s dial, 10s per read, 5s per write — silently cap the configured connect timeout and cut every legitimately long query. A zero read or write deadline survives only on the migration derivation, where it deliberately means "lifted". */
    connectorOptions := []pgdriver.Option{
        pgdriver.WithAddr(address),
        pgdriver.WithDatabase(databaseName),
        pgdriver.WithUser(user),
        pgdriver.WithPassword(password),
        pgdriver.WithDialTimeout(timeoutConfig.ConnectTimeout),
        pgdriver.WithReadTimeout(timeoutConfig.ReadTimeout),
        pgdriver.WithWriteTimeout(timeoutConfig.WriteTimeout),
    }

    if nil != instance.tlsConfig {
        connectorOptions = append(connectorOptions, pgdriver.WithTLSConfig(instance.tlsConfig))
    } else if true == instance.insecure {
        /* pgdriver.WithInsecure(true) disables TLS entirely */
        connectorOptions = append(connectorOptions, pgdriver.WithInsecure(true))
    } else {
        /* do NOT hand this case to pgdriver.WithInsecure(false): despite the name, pgdriver implements it as tls.Config{InsecureSkipVerify: true} — TLS is negotiated but the server certificate is never checked, so the default connection is trivially machine-in-the-middled. Build a verifying config instead: the system roots, and the configured host as the name to verify against. Callers that genuinely want an unverified session pass WithTlsConfig or WithInsecure(true) explicitly. */
        connectorOptions = append(connectorOptions, pgdriver.WithTLSConfig(&tls.Config{
            ServerName: host,
            MinVersion: tls.VersionTLS12,
        }))
    }

    connector := pgdriver.NewConnector(connectorOptions...)

    if nil != instance.postBuildHook {
        hookContext := ctx
        hookCancel := func() {}
        if 0 < timeoutConfig.ConnectTimeout {
            hookContext, hookCancel = context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)
        }
        defer hookCancel()

        hookErr := instance.postBuildHook(hookContext, resolver, connector)
        if nil != hookErr {
            return nil, exception.NewError(
                "pgsql database connector configuration failed",
                connectionConfig.SafeContext(),
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

    pingContext := ctx
    pingCancel := func() {}
    if 0 < timeoutConfig.ConnectTimeout {
        pingContext, pingCancel = context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)
    }
    defer pingCancel()

    pingErr := database.PingContext(pingContext)
    if nil != pingErr {
        _ = database.Close()

        return nil, exception.NewError(
            "database connection failed",
            instance.toConnectionContext(connectionConfig, poolConfig, timeoutConfig),
            pingErr,
        )
    }

    return database, nil
}

func (instance *Provider) toConnectionContext(
    connectionConfig *ConnectionConfig,
    poolConfig *PoolConfig,
    timeoutConfig *TimeoutConfig,
) exceptioncontract.Context {
    return map[string]any{
        "connection":    connectionConfig.SafeContext(),
        "poolConfig":    poolConfig,
        "timeoutConfig": timeoutConfig,
    }
}

func (instance *Provider) computeBackoffDelay(attempt uint32) time.Duration {
    /* non-positive delays and a multiplier below 1 fall back to the defaults: a negative delay makes
       time.Sleep return immediately and a sub-1 multiplier decays the delay toward zero, both collapsing
       the backoff into a re-dial storm; a multiplier of exactly 1 stays a valid constant backoff. */
    initialDelay := instance.retryConfig.InitialDelay
    if 0 >= initialDelay {
        initialDelay = 500 * time.Millisecond
    }

    maxDelay := instance.retryConfig.MaxDelay
    if 0 >= maxDelay {
        maxDelay = 5 * time.Second
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN
       multiplier through, poison the float-space growth below and collapse the backoff into an immediate
       re-dial storm once the NaN converts to a negative duration. */
    backoffMultiplier := instance.retryConfig.BackoffMultiplier
    if false == (backoffMultiplier >= 1) {
        backoffMultiplier = 2.0
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

var (
    _ bunorm.Provider          = (*Provider)(nil)
    _ bunorm.MigrationProvider = (*Provider)(nil)
    _ bunorm.ContextOpener     = (*Provider)(nil)
)
