package logging

import (
    "errors"
    "fmt"
    "log"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const causeChainMaxDepth = 8

/* LevelEnabled asks a logger whether a record at this level would survive its threshold, and answers true for one that cannot be asked. It is the single door onto loggingcontract.LevelReporter, so the "absent means enabled" rule lives in one place rather than being restated at each caller — a site that spelled the fallback the other way would silently stop recording against every logger that does not implement the capability, which is most of them.

   Ask it only where the answer saves work that is otherwise thrown away: a context map assembled at the call site, a name resolved through reflection. A record whose arguments already exist costs nothing to hand over, and gating it here would only add a second place where a level decision is made. */
func LevelEnabled(logger loggingcontract.Logger, level loggingcontract.Level) bool {
    levelReporter, isReporter := logger.(loggingcontract.LevelReporter)
    if false == isReporter {
        return true
    }

    return levelReporter.Enabled(level)
}

/* LogError ignores nil, typed-nil and already-logged errors. It anchors the record on the supplied error: a direct melody error supplies its level and context, while other errors retain their complete outer message at error level with available context and causes. Nil logger uses the process default. */
func LogError(logger loggingcontract.Logger, err error) {
    if nil == err || true == internal.IsNilInterface(err) {
        return
    }

    var alreadyLoggedValue exceptioncontract.AlreadyLogged
    if true == errors.As(err, &alreadyLoggedValue) && false == internal.IsNilInterface(alreadyLoggedValue) {
        if true == alreadyLoggedValue.AlreadyLogged() {
            return
        }
    }

    exceptionValue, isTopException := err.(*exception.Error)
    if true == isTopException {
        levelUpper := strings.ToUpper(string(exceptionValue.Level()))
        enrichedContext := enrichContextWithCause(exceptionValue)

        if nil == logger || true == internal.IsNilInterface(logger) {

            if 0 < len(enrichedContext) {
                log.Printf("[%s] %s context=%v", levelUpper, internal.EscapeControlCharacters(exceptionValue.Message()), internal.EscapeControlCharacters(fmt.Sprintf("%v", enrichedContext)))
            } else {
                log.Printf("[%s] %s", levelUpper, internal.EscapeControlCharacters(exceptionValue.Message()))
            }

            return
        }

        logger.Log(exceptionValue.Level(), exceptionValue.Message(), enrichedContext)
        return
    }

    enrichedContext := exception.LogContext(err)
    renderedMessage, isRendered := enrichedContext["error"].(string)
    if false == isRendered || "" == renderedMessage {
        renderedMessage = "the error message could not be rendered"
    }

    delete(enrichedContext, "error")

    if nil == logger || true == internal.IsNilInterface(logger) {
        if 0 < len(enrichedContext) {
            log.Printf("[ERROR] %s context=%v", internal.EscapeControlCharacters(renderedMessage), internal.EscapeControlCharacters(fmt.Sprintf("%v", enrichedContext)))
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
