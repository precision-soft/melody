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
    }
}

type InMemoryTokenStore struct {
    clock          clockcontract.Clock
    mutex          sync.RWMutex
    entriesByToken map[string]tokenEntry
    tokensByUser   map[string]map[string]struct{}
}

type tokenEntry struct {
    claims    securitycontract.Claims
    expiresAt time.Time
}

func (instance *InMemoryTokenStore) Put(tokenString string, claims securitycontract.Claims) {
    instance.put(tokenString, claims, time.Time{})
}

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

    if existing, found := instance.entriesByToken[tokenString]; true == found && existing.claims.UserIdentifier != claims.UserIdentifier {
        instance.unindexLocked(existing.claims.UserIdentifier, tokenString)
    }

    instance.entriesByToken[tokenString] = tokenEntry{claims: cloneClaims(claims), expiresAt: expiresAt}
    instance.indexLocked(claims.UserIdentifier, tokenString)
}

/** indexLocked records the token under its user in the secondary index. Caller holds the write lock. */
func (instance *InMemoryTokenStore) indexLocked(userIdentifier string, tokenString string) {
    tokens, exists := instance.tokensByUser[userIdentifier]
    if false == exists {
        tokens = make(map[string]struct{})
        instance.tokensByUser[userIdentifier] = tokens
    }

    tokens[tokenString] = struct{}{}
}

/** unindexLocked removes the token from its user's set, dropping the set when empty. Caller holds the write lock. */
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

func cloneClaims(claims securitycontract.Claims) securitycontract.Claims {
    cloned := securitycontract.Claims{
        UserIdentifier: claims.UserIdentifier,
    }

    if nil != claims.Roles {
        cloned.Roles = append([]string{}, claims.Roles...)
    }

    if nil != claims.Scope {
        scope := make(map[string]any, len(claims.Scope))
        for key, value := range claims.Scope {
            scope[key] = value
        }
        cloned.Scope = scope
    }

    if nil != claims.Attributes {
        attributes := make(map[string]any, len(claims.Attributes))
        for key, value := range claims.Attributes {
            attributes[key] = value
        }
        cloned.Attributes = attributes
    }

    return cloned
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

    return cloneClaims(entry.claims), true, nil
}

var _ securitycontract.RevocableTokenStore = (*InMemoryTokenStore)(nil)
