package config

import (
    "os"

    melodymailer "github.com/precision-soft/melody/v3/mailer"
)

/* @info the dev environment ships no SMTP server by default, so the example wires the LogTransport — every send is written to the request logger (recipients, subject, both the text and HTML bodies, and per-attachment metadata) instead of being delivered, which makes melody:cron-style demos runnable out of the box. Set SMTP_ADDRESS (for example mailpit:1025) to deliver through a real SMTP server instead, mirroring the other opt-in integrations (REDIS_ADDRESS, AMQP_DSN, ...). The nil logger makes the LogTransport resolve the request-scoped logger from the runtime at send time. */
func (instance *Module) buildMailer() {
    smtpAddress := os.Getenv("SMTP_ADDRESS")
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
