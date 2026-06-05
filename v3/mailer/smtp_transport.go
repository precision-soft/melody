package mailer

import (
    "crypto/tls"
    "net"
    "net/smtp"

    "github.com/precision-soft/melody/v3/exception"
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
        implicitTls: config.ImplicitTls,
        tlsConfig:   config.TlsConfig,
    }
}

type SmtpConfig struct {
    Address  string
    Host     string
    Username string
    Password string
    /** RequireTls fails the send unless the connection is encrypted: an implicit-TLS dial or a
    successful STARTTLS upgrade. It closes the silent-plaintext downgrade that opportunistic
    STARTTLS leaves open when a man-in-the-middle strips the server's STARTTLS advertisement. */
    RequireTls bool
    /** ImplicitTls dials straight into TLS (the smtps convention, typically port 465) instead of
    connecting in clear text and upgrading with STARTTLS. */
    ImplicitTls bool
    /** TlsConfig overrides the TLS settings; when nil a config pinned to Host as the server name is
    used. Set it to supply custom roots or, for testing only, to relax verification. */
    TlsConfig *tls.Config
}

type SmtpTransport struct {
    address     string
    host        string
    username    string
    password    string
    requireTls  bool
    implicitTls bool
    tlsConfig   *tls.Config
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

    return instance.deliver(message.From.Email, recipientList, payload)
}

func (instance *SmtpTransport) deliver(from string, recipientList []string, payload []byte) error {
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

    if "" != instance.username {
        if supported, _ := client.Extension("AUTH"); true == supported {
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

    return client.Quit()
}

func (instance *SmtpTransport) dial() (*smtp.Client, error) {
    if true == instance.implicitTls {
        connection, dialErr := tls.Dial("tcp", instance.address, instance.resolveTlsConfig())
        if nil != dialErr {
            return nil, dialErr
        }

        return smtp.NewClient(connection, instance.host)
    }

    return smtp.Dial(instance.address)
}

/** startTls upgrades a clear-text connection when the server advertises STARTTLS. When the server
does not advertise it the send is allowed to proceed in plaintext only if RequireTls is false;
otherwise it fails closed rather than leaking the message and credentials over an open connection. */
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
