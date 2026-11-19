package security

import (
    "context"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
)

const DeviceClaim = "device_id"

/* DetachedRuntime answers the runtime a security compensation runs on: its context carries the request's values but not its cancellation, so a client that hangs up cannot stop a revocation or a withdraw, and the store's CallTimeout still bounds each round trip. */
func DetachedRuntime(runtimeInstance melodycontainercontract.Runtime) melodycontainercontract.Runtime {
    return melodyruntime.WithContext(runtimeInstance, context.WithoutCancel(runtimeInstance.Context()))
}

type ResolvedTokenStore struct{}

func (instance ResolvedTokenStore) Lookup(
    runtimeInstance melodycontainercontract.Runtime,
    tokenString string,
) (melodysecuritycontract.Claims, bool, error) {
    return TokenStoreFromResolver(runtimeInstance.Context(), runtimeInstance.Scope()).Lookup(runtimeInstance, tokenString)
}

func (instance ResolvedTokenStore) RevokeBefore(
    _ melodycontainercontract.Runtime,
    _ string,
    _ string,
    _ time.Time,
) error {
    return melodyexception.NewError("a revocation boundary is published through the store resolved from the container, not through this adapter", nil, nil)
}

func (instance ResolvedTokenStore) RevocationEpoch(
    runtimeInstance melodycontainercontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    return TokenStoreFromResolver(runtimeInstance.Context(), runtimeInstance.Scope()).RevocationEpoch(
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
