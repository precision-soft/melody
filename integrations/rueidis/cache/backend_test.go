package cache

import (
    "context"
    "reflect"
    "testing"
    "time"

    "github.com/redis/rueidis"
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

/* @info glob escape */

func TestEscapeRedisGlobMeta(t *testing.T) {
    cases := []struct {
        name     string
        input    string
        expected string
    }{
        {name: "default prefix is glob-safe and unchanged", input: "melody:cache:", expected: "melody:cache:"},
        {name: "square brackets escaped", input: "user[42]:", expected: `user\[42\]:`},
        {name: "star and question mark escaped", input: "a*b?c", expected: `a\*b\?c`},
        {name: "backslash escaped", input: `back\slash`, expected: `back\\slash`},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := escapeRedisGlobMeta(testCase.input)
            if testCase.expected != result {
                t.Fatalf("escapeRedisGlobMeta(%q) = %q, want %q (an unescaped glob metacharacter in the literal prefix makes SCAN MATCH miss or over-match keys)", testCase.input, result, testCase.expected)
            }
        })
    }
}

/* @info backend close must not close the caller-owned client (CR #66 back-port from v2/v3) */

type closeTrackingClientCR66 struct {
    rueidis.Client
    closed bool
}

func (instance *closeTrackingClientCR66) Close() {
    instance.closed = true
}

func TestBackendCloseDoesNotCloseCallerOwnedClient(t *testing.T) {
    client := &closeTrackingClientCR66{}

    backend, backendErr := NewBackend(client, nil, "", 0, 0)
    if nil != backendErr {
        t.Fatalf("NewBackend returned an error: %v", backendErr)
    }

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("Backend.Close returned an error: %v", closeErr)
    }

    if true == client.closed {
        t.Fatalf("Backend.Close closed the caller-owned rueidis client; the client lifecycle is owned by the provider, so the backend closing it too double-closes the shared client at shutdown")
    }
}

func TestFloorPositiveExpiry(t *testing.T) {
    cases := []struct {
        name     string
        ttl      time.Duration
        expected time.Duration
    }{
        {name: "zero stays zero (no expiry)", ttl: 0, expected: 0},
        {name: "negative stays negative (no expiry)", ttl: -5 * time.Second, expected: -5 * time.Second},
        {name: "sub-millisecond floors to one millisecond", ttl: 500 * time.Microsecond, expected: time.Millisecond},
        {name: "one nanosecond floors to one millisecond", ttl: time.Nanosecond, expected: time.Millisecond},
        {name: "exactly one millisecond is unchanged", ttl: time.Millisecond, expected: time.Millisecond},
        {name: "above one millisecond is unchanged", ttl: 1500 * time.Millisecond, expected: 1500 * time.Millisecond},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            if floored := floorPositiveExpiry(testCase.ttl); testCase.expected != floored {
                t.Fatalf("floorPositiveExpiry(%v) = %v, expected %v", testCase.ttl, floored, testCase.expected)
            }
        })
    }
}
