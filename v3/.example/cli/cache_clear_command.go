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

/* CacheClearCommand empties this application's cache namespace and nothing else. The entities are cached with no
   expiry and cleared by name, by the listeners that watch the write events, so a write that reached a database
   through no door of this application — a row edited by hand, a restore — is served stale until something clears
   it; before this the only door that did was example:db:reset, which drops both databases to get there. */
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

/* clearCache empties the cache and says so, and it is the one spelling of the clear for both doors that perform
   it — this command, and example:db:reset after it reseeds — with the caller named in a failure so the operator
   reads which of the two stopped. On the redis backend the clear is a SCAN of the whole keyspace filtered on this
   application's prefix, under the backend's one-second command budget — measured at 0 ms over the development
   keyspace, and declared here because a keyspace shared with much else could take the exit code. The state a
   fresh volume holds includes an EMPTY cache: a reset writes through no door that dispatches a write event, so
   without the clear an account the reset removed kept authenticating on the login door with its old digest. On
   the shared cache this reaches the running server; on the in-process fallback it reaches this process alone,
   which the line says. A clear that fails takes the exit code. */
func clearCache(runtimeInstance melodyruntimecontract.Runtime, writer io.Writer, caller string) error {
    cacheInstance, cacheErr := melodycontainer.FromResolver[melodycachecontract.Cache](
        runtimeInstance.Container(),
        melodycache.ServiceCache,
    )
    if nil != cacheErr {
        return cacheErr
    }

    if clearErr := cacheInstance.Clear(); nil != clearErr {
        return exception.NewError(caller+": clearing the cache did not complete", nil, clearErr)
    }

    fmt.Fprintln(writer, "cache cleared: "+cacheClearedScope(runtimeInstance))

    return nil
}

var _ melodyclicontract.Command = (*CacheClearCommand)(nil)
