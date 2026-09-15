package cli

import (
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

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

const processLocalCacheNotice = "the cache is this process's own (no REDIS_ADDRESS): a running server keeps what it cached until it restarts"

func cacheClearedScope(runtimeInstance melodyruntimecontract.Runtime) string {
    if true == cacheIsProcessLocal(runtimeInstance) {
        return "this process's own entries; " + processLocalCacheNotice
    }

    return "the shared cache, so the running server rereads the database"
}
