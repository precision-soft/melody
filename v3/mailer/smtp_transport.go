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

    /* DialTimeout bounds the tcp connect, the tls handshake and the server's opening greeting; zero selects defaultSmtpDialTimeout. A server that accepts the connection and then never speaks would otherwise block the sending goroutine and hold the socket forever. */
    DialTimeout time.Duration

    /* Timeout bounds every step of the smtp session after the greeting — the hello, auth, the mail/rcpt/data commands, each chunk of the payload write and the quit — by resetting a per-step deadline before each one; a step is one client call, so the STARTTLS upgrade (command, tls handshake and the hello the client repeats over tls) counts as a single step. The payload is written in fixed-size chunks with the deadline re-armed per chunk, so the ceiling measures progress rather than total transfer time: a large message on a slow-but-alive link completes regardless of its size, while a stalled peer still fails within one Timeout. Zero falls back to DialTimeout, then to defaultSmtpDialTimeout. The greeting only bounds the opening handshake, so a relay that greets promptly then stalls mid-conversation (overloaded, a firewall black-holing traffic after the handshake, a slow-loris on DATA) would otherwise pin the sending goroutine and its socket indefinitely — which matters most when mail is sent inline from a request handler. */
    Timeout time.Duration

    /* DataTerminationTimeout bounds the server's acknowledgment of the message-ending dot — the one reply a relay routinely delays far beyond any other while it runs content inspection and spam scoring; rfc 5321 allows it up to ten minutes. A ceiling as tight as the per-step Timeout would turn a scanning relay into an error after the message may already be queued, inviting a retry and a duplicate delivery. Zero derives four times the resolved per-step Timeout, raised to at least two minutes. */
    DataTerminationTimeout time.Duration
}

const defaultSmtpDialTimeout = 30 * time.Second

const smtpDataTerminationTimeoutMinimum = 2 * time.Minute

const smtpClientLocalName = "localhost"

func resolveSmtpCommandTimeout(timeout time.Duration, dialTimeout time.Duration) time.Duration {
    if 0 < timeout {
        return timeout
    }

    if 0 < dialTimeout {
        return dialTimeout
    }

    return defaultSmtpDialTimeout
}

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

    watcherDone := make(chan struct{})
    go watchRuntimeCancellation(runtimeInstance, connection, watcherDone)

    client, clientErr := newSmtpClientWithGreetingDeadline(connection, instance.host, instance.resolveDialTimeout())
    if nil != clientErr {
        close(watcherDone)

        return exception.NewError("smtp dial failed", map[string]any{"address": instance.address}, clientErr)
    }

    defer client.Close()
    defer close(watcherDone)

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

    if deadlineErr := connection.SetDeadline(time.Now().Add(instance.dataTerminationTimeout)); nil != deadlineErr {
        return exception.NewError("smtp set session deadline failed", map[string]any{"address": instance.address}, deadlineErr)
    }

    if closeErr := writer.Close(); nil != closeErr {
        return exception.NewError("smtp payload flush failed", map[string]any{"address": instance.address}, closeErr)
    }

    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning(
                "smtp session deadline reset failed after the message was accepted; skipping quit",
                exception.LogContext(deadlineErr, map[string]any{"address": instance.address}),
            )
        }

        return nil
    }

    if quitErr := client.Quit(); nil != quitErr {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {

            logger.Warning(
                "smtp quit failed after the message was accepted",
                exception.LogContext(quitErr, map[string]any{"address": instance.address}),
            )
        }
    }

    return nil
}

const smtpPayloadChunkSize = 32 * 1024

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

func (instance *SmtpTransport) resetSessionDeadline(connection net.Conn) error {
    if deadlineErr := connection.SetDeadline(time.Now().Add(instance.commandTimeout)); nil != deadlineErr {
        return exception.NewError("smtp set session deadline failed", map[string]any{"address": instance.address}, deadlineErr)
    }

    return nil
}

func watchRuntimeCancellation(runtimeInstance runtimecontract.Runtime, connection net.Conn, done <-chan struct{}) {
    select {
    case <-runtimeInstance.Context().Done():
        connection.Close()
    case <-done:
    }
}

func (instance *SmtpTransport) resolveDialTimeout() time.Duration {
    if 0 >= instance.dialTimeout {
        return defaultSmtpDialTimeout
    }

    return instance.dialTimeout
}

func (instance *SmtpTransport) connect(ctx context.Context) (net.Conn, error) {
    dialer := &net.Dialer{Timeout: instance.resolveDialTimeout()}

    if true == instance.implicitTls {
        tlsDialer := &tls.Dialer{NetDialer: dialer, Config: instance.resolveTlsConfig()}

        return tlsDialer.DialContext(ctx, "tcp", instance.address)
    }

    return dialer.DialContext(ctx, "tcp", instance.address)
}

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
