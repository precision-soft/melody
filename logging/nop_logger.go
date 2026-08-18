package logging

import (
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

/* Enabled reports nothing enabled, because nothing is: every door above discards what it is handed. This is the one logger for which the capability is the whole answer rather than a threshold — a caller that builds its records eagerly used to pay for every one of them against a logger that exists to accept and forget. */
func (instance *nopLogger) Enabled(level loggingcontract.Level) bool {
    return false
}

func EnsureLogger(logger loggingcontract.Logger) loggingcontract.Logger {
    if nil != logger && false == internal.IsNilInterface(logger) {
        return logger
    }

    return NewNopLogger()
}

var _ loggingcontract.Logger = (*nopLogger)(nil)
var _ loggingcontract.LevelReporter = (*nopLogger)(nil)
