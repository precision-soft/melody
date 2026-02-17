package cache

import (
	"testing"
	"time"

	cachecontract "github.com/precision-soft/melody/v2/cache/contract"
	"github.com/precision-soft/melody/v2/container"
	containercontract "github.com/precision-soft/melody/v2/container/contract"
)

func TestCacheServiceResolvers_HappyPath(t *testing.T) {
	serviceContainer := container.NewContainer()

	clockInstance := &cacheTestClock{now: time.Unix(10, 0)}
	backend := NewInMemoryBackend(10, time.Hour, clockInstance)
	serializer := NewJsonSerializer()
	cacheInstance := NewManager(backend, serializer)

	err := serviceContainer.Register(
		ServiceCacheBackend,
		func(resolver containercontract.Resolver) (*InMemoryBackend, error) {
			return backend, nil
		},
	)
	if nil != err {
		t.Fatalf("register backend error: %v", err)
	}

	err = serviceContainer.Register(
		ServiceCacheSerializer,
		func(resolver containercontract.Resolver) (cachecontract.Serializer, error) {
			return serializer, nil
		},
	)
	if nil != err {
		t.Fatalf("register serializer error: %v", err)
	}

	err = serviceContainer.Register(
		ServiceCache,
		func(resolver containercontract.Resolver) (*Manager, error) {
			return cacheInstance, nil
		},
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
