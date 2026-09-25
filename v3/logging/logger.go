package logging

import (
    "log"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const causeChainMaxDepth = 8

/* LevelEnabled asks a logger whether a record at this level would survive its threshold, and answers true for one that cannot be asked; it is the one door onto loggingcontract.LevelReporter. Ask it only where the answer saves work otherwise thrown away. */
func LevelEnabled(logger loggingcontract.Logger, level loggingcontract.Level) bool {
    levelReporter, isReporter := logger.(loggingcontract.LevelReporter)
    if false == isReporter {
        return true
    }

    return levelReporter.Enabled(level)
}

/* LogError writes one record for the error, or none when it is nil, a typed nil or already logged; the mark is read at the depth exception.MarkLogged writes it. A top-level *exception.Error contributes its own level, message and context, while any other error is logged at error level under its full message, with the nearest provider's context and its cause chain. A nil logger falls back to the process default logger. */
func LogError(logger loggingcontract.Logger, err error) {
    if true == internal.IsNilInterface(err) {
        return
    }

    /* the mark is read through the exception package's reader, which asks under a recover: LogError runs from recovery defers, where a foreign Unwrap, As or AlreadyLogged that panics would pass the recovery */
    if true == exception.IsAlreadyLogged(err) {
        return
    }

    exceptionValue, isTopException := err.(*exception.Error)
    if true == isTopException {
        levelUpper := strings.ToUpper(string(exceptionValue.Level()))
        enrichedContext := enrichContextWithCause(exceptionValue)

        if true == internal.IsNilInterface(logger) {
            /* this fallback writes through the raw standard logger, so keeping one record on one line is its own duty */
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

    /* the message is rendered with the context under a recover: this path runs from the recovery handlers, where Error() may dereference the very nil that caused the panic */
    enrichedContext := exception.LogContext(err)
    renderedMessage, isRendered := enrichedContext["error"].(string)
    if false == isRendered || "" == renderedMessage {
        renderedMessage = "the error message could not be rendered"
    }

    delete(enrichedContext, "error")

    if true == internal.IsNilInterface(logger) {
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

    /* a typed-nil cause is the nil its producer meant, and BuildCauseChain answers an empty chain for it */
    causeErr := exceptionValue.CauseErr()
    if true == internal.IsNilInterface(causeErr) {
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
