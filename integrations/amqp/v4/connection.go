package amqp

import (
    "context"
    "crypto/tls"
    "errors"
    "net"
    neturl "net/url"
    "os"
    "strings"
    "sync"
    "time"
    "unicode"

    "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* ProviderConfig declares how the provider opens and closes connections and what the transports and backplanes it builds inherit. A limit outside its rule is refused at NewProvider naming the field. */
type ProviderConfig struct {
    /* Heartbeat is the interval the client and the broker prove the connection alive at: 10s when left out, at least 1s since the client reads a shorter one as the server's choice, unlimited refused since the client has no form without one. */
    Heartbeat bound.Duration

    /* Reconnect is the backoff every transport and backplane built through the provider starts from, each field left out the package default. */
    Reconnect ReconnectConfig

    /* CloseTimeout bounds each stretch of a close that cannot observe its signal: 30s when left out, at least 1ms, unlimited refused, since a teardown is never unbounded. Every connection the provider opens carries it, and the transports and backplanes built through the provider inherit it. */
    CloseTimeout bound.Duration

    /* DialTimeout bounds one dial and its handshake within the caller's context: 30s when left out, at least 1ms, unlimited refused. It replaces the dsn's connection_timeout, which only the client's own dial reads. */
    DialTimeout bound.Duration

    /* TlsConfig is the TLS session of an amqps dsn, copied at NewProvider and cloned for every dial; nil leaves the client's default, and a dsn that is not amqps beside it is refused. */
    TlsConfig *tls.Config

    /* ConnectionName is what the broker's management view shows for each connection: at most 255 bytes with no control character; empty leaves the client's. */
    ConnectionName string
}

const connectionNameMaximumLength = 255

var providerHeartbeatRule = bound.Rule[time.Duration]{
    Name:    "ProviderConfig.Heartbeat",
    Default: 10 * time.Second,
    Minimum: time.Second,
}

var providerCloseTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ProviderConfig.CloseTimeout",
    Default: closeJoinTimeout,
    Minimum: bound.TimeoutMinimum,
}

var providerDialTimeoutRule = bound.Rule[time.Duration]{
    Name:    "ProviderConfig.DialTimeout",
    Default: 30 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

/* NewProvider resolves the configuration once; a limit outside its rule panics naming the field, and a namespace outside config.ValidateNamespace naming the argument. The namespace is the application's: a backplane built through the provider derives its default exchange from it, <namespace>.sse. */
func NewProvider(namespace string, config ProviderConfig) *Provider {
    if validateErr := melodyconfig.ValidateNamespace(namespace); nil != validateErr {
        refusal := exception.FromError(validateErr)
        refusal.SetContextValue("argument", "namespace")

        exception.Panic(refusal)
    }

    heartbeat, heartbeatErr := bound.Resolve(config.Heartbeat, providerHeartbeatRule)
    if nil != heartbeatErr {
        exception.Panic(exception.FromError(heartbeatErr))
    }

    reconnect, reconnectErr := resolveReconnectOver(defaultReconnect, config.Reconnect)
    if nil != reconnectErr {
        exception.Panic(exception.FromError(reconnectErr))
    }

    closeTimeout, closeTimeoutErr := bound.Resolve(config.CloseTimeout, providerCloseTimeoutRule)
    if nil != closeTimeoutErr {
        exception.Panic(exception.FromError(closeTimeoutErr))
    }

    dialTimeout, dialTimeoutErr := bound.Resolve(config.DialTimeout, providerDialTimeoutRule)
    if nil != dialTimeoutErr {
        exception.Panic(exception.FromError(dialTimeoutErr))
    }

    if false == validConnectionName(config.ConnectionName) {
        exception.Panic(exception.NewError(
            "amqp connection name must be at most 255 bytes with no control character",
            map[string]any{"field": "ProviderConfig.ConnectionName", "length": len(config.ConnectionName)},
            nil,
        ))
    }

    var tlsConfig *tls.Config
    if nil != config.TlsConfig {
        tlsConfig = config.TlsConfig.Clone()
    }

    return &Provider{
        heartbeat:      heartbeat.Value,
        dialTimeout:    dialTimeout.Value,
        tlsConfig:      tlsConfig,
        connectionName: config.ConnectionName,
        parent: componentParent{
            namespace:    namespace,
            reconnect:    reconnect,
            closeTimeout: closeTimeout.Value,
        },
    }
}

func validConnectionName(name string) bool {
    if connectionNameMaximumLength < len(name) {
        return false
    }

    for _, character := range name {
        if true == unicode.IsControl(character) {
            return false
        }
    }

    return true
}

type Provider struct {
    heartbeat      time.Duration
    dialTimeout    time.Duration
    tlsConfig      *tls.Config
    connectionName string
    parent         componentParent
}

/* componentParent is what a transport or a backplane takes for the fields its config leaves out. */
type componentParent struct {
    namespace    string
    reconnect    bound.ResolvedBackoff
    closeTimeout time.Duration
}

func resolveInheritedDuration(limit bound.Duration, parent time.Duration, rule bound.Rule[time.Duration]) (time.Duration, error) {
    if true == limit.IsAbsent() {
        return parent, nil
    }

    resolved, resolveErr := bound.Resolve(limit, rule)
    if nil != resolveErr {
        return 0, resolveErr
    }

    return resolved.Value, nil
}

var defaultComponentParent = componentParent{
    namespace:    melodyconfig.DefaultNamespace,
    reconnect:    defaultReconnect,
    closeTimeout: closeJoinTimeout,
}

func (instance *Provider) NewTransport(queue string, registry *MessageRegistry, source ConnectionSource, config TransportConfig) *Transport {
    return newTransport(queue, registry, source, config, instance.parent)
}

func (instance *Provider) NewServerSentEventBackplane(hub *melodyhttp.ServerSentEventHub, source ConnectionSource, config ServerSentEventBackplaneConfig) *ServerSentEventBackplane {
    return newServerSentEventBackplane(hub, source, config, instance.parent)
}

/* Open dials one connection under the runtime's context, bounded by DialTimeout, and answers its owner, which carries the provider's CloseTimeout; connection.Raw() reaches the client's connection. */
func (instance *Provider) Open(runtimeInstance containercontract.Runtime, dsn string) (*Connection, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance, "amqp.Provider.Open"); nil != nilErr {
        return nil, nilErr
    }

    if dsnErr := instance.refuseDsn(dsn); nil != dsnErr {
        return nil, dsnErr
    }

    return instance.dial(runtimeInstance.Context(), dsn)
}

