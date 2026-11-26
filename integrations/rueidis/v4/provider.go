package rueidis

import (
    "context"
    "net"
    "reflect"
    "strconv"
    "strings"
    "time"
    "unicode/utf8"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/redis/rueidis"
)

/* NewProvider resolves the configuration once and keeps it: a limit outside its rule panics naming the field, before any client is built. */
func NewProvider(config ProviderConfig) *Provider {
    resolved, resolveErr := resolveProviderConfig(config)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    return &Provider{
        client: resolved.client,
        retry:  resolved.retry,
        logger: config.Logger,
    }
}

/* Provider opens the redis client a set of connection values names. It holds only client, timeout and retry tuning and the journal the retry reports in; the address, user and password reach it through ConnectionParameters at open time. Handed the values rather than the configuration keys, it knows no key and names no credential, so it carries no marking door. Arming the framework's credential redaction is the application's call, through the registrar's RegisterSecretParameter for a parameter it declares or MarkParameterSecret for one melody registered from the .env artifacts; the mark propagates to every parameter whose template reads the secret. */
type Provider struct {
    client resolvedClient
    retry  resolvedRetry
    logger loggingcontract.Logger
}

/* resolvedLogger is the journal the retry reports in: the configured one, else the emergency logger, so a re-dial is never silent */
func (instance *Provider) resolvedLogger() loggingcontract.Logger {
    if true == isAbsentLogger(instance.logger) {
        return logging.EmergencyLogger()
    }

    return instance.logger
}

/* Open opens the client under the caller's runtime and answers its owner: the runtime's context ends the retry wait and bounds the boot ping, and the owner's CloseWithContext is the one close; connection.Client() reaches the raw client for commands. */
func (instance *Provider) Open(runtimeInstance containercontract.Runtime, parameters ConnectionParameters) (*Connection, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, "rueidis.Provider.Open"); nil != nilErr {
        return nil, nilErr
    }

    openContext := runtimeInstance.Context()
    logger := instance.resolvedLogger()

    attempt := 0
    maxAttempts := instance.retry.maxAttempts

    for {
        if nil != openContext.Err() {
            return nil, exception.NewError(
                "redis connection refused: the caller's context is done",
                instance.connectionContext(parameters),
                context.Cause(openContext),
            )
        }

        attempt = attempt + 1

        client, openErr := instance.open(openContext, parameters)
        if nil == openErr {
            if 1 < attempt {
                logger.Info(
                    "redis connection successful after retry",
                    map[string]interface{}{
                        "attempt": attempt,
                    },
                )
            }

            return NewConnection(client), nil
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

        if waitErr := instance.waitBeforeRetry(openContext, delay, parameters, attempt, maxAttempts, openErr); nil != waitErr {
            return nil, waitErr
        }
    }
}

/* waitBeforeRetry waits the backoff delay unless the caller's context ends first, which is answered with the context's cause and the last dial error */
func (instance *Provider) waitBeforeRetry(
    openContext context.Context,
    delay time.Duration,
    parameters ConnectionParameters,
    attempt int,
    maxAttempts int,
    lastErr error,
) error {
    timer := time.NewTimer(delay)
    defer timer.Stop()

    select {
    case <-timer.C:
        return nil
    case <-openContext.Done():
    }

    waitContext := instance.connectionContext(parameters)
    waitContext["attempt"] = attempt
    waitContext["maxAttempts"] = maxAttempts
    waitContext["lastError"] = boundLastError(lastErr.Error())

    return exception.NewError(
        "redis connection retry ended by the caller's context",
        waitContext,
        context.Cause(openContext),
    )
}

/* maxLastErrorLength bounds the dial error a refusal carries: the error can quote whatever the address answered */
const maxLastErrorLength = 512

