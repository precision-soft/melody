package mailer

import (
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
        address:  config.Address,
        host:     host,
        username: config.Username,
        password: config.Password,
    }
}

type SmtpConfig struct {
    Address  string
    Host     string
    Username string
    Password string
}

type SmtpTransport struct {
    address  string
    host     string
    username string
    password string
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

    var auth smtp.Auth
    if "" != instance.username {
        auth = smtp.PlainAuth("", instance.username, instance.password, instance.host)
    }

    sendErr := smtp.SendMail(instance.address, auth, message.From.Email, recipientList, payload)
    if nil != sendErr {
        return exception.NewError("smtp send failed", map[string]any{"address": instance.address}, sendErr)
    }

    return nil
}

func hostFromAddress(address string) string {
    host, _, splitErr := net.SplitHostPort(address)
    if nil != splitErr {
        return address
    }

    return host
}

var _ mailercontract.Transport = (*SmtpTransport)(nil)