/* Dialer answers the dial a reconnecting component runs on a goroutine of its own, outside any runtime, under the context the component cancels when it stops waiting; a dsn the provider would refuse is refused here, at wiring. */
func (instance *Provider) Dialer(dsn string) Dialer {
    if dsnErr := instance.refuseDsn(dsn); nil != dsnErr {
        exception.Panic(exception.FromError(dsnErr))
    }

    return func(dialContext context.Context) (*Connection, error) {
        return instance.dial(dialContext, dsn)
    }
}

func (instance *Provider) refuseDsn(dsn string) error {
    if "" == dsn {
        return exception.NewError("amqp dsn is empty", nil, nil)
    }

    if nil != instance.tlsConfig && false == strings.EqualFold("amqps", dsnScheme(dsn)) {
        return exception.NewError(
            "amqp tls configuration is handed for a dsn that is not amqps",
            map[string]any{"field": "ProviderConfig.TlsConfig", "dsn": redactDsn(dsn)},
            nil,
        )
    }

    return nil
}

func dsnScheme(dsn string) string {
    scheme, _, found := strings.Cut(dsn, "://")
    if false == found {
        return ""
    }

    return scheme
}

/* dial runs the client's dial over a socket dialled under the context: the handshake observes no context, so the socket carries the context's deadline and is closed when the context ends first. */
func (instance *Provider) dial(dialContext context.Context, dsn string) (*Connection, error) {
    if nil == dialContext.Err() {
        boundedContext, cancel := context.WithTimeout(dialContext, instance.dialTimeout)
        defer cancel()

        return instance.dialWithin(boundedContext, dsn)
    }

    return nil, exception.NewError(
        "amqp connection refused: the caller's context is done",
        map[string]any{"dsn": redactDsn(dsn)},
        context.Cause(dialContext),
    )
}

