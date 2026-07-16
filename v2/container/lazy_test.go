package container

import (
    "errors"
    "sync"
    "testing"
    "time"

    containercontract "github.com/precision-soft/melody/v2/container/contract"
)

func TestLazyService_DefersResolutionUntilFirstGet(t *testing.T) {
    serviceContainer := NewContainer()

    resolveCount := 0
    MustRegister[string](serviceContainer, "service.lazy.value", func(resolver containercontract.Resolver) (string, error) {
        resolveCount++

        return "resolved", nil
    })

    lazy := Lazy[string](serviceContainer, "service.lazy.value")
    if 0 != resolveCount {
        t.Fatalf("expected no resolution before the first Get, got %d", resolveCount)
    }

    if "resolved" != lazy.Get() {
        t.Fatalf("expected the resolved value on first Get")
    }

    if "resolved" != lazy.Get() {
        t.Fatalf("expected the memoized value on the second Get")
    }

    if 1 != resolveCount {
        t.Fatalf("expected exactly one resolution, got %d", resolveCount)
    }
}

func TestLazyService_ResolvesProviderRegisteredAfterTheHandle(t *testing.T) {
    serviceContainer := NewContainer()

    /* the handle is built before the provider exists — exactly the boot-phase ordering the helper is for: a cli command captures the lazy handle while services are still being registered. */
    lazy := Lazy[string](serviceContainer, "service.lazy.late")

    MustRegister[string](serviceContainer, "service.lazy.late", func(resolver containercontract.Resolver) (string, error) {
        return "late", nil
    })

    if "late" != lazy.Get() {
        t.Fatalf("expected the lazily-registered value")
    }
}

func TestLazyService_ResolveReportsMissingService(t *testing.T) {
    serviceContainer := NewContainer()

    lazy := Lazy[string](serviceContainer, "service.lazy.absent")

    _, resolveErr := lazy.Resolve()
    if nil == resolveErr {
        t.Fatalf("expected an error resolving an unregistered service")
    }
}

func TestLazyService_RetriesResolutionAfterFailure(t *testing.T) {
    serviceContainer := NewContainer()

    resolveCount := 0
    MustRegister[string](serviceContainer, "service.lazy.flaky", func(resolver containercontract.Resolver) (string, error) {
        resolveCount++

        if 1 == resolveCount {
            return "", errors.New("store not reachable yet")
        }

        return "recovered", nil
    })

    lazy := Lazy[string](serviceContainer, "service.lazy.flaky")

    _, firstErr := lazy.Resolve()
    if nil == firstErr {
        t.Fatalf("expected the first resolution failure to surface")
    }

    value, secondErr := lazy.Resolve()
    if nil != secondErr {
        t.Fatalf("expected the second resolution to retry the resolver and succeed, got %v", secondErr)
    }

    if "recovered" != value {
        t.Fatalf("expected the recovered value, got %q", value)
    }

    if "recovered" != lazy.Get() {
        t.Fatalf("expected the memoized value after the successful resolution")
    }

    if 2 != resolveCount {
        t.Fatalf("expected the success to be memoized after two attempts, got %d", resolveCount)
    }
}

/** @info the resolver runs outside the handle's lock, so a resolver that reaches back into the same handle — a provider chain that cycles through a lazy handle — recurses into the resolution machinery, where a cycle guard can answer with an error; under a lock held across the resolution it would deadlock instead, unreachable by any cycle detection. */
func TestLazyService_ResolverReachingBackIntoTheHandleIsNotDeadlocked(t *testing.T) {
    depth := 0
    var handle *LazyService[string]
    handle = &LazyService[string]{
        resolve: func() (string, error) {
            depth++
            if 1 < depth {
                return "", errors.New("cycle detected by the resolution stack")
            }

            _, innerErr := handle.Resolve()
            if nil != innerErr {
                return "", innerErr
            }

            return "unreachable", nil
        },
    }

    resolved := make(chan error, 1)
    go func() {
        _, resolveErr := handle.Resolve()
        resolved <- resolveErr
    }()

    select {
    case resolveErr := <-resolved:
        if nil == resolveErr {
            t.Fatal("expected the reentrant resolution to surface the cycle error")
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected the reentrant resolution to return instead of deadlocking on the handle's lock")
    }
}

type lazyProbeItem struct {
    name string
}

/** @info a resolution yielding nil without an error must not be memoized as success: Get keeps its documented retry-on-failure promise only if the next call re-runs the resolver instead of returning the poisoned nil forever. */
func TestLazyService_NilYieldIsNotMemoized(t *testing.T) {
    resolveCount := 0
    handle := &LazyService[*lazyProbeItem]{
        resolve: func() (*lazyProbeItem, error) {
            resolveCount++
            if 1 == resolveCount {
                return nil, nil
            }

            return &lazyProbeItem{name: "real"}, nil
        },
    }

    firstValue, firstErr := handle.Resolve()
    if nil != firstErr {
        t.Fatalf("expected the nil yield to pass through without an error, got %v", firstErr)
    }

    if nil != firstValue {
        t.Fatalf("expected the first resolution to yield nil, got %+v", firstValue)
    }

    secondValue, secondErr := handle.Resolve()
    if nil != secondErr {
        t.Fatalf("expected the second resolution to retry the resolver, got %v", secondErr)
    }

    if nil == secondValue || "real" != secondValue.name {
        t.Fatalf("expected the retried resolution to yield the real value, got %+v", secondValue)
    }

    if "real" != handle.Get().name {
        t.Fatalf("expected the memoized value after the successful resolution")
    }

    if 2 != resolveCount {
        t.Fatalf("expected the nil yield not to be memoized and the success to be, got %d resolutions", resolveCount)
    }
}

func TestLazyService_GetIsSafeForConcurrentUse(t *testing.T) {
    serviceContainer := NewContainer()

    MustRegister[string](serviceContainer, "service.lazy.concurrent", func(resolver containercontract.Resolver) (string, error) {
        return "shared", nil
    })

    lazy := Lazy[string](serviceContainer, "service.lazy.concurrent")

    var waitGroup sync.WaitGroup
    for index := 0; index < 32; index++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            if "shared" != lazy.Get() {
                t.Errorf("expected the shared value under concurrent Get")
            }
        }()
    }

    waitGroup.Wait()
}
