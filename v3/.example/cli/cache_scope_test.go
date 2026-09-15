package cli

import (
    "testing"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
)

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

func TestCacheIsProcessLocal_ReadsAMissingBackendAsThisProcessOwn(t *testing.T) {
    if false == cacheIsProcessLocal(newCacheScopeRuntime(nil)) {
        t.Fatalf("expected a runtime without a backend to be read as this process's own")
    }
}
