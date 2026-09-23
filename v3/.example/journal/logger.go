package journal

import (
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyloggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* LoggerOr answers the logger the runtime resolves — on a request, the scope's, which the kernel gave a logger
   that stamps every record with the request identifier — and the fallback when there is no runtime or the
   runtime holds none. The resolution is asked here rather than through the framework's LoggerFromRuntime,
   which files an emergency record and answers nil when the logger is absent: which journal a record falls back
   to is the caller's decision, and every caller in this application has a record that must reach SOME journal.

   Four doors of this application carried this resolution as a copy each — the server-error presenter, the page,
   the trusted proxy resolution and the product listing. Resolved from the root container instead, the
   record landed on the application's logger without the identifier that ties it to the rest of the request. */
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
