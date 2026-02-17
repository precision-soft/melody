package logging

import (
	"fmt"
	"log"
	"sort"

	loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

func NewDefaultLogger() loggingcontract.Logger {
	return &defaultLogger{}
}

type defaultLogger struct{}

func (instance *defaultLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
	if nil == context {
		context = loggingcontract.Context{}
	}

	log.Printf("[%s] %s %s", level, message, instance.formatContext(context))
}

func (instance *defaultLogger) Debug(message string, context loggingcontract.Context) {
	instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *defaultLogger) Info(message string, context loggingcontract.Context) {
	instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *defaultLogger) Warning(message string, context loggingcontract.Context) {
	instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *defaultLogger) Error(message string, context loggingcontract.Context) {
	instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *defaultLogger) Emergency(message string, context loggingcontract.Context) {
	instance.Log(loggingcontract.LevelEmergency, message, context)
}

func (instance *defaultLogger) formatContext(context loggingcontract.Context) string {
	if 0 == len(context) {
		return ""
	}

	keys := make([]string, 0, len(context))
	for key := range context {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(context))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%v", key, context[key]))
	}

	return fmt.Sprintf("{%s}", instance.joinPairs(pairs))
}

func (instance *defaultLogger) joinPairs(values []string) string {
	result := ""
	for i, v := range values {
		if 0 < i {
			result += " "
		}
		result += v
	}
	return result
}

var _ loggingcontract.Logger = (*defaultLogger)(nil)
