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

    "github.com/precision-soft/melody/integrations/bunorm/v4"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/pgdialect"
    "github.com/uptrace/bun/driver/pgdriver"
)

/* NewProvider resolves the configuration once and keeps it: a limit outside its rule panics naming the field, before any database is opened. */
func NewProvider(config ProviderConfig) *Provider {
    resolved, resolveErr := resolveProviderConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    return &Provider{
        pool:          resolved.pool,
        timeout:       resolved.timeout,
        retry:         resolved.retry,
        postBuildHook: config.PostBuildHook,
        insecure:      config.Insecure,
        tlsConfig:     config.TlsConfig,
    }
}

type Provider struct {
    pool          resolvedPool
    timeout       resolvedTimeout
    retry         resolvedRetry
    postBuildHook PostBuildHook
    insecure      bool
    tlsConfig     *tls.Config
}

func (instance *Provider) Open(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.OpenContext(context.Background(), params, logger)
}

/* OpenContext opens under the caller's context: an already-cancelled context is refused before the attempt, the retry sleeps watch it beside the clock, and the configuration hook and the boot ping derive their budgets from it; pgdialect makes no server round trip at construction, so the first packet is the boot ping's dial. A nil context reads as context.Background(). A nil logger reads as the emergency logger for the open's own records, since the terminal branches mark the returned error as logged; the diagnostics routing takes the logger as handed, so a nil, like an already-cancelled context, routes nothing and leaves bun's channel where the last routing put it. */
func (instance *Provider) OpenContext(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    if nil == ctx {
        ctx = context.Background()
    }

    /* the routing takes the logger as handed, before the open's own records fall back to the emergency logger: a nil routes nothing, so an open given no journal leaves bun's channel where the last routing put it instead of moving it onto standard error; RouteDiagnostics installs nothing when the logger is already routed; an already-cancelled context routes nothing either, since open refuses it before the attempt */
    if nil == ctx.Err() {
        bunorm.RouteDiagnostics(logger)
    }

    if true == isNilInterface(logger) {
        logger = logging.EmergencyLogger()
    }

    return instance.openWithRetry(ctx, params, logger)
}

/* OpenForMigration opens the same database with the driver deadlines lifted: ReadTimeout and WriteTimeout are per-operation socket deadlines baked into the connector, sized for request traffic, and a DDL statement that legitimately runs past them — an ALTER TABLE adding constraints on a large table, a long CREATE INDEX — is cut mid-statement with an i/o timeout. The connect timeout stays armed (a down database must still fail fast), the pool is kept to the two connections a sequential migration run needs, and no connection is recycled mid-run — a lifetime rotation under a running statement is the same cut by another name. */
func (instance *Provider) OpenForMigration(params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.OpenForMigrationContext(context.Background(), params, logger)
}

/* OpenForMigrationContext is OpenForMigration under the caller's context, the way OpenContext is Open under it: the registry hands the context it was constructed with, so an already-cancelled migration is refused before the attempt and a cancellation arriving mid-attempt is honoured at the next cancellable step instead of sleeping out the retry budget. A nil context reads as context.Background(), which is exactly OpenForMigration. */
func (instance *Provider) OpenForMigrationContext(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    return instance.migrationProvider().OpenContext(ctx, params, logger)
}

/* migrationProvider derives the provider OpenForMigration dials with: the same hook, retry policy and transport settings, over the migration pool and the lifted deadlines. */
func (instance *Provider) migrationProvider() *Provider {
    derived := *instance
    derived.pool = migrationPool()
    derived.timeout = instance.timeout.lifted()

    return &derived
}

