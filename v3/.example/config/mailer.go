package config

import (
    melodymailer "github.com/precision-soft/melody/v3/mailer"
)

/* the dev environment ships no SMTP server, so the example wires the LogTransport, which writes every send to the request logger instead of delivering it; set SMTP_ADDRESS to deliver through a real server. The nil logger makes the transport resolve the request-scoped logger at send time. */
func (instance *Module) buildMailer() {
    smtpAddress := instance.environmentValue(environmentKeySmtpAddress)
    if "" != smtpAddress {
        instance.mailer = melodymailer.NewManager(
            melodymailer.NewSmtpTransport(melodymailer.SmtpConfig{
                Address: smtpAddress,
            }),
        )

        return
    }

    instance.mailer = melodymailer.NewManager(
        melodymailer.NewLogTransport(nil),
    )
}
