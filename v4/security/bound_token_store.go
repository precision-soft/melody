package security

import (
    "time"

    "github.com/precision-soft/melody/v4/bound"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* BoundTokenStore is a token store bound to one runtime: every door runs under its context. It is a value for one request, one run or one delivery, never kept in a service, a goroutine that outlives the runtime or a field: a service holds the store and binds it at the top of each method, and a compensation that must outlive the request binds it again over a runtime detached from the request's cancellation. */
type BoundTokenStore struct {
    runtimeInstance containercontract.Runtime
    storeInstance   securitycontract.EpochRevocableTokenStore
}

/* TokenStoreFor binds the store to the runtime; it allocates nothing and never panics, a nil of either being refused by the first door called. */
func TokenStoreFor(runtimeInstance containercontract.Runtime, storeInstance securitycontract.EpochRevocableTokenStore) BoundTokenStore {
    return BoundTokenStore{
        runtimeInstance: runtimeInstance,
        storeInstance:   storeInstance,
    }
}

/* TokenStoreFromRuntime resolves the store the application registered under serviceName through the runtime's scope and binds it; the framework registers none, and a missing one panics as runtime.MustFromRuntime does. */
func TokenStoreFromRuntime(runtimeInstance containercontract.Runtime, serviceName string) BoundTokenStore {
    return TokenStoreFor(runtimeInstance, runtime.MustFromRuntime[securitycontract.EpochRevocableTokenStore](runtimeInstance, serviceName))
}

/* Runtime answers the runtime the view is bound to, for a callee that binds something else to it. */
func (instance BoundTokenStore) Runtime() containercontract.Runtime {
    return instance.runtimeInstance
}

/* TokenStore answers the unbound store, for a callee that binds it to another runtime. */
func (instance BoundTokenStore) TokenStore() securitycontract.EpochRevocableTokenStore {
    return instance.storeInstance
}

func (instance BoundTokenStore) Lookup(tokenString string) (securitycontract.Claims, bool, error) {
    if nilErr := instance.refuseNilStore(); nil != nilErr {
        return securitycontract.Claims{}, false, nilErr
    }

    return instance.storeInstance.Lookup(instance.runtimeInstance, tokenString)
}

func (instance BoundTokenStore) Put(tokenString string, claims securitycontract.Claims, ttl bound.Duration) error {
    if nilErr := instance.refuseNilStore(); nil != nilErr {
        return nilErr
    }

    return instance.storeInstance.Put(instance.runtimeInstance, tokenString, claims, ttl)
}

func (instance BoundTokenStore) Delete(tokenString string) error {
    if nilErr := instance.refuseNilStore(); nil != nilErr {
        return nilErr
    }

    return instance.storeInstance.Delete(instance.runtimeInstance, tokenString)
}

func (instance BoundTokenStore) DeleteByUser(userIdentifier string) (int, error) {
    if nilErr := instance.refuseNilStore(); nil != nilErr {
        return 0, nilErr
    }

    return instance.storeInstance.DeleteByUser(instance.runtimeInstance, userIdentifier)
}

func (instance BoundTokenStore) PurgeExpired() (int, error) {
    if nilErr := instance.refuseNilStore(); nil != nilErr {
        return 0, nilErr
    }

    return instance.storeInstance.PurgeExpired(instance.runtimeInstance)
}

func (instance BoundTokenStore) RevokeBefore(userIdentifier string, instant time.Time, options securitycontract.RevocationOptions) error {
    if nilErr := instance.refuseNilStore(); nil != nilErr {
        return nilErr
    }

    return instance.storeInstance.RevokeBefore(instance.runtimeInstance, userIdentifier, instant, options)
}

func (instance BoundTokenStore) RevocationEpoch(userIdentifier string, options securitycontract.RevocationOptions) (time.Time, error) {
    if nilErr := instance.refuseNilStore(); nil != nilErr {
        return time.Time{}, nilErr
    }

    return instance.storeInstance.RevocationEpoch(instance.runtimeInstance, userIdentifier, options)
}

/* a typed-nil store passes a plain comparison and panics on its first method call */
func (instance BoundTokenStore) refuseNilStore() error {
    if true == internal.IsNilInterface(instance.storeInstance) {
        return exception.NewError("token store instance is nil", nil, nil)
    }

    return nil
}
