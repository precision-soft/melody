package security

import (
    "context"
    "time"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    rueidis "github.com/redis/rueidis"
)

/* ServiceTokenNamespace names the token store's namespace on the shared redis, registered only when the store lives there */
const ServiceTokenNamespace = "service.example.security.token_namespace"

/* tokenNamespaceCallTimeout bounds each round trip of a clear, the bound the token store puts on its own */
const tokenNamespaceCallTimeout = time.Second

/* TokenNamespace is every key the redis token store writes — the device tokens, the per-account indexes and the revocation epochs — under the one prefix the composition root hands it. The store has no door that empties it, and example:db:reset needs one: the reset removes the accounts through no door that publishes a deletion, and identifiers are minted as the highest suffix plus one, so a device token left standing would authenticate as the next holder of its identifier. A store that is the server process's own memory, without redis, is out of a console command's reach and has no namespace here. */
type TokenNamespace struct {
    client rueidis.Client
    prefix string
}

/* NewTokenNamespace answers the namespace of a token store built with WithTokenStorePrefix(prefix): the store writes every key under "{prefix}:" */
func NewTokenNamespace(client rueidis.Client, prefix string) *TokenNamespace {
    return &TokenNamespace{client: client, prefix: prefix}
}

/* TokenNamespaceFromResolver answers the namespace when the composition root registered one, and nil when the store is a process's own memory */
func TokenNamespaceFromResolver(resolver melodycontainercontract.Resolver) *TokenNamespace {
    namespace, resolveErr := melodycontainer.FromResolver[*TokenNamespace](resolver, ServiceTokenNamespace)
    if nil != resolveErr {
        return nil
    }

    return namespace
}

/* Clear deletes every key of the namespace and answers how many it deleted, walking it by SCAN so a large store is never asked for in one reply */
func (instance *TokenNamespace) Clear(ctx context.Context) (int, error) {
    pattern := "{" + instance.prefix + "}:*"
    cursor := uint64(0)
    removed := 0

    for {
        scanContext, scanCancel := context.WithTimeout(ctx, tokenNamespaceCallTimeout)
        scan, scanErr := instance.client.Do(
            scanContext,
            instance.client.B().Scan().Cursor(cursor).Match(pattern).Count(500).Build(),
        ).AsScanEntry()
        scanCancel()
        if nil != scanErr {
            return removed, scanErr
        }

        if 0 < len(scan.Elements) {
            deleteContext, deleteCancel := context.WithTimeout(ctx, tokenNamespaceCallTimeout)
            deleted, deleteErr := instance.client.Do(
                deleteContext,
                instance.client.B().Del().Key(scan.Elements...).Build(),
            ).AsInt64()
            deleteCancel()
            if nil != deleteErr {
                return removed, deleteErr
            }

            removed = removed + int(deleted)
        }

        cursor = scan.Cursor
        if 0 == cursor {
            return removed, nil
        }
    }
}
