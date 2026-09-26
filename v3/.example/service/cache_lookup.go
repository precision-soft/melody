package service

import (
    "context"
    "time"

    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
)

/* absenceCacheTtl bounds how long a lookup that found nothing is remembered. A found entity is cleared by the listeners under its own key and keeps no expiry; an absence has no such owner, and its key is spelled by whoever asked, so an unbounded one would let a client write one permanent entry per name it invents. */
const absenceCacheTtl = time.Minute

/* rememberEntityOrAbsence answers a cached entity, computing it through the loader on a miss: an absence is stored under the bounded ttl Remember writes, and only a value this call found is written again, unbounded. A hit is returned as it is, so an entity served from memory costs no cache write. The loader answers nil for "there is no such entity". */
func rememberEntityOrAbsence(
    cacheInstance melodycachecontract.Cache,
    cacheKey string,
    loader func(ctx context.Context) (any, error),
) (any, error) {
    cached, exists, getErr := cacheInstance.Get(cacheKey)
    if nil == getErr && true == exists {
        return cached, nil
    }

    /* Remember runs the loader on the leader's goroutine alone and hands its answer to every waiter, so the flag marks the one call that computed the value: a waiter's late write would re-install, unbounded, an entity a listener cleared after the leader's load. */
    computedHere := false

    /* a failed read is left to Remember, which treats an undecodable payload as a miss and heals the key and answers any other cache failure unchanged */
    computed, rememberErr := melodycache.Remember(
        cacheInstance,
        cacheKey,
        absenceCacheTtl,
        func(ctx context.Context) (any, error) {
            computedHere = true

            return loader(ctx)
        },
        nil,
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    if nil == computed || false == computedHere {
        return computed, nil
    }

    setErr := cacheInstance.Set(cacheKey, computed, entityCacheTtl)
    if nil != setErr {
        return nil, setErr
    }

    return computed, nil
}

/* entityCacheTtl is the unbounded ttl a found entity keeps: the listeners that watch the write events clear these entries by name. */
const entityCacheTtl = time.Duration(0)
