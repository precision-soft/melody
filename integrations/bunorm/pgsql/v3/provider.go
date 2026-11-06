package pgsql

import (
    "context"
    "crypto/tls"
    "database/sql"
    "math"
    "net"
    "reflect"
    "strings"
    "time"

    "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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

    /* its deliberate zero read and write deadlines mean lifted: the normalization must not re-arm them */
    tunedForMigration bool
}

func (instance *Provider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.OpenContext(context.Background(), params, logger)
}

/* OpenContext reads a nil context as context.Background() and a nil logger as the emergency logger for its own records. */
func (instance *Provider) OpenContext(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if nil == ctx {
        ctx = context.Background()
    }

    /* the routing takes the logger as handed, ahead of the emergency fallback: a nil routes nothing and moves no channel */
    if nil == ctx.Err() {
        bunorm.RouteDiagnostics(logger)
    }

    if true == isNilInterface(logger) {
        logger = logging.EmergencyLogger()
    }

    if nil == instance.retryConfig {
        database, openErr := instance.open(ctx, params, logger)
        /* the caller's context is read before classifying: a spent budget's deadline would be filed as an unreachable database */
        if nil != openErr && nil == ctx.Err() && true == instance.isTransientError(openErr) {
            return nil, unreachable(openErr)
        }

        return database, openErr
    }

    return instance.openWithRetry(ctx, params, logger)
}

/* OpenForMigration lifts the driver deadlines: a DDL statement past them is cut mid-statement with an i/o timeout. */
func (instance *Provider) OpenForMigration(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.OpenForMigrationContext(context.Background(), params, logger)
}

func (instance *Provider) OpenForMigrationContext(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.migrationProvider().OpenContext(ctx, params, logger)
}

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

/* a zero field takes the default, a negative one is Unlimited: the guards below read a zero connect timeout as no deadline */
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

    resolved.ConnectTimeout = resolvedDuration(resolved.ConnectTimeout, defaultConfig.ConnectTimeout)

    if true == instance.tunedForMigration {
        return resolved
    }

    resolved.ReadTimeout = resolvedDuration(resolved.ReadTimeout, defaultConfig.ReadTimeout)

    resolved.WriteTimeout = resolvedDuration(resolved.WriteTimeout, defaultConfig.WriteTimeout)

    return resolved
}

/* a zero field takes the default, a negative one is Unlimited: on database/sql a zero maximum means an UNLIMITED pool */
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

    resolved.MaxOpenConnections = resolvedOpenConnectionCount(resolved.MaxOpenConnections, defaultConfig.MaxOpenConnections)

    resolved.MaxIdleConnections = resolvedIdleConnectionCount(resolved.MaxIdleConnections, defaultConfig.MaxIdleConnections)

    if true == instance.tunedForMigration {
        return resolved
    }

    resolved.ConnectionMaxLifetime = resolvedDuration(resolved.ConnectionMaxLifetime, defaultConfig.ConnectionMaxLifetime)

    resolved.ConnectionMaxIdleTime = resolvedDuration(resolved.ConnectionMaxIdleTime, defaultConfig.ConnectionMaxIdleTime)

    return resolved
}

func (instance *Provider) openWithRetry(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    attempt := uint32(0)
    maxAttempts := instance.retryConfig.MaxAttempts
    if 0 == maxAttempts {
        maxAttempts = DefaultRetryConfig().MaxAttempts
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

        /* whether the caller is done is read off its context: the ping budget's own DeadlineExceeded can be the database */
        if nil != ctx.Err() || true == exception.ChainIs(openErr, context.Canceled) {
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
            terminalErr := unreachable(openErr)
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

func (instance *Provider) open(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if ctxErr := ctx.Err(); nil != ctxErr {
        return nil, exception.NewError(
            "database open cancelled before the attempt",
            nil,
            ctxErr,
        )
    }

    /* pgdriver.WithDatabase and WithUser panic on an empty string; an empty host's ":port" is the local system to a dialer */
    if "" == params.Database {
        return nil, exception.NewError("pgsql database open refused: the database name is empty", params.SafeContext(), nil)
    }

    if "" == params.User {
        return nil, exception.NewError("pgsql database open refused: the user is empty", params.SafeContext(), nil)
    }

    if "" == params.Host {
        return nil, exception.NewError("pgsql database open refused: the host is empty", params.SafeContext(), nil)
    }

    connectionConfig := NewConnectionConfig(params.Host, params.Port, params.Database, params.User, params.Password)

    poolConfig := instance.resolvedPoolConfig()
    timeoutConfig := instance.resolvedTimeoutConfig()

    address := dialAddressOf(params.Host, params.Port)

    connectorOptions := []pgdriver.Option{
        pgdriver.WithAddr(address),
        pgdriver.WithDatabase(params.Database),
        pgdriver.WithUser(params.User),
        pgdriver.WithPassword(params.Password),
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
        /* do NOT use pgdriver.WithInsecure(false): it is tls.Config{InsecureSkipVerify: true}, so the certificate is never checked */
        connectorOptions = append(connectorOptions, pgdriver.WithTLSConfig(&tls.Config{
            ServerName: tlsServerNameOf(params.Host),
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

        hookErr := instance.postBuildHook(hookContext, connector)
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
            instance.toConnectionContext(connectionConfig, poolConfig, timeoutConfig, connector.Config().Addr),
            pingErr,
        )
    }

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
    defaultConfig := DefaultRetryConfig()

    initialDelay := instance.retryConfig.InitialDelay
    if 0 >= initialDelay {
        initialDelay = defaultConfig.InitialDelay
    }

    maxDelay := instance.retryConfig.MaxDelay
    if 0 >= maxDelay {
        maxDelay = defaultConfig.MaxDelay
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
        backoffMultiplier = defaultConfig.BackoffMultiplier
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

/* classes 08, 53 and 57 are the server unable to take the connection now; any other class with an identity is a refusal */
func isTransientSqlState(sqlState string) bool {
    if 2 > len(sqlState) {
        return false
    }

    switch sqlState[:2] {
    case "08", "53", "57":
        return true
    }

    return false
}

func unreachable(openErr error) *exception.Error {
    failure := exception.FromError(openErr)

    return exception.NewError(failure.Message(), failure.Context(), bunorm.DatabaseUnreachable(failure.CauseErr()))
}

const transientErrorLinkLimit = 64

/* a failure carrying the server's identity is classified on it first: the server quotes operands in its message */
func (instance *Provider) isTransientError(inputErr error) bool {
    if nil == inputErr {
        return false
    }

    if sqlState := sqlStateOf(inputErr); "" != sqlState {
        return isTransientSqlState(sqlState)
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
        "the database system is",
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

/* the provider names no credential: the application marks it through application/contract.ParameterRegistrar */
var (
    _ bunorm.Provider               = (*Provider)(nil)
    _ bunorm.MigrationProvider      = (*Provider)(nil)
    _ bunorm.ContextOpener          = (*Provider)(nil)
    _ bunorm.MigrationContextOpener = (*Provider)(nil)
)

/* duplicated from the framework's internal package, which a separate module cannot import */
func isNilInterface(value any) bool {
    if nil == value {
        return true
    }

    reflected := reflect.ValueOf(value)

    switch reflected.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
        return reflected.IsNil()
    default:
        return false
    }
}

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
