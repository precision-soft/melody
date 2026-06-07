package mailer

import (
    "crypto/tls"
    "net"
    "net/smtp"

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
    }
}

type SmtpConfig struct {
    Address  string
    Host     string
    Username string
    Password string
    RequireTls bool
    RequireAuth bool
    ImplicitTls bool
    TlsConfig *tls.Config
}

type SmtpTransport struct {
    address     string
    host        string
    username    string
    password    string
    requireTls  bool
    requireAuth bool
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
    if true == instance.implicitTls {
        connection, dialErr := tls.Dial("tcp", instance.address, instance.resolveTlsConfig())
        if nil != dialErr {
            return nil, dialErr
        }

        return smtp.NewClient(connection, instance.host)
    }

    return smtp.Dial(instance.address)
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
