package mailer

import (
    "context"
    "crypto/tls"
    "io"
    "net"
    "net/smtp"
    "time"

    "github.com/precision-soft/melody/v3/exception"
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

    /* DialTimeout bounds the tcp connect, the tls handshake and the server's opening greeting; zero selects defaultSmtpDialTimeout. A server that accepts the connection and then never speaks would otherwise block the sending goroutine and hold the socket forever. */
    DialTimeout time.Duration

    /* Timeout bounds every step of the smtp session after the greeting — the hello, auth, the mail/rcpt/data commands, each chunk of the payload write and the quit — by resetting a per-step deadline before each one; a step is one client call, so the STARTTLS upgrade (command, tls handshake and the hello the client repeats over tls) counts as a single step. The payload is written in fixed-size chunks with the deadline re-armed per chunk, so the ceiling measures progress rather than total transfer time: a large message on a slow-but-alive link completes regardless of its size, while a stalled peer still fails within one Timeout. Zero falls back to DialTimeout, then to defaultSmtpDialTimeout. The greeting only bounds the opening handshake, so a relay that greets promptly then stalls mid-conversation (overloaded, a firewall black-holing traffic after the handshake, a slow-loris on DATA) would otherwise pin the sending goroutine and its socket indefinitely — which matters most when mail is sent inline from a request handler. */
    Timeout time.Duration

    /* DataTerminationTimeout bounds the server's acknowledgment of the message-ending dot — the one reply a relay routinely delays far beyond any other while it runs content inspection and spam scoring; rfc 5321 allows it up to ten minutes. A ceiling as tight as the per-step Timeout would turn a scanning relay into an error after the message may already be queued, inviting a retry and a duplicate delivery. Zero derives four times the resolved per-step Timeout, raised to at least two minutes. */
    DataTerminationTimeout time.Duration
}

/* defaultSmtpDialTimeout is the ceiling on connect + handshake + greeting when the caller does not set one. */
const defaultSmtpDialTimeout = 30 * time.Second

/* smtpDataTerminationTimeoutMinimum floors the derived dot-acknowledgment ceiling, so a tight per-step Timeout still leaves a scanning relay a realistic acceptance window. */
const smtpDataTerminationTimeoutMinimum = 2 * time.Minute

/* smtpClientLocalName is the client name sent in the hello, the same default net/smtp uses when the hello is left implicit. */
const smtpClientLocalName = "localhost"

/* resolveSmtpCommandTimeout selects the per-step session deadline: an explicit Timeout wins, else the DialTimeout is reused so a single tunable bounds both the handshake and the conversation, else the package default applies. */
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
    /* the runtime's context drives mid-session cancellation, so a nil runtime is rejected up front instead of reaching the cancellation watcher. */
    if nil == runtimeInstance {
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

    /* net/smtp has no context api, so once the session runs a cancelled runtime context can only reach an in-flight read or write by closing the connection out from under it: the blocked call then returns an error and the session unwinds (the connect itself is context-aware). The watcher is armed before the greeting is read, so the whole session — the greeting included — is cancellable; a relay that accepts the connection and then says nothing no longer pins the sender until the dial timeout. */
    watcherDone := make(chan struct{})
    go watchRuntimeCancellation(runtimeInstance, connection, watcherDone)

    client, clientErr := newSmtpClientWithGreetingDeadline(connection, instance.host, instance.resolveDialTimeout())
    if nil != clientErr {
        close(watcherDone)

        return exception.NewError("smtp dial failed", map[string]any{"address": instance.address}, clientErr)
    }

    /* the deferred order is deliberate: close(watcherDone) is registered last so it runs FIRST, stopping the watcher before client.Close() closes the connection — otherwise a clean delivery would race its own shutdown. */
    defer client.Close()
    defer close(watcherDone)

    /* the hello runs as its own step under a fresh deadline instead of riding lazily on the first client operation: left implicit, it would share one deadline with whichever command triggers it — the STARTTLS extension probe on the plain path, MAIL on the implicit-tls path — and every later client call is one round trip only because the hello is already done. */
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
            if true == instance.requireAuth {
                return exception.NewError(
                    "smtp server does not advertise AUTH but it is required",
                    map[string]any{"address": instance.address},
                    nil,
                )
            }
        } else {
            if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
                return deadlineErr
            }

            auth := smtp.PlainAuth("", instance.username, instance.password, instance.host)
            if authErr := client.Auth(auth); nil != authErr {
                return exception.NewError("smtp auth failed", map[string]any{"address": instance.address}, authErr)
            }
        }
    }

    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
    }

    if mailErr := client.Mail(from); nil != mailErr {
        return exception.NewError("smtp sender rejected", map[string]any{"from": from}, mailErr)
    }

    for _, recipient := range recipientList {
        if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
            return deadlineErr
        }

        if rcptErr := client.Rcpt(recipient); nil != rcptErr {
            return exception.NewError("smtp recipient rejected", map[string]any{"recipient": recipient}, rcptErr)
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

    /* the dot acknowledgment is the step where the relay runs its content inspection, so it gets its own, longer ceiling — a per-step deadline here would report a message the server may already have queued as a failure and invite a duplicate delivery. */
    if deadlineErr := connection.SetDeadline(time.Now().Add(instance.dataTerminationTimeout)); nil != deadlineErr {
        return exception.NewError("smtp set session deadline failed", map[string]any{"address": instance.address}, deadlineErr)
    }

    if closeErr := writer.Close(); nil != closeErr {
        return exception.NewError("smtp payload flush failed", map[string]any{"address": instance.address}, closeErr)
    }

    /* from here the message is accepted: reporting any later failure would invite a retry and a duplicate delivery, so the quit path only ever logs. A deadline that cannot be re-armed also means the quit cannot be bounded — skip it and let the deferred close drop the connection. */
    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning("smtp session deadline reset failed after the message was accepted; skipping quit", map[string]any{"address": instance.address})
        }

        return nil
    }

    if quitErr := client.Quit(); nil != quitErr {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning("smtp quit failed after the message was accepted", map[string]any{"address": instance.address})
        }
    }

    return nil
}

