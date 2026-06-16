package cli

import (
    "fmt"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

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
    return "send a demo email through the configured mailer"
}

func (instance *MailSendCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{Name: "to", Usage: "recipient email address"},
        &melodyclicontract.StringFlag{Name: "subject", Usage: "email subject"},
        &melodyclicontract.StringFlag{Name: "text", Usage: "email body"},
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

    sendErr := instance.mailer.Send(runtimeInstance, melodymailercontract.Message{
        From:    melodymailercontract.Address{Name: "Melody Shop", Email: "shop@example.com"},
        To:      []melodymailercontract.Address{{Email: to}},
        Subject: subject,
        Text:    text,
    })
    if nil != sendErr {
        return sendErr
    }

    fmt.Println("sent email to", to)

    return nil
}

var _ melodyclicontract.Command = (*MailSendCommand)(nil)
