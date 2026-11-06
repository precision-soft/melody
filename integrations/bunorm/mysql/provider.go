package mysql

import (
    "context"
    "crypto/tls"
    "database/sql"
    "math"
    "net"
    "strings"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/config"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/integrations/bunorm"
    "github.com/precision-soft/melody/logging"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
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

    /* the migration derivation means its zeroes: they lift the deadlines and recycling, and must not be read as unset */
    tunedForMigration bool
}

/* non-positive fields take the default: on this driver a zero read or write deadline means no deadline */
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

/* non-positive fields take the default: on database/sql a zero maximum is UNLIMITED and a zero lifetime never recycles */
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

func (instance *Provider) Open(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.OpenContext(context.Background(), resolver)
}

func (instance *Provider) SecretParameterNames() []string {
    return []string{instance.passwordParameterName}
}

/* OpenContext refuses an already-cancelled context and reads a nil one as context.Background(), which is exactly Open. */
func (instance *Provider) OpenContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    if nil == ctx {
        ctx = context.Background()
    }

    if nil == instance.retryConfig {
        return instance.open(ctx, resolver)
    }

    return instance.openWithRetry(ctx, resolver)
}

/* OpenForMigration lifts the driver deadlines: a DDL past them is cut mid-statement, outside any transaction to roll back. */
func (instance *Provider) OpenForMigration(resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.OpenForMigrationContext(context.Background(), resolver)
}

