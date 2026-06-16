package messagehandler

import (
    "github.com/precision-soft/melody/v3/.example/message"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func HandleWelcomeEmail(runtimeInstance melodyruntimecontract.Runtime, messageInstance message.WelcomeEmail) error {
    logger := melodylogging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return nil
    }

    logger.Info(
        "welcome email sent",
        loggingcontract.Context{
            "userId":  messageInstance.UserId,
            "address": messageInstance.Address,
        },
    )

    return nil
}
