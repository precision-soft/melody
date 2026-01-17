package logging

import (
	"errors"
	"log"
	"strings"

	"github.com/precision-soft/melody/exception"
	exceptioncontract "github.com/precision-soft/melody/exception/contract"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

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

	if nil == logger {
		var exceptionValue *exception.Error
		if true == errors.As(err, &exceptionValue) {
			levelUpper := strings.ToUpper(string(exceptionValue.Level()))

			context := exceptionValue.Context()
			if nil != context && 0 < len(context) {
				log.Printf("[%s] %s context=%v", levelUpper, exceptionValue.Message(), context)
			} else {
				log.Printf("[%s] %s", levelUpper, exceptionValue.Message())
			}

			return
		}

		log.Printf("[ERROR] %s", err.Error())
		return
	}

	var exceptionValue *exception.Error
	if true == errors.As(err, &exceptionValue) {
		if true == exceptionValue.AlreadyLogged() {
			return
		}

		context := exceptionValue.Context()
		if nil == context {
			context = exceptioncontract.Context{}
		}

		logger.Log(exceptionValue.Level(), exceptionValue.Message(), context)
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
