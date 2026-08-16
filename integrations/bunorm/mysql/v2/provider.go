package mysql

import (
    "context"
    "crypto/tls"
    "database/sql"
    "errors"
    "fmt"
    "net"
    "strings"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/integrations/bunorm/v2"
    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
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

    /* the migration derivation means its zeroes: they lift the deadlines and the recycling on purpose, which is the one intent the resolution below must not read as an unset field */
    tunedForMigration bool
}

/* resolvedTimeoutConfig answers the configuration the connector is built from, with every non-positive field replaced by the constructor default. A zero reaches here far more often from an environment key nobody set than from a caller who means "no deadline", and on this driver a zero read or write deadline means exactly no deadline — so the unset key would disarm the very protection the nil configuration arms, and a negative one would put the deadline in the past, failing every dial instantly with an i/o timeout no network event caused. */
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

func (instance *Provider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.OpenContext(context.Background(), params, logger)
}

/* OpenContext opens under the caller's context: an already-cancelled context is refused before the attempt, the retry sleeps watch it alongside the clock, and the configuration hook and the boot ping derive their budgets from it. The one step outside its reach is the dialect handshake bun performs at construction, which queries the server under no caller context and is bounded by the connect timeout alone — so a cancellation arriving mid-attempt is honoured at the next cancellable step rather than instantly. A nil context reads as context.Background(), which is exactly Open. */
func (instance *Provider) OpenContext(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if nil == ctx {
        ctx = context.Background()
    }

    if nil == instance.retryConfig {
        return instance.open(ctx, params, logger)
    }

    return instance.openWithRetry(ctx, params, logger)
}

/* OpenForMigration opens the same database with the driver deadlines lifted: ReadTimeout and WriteTimeout are per-connection settings baked into the connector, sized for request traffic, and a DDL statement that legitimately runs past them — an ALTER TABLE adding constraints on a large table — is cut mid-statement with "invalid connection", outside any transaction MySQL would roll back. The connect timeout stays armed (a down database must still fail fast), the pool is kept to the two connections a sequential migration run needs, and no connection is recycled mid-run — a lifetime rotation under a running statement is the same cut by another name. */
func (instance *Provider) OpenForMigration(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.OpenForMigrationContext(context.Background(), params, logger)
}

/* OpenForMigrationContext is OpenForMigration under the caller's context, the way OpenContext is Open under it: the registry hands the context it was constructed with, so an already-cancelled migration is refused before the attempt and a cancellation arriving mid-attempt is honoured at the next cancellable step instead of sleeping out the retry budget. A nil context reads as context.Background(), which is exactly OpenForMigration. */
func (instance *Provider) OpenForMigrationContext(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.migrationProvider().OpenContext(ctx, params, logger)
}

