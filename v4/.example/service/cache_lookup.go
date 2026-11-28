package service

import (
    "time"

    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* absenceCacheTtl bounds how long a lookup that found nothing is remembered. A found entity is cleared by the listeners under its own key and keeps the longer entityCacheTtl; an absence has no such owner, and its key is spelled by whoever asked, so an unbounded one would let a client write one permanent entry per name it invents. */
const absenceCacheTtl = time.Minute

/* rememberEntityOrAbsence answers a cached entity, computing it through the loader on a miss: the loader answers nil for "there is no such entity", which is remembered under the short absenceCacheTtl, and a found entity is stored under entityCacheTtl by the one call that loaded it. A hit writes nothing. */
func rememberEntityOrAbsence[T any](
    boundCache melodycache.Bound,
    cacheKey string,
    loader func(flightRuntime melodycontainercontract.Runtime) (*T, error),
) (*T, error) {
    return melodycache.Remember(
        boundCache,
        cacheKey,
        melodycache.RememberOptions{
            /* the short bound is the default, so a path that forgets to raise it leaves an entry that expires soon rather than an absence kept for long */
            Ttl: melodybound.Of(absenceCacheTtl),
        },
        func(flightRuntime melodycontainercontract.Runtime, item *melodycache.RememberItem) (*T, error) {
            loaded, loadErr := loader(flightRuntime)
            if nil != loadErr {
                return nil, loadErr
            }

            if nil != loaded {
                item.SetTtl(melodybound.Of(entityCacheTtl))
            }

            return loaded, nil
        },
    )
}

/* entityCacheTtl bounds a found entity. The listeners that watch the write events clear these entries by name, but the leader's write lands after its load, and a write and its invalidation that run inside that window leave the entity read before the write: the bound is how long such an entry can be served. */
const entityCacheTtl = 10 * time.Minute
