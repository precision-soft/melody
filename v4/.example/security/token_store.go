package security

import (
    "context"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodysecurity "github.com/precision-soft/melody/v4/security"
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
    return TokenStoreFromRuntime(runtimeInstance).Lookup(tokenString)
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
    return TokenStoreFromRuntime(runtimeInstance).RevocationEpoch(userIdentifier, deviceIdentifier)
}

/* TokenStoreFromRuntime binds the store the token firewalls read, redis when wired and the process's own otherwise, both registered under the rueidis module's name */
func TokenStoreFromRuntime(runtimeInstance melodycontainercontract.Runtime) melodysecurity.BoundTokenStore {
    return melodysecurity.TokenStoreFromRuntime(runtimeInstance, melodyrueidis.ServiceTokenStore)
}

var _ melodysecuritycontract.TokenStore = ResolvedTokenStore{}
var _ melodysecuritycontract.RevocationEpochStore = ResolvedTokenStore{}
