package config

import (
    melodymailer "github.com/precision-soft/melody/v3/mailer"
)

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
