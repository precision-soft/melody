package journal

import (
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodylogging "github.com/precision-soft/melody/v4/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

/* LoggerOr answers the logger the runtime resolves (on a request, the scope's, which stamps every record with the request identifier) and the fallback when there is no runtime or it holds none. It does not go through the framework's LoggerFromRuntime, which files an emergency record and answers nil, because which journal a record falls back to is the caller's decision. */
func LoggerOr(runtimeInstance melodycontainercontract.Runtime, fallback melodyloggingcontract.Logger) melodyloggingcontract.Logger {
    if nil == runtimeInstance {
        return fallback
    }

    logger, resolveErr := melodyruntime.FromRuntime[melodyloggingcontract.Logger](runtimeInstance, melodylogging.ServiceLogger)
    if nil != resolveErr || nil == logger {
        return fallback
    }

    return logger
}
