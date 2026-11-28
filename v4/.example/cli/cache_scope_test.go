package cli

import (
    "context"
    "testing"

    melodycache "github.com/precision-soft/melody/v4/cache"
    melodycachecontract "github.com/precision-soft/melody/v4/cache/contract"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

type sharedBackendDouble struct {
    melodycachecontract.Backend
}

func newCacheScopeRuntime(backend melodycachecontract.Backend) melodycontainercontract.Runtime {
    serviceContainer := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})

    if nil != backend {
        melodycontainer.MustRegister(
            serviceContainer,
            melodycache.ServiceCacheBackend,
            func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
                return backend, nil
            },
            melodycontainer.RegisterOptions{},
        )
    }

    return melodyruntime.NewRun(context.Background(), serviceContainer.NewScope())
}

func TestCacheIsProcessLocal_ReadsTheInProcessBackendAsThisProcessOwn(t *testing.T) {
    runtimeInstance := newCacheScopeRuntime(melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{Clock: melodyclock.NewSystemClock()}))

    if false == cacheIsProcessLocal(runtimeInstance) {
        t.Fatalf("expected the in-process backend to be read as this process's own")
    }
}

func TestCacheIsProcessLocal_ReadsAnyOtherBackendAsShared(t *testing.T) {
    runtimeInstance := newCacheScopeRuntime(&sharedBackendDouble{Backend: melodycache.NewInMemoryBackend(melodycache.InMemoryBackendConfig{Clock: melodyclock.NewSystemClock()})})

    if true == cacheIsProcessLocal(runtimeInstance) {
        t.Fatalf("expected a backend that is not the in-process map to be read as shared")
    }

    if "the shared cache, so the running server rereads the database" != cacheClearedScope(runtimeInstance) {
        t.Fatalf("expected the shared scope sentence, got %q", cacheClearedScope(runtimeInstance))
    }
}

func TestCacheIsProcessLocal_ReadsAMissingBackendAsThisProcessOwn(t *testing.T) {
    if false == cacheIsProcessLocal(newCacheScopeRuntime(nil)) {
        t.Fatalf("expected a runtime without a backend to be read as this process's own")
    }
}
