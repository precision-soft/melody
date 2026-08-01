package logging

import (
    "errors"
    "log"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

const causeChainMaxDepth = 8

type logEntry struct {
    Message string                     `json:"message"`
    Level   loggingcontract.LevelLabel `json:"level"`
    Time    string                     `json:"time"`
    Context map[string]any             `json:"context"`
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
            if 0 < len(enrichedContext) {
                log.Printf("[%s] %s context=%v", levelUpper, exceptionValue.Message(), enrichedContext)
            } else {
                log.Printf("[%s] %s", levelUpper, exceptionValue.Message())
            }

            return
        }

        logger.Log(exceptionValue.Level(), exceptionValue.Message(), enrichedContext)
        return
    }

    enrichedContext := enrichForeignContextWithCause(err)

    if nil == logger || true == internal.IsNilInterface(logger) {
        if 0 < len(enrichedContext) {
            log.Printf("[ERROR] %s context=%v", err.Error(), enrichedContext)
        } else {
            log.Printf("[ERROR] %s", err.Error())
        }

        return
    }

    logger.Error(err.Error(), enrichedContext)
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

    causeChain := exception.BuildCauseChain(causeErr, causeChainMaxDepth)
    if 0 < len(causeChain) {
        context["cause"] = causeChain[0]
        context["causeChain"] = causeChain
    } else {
        context["cause"] = causeErr.Error()
    }

    causeContextChain := exception.BuildCauseContextChain(causeErr, causeChainMaxDepth)
    if 0 < len(causeContextChain) {
        context["causeContextChain"] = causeContextChain
    }

    return context
}

/* enrichForeignContextWithCause assembles the context of an error that is not a top-level *exception.Error: the context of the nearest ContextProvider in the chain and the cause chain walked from the error's own wrap link, mirroring what enrichContextWithCause renders for an exception. The error's own message is deliberately absent — it travels as the record's message, and repeating it under a context key would say the same thing twice in one record. */
func enrichForeignContextWithCause(err error) exceptioncontract.Context {
    context := exceptioncontract.Context{}

    var provider exceptioncontract.ContextProvider
    if true == errors.As(err, &provider) && false == internal.IsNilInterface(provider) {
        for key, value := range provider.Context() {
            context[key] = value
        }
    }

    causeErr := errors.Unwrap(err)
    if nil == causeErr || true == internal.IsNilInterface(causeErr) {
        return context
    }

    causeChain := exception.BuildCauseChain(causeErr, causeChainMaxDepth)
    if 0 < len(causeChain) {
        context["cause"] = causeChain[0]
        context["causeChain"] = causeChain
    }

    causeContextChain := exception.BuildCauseContextChain(causeErr, causeChainMaxDepth)
    if 0 < len(causeContextChain) {
        context["causeContextChain"] = causeContextChain
    }

    return context
}
