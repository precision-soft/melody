package cache

import (
    "context"
    "time"

    "github.com/precision-soft/melody/container"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/runtime"
    "reflect"
    "testing"
)

func TestBackendServiceWithContextReturnType(t *testing.T) {
    method, found := reflect.TypeOf((*BackendService)(nil)).MethodByName("WithContext")
    if false == found {
        t.Fatalf("BackendService must expose WithContext")
    }

    methodType := method.Type
    if 1 != methodType.NumOut() {
        t.Fatalf("WithContext must return exactly one value, got %d", methodType.NumOut())
    }

    returnTypeName := methodType.Out(0).String()
    if "*cache.Backend" != returnTypeName {
        t.Fatalf("WithContext must return *cache.Backend for v3.0.x compatibility, got %q", returnTypeName)
    }
}

func TestBackendServiceWithNilContextFallsBackToStoredBackend(t *testing.T) {
    underlying := &Backend{ctx: context.Background()}
    service := &BackendService{
        client:  nil,
        backend: underlying,
    }

    bound := service.WithContext(nil)
    if bound != underlying {
        t.Fatalf("WithContext(nil) must return the stored backend unchanged")
    }
}

/* liveService answers a service over the redis the validation lane starts, under a prefix unique to the calling test. */
func liveService(t *testing.T) *BackendService {
    t.Helper()

    _, client := liveBackend(t)

    service, serviceErr := NewBackendService(client, "melody:test:service:"+t.Name()+":", 0, 0)
    if nil != serviceErr {
        t.Fatalf("NewBackendService: %v", serviceErr)
    }

    t.Cleanup(func() {
        if clearErr := service.Clear(); nil != clearErr {
            t.Logf("could not clear the test prefix: %v", clearErr)
        }
    })

    return service
}

func TestNewBackendService_RefusesANilClient(t *testing.T) {
    if _, err := NewBackendService(nil, "melody:", 0, 0); nil == err {
        t.Fatal("a service over a nil client must be refused at construction")
    }
}

/* every method of the service is a delegate, so they are driven together: a delegate wired to the wrong operation is the defect this shape can carry, and only running each one shows it. */
func TestBackendService_DelegatesEveryOperationToItsBackend(t *testing.T) {
    service := liveService(t)

    if setErr := service.Set("one", []byte("first"), time.Minute); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    payload, found, getErr := service.Get("one")
    if nil != getErr || false == found || "first" != string(payload) {
        t.Fatalf("Get = %q, %v, %v", string(payload), found, getErr)
    }

    present, hasErr := service.Has("one")
    if nil != hasErr || false == present {
        t.Fatalf("Has = %v, %v", present, hasErr)
    }

    if deleteErr := service.Delete("one"); nil != deleteErr {
        t.Fatalf("Delete: %v", deleteErr)
    }

    if present, hasErr = service.Has("one"); nil != hasErr || true == present {
        t.Fatalf("Has after Delete = %v, %v", present, hasErr)
    }

    if setMultipleErr := service.SetMultiple(map[string][]byte{
        "session:a": []byte("alpha"),
        "session:b": []byte("beta"),
        "profile:a": []byte("gamma"),
    }, time.Minute); nil != setMultipleErr {
        t.Fatalf("SetMultiple: %v", setMultipleErr)
    }

    values, manyErr := service.Many([]string{"session:a", "session:b"})
    if nil != manyErr || 2 != len(values) {
        t.Fatalf("Many = %v, %v", values, manyErr)
    }

    if deleteMultipleErr := service.DeleteMultiple([]string{"session:a"}); nil != deleteMultipleErr {
        t.Fatalf("DeleteMultiple: %v", deleteMultipleErr)
    }

    if clearByPrefixErr := service.ClearByPrefix("session:"); nil != clearByPrefixErr {
        t.Fatalf("ClearByPrefix: %v", clearByPrefixErr)
    }

    values, manyErr = service.Many([]string{"session:b", "profile:a"})
    if nil != manyErr || 1 != len(values) || "gamma" != string(values["profile:a"]) {
        t.Fatalf("ClearByPrefix must remove only its subtree, got %v, %v", values, manyErr)
    }

    counter, incrementErr := service.Increment("counter", 4)
    if nil != incrementErr || 4 != counter {
        t.Fatalf("Increment = %d, %v", counter, incrementErr)
    }

    counter, decrementErr := service.Decrement("counter", 1)
    if nil != decrementErr || 3 != counter {
        t.Fatalf("Decrement = %d, %v", counter, decrementErr)
    }

    if clearErr := service.Clear(); nil != clearErr {
        t.Fatalf("Clear: %v", clearErr)
    }

    if values, manyErr = service.Many([]string{"profile:a", "counter"}); nil != manyErr || 0 != len(values) {
        t.Fatalf("Clear must empty the prefix, got %v, %v", values, manyErr)
    }
}

/* Close is a no-op because the provider owns the client: the service must stay usable after it, which is what tells a no-op apart from a close that happened to be silent. */
func TestBackendService_CloseLeavesTheCallerOwnedClientUsable(t *testing.T) {
    service := liveService(t)

    if closeErr := service.Close(); nil != closeErr {
        t.Fatalf("Close: %v", closeErr)
    }

    if setErr := service.Set("after-close", []byte("payload"), time.Minute); nil != setErr {
        t.Fatalf("the client is owned by the provider, so the service stays usable after Close: %v", setErr)
    }
}

func TestBackendService_BackendAnswersTheStoredInstanceAndWithContextBindsANewOne(t *testing.T) {
    service := liveService(t)

    stored := service.Backend()
    if nil == stored {
        t.Fatal("Backend must answer the stored instance")
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    bound := service.WithContext(ctx)
    if bound == stored {
        t.Fatal("WithContext must bind a new instance rather than rewrite the stored one")
    }

    if ctx != bound.ctx {
        t.Fatal("the bound instance must carry the context it was given")
    }

    if stored.prefix != bound.prefix || stored.scanCount != bound.scanCount || stored.deleteBatch != bound.deleteBatch {
        t.Fatal("the bound instance must keep the stored configuration")
    }
}

func TestBackendFromRuntime_BindsTheRequestContextToTheRegisteredService(t *testing.T) {
    service := liveService(t)

    serviceContainer := container.NewContainer()
    serviceContainer.MustRegister(
        "cache.backend",
        func(resolver containercontract.Resolver) (*BackendService, error) { return service, nil },
    )

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    runtimeInstance := runtime.New(ctx, serviceContainer.NewScope(), serviceContainer)

    bound := BackendFromRuntime(runtimeInstance, "cache.backend")
    if nil == bound {
        t.Fatal("BackendFromRuntime must answer a backend")
    }

    if ctx != bound.ctx {
        t.Fatal("the answered backend must carry the runtime's context")
    }
}
