package journal

import (
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* LoggerOr answers the logger the runtime resolves (on a request, the scope's, which stamps every record with the request identifier) and the fallback when there is no runtime or it holds none. It does not go through the framework's LoggerFromRuntime, which files an emergency record and answers nil, because which journal a record falls back to is the caller's decision. */
func LoggerOr(runtimeInstance melodyruntimecontract.Runtime, fallback melodyloggingcontract.Logger) melodyloggingcontract.Logger {
    if nil == runtimeInstance {
        return fallback
    }

    logger, resolveErr := melodyruntime.FromRuntime[melodyloggingcontract.Logger](runtimeInstance, melodylogging.ServiceLogger)
    if nil != resolveErr || nil == logger {
        return fallback
    }

    return logger
}
