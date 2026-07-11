package mailer

import (
    "crypto/tls"
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
        address:     config.Address,
        host:        host,
        username:    config.Username,
        password:    config.Password,
        requireTls:  config.RequireTls,
        requireAuth: config.RequireAuth,
        implicitTls: config.ImplicitTls,
        tlsConfig:   config.TlsConfig,
        dialTimeout: config.DialTimeout,
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
}

/* defaultSmtpDialTimeout is the ceiling on connect + handshake + greeting when the caller does not set one. */
const defaultSmtpDialTimeout = 30 * time.Second

type SmtpTransport struct {
    address     string
    host        string
    username    string
    password    string
    requireTls  bool
    requireAuth bool
    implicitTls bool
    tlsConfig   *tls.Config
    dialTimeout time.Duration
}

func (instance *SmtpTransport) Send(runtimeInstance runtimecontract.Runtime, message mailercontract.Message) error {
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
    client, dialErr := instance.dial()
    if nil != dialErr {
        return exception.NewError("smtp dial failed", map[string]any{"address": instance.address}, dialErr)
    }
    defer client.Close()

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
            auth := smtp.PlainAuth("", instance.username, instance.password, instance.host)
            if authErr := client.Auth(auth); nil != authErr {
                return exception.NewError("smtp auth failed", map[string]any{"address": instance.address}, authErr)
            }
        }
    }

    if mailErr := client.Mail(from); nil != mailErr {
        return exception.NewError("smtp sender rejected", map[string]any{"from": from}, mailErr)
    }

    for _, recipient := range recipientList {
        if rcptErr := client.Rcpt(recipient); nil != rcptErr {
            return exception.NewError("smtp recipient rejected", map[string]any{"recipient": recipient}, rcptErr)
        }
    }

    writer, dataErr := client.Data()
    if nil != dataErr {
        return exception.NewError("smtp data command failed", map[string]any{"address": instance.address}, dataErr)
    }

    if _, writeErr := writer.Write(payload); nil != writeErr {
        return exception.NewError("smtp payload write failed", map[string]any{"address": instance.address}, writeErr)
    }

    if closeErr := writer.Close(); nil != closeErr {
        return exception.NewError("smtp payload flush failed", map[string]any{"address": instance.address}, closeErr)
    }

    if quitErr := client.Quit(); nil != quitErr {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning("smtp quit failed after the message was accepted", map[string]any{"address": instance.address})
        }
    }

    return nil
}

func (instance *SmtpTransport) dial() (*smtp.Client, error) {
    timeout := instance.dialTimeout
    if 0 >= timeout {
        timeout = defaultSmtpDialTimeout
    }

    dialer := &net.Dialer{Timeout: timeout}

    if true == instance.implicitTls {
        connection, dialErr := tls.DialWithDialer(dialer, "tcp", instance.address, instance.resolveTlsConfig())
        if nil != dialErr {
            return nil, dialErr
        }

        return newSmtpClientWithGreetingDeadline(connection, instance.host, timeout)
    }

    /* @important dial the raw connection and build the client with instance.host explicitly, rather than smtp.Dial(address) which derives the client server name from the address host: startTls uses instance.host for the TLS SNI and PlainAuth is constructed with instance.host, so a configured Host that differs from the Address host (dialing by IP, through a tunnel, or a CNAME) must be the server name here too — otherwise smtp.PlainAuth.Start rejects the mismatch with "wrong host name" and authentication can never succeed. Mirrors the implicit-TLS branch, which already passes instance.host to NewClient. */
    connection, dialErr := dialer.Dial("tcp", instance.address)
    if nil != dialErr {
        return nil, dialErr
    }

    return newSmtpClientWithGreetingDeadline(connection, instance.host, timeout)
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

func (instance *SmtpTransport) resolveTlsConfig() *tls.Config {
    if nil != instance.tlsConfig {
        return instance.tlsConfig
    }

    return &tls.Config{ServerName: instance.host}
}

func hostFromAddress(address string) string {
    host, _, splitErr := net.SplitHostPort(address)
    if nil != splitErr {
        return address
    }

    return host
}

var _ mailercontract.Transport = (*SmtpTransport)(nil)
