package mailer

import (
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

    return &SmtpTransport{
        address:        config.Address,
        host:           host,
        username:       config.Username,
        password:       config.Password,
        requireTls:     config.RequireTls,
        requireAuth:    config.RequireAuth,
        implicitTls:    config.ImplicitTls,
        tlsConfig:      config.TlsConfig,
        dialTimeout:    config.DialTimeout,
        commandTimeout: resolveSmtpCommandTimeout(config.Timeout, config.DialTimeout),
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

    /* Timeout bounds every step of the smtp session after the greeting — auth, the mail/rcpt/data commands, each chunk of the payload write and the quit — by resetting a per-step deadline before each one. The payload is written in fixed-size chunks with the deadline re-armed per chunk, so the ceiling measures progress rather than total transfer time: a large message on a slow-but-alive link completes regardless of its size, while a stalled peer still fails within one Timeout. Zero falls back to DialTimeout, then to defaultSmtpDialTimeout. The greeting only bounds the opening handshake, so a relay that greets promptly then stalls mid-conversation (overloaded, a firewall black-holing traffic after the handshake, a slow-loris on DATA) would otherwise pin the sending goroutine and its socket indefinitely — which matters most when mail is sent inline from a request handler. */
    Timeout time.Duration
}

/* defaultSmtpDialTimeout is the ceiling on connect + handshake + greeting when the caller does not set one. */
const defaultSmtpDialTimeout = 30 * time.Second

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

type SmtpTransport struct {
    address        string
    host           string
    username       string
    password       string
    requireTls     bool
    requireAuth    bool
    implicitTls    bool
    tlsConfig      *tls.Config
    dialTimeout    time.Duration
    commandTimeout time.Duration
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
    client, connection, dialErr := instance.dial()
    if nil != dialErr {
        return exception.NewError("smtp dial failed", map[string]any{"address": instance.address}, dialErr)
    }
    defer client.Close()

    /* net/smtp has no context api, so a cancelled runtime context can only reach an in-flight command by closing the connection out from under it: the blocked read or write then returns an error and the session unwinds. The watcher is torn down before the deferred Close runs, so a clean delivery never races its own shutdown. */
    watcherDone := make(chan struct{})
    defer close(watcherDone)
    go watchRuntimeCancellation(runtimeInstance, connection, watcherDone)

    /* the session deadline is armed before the first command regardless of the tls mode: the client sends its initial EHLO lazily on its first operation (Extension, Auth or Mail) and the greeting deadline has already been cleared, so on the implicit-tls path that hello would otherwise run with no deadline at all. */
    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
    }

    if false == instance.implicitTls {
        if upgradeErr := instance.startTls(client); nil != upgradeErr {
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

    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
    }

    if closeErr := writer.Close(); nil != closeErr {
        return exception.NewError("smtp payload flush failed", map[string]any{"address": instance.address}, closeErr)
    }

    if deadlineErr := instance.resetSessionDeadline(connection); nil != deadlineErr {
        return deadlineErr
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

func (instance *SmtpTransport) dial() (*smtp.Client, net.Conn, error) {
    timeout := instance.dialTimeout
    if 0 >= timeout {
        timeout = defaultSmtpDialTimeout
    }

    dialer := &net.Dialer{Timeout: timeout}

    if true == instance.implicitTls {
        connection, dialErr := tls.DialWithDialer(dialer, "tcp", instance.address, instance.resolveTlsConfig())
        if nil != dialErr {
            return nil, nil, dialErr
        }

        client, clientErr := newSmtpClientWithGreetingDeadline(connection, instance.host, timeout)
        if nil != clientErr {
            return nil, nil, clientErr
        }

        return client, connection, nil
    }

    /* @important dial the raw connection and build the client with instance.host explicitly, rather than smtp.Dial(address) which derives the client server name from the address host: startTls uses instance.host for the TLS SNI and PlainAuth is constructed with instance.host, so a configured Host that differs from the Address host (dialing by IP, through a tunnel, or a CNAME) must be the server name here too — otherwise smtp.PlainAuth.Start rejects the mismatch with "wrong host name" and authentication can never succeed. Mirrors the implicit-TLS branch, which already passes instance.host to NewClient. */
    connection, dialErr := dialer.Dial("tcp", instance.address)
    if nil != dialErr {
        return nil, nil, dialErr
    }

    client, clientErr := newSmtpClientWithGreetingDeadline(connection, instance.host, timeout)
    if nil != clientErr {
        return nil, nil, clientErr
    }

    return client, connection, nil
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

func (instance *SmtpTransport) startTls(client *smtp.Client) error {
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