func (instance *Provider) dialWithin(boundedContext context.Context, dsn string) (*Connection, error) {
    var socketMutex sync.Mutex
    var stopSocketClose []func() bool

    config := instance.clientConfig(func(network string, address string) (net.Conn, error) {
        socket, socketErr := (&net.Dialer{}).DialContext(boundedContext, network, address)
        if nil != socketErr {
            return nil, socketErr
        }

        deadline, _ := boundedContext.Deadline()
        if deadlineErr := socket.SetDeadline(deadline); nil != deadlineErr {
            _ = socket.Close()

            return nil, deadlineErr
        }

        socketMutex.Lock()
        stopSocketClose = append(stopSocketClose, context.AfterFunc(boundedContext, func() {
            _ = socket.Close()
        }))
        socketMutex.Unlock()

        return socket, nil
    })

    raw, dialErr := amqp091.DialConfig(dsn, config)

    socketMutex.Lock()
    socketClosedByContext := false
    for _, stop := range stopSocketClose {
        if false == stop() {
            socketClosedByContext = true
        }
    }
    socketMutex.Unlock()

    if nil == dialErr && true == socketClosedByContext {
        _ = raw.CloseDeadline(time.Now())
        raw = nil
        dialErr = boundedContext.Err()
    }

    if nil != dialErr {
        cause := redactDialError(dialErr)

        /* the socket carries the context's deadline, so its timeout can surface before the context's own timer fires, and the client does not always wrap it */
        contextCause := context.Cause(boundedContext)
        deadline, hasDeadline := boundedContext.Deadline()
        if nil == contextCause && (true == errors.Is(dialErr, os.ErrDeadlineExceeded) || (true == hasDeadline && false == time.Now().Before(deadline))) {
            contextCause = context.DeadlineExceeded
        }

        if nil != contextCause {
            cause = errors.Join(contextCause, cause)
        }

        return nil, exception.NewError(
            "amqp connection failed",
            map[string]any{"dsn": redactDsn(dsn)},
            cause,
        )
    }

    return newOwnedConnection(raw, instance.parent.closeTimeout), nil
}

/* clientConfig keeps the client's own properties beside the connection name, since a non-empty table replaces them, and hands the client a clone of the TLS session, which the client writes the host into. */
func (instance *Provider) clientConfig(dial func(network string, address string) (net.Conn, error)) amqp091.Config {
    config := amqp091.Config{Heartbeat: instance.heartbeat, Dial: dial}

    if nil != instance.tlsConfig {
        config.TLSClientConfig = instance.tlsConfig.Clone()
    }

    if "" != instance.connectionName {
        properties := amqp091.NewConnectionProperties()
        properties.SetClientConnectionName(instance.connectionName)
        config.Properties = properties
    }

    return config
}

/* redactedDsnPlaceholder stands in for any dsn this function cannot prove it has stripped credentials from. */
const redactedDsnPlaceholder = "(redacted)"

/* redactDsn strips the password from a dsn for logging. It fails closed: a string that does not parse as a url with a scheme and a host becomes the placeholder, because net/url parses "guest:guest@host" into a scheme of "guest" with no userinfo, which would leak the password verbatim; and so does a dsn carrying an "@" the parser did not read as userinfo, because a password holding an unescaped "/", "?" or "#" ends the authority early and lands in the host, the path or the fragment, the part before it read as a host and a port. */
func redactDsn(dsn string) string {
    parsed, parseErr := neturl.Parse(dsn)
    if nil != parseErr {
        return redactedDsnPlaceholder
    }

    if "" == parsed.Scheme || "" == parsed.Host {
        return redactedDsnPlaceholder
    }

    if nil == parsed.User && true == strings.Contains(dsn, "@") {
        return redactedDsnPlaceholder
    }

    if nil != parsed.User {
        parsed.User = neturl.User(parsed.User.Username())
    }

    return parsed.String()
}

/* redactDialError strips any raw dsn embedded in a dial error before it is wrapped as a cause and logged: amqp091.DialConfig surfaces a net/url parse failure as a *url.Error whose Error() quotes the entire dsn, password included. The url is replaced with redactDsn's fail-closed redaction, which yields the placeholder for a dsn that cannot parse. */
func redactDialError(dialErr error) error {
    var urlErr *neturl.Error
    if true == exception.ChainAs(dialErr, &urlErr) {
        return &neturl.Error{
            Op:  urlErr.Op,
            URL: redactDsn(urlErr.URL),
            Err: redactUrlCause(urlErr.Err),
        }
    }

    return dialErr
}

/* redactUrlCause keeps none of the dsn bytes net/url embeds in its parse-error values: url.EscapeError carries the offending percent-escape triple, which may be two password characters, url.InvalidHostError the offending host byte, and the plain errors quote the offending port, segment or scheme, which a password holding a reserved character turns into password bytes. Only the two typed causes are named, by a fixed text; every other cause becomes "invalid dsn". */
func redactUrlCause(cause error) error {
    var escapeErr neturl.EscapeError
    if true == exception.ChainAs(cause, &escapeErr) {
        return errors.New("invalid percent-escape in the redacted dsn")
    }

    var invalidHostErr neturl.InvalidHostError
    if true == exception.ChainAs(cause, &invalidHostErr) {
        return errors.New("invalid host in the redacted dsn")
    }

    return errors.New("invalid dsn")
}
