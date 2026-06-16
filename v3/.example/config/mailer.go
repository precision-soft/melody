package config

import (
    melodymailer "github.com/precision-soft/melody/v3/mailer"
)

const demoSmtpAddress = "localhost:1025"

func (instance *Module) buildMailer() {
    instance.mailer = melodymailer.NewManager(
        melodymailer.NewSmtpTransport(melodymailer.SmtpConfig{
            Address: demoSmtpAddress,
        }),
    )
}
