package logging

import (
    "errors"
    "log"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

const causeChainMaxDepth = 8

type logEntry struct {
    Message string                     `json:"message"`
    Level   loggingcontract.LevelLabel `json:"level"`
    Time    string                     `json:"time"`
    Context map[string]any             `json:"context"`
}

func LogError(logger loggingcontract.Logger, err error) {
    if nil == err {
        return
    }

    var exceptionValue *exception.Error
    if true == errors.As(err, &exceptionValue) {
        levelUpper := strings.ToUpper(string(exceptionValue.Level()))
        enrichedContext := enrichContextWithCause(exceptionValue)

        if nil == logger {
            if 0 < len(enrichedContext) {
                log.Printf("[%s] %s context=%v", levelUpper, exceptionValue.Message(), enrichedContext)
            } else {
                log.Printf("[%s] %s", levelUpper, exceptionValue.Message())
            }

            return
        }

        if true == exceptionValue.AlreadyLogged() {
            return
        }

        logger.Log(exceptionValue.Level(), exceptionValue.Message(), enrichedContext)
        return
    }

    if nil == logger {
        log.Printf("[ERROR] %s", err.Error())
        return
    }

    logger.Error(err.Error(), nil)
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
    if nil == causeErr {
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
