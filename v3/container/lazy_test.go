package container

import (
    "errors"
    "sync"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
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
