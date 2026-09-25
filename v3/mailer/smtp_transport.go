package mailer

import (
    "context"
    "crypto/tls"
    "io"
    "net"
    "net/smtp"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewSmtpTransport(config SmtpConfig) *SmtpTransport {
    host := config.Host
    if "" == host {
        host = hostFromAddress(config.Address)
    }

    commandTimeout := resolveSmtpCommandTimeout(config.Timeout, config.DialTimeout)

    return &SmtpTransport{
        address:                config.Address,
        host:                   host,
        username:               config.Username,
        password:               config.Password,
        requireTls:             config.RequireTls,
        requireAuth:            config.RequireAuth,
        implicitTls:            config.ImplicitTls,
        tlsConfig:              config.TlsConfig,
        dialTimeout:            config.DialTimeout,
        commandTimeout:         commandTimeout,
        dataTerminationTimeout: resolveSmtpDataTerminationTimeout(config.DataTerminationTimeout, commandTimeout),
    }
}

type SmtpConfig struct {
    Address     string
    Host        string
    Username    string
    Password    string
    RequireTls  bool
    RequireAuth bool
    ImplicitTls bool
    TlsConfig   *tls.Config

    /* DialTimeout bounds the tcp connect, the tls handshake and the server's opening greeting; zero selects defaultSmtpDialTimeout. */
    DialTimeout time.Duration

    /* Timeout bounds every step of the session after the greeting (hello, auth, mail, rcpt, data, each chunk of the payload write, quit) by re-arming a per-step deadline; the STARTTLS upgrade is one step. The payload is written in chunks with the deadline re-armed per chunk, so a slow but live link completes while a stalled peer fails within one Timeout. Zero falls back to DialTimeout, then defaultSmtpDialTimeout. */
    Timeout time.Duration

    /* DataTerminationTimeout bounds the server's acknowledgment of the message-ending dot, which a relay delays while it scans content and RFC 5321 allows up to ten minutes; failing it early would invite a retry of a message the relay may have queued. Zero derives four times the per-step Timeout, raised to at least two minutes. */
    DataTerminationTimeout time.Duration
}

/* defaultSmtpDialTimeout is the ceiling on connect + handshake + greeting when the caller does not set one. */
const defaultSmtpDialTimeout = 30 * time.Second

/* smtpDataTerminationTimeoutMinimum floors the derived dot-acknowledgment ceiling, so a tight per-step Timeout still leaves a scanning relay a realistic acceptance window. */
const smtpDataTerminationTimeoutMinimum = 2 * time.Minute

/* smtpClientLocalName is the client name sent in the hello, the same default net/smtp uses when the hello is left implicit. */
const smtpClientLocalName = "localhost"

/* resolveSmtpCommandTimeout selects the per-step deadline: an explicit Timeout, else the DialTimeout, else the package default. */
func resolveSmtpCommandTimeout(timeout time.Duration, dialTimeout time.Duration) time.Duration {
    if 0 < timeout {
        return timeout
    }

    if 0 < dialTimeout {
        return dialTimeout
    }

    return defaultSmtpDialTimeout
}

/* resolveSmtpDataTerminationTimeout selects the dot-acknowledgment ceiling: an explicit value wins, else four per-step timeouts with the two-minute floor apply. */
func resolveSmtpDataTerminationTimeout(dataTerminationTimeout time.Duration, commandTimeout time.Duration) time.Duration {
    if 0 < dataTerminationTimeout {
        return dataTerminationTimeout
    }

    derived := 4 * commandTimeout
    if smtpDataTerminationTimeoutMinimum > derived {
        return smtpDataTerminationTimeoutMinimum
    }

    return derived
}

type SmtpTransport struct {
    address                string
    host                   string
    username               string
    password               string
    requireTls             bool
    requireAuth            bool
    implicitTls            bool
    tlsConfig              *tls.Config
    dialTimeout            time.Duration
    commandTimeout         time.Duration
    dataTerminationTimeout time.Duration
}

func (instance *SmtpTransport) Send(runtimeInstance runtimecontract.Runtime, message mailercontract.Message) error {
    /* the runtime's context drives cancellation, so a nil or typed-nil runtime is refused before the dial */
    if true == internal.IsNilInterface(runtimeInstance) {
        return exception.NewError("runtime may not be nil", nil, nil)
    }

    payload, renderErr := RenderMessage(message)
    if nil != renderErr {
        return renderErr
    }

    recipientList := recipients(message)
    if 0 == len(recipientList) {
        return exception.NewError("mailer message has no recipients", nil, nil)
    }

    return instance.deliver(runtimeInstance, message.From.Email, recipientList, payload)
}

func (instance *SmtpTransport) deliver(runtimeInstance runtimecontract.Runtime, from string, recipientList []string, payload []byte) error {
    connection, dialErr := instance.connect(runtimeInstance.Context())
    if nil != dialErr {
        return exception.NewError("smtp dial failed", map[string]any{"address": instance.address}, dialErr)
    }

    /* net/smtp has no context api, so a cancelled runtime reaches an in-flight read or write only by closing the connection under it; the watcher is armed before the greeting, so the whole session is cancellable */
    watcherDone := make(chan struct{})
    go watchRuntimeCancellation(runtimeInstance, connection, watcherDone)

    client, clientErr := newSmtpClientWithGreetingDeadline(connection, instance.host, instance.resolveDialTimeout())
    if nil != clientErr {
        close(watcherDone)

        return exception.NewError("smtp dial failed", map[string]any{"address": instance.address}, clientErr)
    }

    /* close(watcherDone) is registered last so it runs first, stopping the watcher before client.Close() closes the connection */
    defer client.Close()
    defer close(watcherDone)

    /* the hello runs as its own step under a fresh deadline, so it never shares one with the command that would trigger it implicitly */
    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
    }

    if helloErr := client.Hello(smtpClientLocalName); nil != helloErr {
        return exception.NewError("smtp hello failed", map[string]any{"address": instance.address}, helloErr)
    }

    if false == instance.implicitTls {
        if upgradeErr := instance.startTls(client, connection); nil != upgradeErr {
            return upgradeErr
        }
    }

    if true == instance.requireAuth && "" == instance.username {
        return exception.NewError(
            "smtp authentication is required but no username is configured",
            map[string]any{"address": instance.address},
            nil,
        )
    }

    if "" != instance.username {
        supported, _ := client.Extension("AUTH")
        if false == supported {
            /* configured credentials fail closed: a server that does not advertise AUTH cannot take them, and skipping the auth would send the message anonymously while reporting success; the common trigger is a relay advertising AUTH only after STARTTLS */
            return exception.NewError(
                "smtp server does not advertise AUTH while credentials are configured",
                map[string]any{"address": instance.address},
                nil,
            )
        }

        if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
            return deadlineErr
        }

        authentication := smtp.PlainAuth("", instance.username, instance.password, instance.host)
        if authErr := client.Auth(authentication); nil != authErr {
            return exception.NewError("smtp auth failed", map[string]any{"address": instance.address}, authErr)
        }
    }

    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
    }

    /* "failed", not "rejected": the cause may be a deadline or the cancellation watcher, not a server verdict */
    if mailErr := client.Mail(from); nil != mailErr {
        return exception.NewError("smtp mail command failed", map[string]any{"from": from}, mailErr)
    }

    for _, recipient := range recipientList {
        if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
            return deadlineErr
        }

        if rcptErr := client.Rcpt(recipient); nil != rcptErr {
            return exception.NewError("smtp rcpt command failed", map[string]any{"recipient": recipient}, rcptErr)
        }
    }

    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
    }

    writer, dataErr := client.Data()
    if nil != dataErr {
        return exception.NewError("smtp data command failed", map[string]any{"address": instance.address}, dataErr)
    }

    if writeErr := instance.writePayload(connection, writer, payload); nil != writeErr {
        return writeErr
    }

    /* the dot acknowledgment gets its own, longer ceiling, since the relay scans content here and a per-step deadline would fail a message it may have queued */
    if deadlineErr := connection.SetDeadline(time.Now().Add(instance.dataTerminationTimeout)); nil != deadlineErr {
        return exception.NewError("smtp set session deadline failed", map[string]any{"address": instance.address}, deadlineErr)
    }

    if closeErr := writer.Close(); nil != closeErr {
        return exception.NewError("smtp payload flush failed", map[string]any{"address": instance.address}, closeErr)
    }

    /* the message is accepted from here, so a later failure is only logged, never returned as an invitation to retry; a deadline that cannot be re-armed skips the quit and lets the deferred close drop the connection */
    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        if logger := logging.LoggerFromRuntime(runtimeInstance); false == internal.IsNilInterface(logger) {
            logger.Warning(
                "smtp session deadline reset failed after the message was accepted; skipping quit",
                exception.LogContext(deadlineErr, map[string]any{"address": instance.address}),
            )
        }

        return nil
    }

    if quitErr := client.Quit(); nil != quitErr {
        if logger := logging.LoggerFromRuntime(runtimeInstance); false == internal.IsNilInterface(logger) {
            /* log-only, since the message is accepted, and the record carries the cause */
            logger.Warning(
                "smtp quit failed after the message was accepted",
                exception.LogContext(quitErr, map[string]any{"address": instance.address}),
            )
        }
    }

    return nil
}