func (instance *Provider) OpenForMigrationContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.migrationProvider().OpenContext(ctx, resolver)
}

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

        /* only Canceled, never DeadlineExceeded: the ping budget derives from the connect timeout, so it can be the database */
        if true == exception.ChainIs(openErr, context.Canceled) {
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

        delayTimer := time.NewTimer(delay)
        select {
        case <-ctx.Done():
            delayTimer.Stop()

            cancelledErr := exception.NewError(
                "database connection retry cancelled by the caller's context",
                exception.LogContext(
                    exception.FromError(openErr),
                    map[string]any{"attempt": attempt},
                ),
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

/* the default is a verifying config: "skip-verify" never checks the certificate, AllowFallbackToPlaintext can downgrade */
func (instance *Provider) connectionTlsConfig(host string) *tls.Config {
    if nil != instance.tlsConfig {
        return instance.tlsConfig
    }

    if true == instance.insecure {
        return nil
    }

    return &tls.Config{
        ServerName: tlsServerNameOf(host),
        MinVersion: tls.VersionTLS12,
    }
}

func (instance *Provider) open(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    if ctxErr := ctx.Err(); nil != ctxErr {
        return nil, exception.NewError(
            "database open cancelled before the attempt",
            nil,
            ctxErr,
        )
    }

    diagnosticsLogger, diagnosticsLoggerErr := logging.LoggerFromResolver(resolver)
    if nil != diagnosticsLoggerErr {
        diagnosticsLogger = logging.EmergencyLogger()
    }

    bunorm.RouteDiagnostics(diagnosticsLogger)

    configuration := config.ConfigMustFromResolver(resolver)

    configuration.MarkSecret(instance.passwordParameterName)

    host := configuration.MustGet(instance.hostParameterName).MustString()
    port := configuration.MustGet(instance.portParameterName).MustString()
    databaseName := configuration.MustGet(instance.databaseParameterName).MustString()
    user := configuration.MustGet(instance.userParameterName).MustString()
    password := configuration.MustGet(instance.passwordParameterName).MustString()

    /* an empty host is refused: ":port" is the local system to a dialer, which would connect with the configured credentials */
    if "" == host {
        return nil, exception.NewError("mysql database open refused: the host is empty", map[string]any{"parameter": instance.hostParameterName}, nil)
    }

    connectionConfig := NewConnectionConfig(host, port, databaseName, user, password)

    poolConfig := instance.resolvedPoolConfig()
    timeoutConfig := instance.resolvedTimeoutConfig()

    address := dialAddressOf(host, port)

    driverConfig := driver.NewConfig()
    driverConfig.User = user
    driverConfig.Passwd = password
    driverConfig.Net = "tcp"
    driverConfig.Addr = address
    driverConfig.DBName = databaseName
    driverConfig.ParseTime = true
    driverConfig.Timeout = timeoutConfig.ConnectTimeout
    driverConfig.ReadTimeout = timeoutConfig.ReadTimeout
    driverConfig.WriteTimeout = timeoutConfig.WriteTimeout

    /* a nil result leaves the connection plaintext, which only WithInsecure produces */
    driverConfig.TLS = instance.connectionTlsConfig(host)

    if nil != instance.postBuildHook {
        hookContext := ctx
        hookCancel := func() {}
        if 0 < timeoutConfig.ConnectTimeout {
            hookContext, hookCancel = context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)
        }
        defer hookCancel()

        hookErr := instance.postBuildHook(hookContext, resolver, driverConfig)
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

    pingContext := ctx
    pingCancel := func() {}
    if 0 < timeoutConfig.ConnectTimeout {
        pingContext, pingCancel = context.WithTimeout(ctx, timeoutConfig.ConnectTimeout)
    }
    defer pingCancel()

    /* the connection is established before bun gets the pool: bun's mysql dialect queries the version with no context */
    pingErr := sqlDatabase.PingContext(pingContext)
    if nil != pingErr {
        _ = sqlDatabase.Close()

        return nil, exception.NewError(
            "database connection failed",
            instance.toConnectionContext(connectionConfig, poolConfig, timeoutConfig, driverConfig.Addr),
            pingErr,
        )
    }

    database := bun.NewDB(sqlDatabase, mysqldialect.New())

    return database, nil
}

func (instance *Provider) toConnectionContext(
    connectionConfig *ConnectionConfig,
    poolConfig *PoolConfig,
    timeoutConfig *TimeoutConfig,
    dialedAddress string,
) exceptioncontract.Context {
    return map[string]any{
        "connection":    connectionConfig.SafeContext(),
        "poolConfig":    poolConfig,
        "timeoutConfig": timeoutConfig,
        "dialedAddress": dialedAddress,
    }
}

const minimumBackoffDelay = time.Millisecond

func (instance *Provider) computeBackoffDelay(attempt uint32) time.Duration {
    initialDelay := instance.retryConfig.InitialDelay
    if 0 >= initialDelay {
        initialDelay = 500 * time.Millisecond
    }

    maxDelay := instance.retryConfig.MaxDelay
    if 0 >= maxDelay {
        maxDelay = 5 * time.Second
    }

    if minimumBackoffDelay > initialDelay {
        initialDelay = minimumBackoffDelay
    }

    if minimumBackoffDelay > maxDelay {
        maxDelay = minimumBackoffDelay
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN multiplier through */
    backoffMultiplier := instance.retryConfig.BackoffMultiplier
    if false == (backoffMultiplier >= 1) {
        backoffMultiplier = 2.0
    }

    if 0 == attempt {
        attempt = 1
    }

    /* capped in float space before the conversion: a large attempt count overflows into a negative duration past a > cap */
    maxDelayFloat := float64(maxDelay)
    delay := float64(initialDelay) * math.Pow(backoffMultiplier, float64(attempt-1))

    if false == (delay < maxDelayFloat) {
        return maxDelay
    }

    return time.Duration(delay)
}

func containsTransientMarker(message string, marker string) bool {
    searchStart := 0

    for {
        offset := strings.Index(message[searchStart:], marker)
        if 0 > offset {
            return false
        }

        matchStart := searchStart + offset
        matchEnd := matchStart + len(marker)

        if false == isWordCharacterAt(message, matchStart-1) && false == isWordCharacterAt(message, matchEnd) {
            return true
        }

        searchStart = matchStart + 1
    }
}

func isWordCharacterAt(value string, index int) bool {
    if 0 > index || len(value) <= index {
        return false
    }

    character := value[index]

    return ('a' <= character && 'z' >= character) ||
        ('A' <= character && 'Z' >= character) ||
        ('0' <= character && '9' >= character) ||
        '_' == character
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

    /* an open failure can come from the application and its chain may reach itself; each pointer link is read once */
    for _, link := range exception.BuildCauseChain(inputErr, transientErrorLinkLimit) {
        message := strings.ToLower(link)

        for _, marker := range transientMarkers {
            if "" == marker {
                continue
            }

            if true == containsTransientMarker(message, marker) {
                return true
            }
        }
    }

    return false
}

var (
    _ bunorm.Provider                = (*Provider)(nil)
    _ bunorm.MigrationProvider       = (*Provider)(nil)
    _ bunorm.ContextOpener           = (*Provider)(nil)
    _ bunorm.MigrationContextOpener  = (*Provider)(nil)
    _ bunorm.SecretParameterProvider = (*Provider)(nil)
)

func dialAddressOf(host string, port string) string {
    if true == strings.Contains(host, ":") && false == strings.HasPrefix(host, "[") {
        return "[" + host + "]:" + port
    }

    return host + ":" + port
}

func tlsServerNameOf(host string) string {
    name := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
    if zone := strings.IndexByte(name, '%'); -1 != zone {
        name = name[:zone]
    }

    return name
}
