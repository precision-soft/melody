package amqp

import (
    "context"
    "errors"
    "fmt"
    "net"
    neturl "net/url"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func newTestTransportConfig() TransportConfig {
    return TransportConfig{
        Dialer:   func() (*amqp091.Connection, error) { return nil, nil },
        Queue:    "orders",
        Registry: NewMessageRegistry(),
    }
}

func TestNewTransport_DefaultReconnectAndBuffer(t *testing.T) {
    instance := NewTransport(newTestTransportConfig())

    if defaultReconnect != instance.reconnect {
        t.Fatalf("expected the default reconnect, got %+v", instance.reconnect)
    }

    if 16 != instance.publishReturnBuffer {
        t.Fatalf("expected the default publish return buffer 16, got %d", instance.publishReturnBuffer)
    }

    if defaultPrefetch != instance.prefetch || 30*time.Second != instance.publishTimeout || closeJoinTimeout != instance.joinBound {
        t.Fatalf("expected prefetch 10, publish timeout 30s and close timeout 30s, got %d / %s / %s", instance.prefetch, instance.publishTimeout, instance.joinBound)
    }
}

func TestNewTransport_PerTransportOverride(t *testing.T) {
    config := newTestTransportConfig()
    config.Reconnect = ReconnectConfig{InitialBackoff: bound.Of(5 * time.Second)}
    config.PublishReturnBuffer = bound.Of(64)
    config.Prefetch = bound.Of(7)
    config.PublishTimeout = bound.Of(3 * time.Second)
    config.CloseTimeout = bound.Of(4 * time.Second)

    instance := NewTransport(config)

    if 5*time.Second != instance.reconnect.initialBackoff {
        t.Fatalf("expected overridden initial backoff 5s, got %s", instance.reconnect.initialBackoff)
    }

    if 64 != instance.publishReturnBuffer || 7 != instance.prefetch || 3*time.Second != instance.publishTimeout || 4*time.Second != instance.joinBound {
        t.Fatalf("expected the given values to reach the transport, got %d / %d / %s / %s", instance.publishReturnBuffer, instance.prefetch, instance.publishTimeout, instance.joinBound)
    }
}

func TestNewTransport_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    cases := []struct {
        name   string
        mutate func(config *TransportConfig)
        field  string
    }{
        {name: "prefetch above the wire limit", mutate: func(config *TransportConfig) { config.Prefetch = bound.Of(maxPrefetch + 1) }, field: "TransportConfig.Prefetch"},
        {name: "prefetch zero", mutate: func(config *TransportConfig) { config.Prefetch = bound.Of(0) }, field: "TransportConfig.Prefetch"},
        {name: "prefetch unlimited", mutate: func(config *TransportConfig) { config.Prefetch = bound.UnlimitedCount() }, field: "TransportConfig.Prefetch"},
        {name: "return buffer zero", mutate: func(config *TransportConfig) { config.PublishReturnBuffer = bound.Of(0) }, field: "TransportConfig.PublishReturnBuffer"},
        {name: "return buffer unlimited", mutate: func(config *TransportConfig) { config.PublishReturnBuffer = bound.UnlimitedCount() }, field: "TransportConfig.PublishReturnBuffer"},
        {name: "publish timeout zero", mutate: func(config *TransportConfig) { config.PublishTimeout = bound.Of(time.Duration(0)) }, field: "TransportConfig.PublishTimeout"},
        {name: "publish timeout below the minimum", mutate: func(config *TransportConfig) { config.PublishTimeout = bound.Of(time.Microsecond) }, field: "TransportConfig.PublishTimeout"},
        {name: "publish timeout unlimited", mutate: func(config *TransportConfig) { config.PublishTimeout = bound.UnlimitedDuration() }, field: "TransportConfig.PublishTimeout"},
        {name: "close timeout zero", mutate: func(config *TransportConfig) { config.CloseTimeout = bound.Of(time.Duration(0)) }, field: "TransportConfig.CloseTimeout"},
        {name: "close timeout unlimited", mutate: func(config *TransportConfig) { config.CloseTimeout = bound.UnlimitedDuration() }, field: "TransportConfig.CloseTimeout"},
        {name: "reconnect initial above the cap", mutate: func(config *TransportConfig) {
            config.Reconnect = ReconnectConfig{InitialBackoff: bound.Of(2 * time.Minute)}
        }, field: "ReconnectConfig.InitialBackoff"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            config := newTestTransportConfig()
            testCase.mutate(&config)

            expectPanicNamingField(t, testCase.field, func() {
                NewTransport(config)
            })
        })
    }
}

