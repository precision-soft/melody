package config

import (
    melodymailer "github.com/precision-soft/melody/v3/mailer"
)

/* @info the demo SMTP endpoint a STARTTLS-capable server would listen on; kept here so swapping the transport below is a one-line change */
const demoSmtpAddress = "localhost:1025"

/* @info the dev environment ships no SMTP server, so the example wires the LogTransport — every send is written to the request logger (recipients, subject, both the text and HTML bodies, and per-attachment metadata) instead of being delivered, which makes melody:cron-style demos runnable out of the box. Swap in NewSmtpTransport(SmtpConfig{Address: demoSmtpAddress}) to deliver for real. The nil logger makes the transport resolve the request-scoped logger from the runtime at send time. */
func (instance *Module) buildMailer() {
    instance.mailer = melodymailer.NewManager(
        melodymailer.NewLogTransport(nil),
    )
}
