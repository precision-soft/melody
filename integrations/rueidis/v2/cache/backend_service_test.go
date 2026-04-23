package cache

import (
    "context"
    "reflect"
    "testing"
    "time"
)

func reflectFieldNames(value any) []string {
    reflectedValue := reflect.ValueOf(value)
    if reflect.Ptr == reflectedValue.Kind() {
        reflectedValue = reflectedValue.Elem()
    }

    reflectedType := reflectedValue.Type()
    fieldNames := make([]string, 0, reflectedType.NumField())
    for fieldIndex := 0; fieldIndex < reflectedType.NumField(); fieldIndex = fieldIndex + 1 {
        fieldNames = append(fieldNames, reflectedType.Field(fieldIndex).Name)
    }

    return fieldNames
}

func TestBackendStructHasStoredCtx(t *testing.T) {
    backend := &Backend{}

    fields := reflectFieldNames(backend)
    hasCtx := false
    for _, fieldName := range fields {
        if "ctx" == fieldName {
            hasCtx = true
            break
        }
    }

    if false == hasCtx {
        t.Fatalf("Backend struct must carry a stored `ctx` field for v3.0.x source-compatibility (MEL-165)")
    }
}

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

func TestBackendCtxMethodsExist(t *testing.T) {
    backendType := reflect.TypeOf((*Backend)(nil))

    expected := []string{
        "GetCtx",
        "SetCtx",
        "DeleteCtx",
        "HasCtx",
        "ClearCtx",
        "ClearByPrefixCtx",
        "ManyCtx",
        "SetMultipleCtx",
        "DeleteMultipleCtx",
        "IncrementCtx",
        "DecrementCtx",
    }

    for _, methodName := range expected {
        if _, found := backendType.MethodByName(methodName); false == found {
            t.Fatalf("expected *Backend to expose %s for the ctx-first API", methodName)
        }
    }
}

func TestBackendLegacyMethodsDelegateToCtx(t *testing.T) {
    backendType := reflect.TypeOf((*Backend)(nil))

    legacy := []string{
        "Get",
        "Set",
        "Delete",
        "Has",
        "Clear",
        "ClearByPrefix",
        "Many",
        "SetMultiple",
        "DeleteMultiple",
        "Increment",
        "Decrement",
    }

    for _, methodName := range legacy {
        method, found := backendType.MethodByName(methodName)
        if false == found {
            t.Fatalf("expected *Backend to expose deprecated %s for v3.0.x source-compatibility", methodName)
        }

        for inputIndex := 1; inputIndex < method.Type.NumIn(); inputIndex = inputIndex + 1 {
            if "context.Context" == method.Type.In(inputIndex).String() {
                t.Fatalf(
                    "deprecated %s must not take a context.Context parameter (ctx is stored on Backend)",
                    methodName,
                )
            }
        }
    }
}

var (
    _ func(*Backend, context.Context, string) ([]byte, bool, error)           = (*Backend).GetCtx
    _ func(*Backend, context.Context, string, []byte, time.Duration) error    = (*Backend).SetCtx
    _ func(*Backend, context.Context, string) error                           = (*Backend).DeleteCtx
    _ func(*Backend, context.Context, string) (bool, error)                   = (*Backend).HasCtx
    _ func(*Backend, context.Context) error                                   = (*Backend).ClearCtx
    _ func(*Backend, context.Context, string) error                           = (*Backend).ClearByPrefixCtx
    _ func(*Backend, context.Context, []string) (map[string][]byte, error)    = (*Backend).ManyCtx
    _ func(*Backend, context.Context, map[string][]byte, time.Duration) error = (*Backend).SetMultipleCtx
    _ func(*Backend, context.Context, []string) error                         = (*Backend).DeleteMultipleCtx
    _ func(*Backend, context.Context, string, int64) (int64, error)           = (*Backend).IncrementCtx
    _ func(*Backend, context.Context, string, int64) (int64, error)           = (*Backend).DecrementCtx
)

var (
    _ func(*Backend, string) ([]byte, bool, error)           = (*Backend).Get
    _ func(*Backend, string, []byte, time.Duration) error    = (*Backend).Set
    _ func(*Backend, string) error                           = (*Backend).Delete
    _ func(*Backend, string) (bool, error)                   = (*Backend).Has
    _ func(*Backend) error                                   = (*Backend).Clear
    _ func(*Backend, string) error                           = (*Backend).ClearByPrefix
    _ func(*Backend, []string) (map[string][]byte, error)    = (*Backend).Many
    _ func(*Backend, map[string][]byte, time.Duration) error = (*Backend).SetMultiple
    _ func(*Backend, []string) error                         = (*Backend).DeleteMultiple
    _ func(*Backend, string, int64) (int64, error)           = (*Backend).Increment
    _ func(*Backend, string, int64) (int64, error)           = (*Backend).Decrement
)
