package logging

import (
	"errors"
	"log"
	"strings"

	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
	loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

const causeChainMaxDepth = 8

type logEntry struct {
	Message string                `json:"message"`
	Level   loggingcontract.Level `json:"level"`
	Time    string                `json:"time"`
	Context map[string]any        `json:"context"`
}

func LogError(logger loggingcontract.Logger, err error) {
	if nil == err {
		return
	}

	var exceptionValue *exception.Error
	if true == errors.As(err, &exceptionValue) {
		if nil != logger {
			if true == exceptionValue.AlreadyLogged() {
				return
			}
		}

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

	causeChain := buildCauseChain(causeErr, causeChainMaxDepth)
	if 0 < len(causeChain) {
		context["cause"] = causeChain[0]
		context["causeChain"] = causeChain
	} else {
		context["cause"] = causeErr.Error()
	}

	causeContextChain := buildCauseContextChain(causeErr, causeChainMaxDepth)
	if 0 < len(causeContextChain) {
		context["causeContextChain"] = causeContextChain
	}

	return context
}

func buildCauseChain(causeErr error, maxDepth int) []string {
	if nil == causeErr {
		return nil
	}

	if 0 >= maxDepth {
		return []string{causeErr.Error()}
	}

	chain := make([]string, 0, maxDepth)

	current := causeErr
	for depth := 0; depth < maxDepth && nil != current; depth++ {
		chain = append(chain, current.Error())
		current = errors.Unwrap(current)
	}

	return chain
}

func buildCauseContextChain(causeErr error, maxDepth int) []map[string]any {
	if nil == causeErr {
		return nil
	}

	if 0 >= maxDepth {
		maxDepth = 1
	}

	chain := make([]map[string]any, 0, maxDepth)
	hasAnyContext := false

	current := causeErr
	for depth := 0; depth < maxDepth && nil != current; depth++ {
		var causeException *exception.Error
		if true == errors.As(current, &causeException) {
			causeContext := causeException.Context()
			if nil != causeContext && 0 < len(causeContext) {
				chain = append(chain, causeContext)
				hasAnyContext = true
			} else {
				chain = append(chain, nil)
			}
		} else {
			chain = append(chain, nil)
		}

		current = errors.Unwrap(current)
	}

	if false == hasAnyContext {
		return nil
	}

	return chain
}