func TestNewTransport_AMissingDependencyIsRefusedNamingTheField(t *testing.T) {
    expectPanicNamingField(t, "TransportConfig.Connection", func() {
        NewTransport(TransportConfig{Queue: "orders", Registry: NewMessageRegistry()})
    })

    expectPanicNamingField(t, "TransportConfig.Queue", func() {
        NewTransport(TransportConfig{Dialer: refusingDialer, Registry: NewMessageRegistry()})
    })

    expectPanicNamingField(t, "TransportConfig.Registry", func() {
        NewTransport(TransportConfig{Dialer: refusingDialer, Queue: "orders"})
    })
}

func TestNewTransport_APrefetchAtTheWireLimitIsAdmitted(t *testing.T) {
    config := newTestTransportConfig()
    config.Prefetch = bound.Of(maxPrefetch)

    if instance := NewTransport(config); maxPrefetch != instance.prefetch {
        t.Fatalf("expected prefetch %d, got %d", maxPrefetch, instance.prefetch)
    }
}

func TestNewProvider_ResolvesEveryFieldToItsDefault(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    if 10*time.Second != provider.heartbeat {
        t.Fatalf("expected the 10s heartbeat, got %s", provider.heartbeat)
    }

    if defaultReconnect != provider.parent.reconnect || closeJoinTimeout != provider.parent.closeTimeout {
        t.Fatalf("expected the default reconnect and close timeout, got %+v", provider.parent)
    }
}

func TestNewProvider_AGivenConfigReachesTheProvider(t *testing.T) {
    provider := NewProvider(ProviderConfig{
        Heartbeat:    bound.Of(time.Second),
        Reconnect:    ReconnectConfig{InitialBackoff: bound.Of(2 * time.Second), MaxBackoff: bound.Of(time.Minute), BackoffFactor: 3},
        CloseTimeout: bound.Of(5 * time.Second),
    })

    if time.Second != provider.heartbeat || 5*time.Second != provider.parent.closeTimeout {
        t.Fatalf("expected heartbeat 1s and close timeout 5s, got %s / %s", provider.heartbeat, provider.parent.closeTimeout)
    }

    if (resolvedReconnect{initialBackoff: 2 * time.Second, maxBackoff: time.Minute, backoffFactor: 3}) != provider.parent.reconnect {
        t.Fatalf("expected 2s / 1m / 3, got %+v", provider.parent.reconnect)
    }
}

func TestNewProvider_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    cases := []struct {
        name   string
        config ProviderConfig
        field  string
    }{
        {name: "heartbeat below one second", config: ProviderConfig{Heartbeat: bound.Of(999 * time.Millisecond)}, field: "ProviderConfig.Heartbeat"},
        {name: "heartbeat unlimited", config: ProviderConfig{Heartbeat: bound.UnlimitedDuration()}, field: "ProviderConfig.Heartbeat"},
        {name: "close timeout below the minimum", config: ProviderConfig{CloseTimeout: bound.Of(time.Microsecond)}, field: "ProviderConfig.CloseTimeout"},
        {name: "close timeout unlimited", config: ProviderConfig{CloseTimeout: bound.UnlimitedDuration()}, field: "ProviderConfig.CloseTimeout"},
        {name: "reconnect factor below one", config: ProviderConfig{Reconnect: ReconnectConfig{BackoffFactor: 0.5}}, field: "ReconnectConfig.BackoffFactor"},
        {name: "reconnect initial above the cap", config: ProviderConfig{Reconnect: ReconnectConfig{InitialBackoff: bound.Of(2 * time.Minute)}}, field: "ReconnectConfig.InitialBackoff"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            expectPanicNamingField(t, testCase.field, func() {
                NewProvider(testCase.config)
            })
        })
    }
}

