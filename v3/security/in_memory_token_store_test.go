package security_test

import (
    "context"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/security"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func tokenStoreRuntime() runtimecontract.Runtime {
    c := container.NewContainer()
    return runtime.New(context.Background(), c.NewScope(), c)
}

func TestInMemoryTokenStore_LookupDeepCopiesNestedAttributeMap(t *testing.T) {
    store := security.NewInMemoryTokenStore()
    rt := tokenStoreRuntime()

    store.Put("tok", securitycontract.Claims{
        UserIdentifier: "u1",
        Attributes: map[string]any{
            "meta": map[string]any{"x": 1},
        },
    })

    c1, found, err := store.Lookup(rt, "tok")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    c1.Attributes["meta"].(map[string]any)["x"] = 99

    c2, _, _ := store.Lookup(rt, "tok")
    if 99 == c2.Attributes["meta"].(map[string]any)["x"] {
        t.Fatalf("mutating a nested map returned by Lookup corrupted the stored entry")
    }
}

func TestInMemoryTokenStore_PutDeepCopiesNestedScopeMap(t *testing.T) {
    store := security.NewInMemoryTokenStore()
    rt := tokenStoreRuntime()

    original := securitycontract.Claims{
        UserIdentifier: "u1",
        Scope: map[string]any{
            "ns": map[string]any{"read": true},
        },
    }

    store.Put("tok", original)

    original.Scope["ns"].(map[string]any)["read"] = false

    c, found, err := store.Lookup(rt, "tok")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    if false == c.Scope["ns"].(map[string]any)["read"].(bool) {
        t.Fatalf("mutating the caller's Scope map after Put corrupted the stored entry")
    }
}
