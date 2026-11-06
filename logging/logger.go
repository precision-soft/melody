package logging

import (
    "log"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

const causeChainMaxDepth = 8

func LevelEnabled(logger loggingcontract.Logger, level loggingcontract.Level) bool {
    levelReporter, isReporter := logger.(loggingcontract.LevelReporter)
    if false == isReporter {
        return true
    }

    return levelReporter.Enabled(level)
}

/* LogError writes nothing for a nil, typed-nil or already logged error; a nil logger uses the standard log package */
func LogError(logger loggingcontract.Logger, err error) {
    if nil == err || true == internal.IsNilInterface(err) {
        return
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if true == exception.ChainAs(err, &alreadyLoggedValue) && false == internal.IsNilInterface(alreadyLoggedValue) {
        if true == alreadyLoggedValue.AlreadyLogged() {
            return
        }
    }

    exceptionValue, isTopException := err.(*exception.Error)
    if true == isTopException {
        levelUpper := strings.ToUpper(string(exceptionValue.Level()))
        enrichedContext := enrichContextWithCause(exceptionValue)

        if nil == logger || true == internal.IsNilInterface(logger) {
            /* this fallback writes through the raw standard logger, so it escapes on its own */
            if 0 < len(enrichedContext) {
                log.Printf("[%s] %s context=%v", levelUpper, internal.EscapeControlCharacters(exceptionValue.Message()), internal.EscapeControlCharacters(renderTextValue(enrichedContext)))
            } else {
                log.Printf("[%s] %s", levelUpper, internal.EscapeControlCharacters(exceptionValue.Message()))
            }

            return
        }

        logger.Log(exceptionValue.Level(), exceptionValue.Message(), enrichedContext)
        return
    }

    /* the message renders under a recover: this path runs from recovery handlers, where Error() may dereference a nil */
    enrichedContext := exception.LogContext(err)
    renderedMessage, isRendered := enrichedContext["error"].(string)
    if false == isRendered || "" == renderedMessage {
        renderedMessage = "the error message could not be rendered"
    }

    delete(enrichedContext, "error")

    if nil == logger || true == internal.IsNilInterface(logger) {
        if 0 < len(enrichedContext) {
            log.Printf("[ERROR] %s context=%v", internal.EscapeControlCharacters(renderedMessage), internal.EscapeControlCharacters(renderTextValue(enrichedContext)))
        } else {
            log.Printf("[ERROR] %s", internal.EscapeControlCharacters(renderedMessage))
        }

        return
    }

    logger.Error(renderedMessage, enrichedContext)
}

func IsValidLevel(value loggingcontract.Level) bool {
    return loggingcontract.LevelDebug == value ||
        loggingcontract.LevelInfo == value ||
        loggingcontract.LevelWarning == value ||
        loggingcontract.LevelError == value ||
        loggingcontract.LevelEmergency == value
}

func priorityForLevel(level loggingcontract.Level) int {
    switch level {
    case loggingcontract.LevelDebug:
        return 0
    case loggingcontract.LevelInfo:
        return 1
    case loggingcontract.LevelWarning:
        return 2
    case loggingcontract.LevelError:
        return 3
    case loggingcontract.LevelEmergency:
        return 4
    default:
        return 0
    }
}

func enrichContextWithCause(exceptionValue *exception.Error) exceptioncontract.Context {
    context := exceptionValue.Context()
    if nil == context {
        context = exceptioncontract.Context{}
    }

    causeErr := exceptionValue.CauseErr()
    if nil == causeErr || true == internal.IsNilInterface(causeErr) {
        return context
    }

    _, hasCause := context["cause"]
    _, hasCauseChain := context["causeChain"]

    causeChain := exception.BuildCauseChain(causeErr, causeChainMaxDepth)
    if 0 < len(causeChain) {
        if false == hasCause {
            context["cause"] = causeChain[0]
        }
        if false == hasCauseChain {
            context["causeChain"] = causeChain
        }
    } else if false == hasCause {
        context["cause"] = causeErr.Error()
    }

    _, hasCauseContextChain := context["causeContextChain"]
    if false == hasCauseContextChain {
        causeContextChain := exception.BuildCauseContextChain(causeErr, causeChainMaxDepth)
        if 0 < len(causeContextChain) {
            context["causeContextChain"] = causeContextChain
        }
    }

    return context
}
