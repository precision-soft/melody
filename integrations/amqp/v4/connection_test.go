package amqp

import (
    "context"
    "crypto/tls"
    "errors"
    "fmt"
    neturl "net/url"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

func newTestTransportConfig() TransportConfig {
    return TransportConfig{}
}

func TestNewTransport_DefaultReconnectAndBuffer(t *testing.T) {
    instance := NewTransport("orders", NewMessageRegistry(), refusingSource(), newTestTransportConfig())

    if defaultReconnect != instance.reconnect {
        t.Fatalf("expected the default reconnect, got %+v", instance.reconnect)
    }

    if 16 != instance.publishReturnBuffer {
        t.Fatalf("expected the default publish return buffer 16, got %d", instance.publishReturnBuffer)
    }

    if defaultPrefetch != instance.prefetch || 30*time.Second != instance.callTimeout || closeJoinTimeout != instance.joinBound {
        t.Fatalf("expected prefetch 10, call timeout 30s and close timeout 30s, got %d / %s / %s", instance.prefetch, instance.callTimeout, instance.joinBound)
    }
}

func TestNewTransport_PerTransportOverride(t *testing.T) {
    config := newTestTransportConfig()
    config.Reconnect = ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(5 * time.Second)}}
    config.PublishReturnBuffer = bound.Of(64)
    config.Prefetch = bound.Of(7)
    config.CallTimeout = bound.Of(3 * time.Second)
    config.CloseTimeout = bound.Of(4 * time.Second)

    instance := NewTransport("orders", NewMessageRegistry(), refusingSource(), config)

    if 5*time.Second != instance.reconnect.Initial {
        t.Fatalf("expected overridden initial backoff 5s, got %s", instance.reconnect.Initial)
    }

    if 64 != instance.publishReturnBuffer || 7 != instance.prefetch || 3*time.Second != instance.callTimeout || 4*time.Second != instance.joinBound {
        t.Fatalf("expected the given values to reach the transport, got %d / %d / %s / %s", instance.publishReturnBuffer, instance.prefetch, instance.callTimeout, instance.joinBound)
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
        {name: "call timeout zero", mutate: func(config *TransportConfig) { config.CallTimeout = bound.Of(time.Duration(0)) }, field: "TransportConfig.CallTimeout"},
        {name: "call timeout below the minimum", mutate: func(config *TransportConfig) { config.CallTimeout = bound.Of(time.Microsecond) }, field: "TransportConfig.CallTimeout"},
        {name: "call timeout unlimited", mutate: func(config *TransportConfig) { config.CallTimeout = bound.UnlimitedDuration() }, field: "TransportConfig.CallTimeout"},
        {name: "close timeout zero", mutate: func(config *TransportConfig) { config.CloseTimeout = bound.Of(time.Duration(0)) }, field: "TransportConfig.CloseTimeout"},
        {name: "close timeout unlimited", mutate: func(config *TransportConfig) { config.CloseTimeout = bound.UnlimitedDuration() }, field: "TransportConfig.CloseTimeout"},
        {name: "reconnect initial above the cap", mutate: func(config *TransportConfig) {
            config.Reconnect = ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Minute)}}
        }, field: "ReconnectConfig.Backoff.Initial"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            config := newTestTransportConfig()
            testCase.mutate(&config)

            expectPanicNamingField(t, testCase.field, func() {
                NewTransport("orders", NewMessageRegistry(), refusingSource(), config)
            })
        })
    }
}

func TestConnectionSource_TheZeroValueIsRefusedNamingBothDoors(t *testing.T) {
    for name, build := range map[string]func(){
        "transport":                 func() { NewTransport("orders", NewMessageRegistry(), ConnectionSource{}, TransportConfig{}) },
        "transport through provider": func() { NewProvider("melody", ProviderConfig{}).NewTransport("orders", NewMessageRegistry(), ConnectionSource{}, TransportConfig{}) },
        "backplane": func() {
            NewServerSentEventBackplane(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), ConnectionSource{}, ServerSentEventBackplaneConfig{})
        },
        "backplane through provider": func() {
            NewProvider("melody", ProviderConfig{}).NewServerSentEventBackplane(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}), ConnectionSource{}, ServerSentEventBackplaneConfig{})
        },
    } {
        t.Run(name, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError {
                    t.Fatal("expected the zero source refused")
                }

                context := exception.LogContext(recoveredErr, exception.LogContextConfig{})
                if "source" != context["argument"] || "amqp.FromConnection, amqp.FromDialer" != context["doors"] {
                    t.Fatalf("expected the refusal to name the argument and both doors, got %v", context)
                }
            }()

            build()
        })
    }
}

