package logging

import (
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* the capturing double stands in for whatever logger a suite exercises; the logger, recover, json-logger and request-logger tests all reach it from here. */
type captureLogger struct {
    lastLevel   loggingcontract.Level
    lastMessage string
    lastContext map[string]any
    calls       int
}

func (instance *captureLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.calls++
    instance.lastLevel = level
    instance.lastMessage = message
    instance.lastContext = context
}

func (instance *captureLogger) Debug(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelDebug, message, context)
}

func (instance *captureLogger) Info(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelInfo, message, context)
}

func (instance *captureLogger) Warning(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelWarning, message, context)
}

func (instance *captureLogger) Error(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelError, message, context)
}

func (instance *captureLogger) Emergency(message string, context loggingcontract.Context) {
    instance.Log(loggingcontract.LevelEmergency, message, context)
}

var _ loggingcontract.Logger = (*captureLogger)(nil)

/* typedNilProbeError is the concrete error type whose typed nil the package has to read as the nil its producer meant, rather than dereference. */
type typedNilProbeError struct {
    message string
}

func (instance *typedNilProbeError) Error() string {
    return instance.message
}
