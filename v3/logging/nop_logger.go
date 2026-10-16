package logging

import (
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type nopLogger struct{}

func NewNopLogger() loggingcontract.Logger {
    return &nopLogger{}
}

func (instance *nopLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
}

func (instance *nopLogger) Debug(message string, context loggingcontract.Context) {}

func (instance *nopLogger) Info(message string, context loggingcontract.Context) {}

func (instance *nopLogger) Warning(message string, context loggingcontract.Context) {}

func (instance *nopLogger) Error(message string, context loggingcontract.Context) {}

func (instance *nopLogger) Emergency(message string, context loggingcontract.Context) {}

/* Enabled reports nothing enabled: every method discards what it is handed. */
func (instance *nopLogger) Enabled(level loggingcontract.Level) bool {
    return false
}

func EnsureLogger(logger loggingcontract.Logger) loggingcontract.Logger {
    if false == internal.IsNilInterface(logger) {
        return logger
    }

    return NewNopLogger()
}

var _ loggingcontract.Logger = (*nopLogger)(nil)
var _ loggingcontract.LevelReporter = (*nopLogger)(nil)
