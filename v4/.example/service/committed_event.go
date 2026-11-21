package service

import (
    "context"
    "errors"

    examplejournal "github.com/precision-soft/melody/v4/.example/journal"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyeventcontract "github.com/precision-soft/melody/v4/event/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyexceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    melodylogging "github.com/precision-soft/melody/v4/logging"
)

/* detachedRuntime is the runtime a cache entry is dropped under once its write committed: the context's values kept and its cancellation not, so a caller that gives up after the write leaves no stale entry */
func detachedRuntime(runtimeInstance melodycontainercontract.Runtime, ctx context.Context) melodycontainercontract.Runtime {
    return melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(ctx))
}

/* dispatchCommitted dispatches the event of a catalogue write that has committed, and a failure refuses nothing: the caller is answered with the row the write stored, where a refusal told it nothing was written. The dispatcher stops at its first failing listener, so the cache invalidation may not have run; the entries the write changed are dropped here, best effort, and the failure, a failed drop joined to it, is filed at error with the event and the entity. */
func dispatchCommitted(
    runtimeInstance melodycontainercontract.Runtime,
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
    detachedCache := melodycache.For(detachedRuntime(runtimeInstance, runtimeInstance.Context()), cacheInstance)
    for _, cacheKey := range cacheKeyList {
        if deleteErr := detachedCache.Delete(cacheKey); nil != deleteErr {
            failureErr = errors.Join(failureErr, deleteErr)
        }
    }

    examplejournal.LoggerOr(runtimeInstance, melodylogging.EmergencyLogger()).Error(
        "an event failed after its write committed; the write is answered and its cache entries are dropped",
        melodyexception.LogContext(failureErr, melodyexception.LogContextConfig{Extra: melodyexceptioncontract.Context{"event": eventName, "entityId": entityId}}),
    )
}
