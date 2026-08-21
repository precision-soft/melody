package security

import (
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewInMemoryTokenStore() *InMemoryTokenStore {
    return NewInMemoryTokenStoreWithClock(clock.NewSystemClock())
}

func NewInMemoryTokenStoreWithClock(clockInstance clockcontract.Clock) *InMemoryTokenStore {
    if true == internal.IsNilInterface(clockInstance) {
        exception.Panic(exception.NewError("token store clock is nil", nil, nil))
    }

    return &InMemoryTokenStore{
        clock:          clockInstance,
        entriesByToken: make(map[string]tokenEntry),
        tokensByUser:   make(map[string]map[string]struct{}),
        epochsByUser:   make(map[string]map[string]time.Time),
    }
}

type InMemoryTokenStore struct {
    clock                    clockcontract.Clock
    mutex                    sync.RWMutex
    entriesByToken           map[string]tokenEntry
    tokensByUser             map[string]map[string]struct{}
    epochsByUser             map[string]map[string]time.Time
    revocationEpochRetention time.Duration
}

/* WithRevocationEpochRetention bounds how long PurgeExpired keeps a published revocation boundary after its instant, mirroring the rueidis store's option of the same name. Zero — the default — keeps boundaries forever: a boundary is what refuses every token issued before it, stateless JWTs above all, so its life may not depend on anything else the store holds. A negative retention is refused rather than silently ignored, because a boundary dropped early is a revocation bypass. Chainable; call it at boot, before the store is shared. */
func (instance *InMemoryTokenStore) WithRevocationEpochRetention(retention time.Duration) *InMemoryTokenStore {
    if 0 > retention {
        exception.Panic(exception.NewError(
            "token store revocation epoch retention may not be negative",
            map[string]any{"retention": retention.String()},
            nil,
        ))
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.revocationEpochRetention = retention

    return instance
}

const (
    revocationUserField = "user"

    revocationDeviceFieldPrefix = "device:"
)

func revocationField(deviceIdentifier string) string {
    if "" == deviceIdentifier {
        return revocationUserField
    }

    return revocationDeviceFieldPrefix + deviceIdentifier
}

type tokenEntry struct {
    claims    securitycontract.Claims
    expiresAt time.Time
}

func (instance *InMemoryTokenStore) Put(tokenString string, claims securitycontract.Claims) {
    instance.put(tokenString, claims, 0)
}

func (instance *InMemoryTokenStore) PutWithTtl(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    /* @important a non-positive ttl is refused instead of falling through to the never-expires sentinel: the likeliest caller of a ttl <= 0 computed a remaining lifetime that had already elapsed, and storing that token FOREVER is the exact inversion of what was asked. The token string never joins the context — it is the credential. */
    if 0 >= ttl {
        exception.Panic(exception.NewError(
            "token store ttl must be positive",
            map[string]any{"ttl": ttl.String(), "user": claims.UserIdentifier},
            nil,
        ))
    }

    instance.put(tokenString, claims, ttl)
}

func (instance *InMemoryTokenStore) Delete(tokenString string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if entry, found := instance.entriesByToken[tokenString]; true == found {
        instance.unindexLocked(entry.claims.UserIdentifier, tokenString)
        delete(instance.entriesByToken, tokenString)
    }
}

func (instance *InMemoryTokenStore) DeleteByUser(userIdentifier string) int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    tokens, exists := instance.tokensByUser[userIdentifier]
    if false == exists {
        return 0
    }

    removed := 0
    for tokenString := range tokens {
        delete(instance.entriesByToken, tokenString)
        removed++
    }

    delete(instance.tokensByUser, userIdentifier)

    return removed
}

func (instance *InMemoryTokenStore) PurgeExpired() int {
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    purged := 0
    for tokenString, entry := range instance.entriesByToken {
        if false == entry.expiresAt.IsZero() && true == now.After(entry.expiresAt) {
            instance.unindexLocked(entry.claims.UserIdentifier, tokenString)
            delete(instance.entriesByToken, tokenString)
            purged++
        }
    }

    /* @important a revocation boundary's life is bound to the configured retention window ALONE, never to whether the user still holds stored tokens: stateless JWTs are validated against these boundaries without ever being stored, so the previous token-linked eviction meant the first purge after a RevokeBefore silently un-revoked every outstanding JWT of a user with no stored tokens. With the default zero retention boundaries are kept forever. */
    if 0 < instance.revocationEpochRetention {
        horizon := now.Add(-instance.revocationEpochRetention)
        for userIdentifier, boundaries := range instance.epochsByUser {
            for field, instant := range boundaries {
                if true == instant.Before(horizon) {
                    delete(boundaries, field)
                }
            }

            if 0 == len(boundaries) {
                delete(instance.epochsByUser, userIdentifier)
            }
        }
    }

    return purged
}

func cloneClaims(claims securitycontract.Claims) securitycontract.Claims {
    cloned := claims

    if nil != claims.Roles {
        cloned.Roles = append([]string{}, claims.Roles...)
    }

    if nil != claims.Scope {
        cloned.Scope = internal.CopyAnyMap(claims.Scope)
    }

    if nil != claims.Attributes {
        cloned.Attributes = internal.CopyAnyMap(claims.Attributes)
    }

    cloned.OriginatingActor = cloneActorData(claims.OriginatingActor)

    return cloned
}

/* @important maxActorImpersonationDepth bounds the impersonator-chain deep-copy recursion so a cyclic ActorData — an in-process caller can point actor.Impersonator back into the chain through the exported field and Put/Lookup it — cannot recurse until the goroutine stack overflows, a fatal error no deferred recover() can catch and which takes down the whole process. A real impersonation chain (a subject acted for by an operator) is a handful of links deep, far below this bound; a JSON-decoded actor is additionally capped by encoding/json's own nesting limit. Mirrors internal.maxCopyDepth. */
const maxActorImpersonationDepth = 10000

/* cloneActorData deep-copies an originating actor, including its nested Impersonator subtree, so a stored or returned actor never aliases the caller's mutable ActorData (its Roles, Attributes, or the accountable impersonator behind it). Recurses through the impersonator chain; a nil carrier clones to nil. */
func cloneActorData(actor *securitycontract.ActorData) *securitycontract.ActorData {
    return cloneActorDataAtDepth(actor, 0)
}

func cloneActorDataAtDepth(actor *securitycontract.ActorData, depth int) *securitycontract.ActorData {
    if nil == actor {
        return nil
    }

    actorCopy := *actor
    if nil != actor.Roles {
        actorCopy.Roles = append([]string{}, actor.Roles...)
    }
    if nil != actor.Attributes {
        actorCopy.Attributes = internal.CopyStringMap(actor.Attributes)
    }

    /* @important at the depth bound stop following the impersonator chain rather than recurse further: this halts a cyclic chain before the stack overflows while leaving every realistically-shallow chain fully deep-copied. The truncated link is dropped (nil), never aliased, so no caller-mutable ActorData leaks into the store. */
    if depth >= maxActorImpersonationDepth {
        actorCopy.Impersonator = nil

        return &actorCopy
    }

    actorCopy.Impersonator = cloneActorDataAtDepth(actor.Impersonator, depth+1)

    return &actorCopy
}

func (instance *InMemoryTokenStore) Lookup(
    runtimeInstance runtimecontract.Runtime,
    tokenString string,
) (securitycontract.Claims, bool, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    entry, found := instance.entriesByToken[tokenString]
    if false == found {
        return securitycontract.Claims{}, false, nil
    }

    if false == entry.expiresAt.IsZero() && true == instance.clock.Now().After(entry.expiresAt) {
        return securitycontract.Claims{}, false, nil
    }

    if true == instance.isRevokedLocked(entry.claims) {
        return securitycontract.Claims{}, false, nil
    }

    return cloneClaims(entry.claims), true, nil
}

func (instance *InMemoryTokenStore) RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time) {
    if "" == userIdentifier {
        exception.Panic(exception.NewError("token store revocation needs a user identifier", nil, nil))
    }

    if true == instant.IsZero() {
        exception.Panic(exception.NewError(
            "token store revocation instant is zero",
            map[string]any{"user": userIdentifier},
            nil,
        ))
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    boundaries, exists := instance.epochsByUser[userIdentifier]
    if false == exists {
        boundaries = make(map[string]time.Time)
        instance.epochsByUser[userIdentifier] = boundaries
    }

    field := revocationField(deviceIdentifier)
    if published, found := boundaries[field]; true == found && false == instant.After(published) {
        return
    }

    boundaries[field] = instant
}

func (instance *InMemoryTokenStore) RevocationEpoch(
    _ runtimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.revocationEpochLocked(userIdentifier, deviceIdentifier), nil
}

func (instance *InMemoryTokenStore) revocationEpochLocked(userIdentifier string, deviceIdentifier string) time.Time {
    boundaries, exists := instance.epochsByUser[userIdentifier]
    if false == exists {
        return time.Time{}
    }

    effective := boundaries[revocationUserField]

    if "" != deviceIdentifier {
        if device, found := boundaries[revocationDeviceFieldPrefix+deviceIdentifier]; true == found && true == device.After(effective) {
            effective = device
        }
    }

    return effective
}

func (instance *InMemoryTokenStore) isRevokedLocked(claims securitycontract.Claims) bool {
    epoch := instance.revocationEpochLocked(claims.UserIdentifier, claims.DeviceIdentifier)
    if true == epoch.IsZero() {
        return false
    }

    return false == claims.IssuedAt.After(epoch)
}

func (instance *InMemoryTokenStore) put(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* @important the IssuedAt stamp is read under the same critical section that inserts the entry, so a RevokeBefore can no longer be published between the stamp and the insert: without this, a token stamped before a concurrent revocation but inserted after it was born already revoked, and a "revoke everything, then log in again" sequence bounced the fresh login. The ttl-derived expiry reads the same instant for the same reason. */
    now := instance.clock.Now()

    claims.IssuedAt = now

    expiresAt := time.Time{}
    if 0 < ttl {
        expiresAt = now.Add(ttl)
    }

    if existing, found := instance.entriesByToken[tokenString]; true == found && existing.claims.UserIdentifier != claims.UserIdentifier {
        instance.unindexLocked(existing.claims.UserIdentifier, tokenString)
    }

    instance.entriesByToken[tokenString] = tokenEntry{claims: cloneClaims(claims), expiresAt: expiresAt}
    instance.indexLocked(claims.UserIdentifier, tokenString)
}

func (instance *InMemoryTokenStore) indexLocked(userIdentifier string, tokenString string) {
    tokens, exists := instance.tokensByUser[userIdentifier]
    if false == exists {
        tokens = make(map[string]struct{})
        instance.tokensByUser[userIdentifier] = tokens
    }

    tokens[tokenString] = struct{}{}
}

func (instance *InMemoryTokenStore) unindexLocked(userIdentifier string, tokenString string) {
    tokens, exists := instance.tokensByUser[userIdentifier]
    if false == exists {
        return
    }

    delete(tokens, tokenString)
    if 0 == len(tokens) {
        delete(instance.tokensByUser, userIdentifier)
    }
}

var _ securitycontract.RevocableTokenStore = (*InMemoryTokenStore)(nil)
var _ securitycontract.EpochRevocableTokenStore = (*InMemoryTokenStore)(nil)