/* smtpPayloadChunkSize is the unit of payload progress the per-step deadline is re-armed for. */
const smtpPayloadChunkSize = 32 * 1024

/* writePayload streams the payload to the DATA writer in fixed-size chunks, re-arming the per-step deadline before each, so a large message on a slow but live link completes while a stalled peer fails within one timeout. */
func (instance *SmtpTransport) writePayload(connection net.Conn, writer io.Writer, payload []byte) error {
    for offset := 0; offset < len(payload); offset += smtpPayloadChunkSize {
        end := offset + smtpPayloadChunkSize
        if end > len(payload) {
            end = len(payload)
        }

        if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
            return deadlineErr
        }

        if _, writeErr := writer.Write(payload[offset:end]); nil != writeErr {
            return exception.NewError("smtp payload write failed", map[string]any{"address": instance.address}, writeErr)
        }
    }

    return nil
}

/* resetSessionDeadline pushes the connection deadline out by commandTimeout before the next session step. */
func (instance *SmtpTransport) resetSessionDeadline(connection net.Conn) error {
    if deadlineErr := connection.SetDeadline(time.Now().Add(instance.commandTimeout)); nil != deadlineErr {
        return exception.NewError("smtp set session deadline failed", map[string]any{"address": instance.address}, deadlineErr)
    }

    return nil
}