func TestNewTransport_RefusesAMissingRequiredArgumentByName(t *testing.T) {
    cases := []struct {
        name     string
        queue    string
        registry *MessageRegistry
        argument string
    }{
        {"an empty queue", "", NewMessageRegistry(), "queue"},
        {"a nil registry", "orders", nil, "registry"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            expectPanicNamingTheArgument(t, testCase.argument, func() {
                NewTransport(testCase.queue, testCase.registry, FromDialer(refusingDialer), TransportConfig{})
            })
        })
    }
}

func TestNewTransport_APrefetchAtTheWireLimitIsAdmitted(t *testing.T) {
    config := newTestTransportConfig()
    config.Prefetch = bound.Of(maxPrefetch)

    if instance := NewTransport("orders", NewMessageRegistry(), refusingSource(), config); maxPrefetch != instance.prefetch {
        t.Fatalf("expected prefetch %d, got %d", maxPrefetch, instance.prefetch)
    }
}

func TestNewProvider_ResolvesEveryFieldToItsDefault(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{})

    if 10*time.Second != provider.heartbeat {
        t.Fatalf("expected the 10s heartbeat, got %s", provider.heartbeat)
    }

    if defaultReconnect != provider.parent.reconnect || closeJoinTimeout != provider.parent.closeTimeout {
        t.Fatalf("expected the default reconnect and close timeout, got %+v", provider.parent)
    }
}

func TestNewProvider_AGivenConfigReachesTheProvider(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{
        Heartbeat:    bound.Of(time.Second),
        Reconnect:    ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(time.Minute), Factor: 3}},
        CloseTimeout: bound.Of(5 * time.Second),
    })

    if time.Second != provider.heartbeat || 5*time.Second != provider.parent.closeTimeout {
        t.Fatalf("expected heartbeat 1s and close timeout 5s, got %s / %s", provider.heartbeat, provider.parent.closeTimeout)
    }

    if (bound.ResolvedBackoff{Initial: 2 * time.Second, Max: time.Minute, Factor: 3}) != provider.parent.reconnect {
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
        {name: "reconnect factor below one", config: ProviderConfig{Reconnect: ReconnectConfig{Backoff: bound.Backoff{Factor: 0.5}}}, field: "ReconnectConfig.Backoff.Factor"},
        {name: "reconnect initial above the cap", config: ProviderConfig{Reconnect: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Minute)}}}, field: "ReconnectConfig.Backoff.Initial"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            expectPanicNamingField(t, testCase.field, func() {
                NewProvider("melody", testCase.config)
            })
        })
    }
}

func TestProviderNewTransport_GeneralLayerInherited(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{Reconnect: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(time.Minute)}}, CloseTimeout: bound.Of(7 * time.Second)})

    instance := provider.NewTransport("orders", NewMessageRegistry(), refusingSource(), newTestTransportConfig())

    if 2*time.Second != instance.reconnect.Initial {
        t.Fatalf("expected general initial backoff 2s, got %s", instance.reconnect.Initial)
    }

    if time.Minute != instance.reconnect.Max {
        t.Fatalf("expected general max backoff 1m, got %s", instance.reconnect.Max)
    }

    if 2.0 != instance.reconnect.Factor {
        t.Fatalf("expected default backoff factor 2.0, got %v", instance.reconnect.Factor)
    }

    if 7*time.Second != instance.joinBound {
        t.Fatalf("expected the provider's close timeout 7s, got %s", instance.joinBound)
    }
}

