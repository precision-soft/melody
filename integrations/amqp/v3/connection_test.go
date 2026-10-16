package amqp

import (
    "context"
    "errors"
    "fmt"
    neturl "net/url"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
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

    defaults := DefaultReconnectConfig()
    if defaults.InitialBackoff != instance.reconnect.InitialBackoff || defaults.MaxBackoff != instance.reconnect.MaxBackoff || defaults.BackoffFactor != instance.reconnect.BackoffFactor {
        t.Fatalf("expected default reconnect config, got %+v", instance.reconnect)
    }

    if defaultPublishReturnBuffer != instance.publishReturnBuffer {
        t.Fatalf("expected default publish return buffer %d, got %d", defaultPublishReturnBuffer, instance.publishReturnBuffer)
    }
}

func TestNewTransport_PerTransportOverride(t *testing.T) {
    config := newTestTransportConfig()
    config.Reconnect = &ReconnectConfig{InitialBackoff: 5 * time.Second}
    config.PublishReturnBuffer = 64

    instance := NewTransport(config)

    if 5*time.Second != instance.reconnect.InitialBackoff {
        t.Fatalf("expected overridden initial backoff 5s, got %s", instance.reconnect.InitialBackoff)
    }

    if 64 != instance.publishReturnBuffer {
        t.Fatalf("expected publish return buffer 64, got %d", instance.publishReturnBuffer)
    }
}

func TestProviderNewTransport_GeneralLayerInherited(t *testing.T) {
    provider := NewProvider(WithReconnectConfig(&ReconnectConfig{InitialBackoff: 2 * time.Second, MaxBackoff: time.Minute}))

    instance := provider.NewTransport(newTestTransportConfig())

    if 2*time.Second != instance.reconnect.InitialBackoff {
        t.Fatalf("expected general initial backoff 2s, got %s", instance.reconnect.InitialBackoff)
    }

    if time.Minute != instance.reconnect.MaxBackoff {
        t.Fatalf("expected general max backoff 1m, got %s", instance.reconnect.MaxBackoff)
    }

    if 2.0 != instance.reconnect.BackoffFactor {
        t.Fatalf("expected default backoff factor 2.0, got %v", instance.reconnect.BackoffFactor)
    }
}

func TestProviderNewTransport_TransportOverridesGeneral(t *testing.T) {
    provider := NewProvider(WithReconnectConfig(&ReconnectConfig{InitialBackoff: 2 * time.Second, MaxBackoff: time.Minute}))

    config := newTestTransportConfig()
    config.Reconnect = &ReconnectConfig{MaxBackoff: 10 * time.Second}

    instance := provider.NewTransport(config)

    if 2*time.Second != instance.reconnect.InitialBackoff {
        t.Fatalf("expected inherited initial backoff 2s, got %s", instance.reconnect.InitialBackoff)
    }

    if 10*time.Second != instance.reconnect.MaxBackoff {
        t.Fatalf("expected overridden max backoff 10s, got %s", instance.reconnect.MaxBackoff)
    }
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

    provider := NewProvider()

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

    for key, value := range exception.LogContext(openErr) {
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
        "amqp://app:pa/ss@broker:5672/vh":     redactedDsnPlaceholder,
        "amqp://app:pa?ss@broker:5672/vh":     redactedDsnPlaceholder,
        "amqp://app:pa#ss@broker:5672/vh":     redactedDsnPlaceholder,
        "amqp://localhost:1/Pass@broker/":     redactedDsnPlaceholder,
        "amqp://localhost:#secret@broker/":    redactedDsnPlaceholder,
        "amqp://app:pa%2Fss@broker:5672/vh":   "amqp://app@broker:5672/vh",
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
    provider := NewProvider()

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

        for key, value := range exception.LogContext(openErr) {
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

    go func() { _ = NewProvider().Close(connection) }()

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
        closed <- NewProvider().CloseWithContext(closeContext, wedge.connection)
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

    closeErr := NewProvider().CloseWithContext(closeContext, fake.connection)
    if nil == closeErr || true == strings.Contains(closeErr.Error(), "did not return within the bound") {
        t.Fatalf("expected the client's own answer to an unanswered close, got %v", closeErr)
    }
}