func (instance *Provider) openWithRetry(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    attempt := 0
    maxAttempts := instance.retry.maxAttempts

    for {
        attempt = attempt + 1

        database, openErr := instance.open(ctx, params, logger)
        if nil == openErr {
            if 1 < attempt {
                logger.Info(
                    "database connection successful after retry",
                    map[string]any{
                        "attempt": attempt,
                    },
                )
            }

            return database, nil
        }

        /* the caller's own cancellation is not an outage: it is recorded at warning under its own name and not retried. Whether the caller is done is read off its context, not the failure's class, since the ping budget's own DeadlineExceeded can be the database and is retried; a refusal the server gave just before the caller's deadline landed is journaled here too, with the refusal still returned */
        if nil != ctx.Err() || true == exception.ChainIs(openErr, context.Canceled) {
            cancelledErr := exception.FromError(openErr)
            logger.Warning(
                "database open cancelled by the caller's context",
                exception.LogContext(
                    cancelledErr,
                    exception.LogContextConfig{Extra: map[string]any{"attempt": attempt}},
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
                    exception.LogContextConfig{Extra: map[string]any{"attempt": attempt}},
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
                    exception.LogContextConfig{Extra: map[string]any{"attempt": attempt, "maxAttempts": maxAttempts}},
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
                exception.LogContextConfig{
                    Extra: map[string]any{
                        "attempt":     attempt,
                        "maxAttempts": maxAttempts,
                        "retryIn":     delay.String(),
                    },
                },
            ),
        )

        /* the sleep watches the caller's context alongside the clock: a shutdown signal arriving mid-retry would otherwise sleep through the whole remaining budget, and the second signal exits with no teardown at all */
        delayTimer := time.NewTimer(delay)
        select {
        case <-ctx.Done():
            delayTimer.Stop()

            /* the same clean stop as the cancelled-open branch, reached while this attempt waits out its backoff: it is recorded and marked here, since an unmarked cancellation travelling up is filed at error by whichever writer meets it. The cause stays the cancellation, which the classification upstream reads, and the failure being retried travels structured beside it through LogContext. */
            cancelledErr := exception.NewError(
                "database connection retry cancelled by the caller's context",
                exception.LogContext(
                    exception.FromError(openErr),
                    exception.LogContextConfig{Extra: map[string]any{"attempt": attempt}},
                ),
                ctx.Err(),
            )

            logger.Warning(
                "database connection retry cancelled by the caller's context",
                exception.LogContext(cancelledErr, exception.LogContextConfig{}),
            )

            return nil, exception.MarkLogged(cancelledErr)
        case <-delayTimer.C:
        }
    }
}

func (instance *Provider) open(ctx context.Context, params bunorm.ConnectionParameters, logger loggingcontract.Logger) (*bun.DB, error) {
    /* an already-cancelled context is refused before the attempt, so a shutdown-cancelled lazy open surfaces as the cancellation rather than as a failed connection naming the database */
    if ctxErr := ctx.Err(); nil != ctxErr {
        return nil, exception.NewError(
            "database open cancelled before the attempt",
            nil,
            ctxErr,
        )
    }

    /* an empty database or user is refused by name before the driver sees it, since pgdriver.WithDatabase and pgdriver.WithUser panic on an empty string. An empty host is refused too: the address ":port" is the local system to a dialer, so an unset host would connect to whatever listens there with the configured credentials. An empty password is legitimate. */
    if "" == params.Database {
        return nil, exception.NewError("pgsql database open refused: the database name is empty", params.SafeContext(), nil)
    }

    if "" == params.User {
        return nil, exception.NewError("pgsql database open refused: the user is empty", params.SafeContext(), nil)
    }

    if "" == params.Host {
        return nil, exception.NewError("pgsql database open refused: the host is empty", params.SafeContext(), nil)
    }

    address := dialAddressOf(params.Host, portOrDefault(params.Port))

    /* every deadline the driver applies is named here, since pgdriver's own defaults would otherwise cap the connect timeout and cut long queries; an unlimited read or write deadline reaches pgdriver as the zero it reads as none */
    connectorOptions := []pgdriver.Option{
        pgdriver.WithAddr(address),
        pgdriver.WithDatabase(params.Database),
        pgdriver.WithUser(params.User),
        pgdriver.WithPassword(params.Password),
        pgdriver.WithDialTimeout(instance.timeout.connectTimeout),
        pgdriver.WithReadTimeout(driverDuration(instance.timeout.readTimeout)),
        pgdriver.WithWriteTimeout(driverDuration(instance.timeout.writeTimeout)),
    }

    if nil != instance.tlsConfig {
        connectorOptions = append(connectorOptions, pgdriver.WithTLSConfig(instance.tlsConfig))
    } else if true == instance.insecure {
        /* pgdriver.WithInsecure(true) disables TLS entirely */
        connectorOptions = append(connectorOptions, pgdriver.WithInsecure(true))
    } else {
        /* do NOT hand this case to pgdriver.WithInsecure(false): despite the name, pgdriver implements it as tls.Config{InsecureSkipVerify: true} — TLS is negotiated but the server certificate is never checked, so the default connection is trivially machine-in-the-middled. Build a verifying config instead: the system roots, and the configured host as the name to verify against. Callers that genuinely want an unverified session set TlsConfig or Insecure explicitly. */
        connectorOptions = append(connectorOptions, pgdriver.WithTLSConfig(&tls.Config{
            ServerName: tlsServerNameOf(params.Host),
            MinVersion: tls.VersionTLS12,
        }))
    }

    connector := pgdriver.NewConnector(connectorOptions...)

    if nil != instance.postBuildHook {
        hookContext, hookCancel := context.WithTimeout(ctx, instance.timeout.connectTimeout)
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

    sqlDatabase.SetMaxOpenConns(instance.pool.openConnectionCount())
    sqlDatabase.SetMaxIdleConns(instance.pool.idleConnectionCount())
    sqlDatabase.SetConnMaxLifetime(driverDuration(instance.pool.connectionMaxLifetime))
    sqlDatabase.SetConnMaxIdleTime(driverDuration(instance.pool.connectionMaxIdleTime))

    database := bun.NewDB(
        sqlDatabase,
        dialectWithDefaultSchema{
            Dialect: pgdialect.New(),
        },
    )

    pingContext, pingCancel := context.WithTimeout(ctx, instance.timeout.connectTimeout)
    defer pingCancel()

    pingErr := database.PingContext(pingContext)
    if nil != pingErr {
        _ = database.Close()

        return nil, exception.NewError(
            "database connection failed",
            instance.toConnectionContext(params, connector.Config().Addr),
            pingErr,
        )
    }

    return database, nil
}

func (instance *Provider) toConnectionContext(
    params bunorm.ConnectionParameters,
    dialedAddress string,
) exceptioncontract.Context {
    return map[string]any{
        "connection":    params.SafeContext(),
        "poolConfig":    instance.pool.context(),
        "timeoutConfig": instance.timeout.context(),
        /* the endpoint the dial actually reached, which is not always the configured one: the connection config is built from the parameters BEFORE the post-build hook runs, and the hook may rewrite the address — it is handed the very field the hook tests read. Named separately rather than folded into the connection, so a record where the two differ says so. */
        "dialedAddress": dialedAddress,
    }
}

func (instance *Provider) computeBackoffDelay(attempt int) time.Duration {
    initialDelay := instance.retry.initialDelay
    maxDelay := instance.retry.maxDelay

    /* the first attempt waits the initial delay, so the growth is over the attempts already made; a non-positive attempt reads as the first */
    if 1 > attempt {
        attempt = 1
    }

    /* the growth is computed in closed form and capped in float space before the conversion: a large attempt count overflows the float64 to int64 conversion into a negative duration that slips past a > cap, and the not-less-than form keeps an infinite growth capped too */
    maxDelayFloat := float64(maxDelay)
    delay := float64(initialDelay) * math.Pow(instance.retry.backoffMultiplier, float64(attempt-1))

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

/* isTransientSqlState answers on the class of a SQLSTATE the server sent: 08 (connection exception), 53 (insufficient resources — too many connections) and 57 (operator intervention — the database system is starting up, or shutting down) are the server saying it cannot take the connection NOW; every other class with an identity — 3D000 for an unknown database, 28P01 for a refused password, a permission — is the server saying no, and a message that happens to carry a transient marker does not make it an outage. */
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

/* unreachable files an open failure the transient classifier admitted — and the retry budget could not get past — under bunorm.ErrDatabaseUnreachable, the class a read/write splitter absorbs. The exception keeps its message and its context; the link sits under it as the cause, so a journal renders the class once, where the driver failure stood, and errors.As still reaches that failure through it. */
func unreachable(openErr error) *exception.Error {
    failure := exception.FromError(openErr)

    return exception.NewError(failure.Message(), failure.Context(), bunorm.DatabaseUnreachable(failure.CauseErr()))
}

/* isTransientError answers whether an open failure is worth another attempt and, on the last attempt, whether it is filed as unreachable. A failure carrying the server's identity is classified on that identity before any message is read, since the server quotes operands such as a database named "timeout" in its message; without one, the net.Error checks and the markers decide. */
const transientErrorLinkLimit = 64

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

    /* an open failure can come from the application, a hook or a verifier, and its chain may reach itself; each pointer link is read once */
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

const defaultPort = "5432"

func portOrDefault(port string) string {
    if "" == port {
        return defaultPort
    }

    return port
}

/* dialAddressOf joins the host and the port the way a dialer reads them: a host carrying a colon is an IPv6 literal and is bracketed unless it already is, and a host name or an IPv4 literal is joined as it is. */
func dialAddressOf(host string, port string) string {
    if true == strings.Contains(host, ":") && false == strings.HasPrefix(host, "[") {
        return "[" + host + "]:" + port
    }

    return host + ":" + port
}

/* tlsServerNameOf answers the name a certificate is verified against for the configured host: a bracketed IPv6 literal loses its brackets and a scoped one its zone, since no certificate carries either and a name keeping them can never verify, while a host name or a plain address is answered as it is */
func tlsServerNameOf(host string) string {
    name := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
    if zone := strings.IndexByte(name, '%'); -1 != zone {
        name = name[:zone]
    }

    return name
}