func TestProviderNewTransport_TransportOverridesGeneral(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{Reconnect: ReconnectConfig{Backoff: bound.Backoff{Initial: bound.Of(2 * time.Second), Max: bound.Of(time.Minute)}}, CloseTimeout: bound.Of(7 * time.Second)})

    config := newTestTransportConfig()
    config.Reconnect = ReconnectConfig{Backoff: bound.Backoff{Max: bound.Of(10 * time.Second)}}
    config.CloseTimeout = bound.Of(3 * time.Second)

    instance := provider.NewTransport("orders", NewMessageRegistry(), refusingSource(), config)

    if 2*time.Second != instance.reconnect.Initial {
        t.Fatalf("expected inherited initial backoff 2s, got %s", instance.reconnect.Initial)
    }

    if 10*time.Second != instance.reconnect.Max {
        t.Fatalf("expected overridden max backoff 10s, got %s", instance.reconnect.Max)
    }

    if 3*time.Second != instance.joinBound {
        t.Fatalf("expected the transport's close timeout 3s, got %s", instance.joinBound)
    }
}

func TestProviderNewTransport_ACapLoweredUnderTheProvidersInitialIsRefusedNamingBoth(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{})

    config := newTestTransportConfig()
    config.Reconnect = ReconnectConfig{Backoff: bound.Backoff{Max: bound.Of(500 * time.Millisecond)}}

    expectPanicNamingField(t, "ReconnectConfig.Backoff.Initial", func() {
        provider.NewTransport("orders", NewMessageRegistry(), refusingSource(), config)
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

    provider := NewProvider("melody", ProviderConfig{})

    _, openErr := provider.Open(newReconnectRuntime(t.Context()), dsn)
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
    provider := NewProvider("melody", ProviderConfig{})

    for dsn, secret := range map[string]string{
        "amqp://localhost:1/Pass@broker/":       "Pass",
        "amqp://melody.invalid:#secret@broker/": "secret",
    } {
        _, openErr := provider.Open(newReconnectRuntime(t.Context()), dsn)
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
func TestConnection_CloseArmsADeadlineOnAWedgedConnection(t *testing.T) {
    dsn := amqpDsnOrSkip(t)
    connection, gated := dialGated(t, dsn)

    gated.Wedge()

    go func() { _ = NewConnection(connection, ConnectionConfig{}).CloseWithContext(context.Background()) }()

    awaitArmedDeadline(t, gated)
}

func TestConnection_CloseWithContextReturnsWithinTheCallersDeadlineWhileTheClientShutdownIsStalled(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    deadline := 300 * time.Millisecond
    closeContext, cancel := context.WithTimeout(context.Background(), deadline)
    defer cancel()

    closed := make(chan error, 1)
    go func() {
        closed <- NewConnection(wedge.connection, ConnectionConfig{}).CloseWithContext(closeContext)
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

func TestConnection_CloseWithContextAnswersTheClientsOwnTimeoutWhenNoShutdownIsInProgress(t *testing.T) {
    fake := dialFakeBroker(t)

    closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

    closeErr := NewConnection(fake.connection, ConnectionConfig{}).CloseWithContext(closeContext)
    if nil == closeErr || true == strings.Contains(closeErr.Error(), "did not return within the bound") {
        t.Fatalf("expected the client's own answer to an unanswered close, got %v", closeErr)
    }
}

/* the fake broker proposes no heartbeat, so the connection runs the client's: the one the provider installed */
func TestProvider_OpenInstallsTheGivenHeartbeat(t *testing.T) {
    address := listenFakeBroker(t)

    provider := NewProvider("melody", ProviderConfig{Heartbeat: bound.Of(3 * time.Second)})

    connection, openErr := provider.Open(newReconnectRuntime(t.Context()), "amqp://guest:guest@"+address+"/")
    if nil != openErr {
        t.Fatalf("open the fake broker: %v", openErr)
    }
    t.Cleanup(func() { _ = connection.Raw().CloseDeadline(time.Now()) })

    if 3*time.Second != connection.Raw().Config.Heartbeat {
        t.Fatalf("expected the given 3s heartbeat on the connection, got %s", connection.Raw().Config.Heartbeat)
    }
}

/* the fake broker reads nothing after its handshake, so a close is never answered and returns at the bound: the provider's close timeout the owner carries, not the 30s default; the second close answers the first's error at once */
func TestConnection_CloseWithContextIsIdempotentAndCarriesTheProvidersTimeout(t *testing.T) {
    address := listenFakeBroker(t)

    provider := NewProvider("melody", ProviderConfig{CloseTimeout: bound.Of(200 * time.Millisecond)})

    connection, openErr := provider.Open(newReconnectRuntime(t.Context()), "amqp://guest:guest@"+address+"/?heartbeat=0")
    if nil != openErr {
        t.Fatalf("open the fake broker: %v", openErr)
    }

    started := time.Now()
    firstErr := connection.CloseWithContext(context.Background())

    if elapsed := time.Since(started); 5*time.Second <= elapsed {
        t.Fatalf("expected the close bounded by the provider's 200ms close timeout and a grace, it took %s", elapsed)
    }

    started = time.Now()
    secondErr := connection.CloseWithContext(context.Background())

    if elapsed := time.Since(started); 50*time.Millisecond <= elapsed || fmt.Sprint(firstErr) != fmt.Sprint(secondErr) {
        t.Fatalf("expected the second close to answer the first's error at once, got %v after %s (first %v)", secondErr, elapsed, firstErr)
    }
}

func TestConnection_ANilOwnerAndANilRawCloseAsNothing(t *testing.T) {
    var nilOwner *Connection
    if closeErr := nilOwner.CloseWithContext(context.Background()); nil != closeErr || nil != nilOwner.Raw() {
        t.Fatalf("expected a nil owner to close as nothing, got %v", closeErr)
    }

    if closeErr := (&Connection{}).CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("expected an owner with no connection to close as nothing, got %v", closeErr)
    }
}

func TestNewConnection_RefusesANilConnectionAndACloseTimeoutItsRuleRefuses(t *testing.T) {
    expectPanicNamingTheArgument(t, "raw", func() {
        NewConnection(nil, ConnectionConfig{})
    })

    for name, limit := range map[string]bound.Duration{"below the minimum": bound.Of(time.Microsecond), "unlimited": bound.UnlimitedDuration()} {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, "ConnectionConfig.CloseTimeout", func() {
                NewConnection(&amqp091.Connection{}, ConnectionConfig{CloseTimeout: limit})
            })
        })
    }
}

func TestProviderOpen_RefusesANilRuntimeNamingTheDoor(t *testing.T) {
    _, openErr := NewProvider("melody", ProviderConfig{}).Open(nil, "amqp://guest:guest@127.0.0.1:1/")
    if nil == openErr || false == strings.Contains(fmt.Sprint(exception.LogContext(openErr, exception.LogContextConfig{})), "amqp.Provider.Open") {
        t.Fatalf("expected the nil runtime refused naming the door, got %v", openErr)
    }
}

func TestProviderOpen_ADoneContextRefusesBeforeTheDial(t *testing.T) {
    address, accepts := listenSilently(t)

    doneContext, cancel := context.WithCancel(context.Background())
    cancel()

    _, openErr := NewProvider("melody", ProviderConfig{}).Open(newReconnectRuntime(doneContext), "amqp://guest:guest@"+address+"/")
    if nil == openErr || false == strings.Contains(openErr.Error(), "the caller's context is done") || false == errors.Is(openErr, context.Canceled) {
        t.Fatalf("expected the done context refused before the dial with its cause, got %v", openErr)
    }

    time.Sleep(50 * time.Millisecond)

    if 0 != accepts.Load() {
        t.Fatalf("expected no dial under a done context, the listener accepted %d", accepts.Load())
    }
}

func TestProviderOpen_AContextEndedDuringTheHandshakeReturnsAtOnce(t *testing.T) {
    address, accepts := listenSilently(t)

    dialContext, cancel := context.WithCancel(context.Background())
    go func() {
        for 0 == accepts.Load() {
            time.Sleep(5 * time.Millisecond)
        }

        cancel()
    }()

    started := time.Now()
    _, openErr := NewProvider("melody", ProviderConfig{}).Open(newReconnectRuntime(dialContext), "amqp://guest:guest@"+address+"/")

    if elapsed := time.Since(started); 5*time.Second <= elapsed {
        t.Fatalf("expected the open to return when the context ended, it took %s", elapsed)
    }

    if nil == openErr || false == strings.Contains(openErr.Error(), "amqp connection failed") || false == errors.Is(openErr, context.Canceled) {
        t.Fatalf("expected the failed handshake to carry the context's cause, got %v", openErr)
    }
}

func TestProviderConfig_DialTimeoutRule(t *testing.T) {
    if 30*time.Second != NewProvider("melody", ProviderConfig{}).dialTimeout {
        t.Fatalf("expected a 30s dial timeout by default, got %s", NewProvider("melody", ProviderConfig{}).dialTimeout)
    }

    if bound.TimeoutMinimum != NewProvider("melody", ProviderConfig{DialTimeout: bound.Of(bound.TimeoutMinimum)}).dialTimeout {
        t.Fatal("expected the minimum dial timeout admitted")
    }

    for name, limit := range map[string]bound.Duration{"below the minimum": bound.Of(time.Microsecond), "unlimited": bound.UnlimitedDuration()} {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, "ProviderConfig.DialTimeout", func() {
                NewProvider("melody", ProviderConfig{DialTimeout: limit})
            })
        })
    }
}

