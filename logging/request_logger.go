package logging

import (
    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
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

/* Closed forwards the liveness question to the base logger: the process-boundary exit handler refuses a logger that reports itself closed, and a decorator that cannot answer hides a dead file logger behind a live-looking wrapper — the final record would be handed to it and silently dropped. A base that does not answer the question is reported open, exactly as the exit handler treats it when asked directly. Close is deliberately not forwarded: the wrapper lives in a request scope and does not own the shared writer of the logger it decorates. */
func (instance *requestLogger) Closed() bool {
    closedChecker, isChecker := instance.base.(interface{ Closed() bool })
    if false == isChecker {
        return false
    }

    return closedChecker.Closed()
}

/* mergeContextWithRequestId writes the real request id under the context key unconditionally: the id this logger was built with is the one trustworthy correlation there is, and a value already sitting under the key frequently originates in an error context assembled from request data — letting it win let whatever the client wrote forge the correlation of the record. A different non-empty claim is kept beside the real id under the key suffixed "Claimed", so nothing the caller said is lost, and the operator sees both the truth and the claim. */
func (instance *requestLogger) mergeContextWithRequestId(context loggingcontract.Context, requestId string) map[string]any {
    if "" == requestId {
        return context
    }

    if nil == context {
        context = map[string]any{}
    }

    mergedContext := make(map[string]any, len(context)+2)
    for key, value := range context {
        mergedContext[key] = value
    }

    if existingValue, exists := mergedContext[instance.contextKey]; true == exists {
        if stringValue, ok := existingValue.(string); true == ok && "" != stringValue && requestId != stringValue {
            mergedContext[instance.contextKey+"Claimed"] = stringValue
        }
    }

    mergedContext[instance.contextKey] = requestId

    return mergedContext
}

var _ loggingcontract.Logger = (*requestLogger)(nil)