/* smtpPayloadChunkSize is the unit of payload progress the session deadline bounds: the payload is written in chunks of this size with the deadline re-armed before each one, so the ceiling applies to per-chunk progress rather than to the whole transfer. */
const smtpPayloadChunkSize = 32 * 1024

/* writePayload streams the payload to the DATA writer in fixed-size chunks, re-arming the per-step session deadline before each chunk: a single absolute deadline over the whole body would kill a large message on a slow-but-alive link once the total transfer time exceeded the timeout even though bytes kept flowing, while the per-chunk deadline lets a slow-but-steady peer complete regardless of the message size and still cuts a genuinely stalled peer within one timeout. */
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

/* resetSessionDeadline pushes the connection deadline out by commandTimeout before the next session step, so a per-step ceiling governs every command and the payload write rather than only the opening greeting. */
func (instance *SmtpTransport) resetSessionDeadline(connection net.Conn) error {
    if deadlineErr := connection.SetDeadline(time.Now().Add(instance.commandTimeout)); nil != deadlineErr {
        return exception.NewError("smtp set session deadline failed", map[string]any{"address": instance.address}, deadlineErr)
    }

    return nil
}

/* watchRuntimeCancellation closes the connection when the runtime context is cancelled, unblocking any smtp command in flight; done is closed by the caller on return so a completed delivery stops the watcher without closing the connection a second time. */
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

/* connect opens the transport connection under the runtime context, so a cancelled runtime aborts a connect still in flight instead of stalling until the dial timeout. It stops at the connection: the greeting read that follows is a session step, and the caller arms the cancellation watcher over it before reading. */
func (instance *SmtpTransport) connect(ctx context.Context) (net.Conn, error) {
    dialer := &net.Dialer{Timeout: instance.resolveDialTimeout()}

    if true == instance.implicitTls {
        tlsDialer := &tls.Dialer{NetDialer: dialer, Config: instance.resolveTlsConfig()}

        return tlsDialer.DialContext(ctx, "tcp", instance.address)
    }

    /* @important dial the raw connection and build the client with instance.host explicitly, rather than smtp.Dial(address) which derives the client server name from the address host: startTls uses instance.host for the TLS SNI and PlainAuth is constructed with instance.host, so a configured Host that differs from the Address host (dialing by IP, through a tunnel, or a CNAME) must be the server name here too — otherwise smtp.PlainAuth.Start rejects the mismatch with "wrong host name" and authentication can never succeed. The implicit-TLS branch passes instance.host to NewClient the same way. */
    return dialer.DialContext(ctx, "tcp", instance.address)
}

/* newSmtpClientWithGreetingDeadline bounds the server's opening 220 greeting, which smtp.NewClient reads synchronously with no deadline of its own: a server that accepts the tcp connection and then says nothing would pin the sending goroutine and its socket indefinitely. The deadline is cleared once the greeting has been read, so the rest of the session is governed by the caller. */
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

/* startTls upgrades the session when the server offers it; the extension probe is a local lookup (the hello has already run) and the upgrade itself — the STARTTLS command, the tls handshake and the hello the client repeats over tls — runs as one step under a fresh deadline. */
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

/* resolveTlsConfig supplies the tls configuration for both the implicit-tls dial and the STARTTLS upgrade. A user config that sets neither ServerName nor InsecureSkipVerify would fail the STARTTLS handshake ("either ServerName or InsecureSkipVerify must be specified"), so the transport host is filled in on a clone — the caller's config may be shared and is never mutated. */
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