func TestProviderNewTransport_GeneralLayerInherited(t *testing.T) {
    provider := NewProvider(ProviderConfig{Reconnect: ReconnectConfig{InitialBackoff: bound.Of(2 * time.Second), MaxBackoff: bound.Of(time.Minute)}, CloseTimeout: bound.Of(7 * time.Second)})

    instance := provider.NewTransport(newTestTransportConfig())

    if 2*time.Second != instance.reconnect.initialBackoff {
        t.Fatalf("expected general initial backoff 2s, got %s", instance.reconnect.initialBackoff)
    }

    if time.Minute != instance.reconnect.maxBackoff {
        t.Fatalf("expected general max backoff 1m, got %s", instance.reconnect.maxBackoff)
    }

    if 2.0 != instance.reconnect.backoffFactor {
        t.Fatalf("expected default backoff factor 2.0, got %v", instance.reconnect.backoffFactor)
    }

    if 7*time.Second != instance.joinBound {
        t.Fatalf("expected the provider's close timeout 7s, got %s", instance.joinBound)
    }
}

func TestProviderNewTransport_TransportOverridesGeneral(t *testing.T) {
    provider := NewProvider(ProviderConfig{Reconnect: ReconnectConfig{InitialBackoff: bound.Of(2 * time.Second), MaxBackoff: bound.Of(time.Minute)}, CloseTimeout: bound.Of(7 * time.Second)})

    config := newTestTransportConfig()
    config.Reconnect = ReconnectConfig{MaxBackoff: bound.Of(10 * time.Second)}
    config.CloseTimeout = bound.Of(3 * time.Second)

    instance := provider.NewTransport(config)

    if 2*time.Second != instance.reconnect.initialBackoff {
        t.Fatalf("expected inherited initial backoff 2s, got %s", instance.reconnect.initialBackoff)
    }

    if 10*time.Second != instance.reconnect.maxBackoff {
        t.Fatalf("expected overridden max backoff 10s, got %s", instance.reconnect.maxBackoff)
    }

    if 3*time.Second != instance.joinBound {
        t.Fatalf("expected the transport's close timeout 3s, got %s", instance.joinBound)
    }
}

func TestProviderNewTransport_ACapLoweredUnderTheProvidersInitialIsRefusedNamingBoth(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    config := newTestTransportConfig()
    config.Reconnect = ReconnectConfig{MaxBackoff: bound.Of(500 * time.Millisecond)}

    expectPanicNamingField(t, "ReconnectConfig.InitialBackoff", func() {
        provider.NewTransport(config)
    })
}

/* net/url parses "guest:guest@host" as scheme "guest" with no userinfo, so the redaction must fail closed rather than echo the input into the connection-failure log */
func TestRedactDsn_FailsClosedOnDsnWithoutParsableUserinfo(t *testing.T) {
    for _, dsn := range []string{
        "guest:secret@rabbitmq:5672",
        "not a url at all",
        "amqp://",
        "://broken",
    } {
        redacted := redactDsn(dsn)

        if true == strings.Contains(redacted, "secret") {
            t.Fatalf("the password leaked for %q: %q", dsn, redacted)
        }
        if redactedDsnPlaceholder != redacted {
            t.Fatalf("expected %q to redact to the placeholder, got %q", dsn, redacted)
        }
    }
}

/* amqp091.DialConfig surfaces a malformed dsn as a *url.Error whose Error() quotes the entire raw dsn, password included. Provider.Open must scrub that before wrapping it as the exception cause, or exception.LogContext prints the secret on every connection and reconnect failure. */
func TestProviderOpen_DoesNotLeakPasswordThroughDialErrorCause(t *testing.T) {
    const dsn = "amqp://user:sup3r%secret@rabbit:5672/"

    /* Precondition: prove the leak vector is real — the raw dial error quotes the password verbatim. */
    if _, rawErr := amqp091.DialConfig(dsn, amqp091.Config{}); nil == rawErr || false == strings.Contains(rawErr.Error(), "secret") {
        t.Fatalf("precondition: expected the raw dial error to quote the password, got %v", rawErr)
    }

    provider := NewProvider(ProviderConfig{})

    _, openErr := provider.Open(dsn)
    if nil == openErr {
        t.Fatalf("expected Open to fail on the malformed dsn")
    }

    cause := errors.Unwrap(openErr)
    if nil == cause {
        t.Fatalf("expected a wrapped cause")
    }
    if true == strings.Contains(cause.Error(), "secret") {
        t.Fatalf("the password leaked through the wrapped cause: %q", cause.Error())
    }

    for key, value := range exception.LogContext(openErr, exception.LogContextConfig{}) {
        if true == strings.Contains(fmt.Sprintf("%v", value), "secret") {
            t.Fatalf("the password leaked through log field %q: %v", key, value)
        }
    }
}

