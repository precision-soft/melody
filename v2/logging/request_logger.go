package logging

import (
    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/internal"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

func NewRequestLogger(logger loggingcontract.Logger, requestId string, contextKey string) loggingcontract.Logger {
    if true == internal.IsNilInterface(logger) {
        exception.Panic(
            exception.NewError("base logger is not provided for request logger", nil, nil),
        )
    }

    if "" == contextKey {
        exception.Panic(
            exception.NewError("invalid context key for request logger", nil, nil),
        )
    }

    if "" == requestId {
        return logger
    }

    return &requestLogger{
        base:       logger,
        requestId:  requestId,
        contextKey: contextKey,
    }
}

type requestLogger struct {
    base       loggingcontract.Logger
    requestId  string
    contextKey string
}

func (instance *requestLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.base.Log(level, message, instance.mergeContextWithRequestId(context, instance.requestId))
}

func (instance *requestLogger) Debug(message string, context loggingcontract.Context) {
    instance.base.Debug(message, instance.mergeContextWithRequestId(context, instance.requestId))
}

func (instance *requestLogger) Info(message string, context loggingcontract.Context) {
    instance.base.Info(message, instance.mergeContextWithRequestId(context, instance.requestId))
}

func (instance *requestLogger) Warning(message string, context loggingcontract.Context) {
    instance.base.Warning(message, instance.mergeContextWithRequestId(context, instance.requestId))
}

func (instance *requestLogger) Error(message string, context loggingcontract.Context) {
    instance.base.Error(message, instance.mergeContextWithRequestId(context, instance.requestId))
}

func (instance *requestLogger) Emergency(message string, context loggingcontract.Context) {
    instance.base.Emergency(message, instance.mergeContextWithRequestId(context, instance.requestId))
}

func (instance *requestLogger) mergeContextWithRequestId(context loggingcontract.Context, requestId string) map[string]any {
    if "" == requestId {
        return context
    }

    if nil == context {
        context = map[string]any{}
    }

    mergedContext := make(map[string]any, len(context)+1)
    for key, value := range context {
        mergedContext[key] = value
    }

    if existingValue, exists := mergedContext[instance.contextKey]; true == exists {
        if stringValue, ok := existingValue.(string); true == ok && "" != stringValue {
            return mergedContext
        }
    }

    mergedContext[instance.contextKey] = requestId

    return mergedContext
}

var _ loggingcontract.Logger = (*requestLogger)(nil)
