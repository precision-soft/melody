package security

import (
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
)

const DeviceClaim = "device_id"

type ResolvedTokenStore struct{}

func (instance ResolvedTokenStore) Lookup(
    runtimeInstance melodyruntimecontract.Runtime,
    tokenString string,
) (melodysecuritycontract.Claims, bool, error) {
    return TokenStoreFromResolver(runtimeInstance.Container()).Lookup(runtimeInstance, tokenString)
}

func (instance ResolvedTokenStore) RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time) {
    panic("a revocation boundary is published through the store resolved from the container, not through this adapter")
}

func (instance ResolvedTokenStore) RevocationEpoch(
    runtimeInstance melodyruntimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    return TokenStoreFromResolver(runtimeInstance.Container()).RevocationEpoch(
        runtimeInstance,
        userIdentifier,
        deviceIdentifier,
    )
}

func TokenStoreFromResolver(resolver melodycontainercontract.Resolver) melodysecuritycontract.EpochRevocableTokenStore {
    return melodyrueidis.EpochRevocableTokenStoreMustFromResolver(resolver)
}

var _ melodysecuritycontract.TokenStore = ResolvedTokenStore{}
var _ melodysecuritycontract.RevocationEpochStore = ResolvedTokenStore{}
