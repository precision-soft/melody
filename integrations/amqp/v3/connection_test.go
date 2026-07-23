package amqp

import (
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

/* @info net/url parses "guest:guest@host" as scheme "guest" with no userinfo, so the old redaction returned it verbatim and the password reached the connection-failure log. Redaction must fail closed. */
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

/* @info amqp091.DialConfig surfaces a malformed dsn as a *url.Error whose Error() quotes the entire raw dsn, password included. Provider.Open must scrub that before wrapping it as the exception cause, or exception.LogContext prints the secret on every connection and reconnect failure. */
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

/* @info net/url embeds dsn fragments inside the *url.Error cause, not just its URL field: a url.EscapeError carries the offending percent-escape triple — the two password characters that follow a literal '%' — and url.Error.Error() renders that value verbatim. Copying urlErr.Err through untouched leaks the password even after the URL field is redacted, so redactDialError must scrub the inner cause too. */
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

/* @info A well-formed dsn keeps its shape so the log stays useful; only the password goes. */
func TestRedactDsn_KeepsWellFormedDsnWithoutThePassword(t *testing.T) {
    redacted := redactDsn("amqp://guest:secret@rabbitmq:5672/vhost")

    if true == strings.Contains(redacted, "secret") {
        t.Fatalf("the password leaked: %q", redacted)
    }
    if false == strings.Contains(redacted, "rabbitmq:5672") || false == strings.Contains(redacted, "guest") {
        t.Fatalf("expected the host and username to survive redaction, got %q", redacted)
    }
}