func TestProviderOpen_DialTimeoutEndsASilentHandshake(t *testing.T) {
    address, _ := listenSilently(t)

    started := time.Now()
    _, openErr := NewProvider("melody", ProviderConfig{DialTimeout: bound.Of(300 * time.Millisecond)}).Open(newReconnectRuntime(t.Context()), "amqp://guest:guest@"+address+"/")

    if elapsed := time.Since(started); 5*time.Second <= elapsed {
        t.Fatalf("expected the silent handshake ended by the 300ms dial timeout, it took %s", elapsed)
    }

    if nil == openErr || false == errors.Is(openErr, context.DeadlineExceeded) {
        t.Fatalf("expected the dial timeout's cause on the refusal, got %v", openErr)
    }
}

func TestProviderDialer_RunsUnderTheContextHanded(t *testing.T) {
    address, accepts := listenSilently(t)

    dialer := NewProvider("melody", ProviderConfig{}).Dialer("amqp://guest:guest@" + address + "/")

    dialContext, cancel := context.WithCancel(context.Background())
    go func() {
        for 0 == accepts.Load() {
            time.Sleep(5 * time.Millisecond)
        }

        cancel()
    }()

    started := time.Now()
    if _, dialErr := dialer(dialContext); nil == dialErr || false == errors.Is(dialErr, context.Canceled) {
        t.Fatalf("expected the dial ended by its context, got %v", dialErr)
    }

    if elapsed := time.Since(started); 5*time.Second <= elapsed {
        t.Fatalf("expected the dial to return when its context ended, it took %s", elapsed)
    }
}

