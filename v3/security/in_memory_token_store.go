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
    }
}

type InMemoryTokenStore struct {
    clock          clockcontract.Clock
    mutex          sync.RWMutex
    entriesByToken map[string]tokenEntry
}

type tokenEntry struct {
    claims    securitycontract.Claims
    expiresAt time.Time
}

func (instance *InMemoryTokenStore) Put(tokenString string, claims securitycontract.Claims) {
    instance.put(tokenString, claims, time.Time{})
}

/** PutWithTtl stores claims that stop resolving once the ttl elapses; a non-positive ttl never expires. */
func (instance *InMemoryTokenStore) PutWithTtl(tokenString string, claims securitycontract.Claims, ttl time.Duration) {
    expiresAt := time.Time{}
    if 0 < ttl {
        expiresAt = instance.clock.Now().Add(ttl)
    }

    instance.put(tokenString, claims, expiresAt)
}

func (instance *InMemoryTokenStore) put(tokenString string, claims securitycontract.Claims, expiresAt time.Time) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.entriesByToken[tokenString] = tokenEntry{claims: claims, expiresAt: expiresAt}
}

func (instance *InMemoryTokenStore) Delete(tokenString string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.entriesByToken, tokenString)
}

/** DeleteByUser revokes every token whose claims resolve to the given user identifier; it is the
logout/"sign out everywhere" primitive that the per-token Delete cannot express. It returns the
number of tokens removed. */
func (instance *InMemoryTokenStore) DeleteByUser(userIdentifier string) int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    removed := 0
    for tokenString, entry := range instance.entriesByToken {
        if entry.claims.UserIdentifier == userIdentifier {
            delete(instance.entriesByToken, tokenString)
            removed++
        }
    }

    return removed
}

/** PurgeExpired drops every entry whose ttl has elapsed. Lookup already ignores expired entries,
but it leaves them in place; a janitor calling PurgeExpired periodically keeps the map bounded when
many short-lived tokens are issued and never explicitly deleted. It returns the number purged. */
func (instance *InMemoryTokenStore) PurgeExpired() int {
    now := instance.clock.Now()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    purged := 0
    for tokenString, entry := range instance.entriesByToken {
        if false == entry.expiresAt.IsZero() && true == now.After(entry.expiresAt) {
            delete(instance.entriesByToken, tokenString)
            purged++
        }
    }

    return purged
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

    return entry.claims, true, nil
}

var _ securitycontract.TokenStore = (*InMemoryTokenStore)(nil)
