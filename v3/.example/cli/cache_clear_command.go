package cli

import (
    "fmt"
    "io"

    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/exception"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* CacheClearCommand empties this application's cache namespace and nothing else. The entities are cached with no expiry and cleared by name by the listeners that watch the write events, so a write that reaches a database through no door of this application, a row edited by hand or a restore, is served stale until something clears it. */
type CacheClearCommand struct{}

func NewCacheClearCommand() *CacheClearCommand {
    return &CacheClearCommand{}
}

func (instance *CacheClearCommand) Name() string {
    return "example:cache:clear"
}

func (instance *CacheClearCommand) Description() string {
    return "empties this application's cache namespace; the databases are not touched"
}

func (instance *CacheClearCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{}
}

func (instance *CacheClearCommand) Run(runtimeInstance melodyruntimecontract.Runtime, commandContext melodyclicontract.Context) error {
    return clearCache(runtimeInstance, commandContext.Writer(), "cache clear")
}

/* clearCache empties the cache and says so, the one spelling of the clear for this command and for example:db:reset after it reseeds, naming the caller in a failure. On redis the clear is a SCAN of the whole keyspace filtered on this application's prefix, under the backend's one-second command budget. A reset writes through no door that dispatches a write event, so without the clear an account it removed would keep authenticating from the cache; on the in-process fallback the clear reaches this process alone, which the line says, and a failed clear takes the exit code. */
func clearCache(runtimeInstance melodyruntimecontract.Runtime, writer io.Writer, caller string) error {
    cacheInstance, cacheErr := resolveCache(runtimeInstance)
    if nil != cacheErr {
        return cacheErr
    }

    return clearResolvedCache(runtimeInstance, cacheInstance, writer, caller)
}

func resolveCache(runtimeInstance melodyruntimecontract.Runtime) (melodycachecontract.Cache, error) {
    return melodycontainer.FromResolver[melodycachecontract.Cache](
        runtimeInstance.Container(),
        melodycache.ServiceCache,
    )
}

/* clearResolvedCache is the clear over a cache already resolved, for example:db:reset, which resolves it before its first drop so a cache it cannot reach refuses the reset before anything is touched. */
func clearResolvedCache(runtimeInstance melodyruntimecontract.Runtime, cacheInstance melodycachecontract.Cache, writer io.Writer, caller string) error {
    if clearErr := cacheInstance.Clear(); nil != clearErr {
        return exception.NewError(caller+": clearing the cache did not complete", nil, clearErr)
    }

    fmt.Fprintln(writer, "cache cleared: "+cacheClearedScope(runtimeInstance))

    return nil
}

var _ melodyclicontract.Command = (*CacheClearCommand)(nil)
