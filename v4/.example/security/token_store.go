package security

import (
    "context"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

const DeviceClaim = "device_id"

type ResolvedTokenStore struct{}

func (instance ResolvedTokenStore) Lookup(
    runtimeInstance melodyruntimecontract.Runtime,
    tokenString string,
) (melodysecuritycontract.Claims, bool, error) {
    return TokenStoreFromResolver(runtimeInstance.Context(), runtimeInstance.Container()).Lookup(runtimeInstance, tokenString)
}

func (instance ResolvedTokenStore) RevokeBefore(userIdentifier string, deviceIdentifier string, instant time.Time) {
    melodyexception.Panic(melodyexception.NewError("a revocation boundary is published through the store resolved from the container, not through this adapter", nil, nil))
}

func (instance ResolvedTokenStore) RevocationEpoch(
    runtimeInstance melodyruntimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    return TokenStoreFromResolver(runtimeInstance.Context(), runtimeInstance.Container()).RevocationEpoch(
        runtimeInstance,
        userIdentifier,
        deviceIdentifier,
    )
}

func TokenStoreFromResolver(ctx context.Context, resolver melodycontainercontract.Resolver) melodysecuritycontract.EpochRevocableTokenStore {
    return melodyrueidis.EpochRevocableTokenStoreMustFromResolver(ctx, resolver)
}

var _ melodysecuritycontract.TokenStore = ResolvedTokenStore{}
var _ melodysecuritycontract.RevocationEpochStore = ResolvedTokenStore{}
