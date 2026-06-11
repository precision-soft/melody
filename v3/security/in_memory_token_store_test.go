package security

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func tokenStoreRuntime() runtimecontract.Runtime {
    c := container.NewContainer()
    return runtime.New(context.Background(), c.NewScope(), c)
}

func TestInMemoryTokenStore_TtlExpiresToken(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    store.PutWithTtl("short-lived", securitycontract.Claims{UserIdentifier: "user-1"}, 30*time.Second)

    runtimeInstance := testRuntime()

    if _, found, _ := store.Lookup(runtimeInstance, "short-lived"); false == found {
        t.Fatalf("expected token to resolve before expiry")
    }

    frozen.Advance(time.Minute)

    if _, found, _ := store.Lookup(runtimeInstance, "short-lived"); true == found {
        t.Fatalf("expected token to stop resolving after the ttl elapses")
    }
}

func TestInMemoryTokenStore_DeleteByUserRevokesEveryToken(t *testing.T) {
    store := NewInMemoryTokenStore()
    store.Put("token-a", securitycontract.Claims{UserIdentifier: "user-1"})
    store.Put("token-b", securitycontract.Claims{UserIdentifier: "user-1"})
    store.Put("token-c", securitycontract.Claims{UserIdentifier: "user-2"})

    runtimeInstance := testRuntime()

    if removed := store.DeleteByUser("user-1"); 2 != removed {
        t.Fatalf("expected two tokens revoked, got %d", removed)
    }

    if _, found, _ := store.Lookup(runtimeInstance, "token-a"); true == found {
        t.Fatalf("expected token-a to be revoked")
    }
    if _, found, _ := store.Lookup(runtimeInstance, "token-c"); false == found {
        t.Fatalf("expected the other user's token to survive")
    }
}

func TestInMemoryTokenStore_PurgeExpiredDropsElapsedEntries(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    store.PutWithTtl("short", securitycontract.Claims{UserIdentifier: "user-1"}, 30*time.Second)
    store.Put("forever", securitycontract.Claims{UserIdentifier: "user-2"})

    frozen.Advance(time.Minute)

    if purged := store.PurgeExpired(); 1 != purged {
        t.Fatalf("expected exactly one expired entry purged, got %d", purged)
    }

    if _, found, _ := store.Lookup(testRuntime(), "forever"); false == found {
        t.Fatalf("expected the non-expiring token to survive the purge")
    }
}

func TestInMemoryTokenStore_LookupReturnsIsolatedClaims(t *testing.T) {
    store := NewInMemoryTokenStore()
    store.Put("opaque-iso", securitycontract.Claims{
        UserIdentifier: "user-1",
        Roles:          []string{"ROLE_USER"},
        Scope:          map[string]any{"company": "acme"},
    })

    first, _, _ := store.Lookup(testRuntime(), "opaque-iso")
    first.Roles[0] = "ROLE_ADMIN"
    first.Scope["company"] = "evil"

    second, _, _ := store.Lookup(testRuntime(), "opaque-iso")
    if "ROLE_USER" != second.Roles[0] {
        t.Fatalf("expected stored roles to be isolated from a returned copy, got %v", second.Roles)
    }
    if "acme" != second.Scope["company"] {
        t.Fatalf("expected stored scope to be isolated from a returned copy, got %v", second.Scope)
    }
}

func TestInMemoryTokenStore_LookupDeepCopiesNestedAttributeMap(t *testing.T) {
    store := NewInMemoryTokenStore()
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
    store := NewInMemoryTokenStore()
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
