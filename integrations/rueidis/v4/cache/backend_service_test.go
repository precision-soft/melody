package cache

import (
    "context"
    "errors"
    "strings"
    "time"

    "testing"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
)

/* liveService answers a service over the redis the validation lane starts, under a prefix unique to the calling test. */
func liveService(t *testing.T) *BackendService {
    t.Helper()

    _, client := liveBackend(t)

    service, serviceErr := NewBackendService(newTestConnection(client), BackendConfig{Prefix: "melody:test:service:" + t.Name() + ":"})
    if nil != serviceErr {
        t.Fatalf("NewBackendService: %v", serviceErr)
    }

    t.Cleanup(func() {
        /* the cleanup clears through its own backend: a test that closed the service under test would otherwise leave its keys behind, since a closed backend refuses Clear like every other operation */
        cleaner, cleanerErr := NewBackend(newTestConnection(client), BackendConfig{Prefix: "melody:test:service:" + t.Name() + ":"})
        if nil != cleanerErr {
            t.Logf("could not build the cleanup backend: %v", cleanerErr)

            return
        }

        if clearErr := cleaner.Clear(testRuntime()); nil != clearErr {
            t.Logf("could not clear the test prefix: %v", clearErr)
        }
    })

    return service
}

func TestNewBackendService_RefusesANilConnection(t *testing.T) {
    if _, err := NewBackendService(nil, BackendConfig{Prefix: "melody:"}); nil == err {
        t.Fatal("a service over a nil connection must be refused at construction")
    }
}

