package cli

import (
    melodycache "github.com/precision-soft/melody/cache"
    melodycachecontract "github.com/precision-soft/melody/cache/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
)

/* cacheIsProcessLocal answers whether the cache this process writes is its own or the one every process of the deployment shares. The entities are cached with no expiry and cleared by name, by listeners subscribed to the write events — and those listeners run in the process that DISPATCHED. With redis the cache is shared and a write made here reaches the running server; on the in-process fallback the listeners clear this process's map, and a server started beside it keeps serving what it cached until it restarts. A console command that writes has to say which of the two it did, and it reads the answer off the wiring rather than off the configuration, because the wiring is what decides. */
func cacheIsProcessLocal(runtimeInstance melodyruntimecontract.Runtime) bool {
    backend, resolveErr := melodycontainer.FromResolver[melodycachecontract.Backend](
        runtimeInstance.Container(),
        melodycache.ServiceCacheBackend,
    )
    if nil != resolveErr {
        return true
    }

    _, inProcess := backend.(*melodycache.InMemoryBackend)

    return inProcess
}

/* processLocalCacheNotice is the sentence a writing command adds when the cache is this process's own: a running server on the same fallback will not see the write until it restarts. */
const processLocalCacheNotice = "the cache is this process's own (no REDIS_ADDRESS): a running server keeps what it cached until it restarts"

/* cacheClearedScope says what a clear of the cache reached. */
func cacheClearedScope(runtimeInstance melodyruntimecontract.Runtime) string {
    if true == cacheIsProcessLocal(runtimeInstance) {
        return "this process's own entries; " + processLocalCacheNotice
    }

    return "the shared cache, so the running server rereads the database"
}
