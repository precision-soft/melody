package logging

import (
    "errors"
    "fmt"
    "log"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

/* LogError writes one record for the error it is given, or none when the error is nil — including a typed nil, which would otherwise panic on the very lines that render it — or when the error was already logged. The mark is read at the depth exception.MarkLogged writes it, the nearest AlreadyLogged implementer in the chain, so marking a wrapping http exception suppresses this record the way the mark promises; the previous read searched for the nearest *exception.Error instead and disagreed with the writer on every chain whose markable link is not that type. The record is anchored on the error the caller handed over: a top-level *exception.Error contributes its own level, message and enriched context, while any other error — a wrapper included — is logged at error level under its full message, with the context of the nearest provider and the cause chain walked from its own wrap link; anchoring on the nearest *exception.Error buried in the chain logged that error's message at that error's level, which dropped the wrapper's framing entirely and let a low-level cause file the whole record below the logger's threshold. A nil logger falls back to the process default logger under the same rules. */
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
            /* the same one-record-one-line guarantee the default logger holds: this fallback writes through the raw standard logger, so the escaping is its own duty */
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

    /* the message is taken from the same assembly as the context, because that is where it is rendered under a recover: this path is reached from inside the recovery handlers, where the error is whatever a panic carried, and an Error() that dereferences the very nil field that made it panic-worthy would take the record down with it — the one record written to explain the failure. */
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

    /* a typed-nil cause is the nil its producer meant: BuildCauseChain refuses it at the entry and returns an empty chain, which routed it into the else branch below — the only input that ever reached that branch — where causeErr.Error() dereferenced the nil receiver on the line that renders a failure */
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

