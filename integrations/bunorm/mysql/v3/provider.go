package mysql

import (
    "context"
    "crypto/tls"
    "database/sql"
    "errors"
    "math"
    "net"
    "reflect"
    "strings"
    "time"

    driver "github.com/go-sql-driver/mysql"
    "github.com/precision-soft/melody/integrations/bunorm/v3"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
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

/* resolvedTimeoutConfig replaces every non-positive field with the constructor default: on this driver a zero read or write deadline means no deadline, so an unset environment key would disarm the protection, and a negative one would fail every dial at once. */
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

func (instance *Provider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.OpenContext(context.Background(), params, logger)
}

/* OpenContext opens under the caller's context: an already-cancelled context is refused before the attempt, the retry sleeps watch it beside the clock, and the configuration hook and the boot ping derive their budgets from it; the dialect handshake bun runs at construction is bounded by the connect timeout alone. A nil context reads as context.Background(). A nil logger reads as the emergency logger, since the terminal branches mark the returned error as logged and the diagnostics routing consumes a process-lifetime once. */
func (instance *Provider) OpenContext(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if nil == ctx {
        ctx = context.Background()
    }

    if true == isNilInterface(logger) {
        logger = logging.EmergencyLogger()
    }

    if nil == instance.retryConfig {
        database, openErr := instance.open(ctx, params, logger)
        /* the caller's context is read before the failure is classified: a budget the caller already spent carries a deadline, which the classifier would otherwise file as an unreachable database for the splitter to absorb */
        if nil != openErr && nil == ctx.Err() && true == instance.isTransientError(openErr) {
            return nil, unreachable(openErr)
        }

        return database, openErr
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

        /* the caller's own cancellation is not an outage: it is recorded at warning under its own name and not retried. Whether the caller is done is read off its context, not the failure's class, since the ping budget's own DeadlineExceeded can be the database and is retried; a refusal the server gave just before the caller's deadline landed is journaled here too, with the refusal still returned */
        if nil != ctx.Err() || true == errors.Is(openErr, context.Canceled) {
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

        /* the retry warnings carry the terminal records' shape: LogContext lifts the failure's own context, the address dialled, the pool sizing and the deadlines, and its cause chain */
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
            /* the cause stays the cancellation, which the classification upstream reads, and the failure being retried travels structured beside it through LogContext */
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
    /* an already-cancelled context is refused before the attempt: the dialect handshake bun performs at construction queries the server outside any caller context, bounded by the connect timeout alone, so without this refusal a shutdown-cancelled lazy open would pay one full dial against a database nothing waits for. */
    if ctxErr := ctx.Err(); nil != ctxErr {
        return nil, exception.NewError(
            "database open cancelled before the attempt",
            nil,
            ctxErr,
        )
    }

    /* the routing lives here because open is the one funnel every door shares; RouteDiagnostics installs nothing when the logger is already routed */
    bunorm.RouteDiagnostics(logger)

    /* an empty host is refused before the driver sees it: the address ":port" is the local system to a dialer, so an unset host would connect to whatever listens there with the configured credentials. The database and the user are left to the server, and an empty password is legitimate. */
    if "" == params.Host {
        return nil, exception.NewError("mysql database open refused: the host is empty", params.SafeContext(), nil)
    }

    connectionConfig := NewConnectionConfig(params.Host, params.Port, params.Database, params.User, params.Password)

    poolConfig := instance.resolvedPoolConfig()
    timeoutConfig := instance.resolvedTimeoutConfig()

    address := dialAddressOf(params.Host, params.Port)

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
            instance.toConnectionContext(connectionConfig, poolConfig, timeoutConfig, driverConfig.Addr),
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
    dialedAddress string,
) exceptioncontract.Context {
    return map[string]any{
        "connection":    connectionConfig.SafeContext(),
        "poolConfig":    poolConfig,
        "timeoutConfig": timeoutConfig,
        /* the endpoint the dial actually reached, which is not always the configured one: the connection config is built from the parameters BEFORE the post-build hook runs, and the hook may rewrite the address — it is handed the very field the hook tests read. Named separately rather than folded into the connection, so a record where the two differ says so. */
        "dialedAddress": dialedAddress,
    }
}

/* minimumBackoffDelay is the floor under every delay this provider computes: under a millisecond the wait is shorter than the dial it separates, a re-dial storm rather than a backoff. */
const minimumBackoffDelay = time.Millisecond

func (instance *Provider) computeBackoffDelay(attempt uint32) time.Duration {
    defaultConfig := DefaultRetryConfig()

    /* non-positive delays and a multiplier below 1 fall back to the defaults: a negative delay makes time.Sleep return immediately and a sub-1 multiplier decays the delay toward zero, both collapsing the backoff into a re-dial storm; a multiplier of exactly 1 stays a valid constant backoff. */
    initialDelay := instance.retryConfig.InitialDelay
    if 0 >= initialDelay {
        initialDelay = defaultConfig.InitialDelay
    }

    maxDelay := instance.retryConfig.MaxDelay
    if 0 >= maxDelay {
        maxDelay = defaultConfig.MaxDelay
    }

    /* the floor is applied to BOTH bounds, so every branch below returns at least it: raising the initial delay alone would still let a sub-millisecond ceiling cap the result straight back under the floor. */
    if minimumBackoffDelay > initialDelay {
        initialDelay = minimumBackoffDelay
    }

    if minimumBackoffDelay > maxDelay {
        maxDelay = minimumBackoffDelay
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN multiplier through, poison the float-space growth below and collapse the backoff into an immediate re-dial storm once the NaN converts to a negative duration. */
    backoffMultiplier := instance.retryConfig.BackoffMultiplier
    if false == (backoffMultiplier >= 1) {
        backoffMultiplier = defaultConfig.BackoffMultiplier
    }

    /* the first attempt waits the initial delay, so the growth is over the attempts already made; a zero attempt reads as the first, since it would wrap the unsigned subtraction below */
    if 0 == attempt {
        attempt = 1
    }

    /* the growth is computed in closed form and capped in float space before the conversion: a large attempt count overflows the float64 to int64 conversion into a negative duration that slips past a > cap, and the not-less-than form keeps an infinite growth capped too */
    maxDelayFloat := float64(maxDelay)
    delay := float64(initialDelay) * math.Pow(backoffMultiplier, float64(attempt-1))

    if false == (delay < maxDelayFloat) {
        return maxDelay
    }

    return time.Duration(delay)
}

/* containsTransientMarker matches a marker as a word, bounded by any character that is not a letter, a digit or an underscore, so "eof" inside an identifier such as geofences does not read a permanent failure as transient. */
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

/* isTransientServerErrorNumber answers on the error number the server sent: 1040, 1053, 1203, 1226 and 1159 (too many connections, a shutdown in progress, too many user connections, a user resource limit, the server's own handshake timeout) are the server unable to take the connection now, and 9001 and 9002 are ProxySQL saying the same of its backend; every other number with an identity is a refusal by name. The number is read rather than the SQLSTATE, since the server files its resource refusals under the generic 42000 and HY000. */
func isTransientServerErrorNumber(number uint16) bool {
    switch number {
    case 1040, 1053, 1159, 1203, 1226, 9001, 9002:
        return true
    }

    return false
}

/* unreachable files an open failure the transient classifier admitted — and the retry budget could not get past — under bunorm.ErrDatabaseUnreachable, the class a read/write splitter absorbs. The exception keeps its message and its context; the link sits under it as the cause, so a journal renders the class once, where the driver failure stood, and errors.As still reaches that failure through it. */
func unreachable(openErr error) *exception.Error {
    failure := exception.FromError(openErr)

    return exception.NewError(failure.Message(), failure.Context(), bunorm.DatabaseUnreachable(failure.CauseErr()))
}

/* isTransientError answers whether an open failure is worth another attempt and, on the retry-less door, whether it is filed as unreachable. A failure carrying the server's identity is classified on that identity before any message is read, since the server quotes operands such as a database named "timeout" in its message; without one, the net.Error checks and the markers decide. */
func (instance *Provider) isTransientError(inputErr error) bool {
    if nil == inputErr {
        return false
    }

    var serverErr *driver.MySQLError
    if true == errors.As(inputErr, &serverErr) {
        return isTransientServerErrorNumber(serverErr.Number)
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

            if true == containsTransientMarker(message, marker) {
                return true
            }
        }

        currentErr = errors.Unwrap(currentErr)
    }

    return false
}

/* this major hands the provider the connection values rather than the parameter names it would read them under, so the provider knows no configuration key and names no credential of its own. Marking one is the application's call, through the framework's own application/contract.ParameterRegistrar — RegisterSecretParameter for a parameter the application declares, MarkParameterSecret for one melody registered from the .env artifacts: the party that resolved the values is the party that knows the keys, and the mark propagates to every parameter whose template reads the secret, so the assembled dsn is redacted with it. */
var (
    _ bunorm.Provider               = (*Provider)(nil)
    _ bunorm.MigrationProvider      = (*Provider)(nil)
    _ bunorm.ContextOpener          = (*Provider)(nil)
    _ bunorm.MigrationContextOpener = (*Provider)(nil)
)

/* isNilInterface answers whether the interface value is nil outright or holds a nil pointer, map, slice, channel or function: a typed nil passes a plain nil comparison and then panics on first use, far from the wiring mistake that produced it. Duplicated from the framework's internal package, which a separate module cannot import. */
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

/* dialAddressOf joins the host and the port the way a dialer reads them: a host carrying a colon is an IPv6 literal and is bracketed unless it already is, and a host name or an IPv4 literal is joined as it is. */
func dialAddressOf(host string, port string) string {
    if true == strings.Contains(host, ":") && false == strings.HasPrefix(host, "[") {
        return "[" + host + "]:" + port
    }

    return host + ":" + port
}
