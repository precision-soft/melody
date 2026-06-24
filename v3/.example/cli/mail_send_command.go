package cli

import (
    "encoding/base64"
    "fmt"
    "html"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* @info a 1x1 transparent PNG standing in for a brand logo; a real application embeds its own asset (for example with go:embed) and attaches it the same way */
const demoLogoPngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func NewMailSendCommand(mailer melodymailercontract.Mailer) *MailSendCommand {
    return &MailSendCommand{
        mailer: mailer,
    }
}

type MailSendCommand struct {
    mailer melodymailercontract.Mailer
}

func (instance *MailSendCommand) Name() string {
    return "mailer:send"
}

func (instance *MailSendCommand) Description() string {
    return "send a demo branded email (HTML body with an inline logo) through the configured mailer"
}

func (instance *MailSendCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{Name: "to", Usage: "recipient email address"},
        &melodyclicontract.StringFlag{Name: "subject", Usage: "email subject"},
        &melodyclicontract.StringFlag{Name: "text", Usage: "plain-text body (also used as the HTML paragraph)"},
    }
}

func (instance *MailSendCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext *melodyclicontract.CommandContext,
) error {
    to := commandContext.String("to")
    if "" == to {
        to = "someone@example.com"
    }

    subject := commandContext.String("subject")
    if "" == subject {
        subject = "Hello from Melody"
    }

    text := commandContext.String("text")
    if "" == text {
        text = "This is a demo email sent through the Melody mailer."
    }

    logo, decodeErr := base64.StdEncoding.DecodeString(demoLogoPngBase64)
    if nil != decodeErr {
        return decodeErr
    }

    sendErr := instance.mailer.Send(runtimeInstance, melodymailercontract.Message{
        From:    melodymailercontract.Address{Name: "Melody Shop", Email: "shop@example.com"},
        To:      []melodymailercontract.Address{{Email: to}},
        Subject: subject,
        Text:    text,
        Html:    "<p><img src=\"cid:logo\" alt=\"Melody\" width=\"48\" height=\"48\"> " + html.EscapeString(text) + "</p>",
        Attachments: []melodymailercontract.Attachment{
            {
                Filename:    "logo.png",
                ContentType: "image/png",
                Content:     logo,
                /* @info a non-empty ContentId embeds the image inline (multipart/related) so the HTML <img src="cid:logo"> resolves in real mail clients */
                ContentId: "logo",
            },
        },
    })
    if nil != sendErr {
        return sendErr
    }

    fmt.Println("sent email to", to)

    return nil
}

var _ melodyclicontract.Command = (*MailSendCommand)(nil)
