package rueidis

import (
    "strings"
    "testing"
    "time"

    securitycontract "github.com/precision-soft/melody/v3/security/contract"
)

func TestRedisTokenStore_PutThenLookupRoundTrips(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:roundtrip"))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:missing"))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:ttl"))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:noexpiry"))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:delete"))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:reindex"))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:deleteuser"))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:expiredmember"))

    store.Put("token-live", securitycontract.Claims{UserIdentifier: "frank"})
    store.PutWithTtl("token-expiring", securitycontract.Claims{UserIdentifier: "frank"}, 100*time.Millisecond)

    time.Sleep(250 * time.Millisecond)

    if removed := store.DeleteByUser("frank"); 1 != removed {
        t.Fatalf("expected only the live token counted, got %d", removed)
    }
}

func TestRedisTokenStore_PurgeExpiredPrunesStaleMembers(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:purge"))

    store.PutWithTtl("token-stale", securitycontract.Claims{UserIdentifier: "grace"}, 100*time.Millisecond)

    time.Sleep(250 * time.Millisecond)

    if pruned := store.PurgeExpired(); 1 > pruned {
        t.Fatalf("expected at least one stale index member pruned, got %d", pruned)
    }

    if 0 != store.DeleteByUser("grace") {
        t.Fatalf("expected the user set to have been dropped by purge")
    }
}

func TestRedisTokenStore_DeleteByUserDoesNotRevokeReissuedTokenOfAnotherUser(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:staleindex"))

    store.PutWithTtl("reused-token", securitycontract.Claims{UserIdentifier: "userA"}, 100*time.Millisecond)

    time.Sleep(250 * time.Millisecond)

    store.Put("reused-token", securitycontract.Claims{UserIdentifier: "userB"})
    defer store.Delete("reused-token")

    store.PurgeExpired()

    if removed := store.DeleteByUser("userA"); 0 != removed {
        t.Fatalf("expected userA to own no live token (its token expired before being re-issued to userB), got removed=%d", removed)
    }

    found, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "reused-token")
    if nil != lookupErr || false == exists || "userB" != found.UserIdentifier {
        t.Fatalf("expected userB's live token to survive DeleteByUser(userA): %+v %v %v", found, exists, lookupErr)
    }
}

func TestRedisTokenStore_NewTokenStorePanicsOnNilClient(t *testing.T) {
    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatalf("expected a panic on a nil client")
        }
    }()

    NewTokenStore(nil)
}

var _ securitycontract.RevocableTokenStore = (*RedisTokenStore)(nil)

/** @info hash-tag cluster co-location */

func hashTagOf(key string) string {
    start := strings.Index(key, "{")
    if -1 == start {
        return ""
    }

    end := strings.Index(key[start+1:], "}")
    if -1 == end {
        return ""
    }

    return key[start+1 : start+1+end]
}

func TestTokenStoreKeysShareHashTagForClusterColocation(t *testing.T) {
    store := &RedisTokenStore{prefix: "melody:token"}

    tokenTag := hashTagOf(store.tokenKey("abc"))
    userTag := hashTagOf(store.userKey("alice"))
    userPrefixTag := hashTagOf(store.userKeyPrefix())

    if "" == tokenTag {
        t.Fatalf("expected the token key to carry a hash tag, got %q", store.tokenKey("abc"))
    }

    if tokenTag != userTag || tokenTag != userPrefixTag {
        t.Fatalf(
            "expected every token-store key to share one hash tag for cluster co-location, got token=%q user=%q prefix=%q",
            tokenTag,
            userTag,
            userPrefixTag,
        )
    }
}