/* every method of the service is a delegate, so they are driven together: a delegate wired to the wrong operation is the defect this shape can carry, and only running each one shows it. */
func TestBackendService_DelegatesEveryOperationToItsBackend(t *testing.T) {
    service := liveService(t)

    if setErr := service.Set(testRuntime(), "one", []byte("first"), bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    payload, found, getErr := service.Get(testRuntime(), "one")
    if nil != getErr || false == found || "first" != string(payload) {
        t.Fatalf("Get = %q, %v, %v", string(payload), found, getErr)
    }

    present, hasErr := service.Has(testRuntime(), "one")
    if nil != hasErr || false == present {
        t.Fatalf("Has = %v, %v", present, hasErr)
    }

    if deleteErr := service.Delete(testRuntime(), "one"); nil != deleteErr {
        t.Fatalf("Delete: %v", deleteErr)
    }

    if present, hasErr = service.Has(testRuntime(), "one"); nil != hasErr || true == present {
        t.Fatalf("Has after Delete = %v, %v", present, hasErr)
    }

    if setMultipleErr := service.SetMultiple(testRuntime(), map[string][]byte{
        "session:a": []byte("alpha"),
        "session:b": []byte("beta"),
        "profile:a": []byte("gamma"),
    }, bound.Of(time.Minute), cachecontract.SetOptions{}); nil != setMultipleErr {
        t.Fatalf("SetMultiple: %v", setMultipleErr)
    }

    values, manyErr := service.Many(testRuntime(), []string{"session:a", "session:b"})
    if nil != manyErr || 2 != len(values) {
        t.Fatalf("Many = %v, %v", values, manyErr)
    }

    if deleteMultipleErr := service.DeleteMultiple(testRuntime(), []string{"session:a"}); nil != deleteMultipleErr {
        t.Fatalf("DeleteMultiple: %v", deleteMultipleErr)
    }

    if clearByPrefixErr := service.ClearByPrefix(testRuntime(), "session:"); nil != clearByPrefixErr {
        t.Fatalf("ClearByPrefix: %v", clearByPrefixErr)
    }

    values, manyErr = service.Many(testRuntime(), []string{"session:b", "profile:a"})
    if nil != manyErr || 1 != len(values) || "gamma" != string(values["profile:a"]) {
        t.Fatalf("ClearByPrefix must remove only its subtree, got %v, %v", values, manyErr)
    }

    counter, incrementErr := service.Increment(testRuntime(), "counter", 4, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != incrementErr || 4 != counter {
        t.Fatalf("Increment = %d, %v", counter, incrementErr)
    }

    counter, decrementErr := service.Decrement(testRuntime(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != decrementErr || 3 != counter {
        t.Fatalf("Decrement = %d, %v", counter, decrementErr)
    }

    if clearErr := service.Clear(testRuntime()); nil != clearErr {
        t.Fatalf("Clear: %v", clearErr)
    }

    if values, manyErr = service.Many(testRuntime(), []string{"profile:a", "counter"}); nil != manyErr || 0 != len(values) {
        t.Fatalf("Clear must empty the prefix, got %v, %v", values, manyErr)
    }
}

/* Close ends the service and leaves the provider-owned client alone: every later call refuses — the in-memory backend's answer behind the same contract — while the client keeps serving its other borrowers, proven through a sibling backend over it. */
func TestBackendService_CloseRefusesLaterCallsAndLeavesTheClientOpen(t *testing.T) {
    service := liveService(t)

    if closeErr := service.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("Close: %v", closeErr)
    }

    setErr := service.Set(testRuntime(), "after-close", []byte("payload"), bound.Of(time.Minute), cachecontract.SetOptions{})
    if nil == setErr {
        t.Fatal("expected the closed service to refuse")
    }

    if false == strings.Contains(setErr.Error(), "cache backend is closed") {
        t.Fatalf("expected the in-memory backend's refusal, got %v", setErr)
    }

    sibling, siblingErr := NewBackend(newTestConnection(service.backend.client), BackendConfig{Prefix: "melody:test:service:" + t.Name() + ":sibling:"})
    if nil != siblingErr {
        t.Fatalf("sibling backend: %v", siblingErr)
    }
    t.Cleanup(func() {
        _ = sibling.Clear(testRuntime())
    })

    if siblingSetErr := sibling.Set(testRuntime(), "after-close", []byte("payload"), bound.Of(time.Minute), cachecontract.SetOptions{}); nil != siblingSetErr {
        t.Fatalf("the provider-owned client must stay open, got %v", siblingSetErr)
    }
}

func TestBackendService_BackendAnswersTheStoredInstance(t *testing.T) {
    service := liveService(t)

    stored := service.Backend()
    if nil == stored {
        t.Fatal("Backend must answer the stored instance")
    }

    if stored != service.Backend() {
        t.Fatal("Backend must answer the same instance on every call")
    }
}

func TestBackendFromRuntime_AnswersTheRegisteredServicesBackend(t *testing.T) {
    service := liveService(t)

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    serviceContainer.MustRegister(
        "cache.backend",
        func(resolver containercontract.Resolver) (*BackendService, error) { return service, nil },
        container.RegisterOptions{},
    )

    runtimeInstance := runtime.NewRun(t.Context(), serviceContainer.NewScope())

    answered := BackendFromRuntime(runtimeInstance, "cache.backend")
    if service.Backend() != answered {
        t.Fatal("BackendFromRuntime must answer the registered service's backend")
    }
}


func TestBackendService_EveryDoorHandsTheCallersContextToTheBackend(t *testing.T) {
    service := liveService(t)

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    doors := map[string]func() error{
        "Get":            func() error { _, _, err := service.Get(testRuntimeUnder(canceledContext), "key"); return err },
        "Set":            func() error { return service.Set(testRuntimeUnder(canceledContext), "key", []byte("value"), bound.Of(time.Minute), cachecontract.SetOptions{}) },
        "Delete":         func() error { return service.Delete(testRuntimeUnder(canceledContext), "key") },
        "Has":            func() error { _, err := service.Has(testRuntimeUnder(canceledContext), "key"); return err },
        "Clear":          func() error { return service.Clear(testRuntimeUnder(canceledContext)) },
        "ClearByPrefix":  func() error { return service.ClearByPrefix(testRuntimeUnder(canceledContext), "key") },
        "Many":           func() error { _, err := service.Many(testRuntimeUnder(canceledContext), []string{"key"}); return err },
        "SetMultiple":    func() error { return service.SetMultiple(testRuntimeUnder(canceledContext), map[string][]byte{"key": []byte("value")}, bound.Of(time.Minute), cachecontract.SetOptions{}) },
        "DeleteMultiple": func() error { return service.DeleteMultiple(testRuntimeUnder(canceledContext), []string{"key"}) },
        "Increment":      func() error { _, err := service.Increment(testRuntimeUnder(canceledContext), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
        "Decrement":      func() error { _, err := service.Decrement(testRuntimeUnder(canceledContext), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
    }

    for door, call := range doors {
        if doorErr := call(); false == errors.Is(doorErr, context.Canceled) {
            t.Fatalf("%s: expected the caller's done context to reach the backend, got %v", door, doorErr)
        }
    }
}

func TestBackendService_ACounterHandsItsOptionsToTheBackend(t *testing.T) {
    backend, client := liveBackend(t)
    service := &BackendService{backend: backend}
    ctx := context.Background()
    prefix := "melody:test:" + t.Name() + ":"

    if _, incrementErr := service.Increment(testRuntimeUnder(ctx), "up", 1, cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)}); nil != incrementErr {
        t.Fatal(incrementErr)
    }
    if _, decrementErr := service.Decrement(testRuntimeUnder(ctx), "down", 1, cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)}); nil != decrementErr {
        t.Fatal(decrementErr)
    }

    for _, key := range []string{"up", "down"} {
        if remaining := rawTtl(t, client, prefix+key); remaining <= 0 {
            t.Fatalf("expected %s to carry the ttl its options asked, got %d", key, remaining)
        }
    }
}
