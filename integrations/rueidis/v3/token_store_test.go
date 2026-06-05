package rueidis_test

import (
    "context"
    "os"
    "testing"
    "time"

    rueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    redisclient "github.com/redis/rueidis"
)

func newTokenStoreRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func newTokenStoreClient(t *testing.T) redisclient.Client {
    t.Helper()

    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping redis token store integration test")
    }

    provider := rueidis.NewProvider()
    client, openErr := provider.Open(rueidis.NewConnectionParams(address, "", ""))
    if nil != openErr {
        t.Fatalf("open: %v", openErr)
    }

    t.Cleanup(func() {
        provider.Close(client)
    })

    return client
}

func TestRedisTokenStore_PutThenLookupRoundTrips(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:roundtrip"))

    claims := securitycontract.Claims{
        UserIdentifier: "alice",
        Roles:          []string{"ROLE_USER", "ROLE_ADMIN"},
        Scope:          map[string]any{"tenant": "acme"},
        Attributes:     map[string]any{"name": "Alice"},
    }

    store.Put("token-roundtrip", claims)
    defer store.Delete("token-roundtrip")

    found, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "token-roundtrip")
    if nil != lookupErr {
        t.Fatalf("lookup: %v", lookupErr)
    }

    if false == exists {
        t.Fatalf("expected the token to be found")
    }

    if "alice" != found.UserIdentifier || 2 != len(found.Roles) {
        t.Fatalf("unexpected claims: %+v", found)
    }

    if "acme" != found.Scope["tenant"] || "Alice" != found.Attributes["name"] {
        t.Fatalf("scope/attributes did not round-trip: %+v", found)
    }
}

func TestRedisTokenStore_LookupMissingReturnsFalse(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:missing"))

    _, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "absent")
    if nil != lookupErr {
        t.Fatalf("lookup: %v", lookupErr)
    }

    if true == exists {
        t.Fatalf("expected an unknown token to be absent")
    }
}

func TestRedisTokenStore_PutWithTtlExpires(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:ttl"))

    store.PutWithTtl("token-ttl", securitycontract.Claims{UserIdentifier: "bob"}, 100*time.Millisecond)

    time.Sleep(250 * time.Millisecond)

    _, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "token-ttl")
    if nil != lookupErr {
        t.Fatalf("lookup: %v", lookupErr)
    }

    if true == exists {
        t.Fatalf("expected the token to have expired")
    }
}

func TestRedisTokenStore_ZeroTtlDoesNotExpire(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:noexpiry"))

    store.Put("token-noexpiry", securitycontract.Claims{UserIdentifier: "carol"})
    defer store.Delete("token-noexpiry")

    time.Sleep(150 * time.Millisecond)

    _, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "token-noexpiry")
    if nil != lookupErr {
        t.Fatalf("lookup: %v", lookupErr)
    }

    if false == exists {
        t.Fatalf("expected a token without ttl to persist")
    }
}

func TestRedisTokenStore_DeleteRemovesTokenAndIndex(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:delete"))

    store.Put("token-delete", securitycontract.Claims{UserIdentifier: "dave"})
    store.Delete("token-delete")

    _, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "token-delete")
    if nil != lookupErr {
        t.Fatalf("lookup: %v", lookupErr)
    }

    if true == exists {
        t.Fatalf("expected the deleted token to be gone")
    }

    if 0 != store.DeleteByUser("dave") {
        t.Fatalf("expected the index member to have been removed by delete")
    }
}

func TestRedisTokenStore_PutReindexesOnUserChange(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:reindex"))

    store.Put("token-shared", securitycontract.Claims{UserIdentifier: "userA"})
    store.Put("token-shared", securitycontract.Claims{UserIdentifier: "userB"})
    defer store.Delete("token-shared")

    if 0 != store.DeleteByUser("userA") {
        t.Fatalf("expected userA to no longer index the re-issued token")
    }

    found, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "token-shared")
    if nil != lookupErr || false == exists || "userB" != found.UserIdentifier {
        t.Fatalf("expected the token to resolve to userB: %+v %v %v", found, exists, lookupErr)
    }
}

func TestRedisTokenStore_DeleteByUserCountsAndClears(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:deleteuser"))

    store.Put("token-1", securitycontract.Claims{UserIdentifier: "erin"})
    store.Put("token-2", securitycontract.Claims{UserIdentifier: "erin"})
    store.Put("token-3", securitycontract.Claims{UserIdentifier: "erin"})

    if removed := store.DeleteByUser("erin"); 3 != removed {
        t.Fatalf("expected 3 tokens removed, got %d", removed)
    }

    for _, tokenString := range []string{"token-1", "token-2", "token-3"} {
        _, exists, _ := store.Lookup(newTokenStoreRuntime(), tokenString)
        if true == exists {
            t.Fatalf("expected %s to be revoked", tokenString)
        }
    }

    if 0 != store.DeleteByUser("erin") {
        t.Fatalf("expected the user set to be cleared")
    }
}

func TestRedisTokenStore_DeleteByUserSkipsExpiredMembers(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:expiredmember"))

    store.Put("token-live", securitycontract.Claims{UserIdentifier: "frank"})
    store.PutWithTtl("token-expiring", securitycontract.Claims{UserIdentifier: "frank"}, 100*time.Millisecond)

    time.Sleep(250 * time.Millisecond)

    if removed := store.DeleteByUser("frank"); 1 != removed {
        t.Fatalf("expected only the live token counted, got %d", removed)
    }
}

func TestRedisTokenStore_PurgeExpiredPrunesStaleMembers(t *testing.T) {
    client := newTokenStoreClient(t)
    store := rueidis.NewTokenStore(client, rueidis.WithTokenStorePrefix("melody:token:test:purge"))

    store.PutWithTtl("token-stale", securitycontract.Claims{UserIdentifier: "grace"}, 100*time.Millisecond)

    time.Sleep(250 * time.Millisecond)

    if pruned := store.PurgeExpired(); 1 > pruned {
        t.Fatalf("expected at least one stale index member pruned, got %d", pruned)
    }

    if 0 != store.DeleteByUser("grace") {
        t.Fatalf("expected the user set to have been dropped by purge")
    }
}

func TestRedisTokenStore_NewTokenStorePanicsOnNilClient(t *testing.T) {
    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatalf("expected a panic on a nil client")
        }
    }()

    rueidis.NewTokenStore(nil)
}

var _ securitycontract.RevocableTokenStore = (*rueidis.RedisTokenStore)(nil)
