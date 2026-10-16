package cli

import (
    "context"
    "testing"

    melodycache "github.com/precision-soft/melody/v2/cache"
    melodycachecontract "github.com/precision-soft/melody/v2/cache/contract"
    melodyclock "github.com/precision-soft/melody/v2/clock"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyruntime "github.com/precision-soft/melody/v2/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* sharedBackendDouble stands for a backend that is not the in-process map — the redis one, in production — and it is a distinct type on purpose: what the scope helper reads is the TYPE of the backend the wiring registered, not what the backend answers. */
type sharedBackendDouble struct {
    melodycachecontract.Backend
}

func newCacheScopeRuntime(backend melodycachecontract.Backend) melodyruntimecontract.Runtime {
    serviceContainer := melodycontainer.NewContainer()

    if nil != backend {
        melodycontainer.MustRegister(
            serviceContainer,
            melodycache.ServiceCacheBackend,
            func(resolver melodycontainercontract.Resolver) (melodycachecontract.Backend, error) {
                return backend, nil
            },
        )
    }

    return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestCacheIsProcessLocal_ReadsTheInProcessBackendAsThisProcessOwn(t *testing.T) {
    runtimeInstance := newCacheScopeRuntime(melodycache.NewInMemoryBackend(0, 0, melodyclock.NewSystemClock()))

    if false == cacheIsProcessLocal(runtimeInstance) {
        t.Fatalf("expected the in-process backend to be read as this process's own")
    }
}

func TestCacheIsProcessLocal_ReadsAnyOtherBackendAsShared(t *testing.T) {
    runtimeInstance := newCacheScopeRuntime(&sharedBackendDouble{Backend: melodycache.NewInMemoryBackend(0, 0, melodyclock.NewSystemClock())})

    if true == cacheIsProcessLocal(runtimeInstance) {
        t.Fatalf("expected a backend that is not the in-process map to be read as shared")
    }

    if "the shared cache, so the running server rereads the database" != cacheClearedScope(runtimeInstance) {
        t.Fatalf("expected the shared scope sentence, got %q", cacheClearedScope(runtimeInstance))
    }
}

/* a wiring without a backend cannot be shared with anyone, so the helper answers process-local rather than failing the command over a sentence */
func TestCacheIsProcessLocal_ReadsAMissingBackendAsThisProcessOwn(t *testing.T) {
    if false == cacheIsProcessLocal(newCacheScopeRuntime(nil)) {
        t.Fatalf("expected a runtime without a backend to be read as this process's own")
    }
}
