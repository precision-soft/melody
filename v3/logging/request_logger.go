package logging

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

/* NewRequestLogger decorates the base logger with the request path's correlation rule: the real id wins the context key unconditionally, and a different non-empty string claim survives beside it under the key suffixed "Claimed" — the claim frequently originates in an error context assembled from request data, so the suffix says exactly what it is. Console processes use NewProcessLogger, whose caller is trusted. */
func NewRequestLogger(logger loggingcontract.Logger, requestId string, contextKey string) loggingcontract.Logger {
    return newCorrelationLogger(logger, requestId, contextKey, "Claimed", false)
}

/* NewProcessLogger decorates the base logger with the console path's correlation rule: the generated id wins the context key on every record, so the correlation never has holes, and a caller's own value under the key is preserved verbatim — any type, not only strings — under the key suffixed "Provided". The console caller is the process's own code, not a client that can forge, so what it wrote is legitimate data displaced by the correlation, not a claim to be suspected. */
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
    /* the two knobs the constructors set: the suffix under which a caller's value survives, and whether it survives whatever its type — the request path keeps only non-empty string claims, because everything else there is request-derived noise, while the console path preserves verbatim what its trusted caller wrote */
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

/* Closed forwards the liveness question to the base logger: the process-boundary exit handler refuses a logger that reports itself closed, and a decorator that cannot answer hides a dead file logger behind a live-looking wrapper — the final record would be handed to it and silently dropped. A base that does not answer the question is reported open, exactly as the exit handler treats it when asked directly. Close is deliberately not forwarded: the wrapper lives in a request scope and does not own the shared writer of the logger it decorates. */
func (instance *requestLogger) Closed() bool {
    closedChecker, isChecker := instance.base.(interface{ Closed() bool })
    if false == isChecker {
        return false
    }

    return closedChecker.Closed()
}

/* Enabled forwards the level question to the base logger, and the forwarding is what makes the capability worth anything on the request path: this decorator is what every handler and every listener holds, installed as a scope override, so a caller asking the logger it was handed asks THIS one. Answering "enabled" from here would report every level enabled for a journal configured at error, which is the exact opposite of what the caller is asking for. A base that does not implement the capability is reported enabled, which is the contract's floor. */
func (instance *requestLogger) Enabled(level loggingcontract.Level) bool {
    levelReporter, isReporter := instance.base.(loggingcontract.LevelReporter)
    if false == isReporter {
        return true
    }

    return levelReporter.Enabled(level)
}

/* mergeContextWithRequestId writes the real correlation id under the context key unconditionally: the id this logger was built with is the one trustworthy correlation there is, and a value already sitting under the key would otherwise break the chain every reader of the log greps by. What happens to that value is the constructor's policy: on the request path a different non-empty string claim survives under "...Claimed" — letting it win would let whatever the client wrote forge the correlation of the record — while on the console path the caller's value survives verbatim under "...Provided", whatever its type, because the console caller is trusted and its data is displaced, not suspected. */
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
