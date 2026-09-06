package service

import (
    "context"
    "time"

    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
)

/* absenceCacheTtl bounds how long a lookup that found NOTHING is remembered for.

   An entity that was found keeps an unbounded entry: the event listeners clear it under its own key the
   moment it is created, renamed or deleted, so an expiry would only add a second, weaker mechanism to a
   correct one. An absence has no such owner. Its key is spelled by whoever asked — a username typed into
   the unauthenticated login form, an identifier taken from a path segment — so an unbounded absence lets
   one caller write one permanent entry per name it invents, and this application's own request budget
   allows a hundred thousand of them per client per hour. Bounded to a minute, the same traffic can hold
   roughly seventeen hundred entries at a time instead of every name it has ever tried, while a burst on
   one name still answers from memory, which is what remembering an absence was for. */
const absenceCacheTtl = time.Minute

/* rememberEntityOrAbsence answers a cached entity, computing it through the loader on a miss and storing
   the answer under the ttl its presence earns: an absence lapses, a value found does not.

   Remember takes one ttl, decided before the loader runs, and the ttl here has to be chosen AFTER it has
   answered; so the absence is left with the ttl Remember wrote — already the bounded one — and only a
   value that was FOUND is written a second time, unbounded. That second write is why the cache is read
   here rather than left to Remember: on a hit Remember answers the stored value, which would then be
   re-stored by every reader, so an entity served from memory would cost a cache write per request. A
   hit is returned as it is, and only a value this call computed is written back.

   The window of a remembered absence does not slide either way, and that is Remember's doing rather than
   this early read's: on a hit it answers from the store without writing, so the absence keeps the expiry
   it was given whoever reads it. Measured by mutation — with this early return disarmed, an absence
   behaves identically and only the found value shows the extra write.

   The loader answers a nil value for "there is no such entity", which is the shape every caller of this
   helper already used with Remember: the absence is what gets remembered, not an error. */
func rememberEntityOrAbsence(
    cacheInstance melodycachecontract.Cache,
    cacheKey string,
    loader func(ctx context.Context) (any, error),
) (any, error) {
    cached, exists, getErr := cacheInstance.Get(cacheKey)
    if nil == getErr && true == exists {
        return cached, nil
    }

    /* a read that failed is handed to Remember rather than reported here: it treats a payload it cannot
       decode as a miss and heals the key, and any other cache failure comes back from it unchanged. */
    computed, rememberErr := melodycache.Remember(
        cacheInstance,
        cacheKey,
        absenceCacheTtl,
        loader,
        nil,
    )
    if nil != rememberErr {
        return nil, rememberErr
    }

    if nil == computed {
        return nil, nil
    }

    setErr := cacheInstance.Set(cacheKey, computed, entityCacheTtl)
    if nil != setErr {
        return nil, setErr
    }

    return computed, nil
}

/* entityCacheTtl is the unbounded ttl an entity keeps, spelled once so the reason travels with it: these
   entries are cleared by name, by the listeners that watch the write events, and an entity that lapsed
   quietly would be re-read from the database by every request that followed. */
const entityCacheTtl = time.Duration(0)
