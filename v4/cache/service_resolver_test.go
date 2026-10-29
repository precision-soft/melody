package cache

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

func TestCacheServiceResolvers_HappyPath(t *testing.T) {
    serviceContainer := container.NewContainer()

    clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
    backend := NewInMemoryBackend(InMemoryBackendConfig{MaxItems: bound.Of(10), CleanupInterval: bound.Of(time.Hour), Clock: clockInstance})
    serializer := NewJsonSerializer()
    cacheInstance := NewManager(backend, serializer, ManagerConfig{})

    err := serviceContainer.Register(
        ServiceCacheBackend,
        func(resolver containercontract.Resolver) (*InMemoryBackend, error) {
            return backend, nil
        },
        container.RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("register backend error: %v", err)
    }

    err = serviceContainer.Register(
        ServiceCacheSerializer,
        func(resolver containercontract.Resolver) (cachecontract.Serializer, error) {
            return serializer, nil
        },
        container.RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("register serializer error: %v", err)
    }

    err = serviceContainer.Register(
        ServiceCache,
        func(resolver containercontract.Resolver) (*Manager, error) {
            return cacheInstance, nil
        },
        container.RegisterOptions{},
    )
    if nil != err {
        t.Fatalf("register cache error: %v", err)
    }

    resolvedBackend := CacheBackendMustFromContainer(serviceContainer)
    if nil == resolvedBackend {
        t.Fatalf("expected backend")
    }

    resolvedSerializer := CacheSerializerMustFromContainer(serviceContainer)
    if nil == resolvedSerializer {
        t.Fatalf("expected serializer")
    }

    resolvedCache := CacheMustFromContainer(serviceContainer)
    if nil == resolvedCache {
        t.Fatalf("expected cache")
    }

    resolvedBackend = CacheBackendMustFromResolver(serviceContainer)
    if nil == resolvedBackend {
        t.Fatalf("expected backend from resolver")
    }

    resolvedSerializer = CacheSerializerMustFromResolver(serviceContainer)
    if nil == resolvedSerializer {
        t.Fatalf("expected serializer from resolver")
    }

    _ = backend.Close()
}