/* watchRuntimeCancellation closes the connection when the runtime context is cancelled, unblocking any command in flight; the caller closes done on return, so a completed delivery stops the watcher without a second close. */
func watchRuntimeCancellation(runtimeInstance runtimecontract.Runtime, connection net.Conn, done <-chan struct{}) {
    select {
    case <-runtimeInstance.Context().Done():
        connection.Close()
    case <-done:
    }
}

/* resolveDialTimeout is the ceiling on the connect, the tls handshake and the opening greeting. */
func (instance *SmtpTransport) resolveDialTimeout() time.Duration {
    if 0 >= instance.dialTimeout {
        return defaultSmtpDialTimeout
    }

    return instance.dialTimeout
}

/* connect opens the connection under the runtime context, so a cancelled runtime aborts a connect in flight. The greeting read that follows is a session step, over which the caller arms the cancellation watcher. */
func (instance *SmtpTransport) connect(ctx context.Context) (net.Conn, error) {
    dialer := &net.Dialer{Timeout: instance.resolveDialTimeout()}

    if true == instance.implicitTls {
        tlsDialer := &tls.Dialer{NetDialer: dialer, Config: instance.resolveTlsConfig()}

        return tlsDialer.DialContext(ctx, "tcp", instance.address)
    }

    /* the client is built with instance.host rather than smtp.Dial(address), since startTls uses instance.host for SNI and PlainAuth was built with it, and a Host differing from the Address host would fail with "wrong host name"; the implicit-TLS branch does the same */
    return dialer.DialContext(ctx, "tcp", instance.address)
}

/* newSmtpClientWithGreetingDeadline bounds the opening 220 greeting, which smtp.NewClient reads with no deadline of its own; the deadline is cleared once the greeting is read. */
func newSmtpClientWithGreetingDeadline(connection net.Conn, host string, timeout time.Duration) (*smtp.Client, error) {
    if deadlineErr := connection.SetDeadline(time.Now().Add(timeout)); nil != deadlineErr {
        connection.Close()

        return nil, deadlineErr
    }

    client, clientErr := smtp.NewClient(connection, host)
    if nil != clientErr {
        connection.Close()

        return nil, clientErr
    }

    if clearErr := connection.SetDeadline(time.Time{}); nil != clearErr {
        client.Close()

        return nil, clearErr
    }

    return client, nil
}

/* startTls upgrades the session when the server offers it; the STARTTLS command, the handshake and the repeated hello run as one step under a fresh deadline. */
func (instance *SmtpTransport) startTls(client *smtp.Client, connection net.Conn) error {
    supported, _ := client.Extension("STARTTLS")
    if false == supported {
        if true == instance.requireTls {
            return exception.NewError(
                "smtp server does not offer STARTTLS but tls is required",
                map[string]any{"address": instance.address},
                nil,
            )
        }

        return nil
    }

    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
    }

    if startErr := client.StartTLS(instance.resolveTlsConfig()); nil != startErr {
        return exception.NewError("smtp starttls failed", map[string]any{"address": instance.address}, startErr)
    }

    return nil
}

/* resolveTlsConfig supplies the tls configuration for the implicit-tls dial and the STARTTLS upgrade. A config that sets neither ServerName nor InsecureSkipVerify gets the transport host on a clone, since the handshake requires one and the caller's config may be shared. */
func (instance *SmtpTransport) resolveTlsConfig() *tls.Config {
    if nil == instance.tlsConfig {
        return &tls.Config{ServerName: instance.host}
    }

    if "" == instance.tlsConfig.ServerName && false == instance.tlsConfig.InsecureSkipVerify {
        cloned := instance.tlsConfig.Clone()
        cloned.ServerName = instance.host

        return cloned
    }

    return instance.tlsConfig
}

func hostFromAddress(address string) string {
    host, _, splitErr := net.SplitHostPort(address)
    if nil != splitErr {
        return address
    }

    return host
}

var _ mailercontract.Transport = (*SmtpTransport)(nil)