func boundLastError(value string) string {
    if maxLastErrorLength >= len(value) {
        return value
    }

    cut := maxLastErrorLength
    for 0 < cut && false == utf8.RuneStart(value[cut]) {
        cut--
    }

    return value[:cut] + "...(truncated " + strconv.Itoa(len(value)-cut) + " bytes)"
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

    /* an open failure can come from the application, a hook or a verifier, and its chain may reach itself; each pointer link is read once */
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

func (instance *Provider) computeBackoffDelay(attempt int) time.Duration {
    return instance.retry.backoff.Delay(attempt)
}

func (instance *Provider) open(openContext context.Context, parameters ConnectionParameters) (rueidis.Client, error) {
    addresses := parseAddressList(parameters.Address)
    if 0 == len(addresses) {
        return nil, exception.NewError(
            "redis address is empty",
            instance.connectionContext(parameters),
            nil,
        )
    }

    client, createErr := rueidis.NewClient(instance.clientOption(addresses, parameters))
    if nil != createErr {
        return nil, exception.NewError(
            "redis client creation failed",
            instance.connectionContext(parameters),
            createErr,
        )
    }

    if true == instance.client.bootPingDisabled {
        return client, nil
    }

    pingContext, pingCancel := context.WithTimeout(openContext, instance.client.bootPingTimeout)
    defer pingCancel()

    pingErr := client.Do(pingContext, client.B().Ping().Build()).Error()
    if nil == pingErr {
        return client, nil
    }

    client.Close()

    return nil, exception.NewError(
        "redis connection failed",
        instance.connectionContext(parameters),
        pingErr,
    )
}

/* clientOption is the one translation of the resolved configuration to the library: the dialer and the write deadline are always installed, so the library's own defaults never govern them. */
func (instance *Provider) clientOption(addresses []string, parameters ConnectionParameters) rueidis.ClientOption {
    option := rueidis.ClientOption{
        InitAddress:      addresses,
        Username:         parameters.User,
        Password:         parameters.Password,
        ClientName:       instance.client.clientName,
        SelectDB:         instance.client.selectDatabase,
        DisableCache:     false == instance.client.clientSideCacheEnabled,
        TLSConfig:        instance.client.tlsConfig,
        Dialer:           net.Dialer{Timeout: instance.client.dialTimeout},
        ConnWriteTimeout: instance.client.connectionWriteTimeout,
    }

    if true == instance.client.clientSideCacheEnabled {
        option.CacheSizeEachConn = instance.client.clientSideCacheBytes
    }

    return option
}

/* connectionContext is the diagnostic shape of every refusal this provider writes, assembled here because only the provider knows the deadlines that governed the attempt; it is the shape the bunorm siblings' toConnectionContext writes. The password is never part of it, and it names no configuration parameter, since this provider is handed values, not keys. */
func (instance *Provider) connectionContext(parameters ConnectionParameters) exceptioncontract.Context {
    connectionContext := parameters.SafeContext()

    connectionContext["bootPingTimeout"] = instance.client.bootPingTimeout.String()
    connectionContext["dialTimeout"] = instance.client.dialTimeout.String()
    connectionContext["selectDatabase"] = instance.client.selectDatabase

    return connectionContext
}

/* Ping runs one round trip under the caller's runtime alone: its context is the only deadline. */
func (instance *Provider) Ping(runtimeInstance containercontract.Runtime, connection *Connection) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, "rueidis.Provider.Ping"); nil != nilErr {
        return nilErr
    }

    if nil == connection || true == clientIsNil(connection.Client()) {
        return exception.NewError(
            "redis connection is nil",
            nil,
            nil,
        )
    }

    client := connection.Client()

    return client.Do(runtimeInstance.Context(), client.B().Ping().Build()).Error()
}

/* isAbsentLogger answers whether a logger is nil, the nil interface or a nil value of a nilable kind behind it */
func isAbsentLogger(logger loggingcontract.Logger) bool {
    if nil == logger {
        return true
    }

    value := reflect.ValueOf(logger)
    switch value.Kind() {
    case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
        return value.IsNil()
    default:
        return false
    }
}