func TestProviderOpen_TlsConfigAndConnectionNameReachTheClientConfig(t *testing.T) {
    tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
    provider := NewProvider("melody", ProviderConfig{TlsConfig: tlsConfig, ConnectionName: "orders-worker"})

    tlsConfig.MinVersion = tls.VersionTLS10

    config := provider.clientConfig(nil)

    if nil == config.TLSClientConfig || provider.tlsConfig == config.TLSClientConfig || tls.VersionTLS12 != config.TLSClientConfig.MinVersion {
        t.Fatalf("expected a clone of the tls configuration copied at NewProvider handed to the client, got %+v", config.TLSClientConfig)
    }

    if "orders-worker" != config.Properties["connection_name"] || nil == config.Properties["product"] {
        t.Fatalf("expected the connection name beside the client's own properties, got %v", config.Properties)
    }

    if plain := NewProvider("melody", ProviderConfig{}).clientConfig(nil); nil != plain.TLSClientConfig || 0 != len(plain.Properties) {
        t.Fatalf("expected the client's defaults without a tls configuration or a name, got %+v", plain)
    }
}

func TestProvider_TlsConfigWithAPlainDsnIsRefused(t *testing.T) {
    provider := NewProvider("melody", ProviderConfig{TlsConfig: &tls.Config{}})

    _, openErr := provider.Open(newReconnectRuntime(t.Context()), "amqp://guest:guest@127.0.0.1:1/")
    expectErrorNamingField(t, openErr, "ProviderConfig.TlsConfig")

    expectPanicNamingField(t, "ProviderConfig.TlsConfig", func() {
        provider.Dialer("amqp://guest:guest@127.0.0.1:1/")
    })

    provider.Dialer("amqps://guest:guest@127.0.0.1:1/")
}

