package security

import (
    "sync"

    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func NewInMemoryTokenStore() *InMemoryTokenStore {
    return &InMemoryTokenStore{
        claimsByToken: make(map[string]securitycontract.Claims),
    }
}

type InMemoryTokenStore struct {
    mutex         sync.RWMutex
    claimsByToken map[string]securitycontract.Claims
}

func (instance *InMemoryTokenStore) Put(tokenString string, claims securitycontract.Claims) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.claimsByToken[tokenString] = claims
}

func (instance *InMemoryTokenStore) Delete(tokenString string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    delete(instance.claimsByToken, tokenString)
}

func (instance *InMemoryTokenStore) Lookup(
    runtimeInstance runtimecontract.Runtime,
    tokenString string,
) (securitycontract.Claims, bool, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    claims, found := instance.claimsByToken[tokenString]
    return claims, found, nil
}

var _ securitycontract.TokenStore = (*InMemoryTokenStore)(nil)
