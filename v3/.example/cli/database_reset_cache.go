package cli

import (
    "errors"
    "fmt"
    "io"

    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* Resolve before destructive work, then invalidate on every exit: a failed reset may already have changed rows. */
func prepareDatabaseResetCache(runtimeInstance melodyruntimecontract.Runtime, writer io.Writer) (func(*error), error) {
    cacheInstance, err := melodycontainer.FromResolver[melodycachecontract.Cache](runtimeInstance.Container(), melodycache.ServiceCache)
    if nil != err { return nil, err }
    return func(runErr *error) {
        if err := cacheInstance.Clear(); nil != err {
            *runErr = errors.Join(*runErr, fmt.Errorf("database reset: clearing the cache did not complete: %w", err))
            return
        }
        fmt.Fprintln(writer, "cache cleared: "+cacheClearedScope(runtimeInstance))
    }, nil
}
