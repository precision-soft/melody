package messagehandler

import (
    "github.com/precision-soft/melody/v4/.example/message"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    melodymailer "github.com/precision-soft/melody/v4/mailer"
    melodymailercontract "github.com/precision-soft/melody/v4/mailer/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

/* HandleWelcomeEmail hands the welcome mail to the mailer the application wires and fails the message when the mailer refuses it, so the consumer's policy, a retry or the dead letter, decides instead of a mail counted as sent */
func HandleWelcomeEmail(runtimeInstance melodycontainercontract.Runtime, messageInstance message.WelcomeEmail, envelope melodymessagebuscontract.Envelope) error {
    mailer, resolveErr := melodyruntime.FromRuntime[melodymailercontract.Mailer](runtimeInstance, melodymailer.ServiceMailer)
    if nil != resolveErr {
        return melodyexception.MarkInfrastructureFailure(resolveErr)
    }

    _, sendErr := mailer.Send(runtimeInstance, melodymailercontract.Message{
        From:    melodymailercontract.Address{Name: "Melody Shop", Email: "shop@example.com"},
        To:      []melodymailercontract.Address{{Email: messageInstance.Address}},
        Subject: "Welcome to the Melody shop",
        Text:    "Your account is ready.",
    })
    if nil != sendErr {
        return sendErr
    }

    if logger := melodylogging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Info("welcome email handed to the mailer", loggingcontract.Context{"userId": messageInstance.UserId, "address": messageInstance.Address})
    }

    return nil
}