func TestProviderDialer_RefusesAnEmptyDsnAtWiring(t *testing.T) {
    defer func() {
        if nil == recover() {
            t.Fatal("expected the empty dsn refused when the dialer is built")
        }
    }()

    NewProvider("melody", ProviderConfig{}).Dialer("")
}

func TestProviderConfig_ConnectionNameRule(t *testing.T) {
    for name, connectionName := range map[string]string{"too long": strings.Repeat("a", 256), "a control character": "orders\nworker"} {
        t.Run(name, func(t *testing.T) {
            expectPanicNamingField(t, "ProviderConfig.ConnectionName", func() {
                NewProvider("melody", ProviderConfig{ConnectionName: connectionName})
            })
        })
    }

    NewProvider("melody", ProviderConfig{ConnectionName: strings.Repeat("a", 255)})
}

func TestConnectionSource_ANilConnectionAndANilDialerAreRefusedAtTheDoor(t *testing.T) {
    for door, callback := range map[string]func(){
        "amqp.FromConnection": func() { FromConnection(nil) },
        "amqp.FromConnection with no connection": func() { FromConnection(&Connection{}) },
        "amqp.FromDialer": func() { FromDialer(nil) },
    } {
        t.Run(door, func(t *testing.T) {
            defer func() {
                recoveredErr, isError := recover().(error)
                if false == isError || false == strings.HasPrefix(door, fmt.Sprint(exception.LogContext(recoveredErr, exception.LogContextConfig{})["door"])) {
                    t.Fatalf("expected a refusal naming the door %s, got %v", door, recoveredErr)
                }
            }()

            callback()
        })
    }
}

func TestRedactDialError_ReturnsOnACyclicChain(t *testing.T) {
    assertReturnsOnACyclicChain(t, func() {
        if nil == redactDialError(newCyclicChainError()) {
            t.Error("expected the dial error back")
        }

        if nil == redactUrlCause(newCyclicChainError()) {
            t.Error("expected the fixed text")
        }
    })
}

func TestProvider_TheBackplaneExchangeFollowsTheNamespace(t *testing.T) {
    provider := NewProvider("shop", ProviderConfig{})

    for label, testCase := range map[string]struct {
        build    func(hub *melodyhttp.ServerSentEventHub) *ServerSentEventBackplane
        expected string
    }{
        "built through the provider": {
            build: func(hub *melodyhttp.ServerSentEventHub) *ServerSentEventBackplane {
                return provider.NewServerSentEventBackplane(hub, FromDialer(refusingDialer), ServerSentEventBackplaneConfig{})
            },
            expected: "shop.sse",
        },
        "the package's own constructor": {
            build: func(hub *melodyhttp.ServerSentEventHub) *ServerSentEventBackplane {
                return NewServerSentEventBackplane(hub, FromDialer(refusingDialer), ServerSentEventBackplaneConfig{})
            },
            expected: "melody.sse",
        },
        "an explicit exchange": {
            build: func(hub *melodyhttp.ServerSentEventHub) *ServerSentEventBackplane {
                return provider.NewServerSentEventBackplane(hub, FromDialer(refusingDialer), ServerSentEventBackplaneConfig{Exchange: "legacy.sse"})
            },
            expected: "legacy.sse",
        },
    } {
        backplane := testCase.build(melodyhttp.NewServerSentEventHub(melodyhttp.ServerSentEventHubConfig{}))
        exchange := backplane.exchange
        _ = backplane.CloseWithContext(context.Background())

        if testCase.expected != exchange {
            t.Fatalf("%s: expected the exchange %q, got %q", label, testCase.expected, exchange)
        }
    }
}

func TestNewProvider_AnEmptyNamespaceIsRefused(t *testing.T) {
    for _, namespace := range []string{"", "a.b", "Shop"} {
        expectPanicNamingTheArgument(t, "namespace", func() {
            NewProvider(namespace, ProviderConfig{})
        })
    }
}
