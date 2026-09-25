package logging

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* NewRequestLogger decorates the base logger with the request path's correlation rule: the real id wins the context key, and a different non-empty string claim survives beside it under the key suffixed "Claimed". Console processes use NewProcessLogger, whose caller is trusted. */
func NewRequestLogger(logger loggingcontract.Logger, requestId string, contextKey string) loggingcontract.Logger {
    return newCorrelationLogger(logger, requestId, contextKey, "Claimed", false)
}

/* NewProcessLogger decorates the base logger with the console path's correlation rule: the generated id wins the context key on every record, and the caller's own value, of any type, is kept under the key suffixed "Provided". */
func NewProcessLogger(logger loggingcontract.Logger, processId string, contextKey string) loggingcontract.Logger {
    return newCorrelationLogger(logger, processId, contextKey, "Provided", true)
}

func newCorrelationLogger(
    logger loggingcontract.Logger,
    correlationId string,
    contextKey string,
    preservedKeySuffix string,
    preserveAnyValue bool,
) loggingcontract.Logger {
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

    if "" == correlationId {
        return logger
    }

    return &requestLogger{
        base:               logger,
        requestId:          correlationId,
        contextKey:         contextKey,
        preservedKeySuffix: preservedKeySuffix,
        preserveAnyValue:   preserveAnyValue,
    }
}

type requestLogger struct {
    base       loggingcontract.Logger
    requestId  string
    contextKey string
    /* the suffix under which a caller's value survives, and whether it survives whatever its type: the request path keeps only non-empty string claims, the console path keeps its trusted caller's value verbatim */
    preservedKeySuffix string
    preserveAnyValue   bool
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

/* Closed forwards the liveness question to the base logger, so the exit handler does not hand its final record to a decorator over a dead file logger; a base that cannot answer is reported open. Close is not forwarded: the wrapper does not own the shared writer. */
func (instance *requestLogger) Closed() bool {
    closedChecker, isChecker := instance.base.(interface{ Closed() bool })
    if false == isChecker {
        return false
    }

    return closedChecker.Closed()
}

/* Enabled forwards the level question to the base logger, since this decorator is what handlers and listeners hold; a base that does not implement the capability is reported enabled. */
func (instance *requestLogger) Enabled(level loggingcontract.Level) bool {
    levelReporter, isReporter := instance.base.(loggingcontract.LevelReporter)
    if false == isReporter {
        return true
    }

    return levelReporter.Enabled(level)
}

/* mergeContextWithRequestId writes the real correlation id under the context key unconditionally, so a client cannot forge the correlation of a record. A value already under the key survives per the constructor's policy, as a "Claimed" string on the request path or a "Provided" value on the console path. */
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
        if true == instance.preserveAnyValue {
            if requestId != existingValue {
                mergedContext[instance.contextKey+instance.preservedKeySuffix] = existingValue
            }
        } else if stringValue, ok := existingValue.(string); true == ok && "" != stringValue && requestId != stringValue {
            mergedContext[instance.contextKey+instance.preservedKeySuffix] = stringValue
        }
    }

    mergedContext[instance.contextKey] = requestId

    return mergedContext
}

var _ loggingcontract.Logger = (*requestLogger)(nil)
var _ loggingcontract.LevelReporter = (*requestLogger)(nil)
