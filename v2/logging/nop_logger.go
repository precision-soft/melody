package logging

import loggingcontract "github.com/precision-soft/melody/v2/logging/contract"

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

func EnsureLogger(logger loggingcontract.Logger) loggingcontract.Logger {
    if nil != logger {
        return logger
    }

    return NewNopLogger()
}

var _ loggingcontract.Logger = (*nopLogger)(nil)
