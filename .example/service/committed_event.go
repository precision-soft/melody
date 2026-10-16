package service

import (
    "errors"

    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodyeventcontract "github.com/precision-soft/melody/event/contract"
    melodyexception "github.com/precision-soft/melody/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/exception/contract"
    melodylogging "github.com/precision-soft/melody/logging"
    melodyloggingcontract "github.com/precision-soft/melody/logging/contract"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

/* dispatchCommitted dispatches the event of a catalogue write that has committed, and a failure refuses nothing: the caller is answered with the row the write stored, where a refusal told it nothing was written. The dispatcher stops at its first failing listener, so the cache invalidation may not have run; the entries the write changed are dropped here, best effort, and the failure, a failed drop joined to it, is filed at error with the event and the entity. */
func dispatchCommitted(
    runtimeInstance melodyruntimecontract.Runtime,
    eventDispatcher melodyeventcontract.EventDispatcher,
    cacheInstance melodycachecontract.Cache,
    eventName string,
    payload any,
    entityId string,
    cacheKeyList ...string,
) {
    _, dispatchErr := eventDispatcher.DispatchName(runtimeInstance, eventName, payload)
    if nil == dispatchErr {
        return
    }

    failureErr := dispatchErr
    for _, cacheKey := range cacheKeyList {
        if deleteErr := cacheInstance.Delete(cacheKey); nil != deleteErr {
            failureErr = errors.Join(failureErr, deleteErr)
        }
    }

    committedWriteLoggerOf(runtimeInstance).Error(
        "an event failed after its write committed; the write is answered and its cache entries are dropped",
        melodyexception.LogContext(failureErr, melodyexceptioncontract.Context{"event": eventName, "entityId": entityId}),
    )
}

/* committedWriteLoggerOf is the logger the runtime resolves, the request's on a request, and the emergency logger when it holds none, since the failure has to reach some journal */
func committedWriteLoggerOf(runtimeInstance melodyruntimecontract.Runtime) melodyloggingcontract.Logger {
    logger, resolveErr := melodyruntime.FromRuntime[melodyloggingcontract.Logger](runtimeInstance, melodylogging.ServiceLogger)
    if nil != resolveErr || nil == logger {
        return melodylogging.EmergencyLogger()
    }

    return logger
}