/* net/url embeds dsn fragments inside the *url.Error cause, not just its URL field: a url.EscapeError carries the offending percent-escape triple — the two password characters that follow a literal '%' — and url.Error.Error() renders that value verbatim. Copying urlErr.Err through untouched leaks the password even after the URL field is redacted, so redactDialError must scrub the inner cause too. */
func TestRedactDialError_ScrubsEscapeErrorPasswordFragment(t *testing.T) {
    dialErr := &neturl.Error{
        Op:  "parse",
        URL: "amqp://guest:pa%ss@rabbit:5672/",
        Err: neturl.EscapeError("%ss"),
    }

    /* Precondition: prove the leak vector is real — the raw url.Error quotes the escape fragment verbatim. */
    if false == strings.Contains(dialErr.Error(), "%ss") {
        t.Fatalf("precondition: expected the raw error to quote the escape fragment, got %q", dialErr.Error())
    }

    redacted := redactDialError(dialErr).Error()

    if true == strings.Contains(redacted, "%ss") {
        t.Fatalf("the percent-escape fragment leaked: %q", redacted)
    }
    if true == strings.Contains(redacted, "ss") {
        t.Fatalf("the password characters leaked: %q", redacted)
    }
    if false == strings.Contains(redacted, redactedDsnPlaceholder) {
        t.Fatalf("expected the redacted url placeholder in %q", redacted)
    }
}

/* A well-formed dsn keeps its shape so the log stays useful; only the password goes. */
func TestRedactDsn_KeepsWellFormedDsnWithoutThePassword(t *testing.T) {
    redacted := redactDsn("amqp://guest:secret@rabbitmq:5672/vhost")

    if true == strings.Contains(redacted, "secret") {
        t.Fatalf("the password leaked: %q", redacted)
    }
    if false == strings.Contains(redacted, "rabbitmq:5672") || false == strings.Contains(redacted, "guest") {
        t.Fatalf("expected the host and username to survive redaction, got %q", redacted)
    }
}

func TestRedactDsn_APasswordHoldingAReservedCharacterYieldsThePlaceholder(t *testing.T) {
    for dsn, expected := range map[string]string{
        "amqp://app:pa/ss@broker:5672/vh":   redactedDsnPlaceholder,
        "amqp://app:pa?ss@broker:5672/vh":   redactedDsnPlaceholder,
        "amqp://app:pa#ss@broker:5672/vh":   redactedDsnPlaceholder,
        "amqp://localhost:1/Pass@broker/":   redactedDsnPlaceholder,
        "amqp://localhost:#secret@broker/":  redactedDsnPlaceholder,
        "amqp://app:pa%2Fss@broker:5672/vh": "amqp://app@broker:5672/vh",
    } {
        if redacted := redactDsn(dsn); expected != redacted {
            t.Fatalf("expected %q to redact to %q, got %q", dsn, expected, redacted)
        }
    }
}

func TestRedactDialError_AParseCauseNeverCarriesTheInput(t *testing.T) {
    for _, dsn := range []string{
        "amqp://app:pa/ss@broker:5672/vh",
        "amqp://app:pa?ss@broker:5672/vh",
        "amqp://app:pa#ss@broker:5672/vh",
    } {
        _, rawErr := amqp091.DialConfig(dsn, amqp091.Config{})
        if nil == rawErr || false == strings.Contains(rawErr.Error(), ":pa") {
            t.Fatalf("precondition: expected the raw dial error of %q to quote the password head, got %v", dsn, rawErr)
        }

        redacted := redactDialError(rawErr)

        var urlErr *neturl.Error
        if false == errors.As(redacted, &urlErr) || "invalid dsn" != urlErr.Err.Error() {
            t.Fatalf("expected the cause of %q replaced by invalid dsn, got %v", dsn, redacted)
        }

        if true == strings.Contains(redacted.Error(), ":pa") {
            t.Fatalf("the password head of %q leaked: %q", dsn, redacted.Error())
        }
    }
}

