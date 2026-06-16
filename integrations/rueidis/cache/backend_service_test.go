package cache

import (
    "context"
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