/* migrationProvider derives the provider OpenForMigration dials with: the same hook, retry policy and transport settings, over the migration pool and the lifted deadlines. */
func (instance *Provider) migrationProvider() *Provider {
    return &Provider{
        poolConfig:        migrationPoolConfig(),
        timeoutConfig:     migrationTimeoutConfig(instance.timeoutConfig),
        retryConfig:       instance.retryConfig,
        postBuildHook:     instance.postBuildHook,
        insecure:          instance.insecure,
        tlsConfig:         instance.tlsConfig,
        tunedForMigration: true,
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

func (instance *Provider) openWithRetry(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    logger = logging.EnsureLogger(logger)

    attempt := uint32(0)
    maxAttempts := instance.retryConfig.MaxAttempts
    if 0 == maxAttempts {
        maxAttempts = 3
    }

    for {
        attempt = attempt + 1

        database, openErr := instance.open(ctx, params, logger)
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

        /* the caller's own cancellation is not a database outage. The transient classifier reads messages and error types, none of which a cancellation carries, so a SIGTERM that cancelled the open mid-deploy fell through to the terminal branch and paged whoever was on call with "database connection failed with non-transient error" against a perfectly healthy database. It is a clean stop: recorded at warning under its own name and not retried, because the context that would carry the retry is already gone. Only Canceled, never DeadlineExceeded — the ping budget is derived from the connect timeout, so a deadline here can be the database itself. */
        if true == errors.Is(openErr, context.Canceled) {
            cancelledErr := exception.FromError(openErr)
            logger.Warning(
                "database open cancelled by the caller's context",
                exception.LogContext(
                    cancelledErr,
                    map[string]any{"attempt": attempt},
                ),
            )

            return nil, exception.MarkLogged(cancelledErr)
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

        /* the retry warnings are the first two records the operator sees when a database is down, and they carry the same diagnostic shape as the terminal records above: LogContext lifts the failure's own context — the host and port dialed, the pool sizing, the deadlines that governed the attempt — and its cause chain, where the flattened openErr.Error() handed on a message and nothing to act on */
        retryErr := exception.FromError(openErr)

        logger.Warning(
            "database connection failed and retrying",
            exception.LogContext(
                retryErr,
                map[string]any{
                    "attempt":     attempt,
                    "maxAttempts": maxAttempts,
                    "retryIn":     delay.String(),
                },
            ),
        )

        /* the sleep watches the caller's context alongside the clock: a shutdown signal arriving mid-retry would otherwise sleep through the whole remaining budget, and the second signal exits with no teardown at all */
        delayTimer := time.NewTimer(delay)
        select {
        case <-ctx.Done():
            delayTimer.Stop()

            /* the same clean stop as the branch above, reached one step later: the cancellation arrived while this attempt was waiting out its backoff. It is recorded here and marked, because an unmarked cancellation travelling up as a bare resolution failure is filed at error by whichever writer meets it — the very record this classification exists to prevent. */
            cancelledErr := exception.NewError(
                "database connection retry cancelled by the caller's context",
                map[string]any{"attempt": attempt, "error": openErr.Error()},
                ctx.Err(),
            )

            logger.Warning(
                "database connection retry cancelled by the caller's context",
                exception.LogContext(cancelledErr),
            )

            return nil, exception.MarkLogged(cancelledErr)
        case <-delayTimer.C:
        }
    }
}

/* connectionTlsConfig answers the TLS configuration the connector is built with, or nil for a plaintext connection. WithTlsConfig wins outright; WithInsecure means nil, the one plaintext path; the default is a VERIFYING config rather than the driver's convenience spellings — `TLSConfig = "skip-verify"` negotiates TLS but never checks the server certificate, and AllowFallbackToPlaintext would silently drop to an unencrypted session against a server that speaks no TLS, the very downgrade a secure default exists to refuse. The default is the system roots with the configured host as the name to verify against, so a server that speaks no TLS fails the dial and the operator arms WithInsecure deliberately rather than getting plaintext by surprise. */
func (instance *Provider) connectionTlsConfig(host string) *tls.Config {
    if nil != instance.tlsConfig {
        return instance.tlsConfig
    }

    if true == instance.insecure {
        return nil
    }

    return &tls.Config{
        ServerName: host,
        MinVersion: tls.VersionTLS12,
    }
}

func (instance *Provider) open(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    /* an already-cancelled context is refused before the attempt: the dialect handshake bun performs at construction queries the server outside any caller context, bounded by the connect timeout alone, so without this refusal a shutdown-cancelled lazy open still paid one full dial against a database that no longer matters. */
    if ctxErr := ctx.Err(); nil != ctxErr {
        return nil, exception.NewError(
            "database open cancelled before the attempt",
            nil,
            ctxErr,
        )
    }

    /* the routing lives here because open is the one funnel every door shares — Open, OpenContext, the retry loop and the migration door all pass through it. Routed only on the retry path, the default retry-less open left bun's declaration mistakes on standard error. RouteDiagnostics is once per process, so repeated attempts cost nothing. */
    bunorm.RouteDiagnostics(logging.EnsureLogger(logger))

    connectionConfig := NewConnectionConfig(params.Host, params.Port, params.Database, params.User, params.Password)

    poolConfig := instance.resolvedPoolConfig()
    timeoutConfig := instance.resolvedTimeoutConfig()

    address := fmt.Sprintf("%s:%s", params.Host, params.Port)

    driverConfig := driver.NewConfig()
    driverConfig.User = params.User
    driverConfig.Passwd = params.Password
    driverConfig.Net = "tcp"
    driverConfig.Addr = address
    driverConfig.DBName = params.Database
    driverConfig.ParseTime = true
    driverConfig.Timeout = timeoutConfig.ConnectTimeout
    driverConfig.ReadTimeout = timeoutConfig.ReadTimeout
    driverConfig.WriteTimeout = timeoutConfig.WriteTimeout

    /* a nil result leaves the connection plaintext, which only WithInsecure produces; the default and WithTlsConfig both set a config, so the driver never falls back to an unencrypted session by omission */
    driverConfig.TLS = instance.connectionTlsConfig(params.Host)

    if nil != instance.postBuildHook {
        hookContext := ctx
        hookCancel := func() {}
        if 0 < timeoutConfig.ConnectTimeout {
            hookContext, hookCancel = context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)
        }
        defer hookCancel()

        hookErr := instance.postBuildHook(hookContext, driverConfig)
        if nil != hookErr {
            return nil, exception.NewError(
                "mysql database connector configuration failed",
                connectionConfig.SafeContext(),
                hookErr,
            )
        }
    }

    connector, connectorErr := driver.NewConnector(driverConfig)
    if nil != connectorErr {
        return nil, exception.NewError(
            "database connector creation failed",
            connectionConfig.SafeContext(),
            connectorErr,
        )
    }

    sqlDatabase := sql.OpenDB(connector)

    sqlDatabase.SetMaxOpenConns(poolConfig.MaxOpenConnections)
    sqlDatabase.SetMaxIdleConns(poolConfig.MaxIdleConnections)
    sqlDatabase.SetConnMaxLifetime(poolConfig.ConnectionMaxLifetime)
    sqlDatabase.SetConnMaxIdleTime(poolConfig.ConnectionMaxIdleTime)

    database := bun.NewDB(sqlDatabase, mysqldialect.New())

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

/* toConnectionContext is the diagnostic context of a failed connection, the pgsql sibling's shape: the operator reading the record sees the pool sizing and the deadlines that governed the attempt, not only the address that refused. */
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
        "server shutdown in progress",
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

/* this major hands the provider the connection values rather than the parameter names it would read them under, so the provider knows no configuration key and does not carry bunorm.SecretParameterProvider. The application names its credential parameters to ManagerRegistry.MarkSecretParameters instead. */
var (
    _ bunorm.Provider               = (*Provider)(nil)
    _ bunorm.MigrationProvider      = (*Provider)(nil)
    _ bunorm.ContextOpener          = (*Provider)(nil)
    _ bunorm.MigrationContextOpener = (*Provider)(nil)
)
