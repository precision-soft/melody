package pgsql

import (
    "context"
    "crypto/tls"
    "database/sql"
    "errors"
    "fmt"
    "math"
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

/* SecretParameterNames names the password parameter, so the registry can arm the redaction at construction: the marking inside open covers only a process that reaches the dial, and debug:parameters is precisely the process that does not. */
func (instance *Provider) SecretParameterNames() []string {
    return []string{instance.passwordParameterName}
}

/* OpenContext opens under the caller's context: an already-cancelled context is refused before the attempt, the retry sleeps watch it alongside the clock, and the configuration hook and the boot ping derive their budgets from it. The dialect performs no server round trip at construction — pgdialect's Init is empty, unlike its mysql twin — so the first packet on the wire is the boot ping's dial, made under the caller's context and bounded by the connect timeout. A nil context reads as context.Background(), which is exactly Open. */
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
    return instance.OpenForMigrationContext(context.Background(), resolver)
}

/* OpenForMigrationContext is OpenForMigration under the caller's context, the way OpenContext is Open under it: the registry hands the context it was constructed with, so an already-cancelled migration is refused before the attempt and a cancellation arriving mid-attempt is honoured at the next cancellable step instead of sleeping out the retry budget. A nil context reads as context.Background(), which is exactly OpenForMigration. */
func (instance *Provider) OpenForMigrationContext(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    return instance.migrationProvider().OpenContext(ctx, resolver)
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
            /* the cause stays the cancellation — the classification upstream reads it, and an outage put in its place would file a clean stop as an outage — but the failure that was being retried travels STRUCTURED beside it rather than flattened into one string. LogContext lifts that failure's own context and its cause chain, which is the shape the retry warning twenty lines above already hands the operator: the host and port dialled, the pool sizing, the deadlines that governed the attempt. openErr.Error() handed on a message and nothing to act on, the exact aplatizare the comment at retryErr condemns. */
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

func (instance *Provider) open(ctx context.Context, resolver containercontract.Resolver) (*bun.DB, error) {
    /* an already-cancelled context is refused before the attempt: nothing below dials outside the caller's context — pgdialect performs no construction-time query, unlike its mysql twin — but without this refusal a shutdown-cancelled lazy open still built the connector, ran the configuration hook and surfaced the cancellation as "database connection failed" naming the database, rather than as the shutdown that caused it. */
    if ctxErr := ctx.Err(); nil != ctxErr {
        return nil, exception.NewError(
            "database open cancelled before the attempt",
            nil,
            ctxErr,
        )
    }

    /* the routing lives here because open is the one funnel every door shares — Open, OpenContext, the retry loop and the migration door all pass through it. Routed only on the retry path, the default retry-less open left bun's declaration mistakes on standard error. RouteDiagnostics is once per process, so repeated attempts cost nothing. */
    diagnosticsLogger, diagnosticsLoggerErr := logging.LoggerFromResolver(resolver)
    if nil != diagnosticsLoggerErr {
        diagnosticsLogger = logging.EmergencyLogger()
    }

    bunorm.RouteDiagnostics(diagnosticsLogger)

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
        /* the endpoint the dial actually reached, which is not always the configured one: the connection config is built from the parameters BEFORE the post-build hook runs, and the hook may rewrite the address — it is handed the very field the hook tests read. Named separately rather than folded into the connection, so a record where the two differ says so. */
        "dialedAddress": dialedAddress,
    }
}

/* minimumBackoffDelay is the floor under every delay this provider computes. The guards below refuse a
   non-positive delay, which left ONE NANOSECOND as the smallest thing a configuration could ask for — and a
   one-nanosecond wait between dials is not a backoff, it is the re-dial storm those guards exist to prevent,
   arriving through the door they left open. Under a millisecond the wait is shorter than the dial it is
   meant to separate, so a millisecond is where a delay starts meaning anything at all. */
const minimumBackoffDelay = time.Millisecond

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

    /* the floor is applied to BOTH bounds, so every branch below returns at least it: raising the initial
       delay alone would still let a sub-millisecond ceiling cap the result straight back under the floor. */
    if minimumBackoffDelay > initialDelay {
        initialDelay = minimumBackoffDelay
    }

    if minimumBackoffDelay > maxDelay {
        maxDelay = minimumBackoffDelay
    }

    /* the not-at-least-1 form is deliberate: NaN fails every comparison, so `1 > NaN` would let a NaN
       multiplier through, poison the float-space growth below and collapse the backoff into an immediate
       re-dial storm once the NaN converts to a negative duration. */
    backoffMultiplier := instance.retryConfig.BackoffMultiplier
    if false == (backoffMultiplier >= 1) {
        backoffMultiplier = 2.0
    }

    /* the first attempt waits the initial delay, so the growth is over the attempts ALREADY made. A zero
       attempt is not one of them and would wrap the unsigned subtraction below into a growth of four
       billion steps; it reads as the first attempt, which is the answer the growth loop this replaced gave
       it by never running. */
    if 0 == attempt {
        attempt = 1
    }

    /* the growth is computed in CLOSED FORM rather than by multiplying once per attempt already made. The
       loop that did it cost O(attempt) per call and therefore O(attempt²) over a run, and it left early
       only once the delay had passed the ceiling — which a multiplier of exactly 1, a valid constant
       backoff, never does, so a large attempt budget paid that square in full for a delay that never moved.
       What the loop was right about is kept: the growth stays in float space and is capped BEFORE the
       conversion, because a large attempt count overflows the float64->int64 conversion to a negative
       duration, which slips past a `> maxDelay` cap and collapses the backoff to zero. The cap is written
       as the not-less-than form for the same reason the multiplier guard is: an infinite growth — which is
       where a big enough exponent lands — compares false against every ceiling it is asked about. */
    maxDelayFloat := float64(maxDelay)
    delay := float64(initialDelay) * math.Pow(backoffMultiplier, float64(attempt-1))

    if false == (delay < maxDelayFloat) {
        return maxDelay
    }

    return time.Duration(delay)
}

/* containsTransientMarker matches a marker as a WORD rather than as a bare substring. The short spellings fire inside ordinary identifiers otherwise — "eof" sits inside `Table 'app.geofences' doesn't exist`, "timeout" inside a `session_timeout` column — so a PERMANENT failure was retried for the whole budget and then died under "failed after max retry attempts" instead of "non-transient", costing the delay and telling the operator the wrong thing. A boundary is any character that is not a letter, a digit or an underscore, so the spellings carrying spaces and slashes match exactly as they did. The types io.EOF and net.Error are read above this scan and are unaffected. */
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

            if true == containsTransientMarker(message, marker) {
                return true
            }
        }

        currentErr = errors.Unwrap(currentErr)
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