func TestProvider_OpenRecordsNeitherTheDsnNorItsHeadOnAMisparsedPassword(t *testing.T) {
    provider := NewProvider(ProviderConfig{})

    for dsn, secret := range map[string]string{
        "amqp://localhost:1/Pass@broker/":       "Pass",
        "amqp://melody.invalid:#secret@broker/": "secret",
    } {
        _, openErr := provider.Open(dsn)
        if nil == openErr {
            t.Fatalf("expected Open of %q to fail", dsn)
        }

        var openException *exception.Error
        if false == errors.As(openErr, &openException) || redactedDsnPlaceholder != openException.Context()["dsn"] {
            t.Fatalf("expected the dsn of %q recorded as the placeholder, got %v", dsn, openErr)
        }

        if true == strings.Contains(openErr.Error(), secret) {
            t.Fatalf("the password of %q leaked through the error: %q", dsn, openErr.Error())
        }

        for key, value := range exception.LogContext(openErr, exception.LogContextConfig{}) {
            if true == strings.Contains(fmt.Sprintf("%v", value), secret) {
                t.Fatalf("the password of %q leaked through log field %q: %v", dsn, key, value)
            }
        }
    }
}

/* the close is asserted on the deadline it arms on the socket, not on its return: the return is bounded by closeJoinTimeout, thirty seconds, while a plain close arms nothing and never returns over a wedged socket */
func TestProvider_CloseArmsADeadlineOnAWedgedConnection(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    gated.Wedge()

    go func() { _ = NewProvider(ProviderConfig{}).Close(connection) }()

    awaitArmedDeadline(t, gated)
}

func TestProvider_CloseWithContextReturnsWithinTheCallersDeadlineWhileTheClientShutdownIsStalled(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    deadline := 300 * time.Millisecond
    closeContext, cancel := context.WithTimeout(context.Background(), deadline)
    defer cancel()

    closed := make(chan error, 1)
    go func() {
        closed <- NewProvider(ProviderConfig{}).CloseWithContext(closeContext, wedge.connection)
    }()

    select {
    case closeErr := <-closed:
        if nil == closeErr || false == strings.Contains(closeErr.Error(), "did not return within the bound") {
            t.Fatalf("expected the close that did not return reported as such, got %v", closeErr)
        }
    case <-time.After(deadline + 2*time.Second):
        t.Fatalf("the close did not return within the deadline %s plus two seconds; it is held behind the client's stalled shutdown", deadline)
    }
}

func TestProvider_CloseWithContextAnswersTheClientsOwnTimeoutWhenNoShutdownIsInProgress(t *testing.T) {
    fake := dialFakeBroker(t)

    closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    closeErr := NewProvider(ProviderConfig{}).CloseWithContext(closeContext, fake.connection)
    if nil == closeErr || true == strings.Contains(closeErr.Error(), "did not return within the bound") {
        t.Fatalf("expected the client's own answer to an unanswered close, got %v", closeErr)
    }
}

/* the fake broker proposes no heartbeat, so the connection runs the client's: the one the provider installed */
func TestProvider_OpenInstallsTheGivenHeartbeat(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    t.Cleanup(func() { _ = listener.Close() })

    go func() {
        brokerSide, acceptErr := listener.Accept()
        if nil != acceptErr {
            return
        }
        t.Cleanup(func() { _ = brokerSide.Close() })

        _ = serveFakeBrokerHandshake(brokerSide)
    }()

    provider := NewProvider(ProviderConfig{Heartbeat: bound.Of(3 * time.Second)})

    connection, openErr := provider.Open("amqp://guest:guest@" + listener.Addr().String() + "/")
    if nil != openErr {
        t.Fatalf("open the fake broker: %v", openErr)
    }
    t.Cleanup(func() { _ = connection.CloseDeadline(time.Now()) })

    if 3*time.Second != connection.Config.Heartbeat {
        t.Fatalf("expected the given 3s heartbeat on the connection, got %s", connection.Config.Heartbeat)
    }
}

/* the fake broker reads nothing after its handshake, so a close is never answered and returns at the bound: the provider's close timeout, not the 30s default */
func TestProvider_CloseWithContextReturnsWithinTheProvidersCloseTimeout(t *testing.T) {
    fake := dialFakeBroker(t)

    provider := NewProvider(ProviderConfig{CloseTimeout: bound.Of(200 * time.Millisecond)})

    started := time.Now()
    _ = provider.CloseWithContext(context.Background(), fake.connection)

    if elapsed := time.Since(started); 5*time.Second <= elapsed {
        t.Fatalf("expected the close bounded by the provider's 200ms close timeout and a grace, it took %s", elapsed)
    }
}
