package service

import (
    "errors"

    examplejournal "github.com/precision-soft/melody/v3/.example/journal"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyeventcontract "github.com/precision-soft/melody/v3/event/contract"
    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodylogging "github.com/precision-soft/melody/v3/logging"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

    examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Error(
        "an event failed after its write committed; the write is answered and its cache entries are dropped",
        melodyexception.LogContext(failureErr, melodyexceptioncontract.Context{"event": eventName, "entityId": entityId}),
    )
}
