package rueidis

import (
    "context"
    "errors"
    "fmt"
    "strconv"
    "strings"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodyclock "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
    redisclient "github.com/redis/rueidis"
)

func TestRedisTokenStore_PutThenLookupRoundTrips(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:roundtrip"})

    claims := securitycontract.Claims{
        UserIdentifier: "alice",
        Roles:          []string{"ROLE_USER", "ROLE_ADMIN"},
        Scope:          map[string]any{"tenant": "acme"},
        Attributes:     map[string]any{"name": "Alice"},
    }

    store.Put(newTokenStoreRuntime(), "token-roundtrip", claims, bound.UnlimitedDuration())
    defer store.Delete(newTokenStoreRuntime(), "token-roundtrip")

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
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:missing"})

    _, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "absent")
    if nil != lookupErr {
        t.Fatalf("lookup: %v", lookupErr)
    }

    if true == exists {
        t.Fatalf("expected an unknown token to be absent")
    }
}

func TestRedisTokenStore_PutWithATtlExpires(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:ttl"})

    store.Put(newTokenStoreRuntime(), "token-ttl", securitycontract.Claims{UserIdentifier: "bob"}, bound.Of(time.Second))

    time.Sleep(1150 * time.Millisecond)

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
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:noexpiry"})

    store.Put(newTokenStoreRuntime(), "token-noexpiry", securitycontract.Claims{UserIdentifier: "carol"}, bound.UnlimitedDuration())
    defer store.Delete(newTokenStoreRuntime(), "token-noexpiry")

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
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:delete"})

    store.Put(newTokenStoreRuntime(), "token-delete", securitycontract.Claims{UserIdentifier: "dave"}, bound.UnlimitedDuration())
    store.Delete(newTokenStoreRuntime(), "token-delete")

    _, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "token-delete")
    if nil != lookupErr {
        t.Fatalf("lookup: %v", lookupErr)
    }

    if true == exists {
        t.Fatalf("expected the deleted token to be gone")
    }

    if 0 != mustDeleteByUser(t, store, "dave") {
        t.Fatalf("expected the index member to have been removed by delete")
    }
}

func TestRedisTokenStore_PutReindexesOnUserChange(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:reindex"})

    store.Put(newTokenStoreRuntime(), "token-shared", securitycontract.Claims{UserIdentifier: "userA"}, bound.UnlimitedDuration())
    store.Put(newTokenStoreRuntime(), "token-shared", securitycontract.Claims{UserIdentifier: "userB"}, bound.UnlimitedDuration())
    defer store.Delete(newTokenStoreRuntime(), "token-shared")

    if 0 != mustDeleteByUser(t, store, "userA") {
        t.Fatalf("expected userA to no longer index the re-issued token")
    }

    found, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "token-shared")
    if nil != lookupErr || false == exists || "userB" != found.UserIdentifier {
        t.Fatalf("expected the token to resolve to userB: %+v %v %v", found, exists, lookupErr)
    }
}

func TestRedisTokenStore_DeleteByUserCountsAndClears(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:deleteuser"})

    store.Put(newTokenStoreRuntime(), "token-1", securitycontract.Claims{UserIdentifier: "erin"}, bound.UnlimitedDuration())
    store.Put(newTokenStoreRuntime(), "token-2", securitycontract.Claims{UserIdentifier: "erin"}, bound.UnlimitedDuration())
    store.Put(newTokenStoreRuntime(), "token-3", securitycontract.Claims{UserIdentifier: "erin"}, bound.UnlimitedDuration())

    if removed, _ := store.DeleteByUser(newTokenStoreRuntime(), "erin"); 3 != removed {
        t.Fatalf("expected 3 tokens removed, got %d", removed)
    }

    for _, tokenString := range []string{"token-1", "token-2", "token-3"} {
        _, exists, _ := store.Lookup(newTokenStoreRuntime(), tokenString)
        if true == exists {
            t.Fatalf("expected %s to be revoked", tokenString)
        }
    }

    if 0 != mustDeleteByUser(t, store, "erin") {
        t.Fatalf("expected the user set to be cleared")
    }
}

func TestRedisTokenStore_DeleteByUserSkipsExpiredMembers(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:expiredmember"})

    store.Put(newTokenStoreRuntime(), "token-live", securitycontract.Claims{UserIdentifier: "frank"}, bound.UnlimitedDuration())
    store.Put(newTokenStoreRuntime(), "token-expiring", securitycontract.Claims{UserIdentifier: "frank"}, bound.Of(time.Second))

    time.Sleep(1150 * time.Millisecond)

    if removed, _ := store.DeleteByUser(newTokenStoreRuntime(), "frank"); 1 != removed {
        t.Fatalf("expected only the live token counted, got %d", removed)
    }
}

func TestRedisTokenStore_PurgeExpiredPrunesStaleMembers(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:purge"})

    store.Put(newTokenStoreRuntime(), "token-stale", securitycontract.Claims{UserIdentifier: "grace"}, bound.Of(time.Second))

    time.Sleep(1150 * time.Millisecond)

    if pruned, _ := store.PurgeExpired(newTokenStoreRuntime()); 1 > pruned {
        t.Fatalf("expected at least one stale index member pruned, got %d", pruned)
    }

    if 0 != mustDeleteByUser(t, store, "grace") {
        t.Fatalf("expected the user set to have been dropped by purge")
    }
}

func TestRedisTokenStore_DeleteByUserDoesNotRevokeReissuedTokenOfAnotherUser(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:staleindex"})

    store.Put(newTokenStoreRuntime(), "reused-token", securitycontract.Claims{UserIdentifier: "userA"}, bound.Of(time.Second))

    time.Sleep(1150 * time.Millisecond)

    store.Put(newTokenStoreRuntime(), "reused-token", securitycontract.Claims{UserIdentifier: "userB"}, bound.UnlimitedDuration())
    defer store.Delete(newTokenStoreRuntime(), "reused-token")

    store.PurgeExpired(newTokenStoreRuntime())

    if removed, _ := store.DeleteByUser(newTokenStoreRuntime(), "userA"); 0 != removed {
        t.Fatalf("expected userA to own no live token (its token expired before being re-issued to userB), got removed=%d", removed)
    }

    found, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "reused-token")
    if nil != lookupErr || false == exists || "userB" != found.UserIdentifier {
        t.Fatalf("expected userB's live token to survive DeleteByUser(userA): %+v %v %v", found, exists, lookupErr)
    }
}

var _ securitycontract.RevocableTokenStore = (*RedisTokenStore)(nil)

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
    epochTag := hashTagOf(store.epochKey("alice"))
    epochPrefixTag := hashTagOf(store.epochKeyPrefix())

    if "" == tokenTag {
        t.Fatalf("expected the token key to carry a hash tag, got %q", store.tokenKey("abc"))
    }

    if tokenTag != userTag || tokenTag != userPrefixTag || tokenTag != epochTag || tokenTag != epochPrefixTag {
        t.Fatalf(
            "expected every token-store key to share one hash tag for cluster co-location, got token=%q user=%q userPrefix=%q epoch=%q epochPrefix=%q",
            tokenTag,
            userTag,
            userPrefixTag,
            epochTag,
            epochPrefixTag,
        )
    }
}

func TestNewTokenStore_KeepsAConfiguredScanCount(t *testing.T) {
    store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{ScanCount: bound.Of(512)})

    if 512 != store.scanCount {
        t.Fatalf("expected scan count 512, got %d", store.scanCount)
    }
}

func TestNewTokenStore_AnEmptyPrefixIsTheDefault(t *testing.T) {
    if store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{}); "melody:token" != store.prefix {
        t.Fatalf("expected the default prefix, got %q", store.prefix)
    }

    if store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{Prefix: "app:token"}); "app:token" != store.prefix {
        t.Fatalf("expected the configured prefix, got %q", store.prefix)
    }
}

func TestNewTokenStore_DefaultScanCount(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{})

    if defaultTokenStoreScanCount != store.scanCount {
        t.Fatalf("expected default scan count %d, got %d", defaultTokenStoreScanCount, store.scanCount)
    }
}

func TestNewTokenStore_ScanCountOverride(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{ScanCount: bound.Of(512)})

    if 512 != store.scanCount {
        t.Fatalf("expected overridden scan count 512, got %d", store.scanCount)
    }
}

func tokenStorePttl(t *testing.T, client redisclient.Client, key string) int64 {
    t.Helper()

    pttl, pttlErr := client.Do(context.Background(), client.B().Pttl().Key(key).Build()).AsInt64()
    if nil != pttlErr {
        t.Fatalf("pttl %q: %v", key, pttlErr)
    }

    return pttl
}

func TestRedisTokenStore_PutGivesTheUserIndexAnExpiryThatOutlivesItsToken(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:indexttl"})

    store.Put(newTokenStoreRuntime(), "token-indexed", securitycontract.Claims{UserIdentifier: "helen"}, bound.Of(30*time.Second))
    defer store.DeleteByUser(newTokenStoreRuntime(), "helen")

    tokenPttl := tokenStorePttl(t, client, store.tokenKey("token-indexed"))
    indexPttl := tokenStorePttl(t, client, store.userKey("helen"))

    if 0 >= indexPttl {
        t.Fatalf("expected the user index to carry an expiry, got pttl %d", indexPttl)
    }

    if indexPttl <= tokenPttl {
        t.Fatalf("expected the user index (pttl %d) to outlive its token (pttl %d), or a live token would become unrevocable", indexPttl, tokenPttl)
    }
}

func TestRedisTokenStore_PutRaisesButNeverShortensTheUserIndexExpiry(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:indexraise"})

    store.Put(newTokenStoreRuntime(), "token-long", securitycontract.Claims{UserIdentifier: "ivan"}, bound.Of(5*time.Minute))
    defer store.DeleteByUser(newTokenStoreRuntime(), "ivan")

    afterLong := tokenStorePttl(t, client, store.userKey("ivan"))

    store.Put(newTokenStoreRuntime(), "token-short", securitycontract.Claims{UserIdentifier: "ivan"}, bound.Of(2*time.Second))

    afterShort := tokenStorePttl(t, client, store.userKey("ivan"))
    if 4*time.Minute.Milliseconds() > afterShort {
        t.Fatalf("expected the index to keep the long-lived deadline (was %d), got %d", afterLong, afterShort)
    }

    store.Put(newTokenStoreRuntime(), "token-longer", securitycontract.Claims{UserIdentifier: "ivan"}, bound.Of(30*time.Minute))

    if afterLonger := tokenStorePttl(t, client, store.userKey("ivan")); 25*time.Minute.Milliseconds() > afterLonger {
        t.Fatalf("expected a longer-lived token to raise the index deadline, got %d", afterLonger)
    }
}

func TestRedisTokenStore_NonExpiringTokenKeepsTheUserIndexPersistent(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:indexpersist"})

    store.Put(newTokenStoreRuntime(), "token-expiring", securitycontract.Claims{UserIdentifier: "judy"}, bound.Of(10*time.Second))
    store.Put(newTokenStoreRuntime(), "token-forever", securitycontract.Claims{UserIdentifier: "judy"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "judy")

    if indexPttl := tokenStorePttl(t, client, store.userKey("judy")); -1 != indexPttl {
        t.Fatalf("expected the index of a non-expiring token to be persistent, got pttl %d", indexPttl)
    }
}

func sscanCallCount(t *testing.T, client redisclient.Client) int64 {
    t.Helper()

    info, infoErr := client.Do(context.Background(), client.B().Info().Section("commandstats").Build()).ToString()
    if nil != infoErr {
        t.Fatalf("info commandstats: %v", infoErr)
    }

    for _, line := range strings.Split(info, "\n") {
        if false == strings.HasPrefix(line, "cmdstat_sscan:") {
            continue
        }

        for _, field := range strings.Split(strings.TrimPrefix(strings.TrimSpace(line), "cmdstat_sscan:"), ",") {
            if false == strings.HasPrefix(field, "calls=") {
                continue
            }

            calls, parseErr := strconv.ParseInt(strings.TrimPrefix(field, "calls="), 10, 64)
            if nil != parseErr {
                t.Fatalf("parse %q: %v", field, parseErr)
            }

            return calls
        }
    }

    return 0
}

func TestRedisTokenStore_DeleteByUserRevokesInBoundedBatches(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:batched", ScanCount: bound.Of(10)})

    tokenCount := 250
    for index := 0; index < tokenCount; index++ {
        store.Put(newTokenStoreRuntime(), "batched-token-"+strconv.Itoa(index), securitycontract.Claims{UserIdentifier: "karl"}, bound.UnlimitedDuration())
    }

    store.Put(newTokenStoreRuntime(), "batched-token-other", securitycontract.Claims{UserIdentifier: "lena"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "lena")

    before := sscanCallCount(t, client)

    removed, _ := store.DeleteByUser(newTokenStoreRuntime(), "karl")
    if tokenCount != removed {
        t.Fatalf("expected %d tokens removed, got %d", tokenCount, removed)
    }

    if calls := sscanCallCount(t, client) - before; 2 > calls {
        t.Fatalf("expected the revocation to walk the index in more than one bounded batch, got %d sscan calls", calls)
    }

    for index := 0; index < tokenCount; index++ {
        if _, exists, _ := store.Lookup(newTokenStoreRuntime(), "batched-token-"+strconv.Itoa(index)); true == exists {
            t.Fatalf("expected batched-token-%d to be revoked", index)
        }
    }

    if indexPttl := tokenStorePttl(t, client, store.userKey("karl")); -2 != indexPttl {
        t.Fatalf("expected the emptied index set to be gone, got pttl %d", indexPttl)
    }

    found, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "batched-token-other")
    if nil != lookupErr || false == exists || "lena" != found.UserIdentifier {
        t.Fatalf("expected another user's token to survive: %+v %v %v", found, exists, lookupErr)
    }
}

func TestRedisTokenStore_PurgeExpiredPrunesInBoundedBatches(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:purgebatched", ScanCount: bound.Of(10)})

    defer store.DeleteByUser(newTokenStoreRuntime(), "mona")

    staleCount := 250
    for index := 0; index < staleCount; index++ {
        tokenString := "purge-batched-token-" + strconv.Itoa(index)

        store.Put(newTokenStoreRuntime(), tokenString, securitycontract.Claims{UserIdentifier: "mona"}, bound.UnlimitedDuration())

        if deleteErr := client.Do(
            context.Background(),
            client.B().Del().Key(store.tokenKey(tokenString)).Build(),
        ).Error(); nil != deleteErr {
            t.Fatalf("del: %v", deleteErr)
        }
    }

    store.Put(newTokenStoreRuntime(), "purge-batched-token-live", securitycontract.Claims{UserIdentifier: "mona"}, bound.UnlimitedDuration())

    before := sscanCallCount(t, client)

    if pruned, _ := store.PurgeExpired(newTokenStoreRuntime()); staleCount != pruned {
        t.Fatalf("expected %d stale index members pruned, got %d", staleCount, pruned)
    }

    if calls := sscanCallCount(t, client) - before; 2 > calls {
        t.Fatalf("expected the purge to walk the index in more than one bounded batch, got %d sscan calls", calls)
    }

    if members := tokenStoreCardinality(t, client, store.userKey("mona")); 1 != members {
        t.Fatalf("expected only the live token left in the index, got %d members", members)
    }

    found, exists, lookupErr := store.Lookup(newTokenStoreRuntime(), "purge-batched-token-live")
    if nil != lookupErr || false == exists || "mona" != found.UserIdentifier {
        t.Fatalf("expected the live token to survive the purge: %+v %v %v", found, exists, lookupErr)
    }
}

func tokenStoreCardinality(t *testing.T, client redisclient.Client, key string) int64 {
    t.Helper()

    cardinality, cardinalityErr := client.Do(context.Background(), client.B().Scard().Key(key).Build()).AsInt64()
    if nil != cardinalityErr {
        t.Fatalf("scard %q: %v", key, cardinalityErr)
    }

    return cardinality
}

func tokenStoreKeyExists(t *testing.T, client redisclient.Client, key string) bool {
    t.Helper()

    exists, existsErr := client.Do(context.Background(), client.B().Exists().Key(key).Build()).AsInt64()
    if nil != existsErr {
        t.Fatalf("exists %q: %v", key, existsErr)
    }

    return 1 == exists
}

func TestRedisTokenStore_RevokeBeforeRejectsAnOlderTokenWithoutDeletingIt(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokekeeps"})
    runtimeInstance := newTokenStoreRuntime()
    store.Put(newTokenStoreRuntime(), "token-revoked", securitycontract.Claims{UserIdentifier: "nina", Roles: []string{"ROLE_USER"}}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "nina")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("nina")).Build())

    if _, found, _ := store.Lookup(runtimeInstance, "token-revoked"); false == found {
        t.Fatalf("expected the token to resolve before it was revoked")
    }

    store.RevokeBefore(newTokenStoreRuntime(), "nina", time.Now().Add(time.Second), securitycontract.RevocationOptions{})

    claims, found, lookupErr := store.Lookup(runtimeInstance, "token-revoked")
    if nil != lookupErr {
        t.Fatalf("lookup after revoke: %v", lookupErr)
    }

    if true == found {
        t.Fatalf("expected the revoked token to stop resolving, got %+v", claims)
    }

    if false == tokenStoreKeyExists(t, client, store.tokenKey("token-revoked")) {
        t.Fatalf("the token key is gone from redis, so the miss proves the entry was DELETED rather than that the epoch refused it — this test asserts nothing about revocation epochs")
    }

    if 1 != tokenStoreCardinality(t, client, store.userKey("nina")) {
        t.Fatalf("the index member is gone, so the miss came from a removal rather than from the boundary")
    }
}

func TestRedisTokenStore_RevokeBeforeDoesNotRejectATokenIssuedAfterIt(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokelater"})
    runtimeInstance := newTokenStoreRuntime()

    boundary := time.Now()
    store.RevokeBefore(newTokenStoreRuntime(), "owen", boundary, securitycontract.RevocationOptions{})
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("owen")).Build())

    store.Put(newTokenStoreRuntime(), "token-fresh", securitycontract.Claims{UserIdentifier: "owen"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "owen")

    if _, found, lookupErr := store.Lookup(runtimeInstance, "token-fresh"); false == found || nil != lookupErr {
        t.Fatalf("expected a token issued after the boundary to resolve, got found=%v err=%v", found, lookupErr)
    }

    published, epochErr := store.RevocationEpoch(runtimeInstance, "owen", securitycontract.RevocationOptions{})
    if nil != epochErr {
        t.Fatalf("revocation epoch: %v", epochErr)
    }

    if false == published.Equal(boundary) {
        t.Fatalf("expected the published boundary to be %v, got %v", boundary, published)
    }
}

func TestRedisTokenStore_RevocationEpochOnlyEverMovesForward(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokemonotonic"})
    runtimeInstance := newTokenStoreRuntime()

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("pete")).Build())

    later := time.Now().Add(time.Hour)
    earlier := later.Add(-time.Minute)

    store.RevokeBefore(newTokenStoreRuntime(), "pete", later, securitycontract.RevocationOptions{})
    store.RevokeBefore(newTokenStoreRuntime(), "pete", earlier, securitycontract.RevocationOptions{})

    published, epochErr := store.RevocationEpoch(runtimeInstance, "pete", securitycontract.RevocationOptions{})
    if nil != epochErr {
        t.Fatalf("revocation epoch: %v", epochErr)
    }

    if false == published.Equal(later) {
        t.Fatalf("expected the later boundary %v to stand, got %v", later, published)
    }

    store.Put(newTokenStoreRuntime(), "token-between", securitycontract.Claims{UserIdentifier: "pete"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "pete")

    if _, found, _ := store.Lookup(runtimeInstance, "token-between"); true == found {
        t.Fatalf("a token stamped before the standing boundary resolved, so the boundary was lowered")
    }
}

func TestRedisTokenStore_RevocationEpochRaisesByASingleNanosecond(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokenanos"})
    runtimeInstance := newTokenStoreRuntime()

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("quinn")).Build())

    earlier := time.Now()
    later := earlier.Add(time.Nanosecond)

    store.RevokeBefore(newTokenStoreRuntime(), "quinn", earlier, securitycontract.RevocationOptions{})
    store.RevokeBefore(newTokenStoreRuntime(), "quinn", later, securitycontract.RevocationOptions{})

    published, epochErr := store.RevocationEpoch(runtimeInstance, "quinn", securitycontract.RevocationOptions{})
    if nil != epochErr {
        t.Fatalf("revocation epoch: %v", epochErr)
    }

    if published.UnixNano() != later.UnixNano() {
        t.Fatalf(
            "expected the boundary to be raised to %d, got %d — a one-nanosecond raise was rounded away, so the boundary is stale and tokens issued inside that window are still honoured",
            later.UnixNano(),
            published.UnixNano(),
        )
    }
}

func TestRedisTokenStore_DeviceRevocationSparesAnotherDeviceOfTheSameUser(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokedevice"})
    runtimeInstance := newTokenStoreRuntime()

    store.Put(newTokenStoreRuntime(), "token-phone", securitycontract.Claims{UserIdentifier: "rita", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())
    store.Put(newTokenStoreRuntime(), "token-laptop", securitycontract.Claims{UserIdentifier: "rita", DeviceIdentifier: "laptop"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "rita")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("rita")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "rita", time.Now().Add(time.Second), securitycontract.RevocationOptions{DeviceIdentifier: "phone"})

    if _, found, _ := store.Lookup(runtimeInstance, "token-phone"); true == found {
        t.Fatalf("expected the revoked device's token to stop resolving")
    }

    if _, found, lookupErr := store.Lookup(runtimeInstance, "token-laptop"); false == found || nil != lookupErr {
        t.Fatalf("revoking one device ended another device's token, got found=%v err=%v", found, lookupErr)
    }

    if false == tokenStoreKeyExists(t, client, store.tokenKey("token-phone")) {
        t.Fatalf("the revoked device's key is gone, so the miss came from a removal rather than from the boundary")
    }
}

func TestRedisTokenStore_RevocationEpochReadsTheLaterOfTheUserAndTheDevice(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:epochdevice"})
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("tess")).Build())
    userInstant := time.Unix(1000, 0)
    deviceInstant := time.Unix(2000, 0)

    store.RevokeBefore(newTokenStoreRuntime(), "tess", userInstant, securitycontract.RevocationOptions{})
    store.RevokeBefore(newTokenStoreRuntime(), "tess", deviceInstant, securitycontract.RevocationOptions{DeviceIdentifier: "phone"})

    if epoch, _ := store.RevocationEpoch(newTokenStoreRuntime(), "tess", securitycontract.RevocationOptions{DeviceIdentifier: "phone"}); false == deviceInstant.Equal(epoch) {
        t.Fatalf("expected the device's later boundary, got %v", epoch)
    }

    if epoch, _ := store.RevocationEpoch(newTokenStoreRuntime(), "tess", securitycontract.RevocationOptions{}); false == userInstant.Equal(epoch) {
        t.Fatalf("expected the whole user's boundary when the device is left out, got %v", epoch)
    }

    if epoch, _ := store.RevocationEpoch(newTokenStoreRuntime(), "tess", securitycontract.RevocationOptions{DeviceIdentifier: "laptop"}); false == userInstant.Equal(epoch) {
        t.Fatalf("expected another device to read the user's boundary alone, got %v", epoch)
    }
}

func TestRedisTokenStore_UserRevocationCoversEveryDevice(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokeuserwide"})
    runtimeInstance := newTokenStoreRuntime()

    store.Put(newTokenStoreRuntime(), "token-one", securitycontract.Claims{UserIdentifier: "sam", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())
    store.Put(newTokenStoreRuntime(), "token-two", securitycontract.Claims{UserIdentifier: "sam", DeviceIdentifier: "laptop"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "sam")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("sam")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "sam", time.Now().Add(time.Second), securitycontract.RevocationOptions{})

    for _, tokenString := range []string{"token-one", "token-two"} {
        if _, found, _ := store.Lookup(runtimeInstance, tokenString); true == found {
            t.Fatalf("a user-wide revocation left %q alive", tokenString)
        }
    }
}

func TestRedisTokenStore_ADeviceNamedUserCannotWriteTheUserWideBoundary(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokefieldclash"})
    runtimeInstance := newTokenStoreRuntime()

    store.Put(newTokenStoreRuntime(), "token-other", securitycontract.Claims{UserIdentifier: "tess", DeviceIdentifier: "laptop"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "tess")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("tess")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "tess", time.Now().Add(time.Hour), securitycontract.RevocationOptions{DeviceIdentifier: revocationUserField})

    if _, found, lookupErr := store.Lookup(runtimeInstance, "token-other"); false == found || nil != lookupErr {
        t.Fatalf("a device named %q revoked the whole account, got found=%v err=%v", revocationUserField, found, lookupErr)
    }
}

func TestRedisTokenStore_LookupRejectsAnUnstampedTokenOnlyOnceABoundaryExists(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokeunstamped"})
    runtimeInstance := newTokenStoreRuntime()

    tokenKey := store.tokenKey("token-legacy")
    defer client.Do(context.Background(), client.B().Del().Key(tokenKey).Build())
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("uma")).Build())
    if setErr := client.Do(
        context.Background(),
        client.B().Set().Key(tokenKey).Value(`{"userIdentifier":"uma","roles":["ROLE_USER"]}`).Build(),
    ).Error(); nil != setErr {
        t.Fatalf("planting the legacy payload: %v", setErr)
    }

    claims, found, lookupErr := store.Lookup(runtimeInstance, "token-legacy")
    if false == found || nil != lookupErr {
        t.Fatalf("an unstamped token was refused although nothing had been revoked, got found=%v err=%v", found, lookupErr)
    }

    if false == claims.IssuedAt.IsZero() {
        t.Fatalf("expected the planted payload to carry no issue instant, got %v", claims.IssuedAt)
    }

    store.RevokeBefore(newTokenStoreRuntime(), "uma", time.Now(), securitycontract.RevocationOptions{})

    if _, found, _ := store.Lookup(runtimeInstance, "token-legacy"); true == found {
        t.Fatalf("an unstamped token survived a revocation, so it can never be revoked at all")
    }
}

func TestRedisTokenStore_PutStampsTheIssueInstantFromTheInjectedClock(t *testing.T) {
    client := newTokenStoreClient(t)
    frozen := melodyclock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokestamp", Clock: frozen})
    runtimeInstance := newTokenStoreRuntime()

    stale := time.Unix(1_600_000_000, 0)
    store.Put(newTokenStoreRuntime(), "token-stamped", securitycontract.Claims{UserIdentifier: "vera", IssuedAt: stale}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "vera")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("vera")).Build())

    claims, found, lookupErr := store.Lookup(runtimeInstance, "token-stamped")
    if false == found || nil != lookupErr {
        t.Fatalf("lookup: found=%v err=%v", found, lookupErr)
    }

    if false == claims.IssuedAt.Equal(frozen.Now()) {
        t.Fatalf("expected the store's clock to stamp %v, got %v", frozen.Now(), claims.IssuedAt)
    }
    store.RevokeBefore(newTokenStoreRuntime(), "vera", time.Unix(1_650_000_000, 0), securitycontract.RevocationOptions{})

    if _, found, _ := store.Lookup(runtimeInstance, "token-stamped"); false == found {
        t.Fatalf("the caller's stale issue instant was kept, so the token was born already revoked")
    }
}

func TestRedisTokenStore_RevocationEpochOutlivesTheTokenItRefuses(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokeoutlives", RevocationEpochRetention: bound.Of(time.Second)})

    store.Put(newTokenStoreRuntime(), "token-long", securitycontract.Claims{UserIdentifier: "wade"}, bound.Of(time.Hour))
    defer store.DeleteByUser(newTokenStoreRuntime(), "wade")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("wade")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "wade", time.Now(), securitycontract.RevocationOptions{})

    tokenPttl := tokenStorePttl(t, client, store.tokenKey("token-long"))
    epochPttl := tokenStorePttl(t, client, store.epochKey("wade"))

    if epochPttl <= tokenPttl {
        t.Fatalf("the boundary expires at or before the token it refuses (epoch pttl %d, token pttl %d), so the token would come back to life", epochPttl, tokenPttl)
    }
}

func TestRedisTokenStore_NonExpiringTokenKeepsTheRevocationEpochPersistent(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokepersist"})

    store.Put(newTokenStoreRuntime(), "token-forever", securitycontract.Claims{UserIdentifier: "xena"}, bound.UnlimitedDuration())
    defer store.DeleteByUser(newTokenStoreRuntime(), "xena")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("xena")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "xena", time.Now(), securitycontract.RevocationOptions{})

    if epochPttl := tokenStorePttl(t, client, store.epochKey("xena")); -1 != epochPttl {
        t.Fatalf("expected the boundary of a non-expiring token to be persistent, got pttl %d", epochPttl)
    }
}

func TestRedisTokenStore_RevokeBeforeGivesABrandNewEpochHashAnExpiry(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokenewexpiry", RevocationEpochRetention: bound.Of(time.Hour)})

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("yuri")).Build())
    store.RevokeBefore(newTokenStoreRuntime(), "yuri", time.Now(), securitycontract.RevocationOptions{})

    if epochPttl := tokenStorePttl(t, client, store.epochKey("yuri")); 0 >= epochPttl {
        t.Fatalf("expected a new boundary to carry the retention floor, got pttl %d", epochPttl)
    }
}

func TestRedisTokenStore_AnUnlimitedRetentionKeepsTheBoundaryPersistent(t *testing.T) {
    client := newTokenStoreClient(t)
    prefix := "melody:token:test:revokeunlimited"
    expiring := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: prefix, RevocationEpochRetention: bound.Of(time.Hour)})
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: prefix, RevocationEpochRetention: bound.UnlimitedDuration()})

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("zoe"), store.epochKey("zack")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "zoe", time.Now(), securitycontract.RevocationOptions{})
    if epochPttl := tokenStorePttl(t, client, store.epochKey("zoe")); -1 != epochPttl {
        t.Fatalf("expected a new boundary kept for ever, got pttl %d", epochPttl)
    }

    expiring.RevokeBefore(newTokenStoreRuntime(), "zack", time.Now(), securitycontract.RevocationOptions{})
    store.RevokeBefore(newTokenStoreRuntime(), "zack", time.Now(), securitycontract.RevocationOptions{DeviceIdentifier: "device"})
    if epochPttl := tokenStorePttl(t, client, store.epochKey("zack")); -1 != epochPttl {
        t.Fatalf("expected an expiring boundary made persistent, got pttl %d", epochPttl)
    }
}

func TestRedisTokenStore_LookupReportsACorruptPayloadRatherThanFailingTheScript(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokecorrupt"})
    runtimeInstance := newTokenStoreRuntime()

    tokenKey := store.tokenKey("token-corrupt")
    defer client.Do(context.Background(), client.B().Del().Key(tokenKey).Build())

    if setErr := client.Do(context.Background(), client.B().Set().Key(tokenKey).Value("not json at all").Build()).Error(); nil != setErr {
        t.Fatalf("planting the corrupt payload: %v", setErr)
    }

    _, found, lookupErr := store.Lookup(runtimeInstance, "token-corrupt")
    if true == found {
        t.Fatalf("a corrupt payload resolved")
    }

    if nil == lookupErr || false == strings.Contains(lookupErr.Error(), "could not decode claims") {
        t.Fatalf("expected the decode error, got %v", lookupErr)
    }
}

func TestRedisTokenStore_RevokeBeforeRefusesAnUnrepresentableInstant(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokebadinstant"})

    for name, instant := range map[string]time.Time{
        "zero":          {},
        "before-unix":   time.Unix(-1, 0),
        "past-int64-ns": time.Unix(1<<40, 0),
    } {
        revokeErr := store.RevokeBefore(newTokenStoreRuntime(), "zack", instant, securitycontract.RevocationOptions{})
        if nil == revokeErr || true == exception.AnswersUnavailable(revokeErr) {
            t.Fatalf("expected an unmarked refusal for the %s instant, got %v", name, revokeErr)
        }
    }
}

func TestRedisTokenStore_RevokeBeforeRefusesAnEmptyUserIdentifier(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokenouser"})

    revokeErr := store.RevokeBefore(newTokenStoreRuntime(), "", time.Now(), securitycontract.RevocationOptions{})
    if nil == revokeErr || true == exception.AnswersUnavailable(revokeErr) {
        t.Fatalf("expected an unmarked refusal for an empty user identifier, got %v", revokeErr)
    }
}

func TestRedisTokenStore_PurgeExpiredDoesNotWalkTheEpochKeys(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:revokepurgescan"})

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("abby")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "abby", time.Now(), securitycontract.RevocationOptions{})

    store.PurgeExpired(newTokenStoreRuntime())

    if false == tokenStoreKeyExists(t, client, store.epochKey("abby")) {
        t.Fatalf("the purge removed a boundary it should never have walked")
    }
}

func TestRedisTokenStore_ClockSkewBoundRefusesATokenStampedByAnAheadNode(t *testing.T) {
    client := newTokenStoreClient(t)
    runtimeInstance := newTokenStoreRuntime()

    realTime := time.Unix(1_800_000_000, 0)
    issuingNodeClock := melodyclock.NewFrozenClock(realTime.Add(30 * time.Second))
    revokingNodeClock := melodyclock.NewFrozenClock(realTime)

    issuingNode := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:skew", Clock: issuingNodeClock})
    unbounded := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:skew", Clock: revokingNodeClock})
    bounded := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:skew", Clock: revokingNodeClock, MaximumClockSkew: bound.Of(10 * time.Second)})

    defer issuingNode.DeleteByUser(newTokenStoreRuntime(), "compromised")
    defer client.Do(context.Background(), client.B().Del().Key(issuingNode.epochKey("compromised")).Build())

    issuingNode.Put(newTokenStoreRuntime(), "attacker-token", securitycontract.Claims{UserIdentifier: "compromised", Roles: []string{"ROLE_USER"}}, bound.UnlimitedDuration())

    revokingNodeClock.Advance(5 * time.Second)
    unbounded.RevokeBefore(newTokenStoreRuntime(), "compromised", revokingNodeClock.Now(), securitycontract.RevocationOptions{})

    if _, found, _ := unbounded.Lookup(runtimeInstance, "attacker-token"); false == found {
        t.Fatalf("the unbounded store refused the token, so this scenario no longer reproduces the skew window and the bounded half below proves nothing")
    }

    if _, found, lookupErr := bounded.Lookup(runtimeInstance, "attacker-token"); true == found || nil != lookupErr {
        t.Fatalf("a token issued before the revocation survived it although the skew bound covers the gap: found=%v err=%v", found, lookupErr)
    }
}

func TestRedisTokenStore_ClockSkewBoundHonoursATokenIssuedBeyondTheBound(t *testing.T) {
    client := newTokenStoreClient(t)
    runtimeInstance := newTokenStoreRuntime()

    realTime := time.Unix(1_800_000_000, 0)
    nodeClock := melodyclock.NewFrozenClock(realTime)

    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:skewcontrol", Clock: nodeClock, MaximumClockSkew: bound.Of(10 * time.Second)})

    defer store.DeleteByUser(newTokenStoreRuntime(), "owen")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("owen")).Build())

    store.RevokeBefore(newTokenStoreRuntime(), "owen", nodeClock.Now(), securitycontract.RevocationOptions{})

    nodeClock.Advance(30 * time.Second)
    store.Put(newTokenStoreRuntime(), "fresh-token", securitycontract.Claims{UserIdentifier: "owen"}, bound.UnlimitedDuration())

    if _, found, lookupErr := store.Lookup(runtimeInstance, "fresh-token"); false == found || nil != lookupErr {
        t.Fatalf("a token issued well beyond the skew bound after the revocation was refused: found=%v err=%v", found, lookupErr)
    }
}

func TestRedisTokenStore_ClockSkewBoundWidensTheBoundaryItself(t *testing.T) {
    client := newTokenStoreClient(t)
    runtimeInstance := newTokenStoreRuntime()

    realTime := time.Unix(1_800_000_000, 0)
    issuingNodeClock := melodyclock.NewFrozenClock(realTime.Add(8 * time.Second))
    revokingNodeClock := melodyclock.NewFrozenClock(realTime)

    issuingNode := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:skewwiden", Clock: issuingNodeClock})
    unbounded := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:skewwiden", Clock: revokingNodeClock})
    bounded := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:skewwiden", Clock: revokingNodeClock, MaximumClockSkew: bound.Of(10 * time.Second)})

    defer issuingNode.DeleteByUser(newTokenStoreRuntime(), "compromised")
    defer client.Do(context.Background(), client.B().Del().Key(issuingNode.epochKey("compromised")).Build())

    issuingNode.Put(newTokenStoreRuntime(), "attacker-token", securitycontract.Claims{UserIdentifier: "compromised", Roles: []string{"ROLE_USER"}}, bound.UnlimitedDuration())

    revokingNodeClock.Advance(5 * time.Second)
    unbounded.RevokeBefore(newTokenStoreRuntime(), "compromised", revokingNodeClock.Now(), securitycontract.RevocationOptions{})

    if _, found, _ := unbounded.Lookup(runtimeInstance, "attacker-token"); false == found {
        t.Fatalf("the unbounded store refused the token, so the bounded half below cannot show the boundary being widened")
    }

    if _, found, _ := bounded.Lookup(runtimeInstance, "attacker-token"); true == found {
        t.Fatalf("the stamp sits inside the verifying node's own tolerance, so only widening the boundary can refuse it — and it was honoured")
    }
}

func TestRedisTokenStore_PutRefusesAnAbsentTtlAndOneUnderASecond(t *testing.T) {
    client := &stubClient{}
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:refusedttl"})

    for name, ttl := range map[string]bound.Duration{
        "absent":     {},
        "zero":       bound.Of(time.Duration(0)),
        "negative":   bound.Of(-time.Second),
        "sub-second": bound.Of(time.Second - time.Nanosecond),
    } {
        putErr := store.Put(newTokenStoreRuntime(), "token-bad-ttl", securitycontract.Claims{UserIdentifier: "alice"}, ttl)
        if nil == putErr || securitycontract.TokenTtlRule().Name != exception.LogContext(putErr, exception.LogContextOptions{})["field"] || true == exception.AnswersUnavailable(putErr) {
            t.Fatalf("a %s ttl fell through to a store, where a lapsed remaining lifetime must be refused unmarked naming the ttl, got %v", name, putErr)
        }
    }
}

func TestNewTokenStore_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    for _, testCase := range []struct {
        field  string
        config TokenStoreConfig
    }{
        {field: "TokenStoreConfig.ScanCount", config: TokenStoreConfig{ScanCount: bound.Of(0)}},
        {field: "TokenStoreConfig.ScanCount", config: TokenStoreConfig{ScanCount: bound.UnlimitedCount()}},
        {field: "TokenStoreConfig.CallTimeout", config: TokenStoreConfig{CallTimeout: bound.Of(time.Microsecond)}},
        {field: "TokenStoreConfig.CallTimeout", config: TokenStoreConfig{CallTimeout: bound.UnlimitedDuration()}},
        {field: "TokenStoreConfig.MaximumClockSkew", config: TokenStoreConfig{MaximumClockSkew: bound.Of(-time.Second)}},
        {field: "TokenStoreConfig.MaximumClockSkew", config: TokenStoreConfig{MaximumClockSkew: bound.UnlimitedDuration()}},
        {field: "TokenStoreConfig.RevocationEpochRetention", config: TokenStoreConfig{RevocationEpochRetention: bound.Of(-time.Second)}},
        {field: "TokenStoreConfig.RevocationEpochRetention", config: TokenStoreConfig{RevocationEpochRetention: bound.Of(500 * time.Millisecond)}},
        {field: "TokenStoreConfig.MaxIdentifierBytes", config: TokenStoreConfig{MaxIdentifierBytes: bound.Of(0)}},
    } {
        expectPanicNamingField(t, testCase.field, func() {
            NewTokenStore(newTestConnection(&stubClient{}), testCase.config)
        })
    }
}

func TestNewTokenStore_AnAbsentOrZeroMaximumClockSkewIsNoTolerance(t *testing.T) {
    for _, skew := range []bound.Duration{{}, bound.Of(time.Duration(0))} {
        if store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{MaximumClockSkew: skew}); 0 != store.maximumClockSkew {
            t.Fatalf("expected no tolerance, got %s", store.maximumClockSkew)
        }
    }

    if store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{MaximumClockSkew: bound.Of(time.Second)}); time.Second != store.maximumClockSkew {
        t.Fatalf("expected a 1s tolerance, got %s", store.maximumClockSkew)
    }
}

func newWedgedTokenStore(t *testing.T, config TokenStoreConfig) (*RedisTokenStore, *gate) {
    t.Helper()

    config.Prefix = "melody:token:test:wedge:" + t.Name()
    if true == config.CallTimeout.IsAbsent() {
        config.CallTimeout = bound.Of(50 * time.Millisecond)
    }

    client, storeGate := dialGated(t)
    store := NewTokenStore(newTestConnection(client), config)

    store.Put(newTokenStoreRuntime(), "warm", securitycontract.Claims{UserIdentifier: "wedge"}, bound.UnlimitedDuration())
    store.Delete(newTokenStoreRuntime(), "warm")

    storeGate.Wedge()

    return store, storeGate
}

func TestRedisTokenStore_PutIsBoundedByTheCallTimeout(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        return store.Put(newTokenStoreRuntime(), "token", securitycontract.Claims{UserIdentifier: "wedge"}, bound.UnlimitedDuration())
    }))
}

func TestRedisTokenStore_DeleteIsBoundedByTheCallTimeout(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        return store.Delete(newTokenStoreRuntime(), "token")
    }))
}

func TestRedisTokenStore_DeleteByUserScanIsBoundedByTheCallTimeout(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, walkErr := store.DeleteByUser(newTokenStoreRuntime(), "wedge")

        return walkErr
    }))
}

func TestRedisTokenStore_PurgeExpiredScanIsBoundedByTheCallTimeout(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, walkErr := store.PurgeExpired(newTokenStoreRuntime())

        return walkErr
    }))
}

func TestRedisTokenStore_RevokeBeforeIsBoundedByTheCallTimeout(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        return store.RevokeBefore(newTokenStoreRuntime(), "wedge", time.Now(), securitycontract.RevocationOptions{})
    }))
}

func TestRedisTokenStore_LookupCapsARuntimeWithoutDeadlineAtTheCallTimeout(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, _, lookupErr := store.Lookup(newTokenStoreRuntime(), "token")

        return lookupErr
    }))
}

func TestRedisTokenStore_RevocationEpochCapsARuntimeWithoutDeadlineAtTheCallTimeout(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, epochErr := store.RevocationEpoch(newTokenStoreRuntime(), "wedge", securitycontract.RevocationOptions{})

        return epochErr
    }))
}

func TestRedisTokenStore_LookupKeepsATighterRequestDeadline(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{CallTimeout: bound.Of(time.Second)})

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancel()

    serviceContainer := container.NewContainer(container.ContainerConfig{})
    runtimeInstance := runtime.NewRun(ctx, serviceContainer.NewScope())

    started := time.Now()
    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, _, lookupErr := store.Lookup(runtimeInstance, "token")

        return lookupErr
    }))

    if elapsed := time.Since(started); 500*time.Millisecond < elapsed {
        t.Fatalf("expected the request's own deadline to end the lookup, it took %s", elapsed)
    }
}

func TestNewTokenStore_KeepsAConfiguredCallTimeout(t *testing.T) {
    store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{CallTimeout: bound.Of(750 * time.Millisecond)})

    if 750*time.Millisecond != store.callTimeout {
        t.Fatalf("expected the configured call timeout, got %v", store.callTimeout)
    }
}

func TestNewTokenStore_DefaultCallTimeout(t *testing.T) {
    store := NewTokenStore(newTestConnection(newTokenStoreClient(t)), TokenStoreConfig{})

    if defaultTokenStoreCallTimeout != store.callTimeout {
        t.Fatalf("expected the default call timeout, got %v", store.callTimeout)
    }
}

func TestRedisTokenStore_DeleteByUserBatchIsBoundedByTheCallTimeout(t *testing.T) {
    client, storeGate := dialGated(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:wedge:" + t.Name(), CallTimeout: bound.Of(50 * time.Millisecond)})

    store.Put(newTokenStoreRuntime(), "member", securitycontract.Claims{UserIdentifier: "batch"}, bound.UnlimitedDuration())

    storeGate.WedgeIntegerReplies()

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, walkErr := store.DeleteByUser(newTokenStoreRuntime(), "batch")

        return walkErr
    }))
}

func TestRedisTokenStore_PurgeExpiredBatchIsBoundedByTheCallTimeout(t *testing.T) {
    client, storeGate := dialGated(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:wedge:" + t.Name(), CallTimeout: bound.Of(50 * time.Millisecond)})

    store.Put(newTokenStoreRuntime(), "member", securitycontract.Claims{UserIdentifier: "purge"}, bound.UnlimitedDuration())

    storeGate.WedgeIntegerReplies()

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, walkErr := store.PurgeExpired(newTokenStoreRuntime())

        return walkErr
    }))
}

func TestRedisTokenStore_PurgeExpiredMemberScanIsBoundedByTheCallTimeout(t *testing.T) {
    client, storeGate := dialGated(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:wedge:" + t.Name(), CallTimeout: bound.Of(50 * time.Millisecond)})

    store.Put(newTokenStoreRuntime(), "member", securitycontract.Claims{UserIdentifier: "members"}, bound.UnlimitedDuration())

    storeGate.WedgeArrayRepliesAfter(1)

    requireDeadlineExceeded(t, awaitOutcome(t, boundProbeBudget, func() error {
        _, walkErr := store.PurgeExpired(newTokenStoreRuntime())

        return walkErr
    }))
}

func TestNewTokenStore_TheRevocationEpochRetentionIsSevenDaysByDefaultAndUnlimitedIsZero(t *testing.T) {
    if retention := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{}).epochRetentionMilliseconds; 7*24*60*60*1000 != retention {
        t.Fatalf("expected an absent retention to be seven days, got %d", retention)
    }

    if retention := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{RevocationEpochRetention: bound.Of(time.Hour)}).epochRetentionMilliseconds; 3600000 != retention {
        t.Fatalf("expected an hour's retention in milliseconds, got %d", retention)
    }

    if retention := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{RevocationEpochRetention: bound.UnlimitedDuration()}).epochRetentionMilliseconds; 0 != retention {
        t.Fatalf("expected an unlimited retention to reach the revoke script as 0, got %d", retention)
    }
}

func TestRedisTokenStore_ANonUtf8UserIdentifierIsRefusedAtEveryWriteDoor(t *testing.T) {
    store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{})
    invalid := "alice\xff"

    for door, call := range map[string]func() error{
        "Put": func() error {
            return store.Put(newTokenStoreRuntime(), "token", securitycontract.Claims{UserIdentifier: invalid}, bound.UnlimitedDuration())
        },
        "Put with ttl": func() error {
            return store.Put(newTokenStoreRuntime(), "token", securitycontract.Claims{UserIdentifier: invalid}, bound.Of(time.Minute))
        },
        "RevokeBefore": func() error { return store.RevokeBefore(newTokenStoreRuntime(), invalid, time.Now(), securitycontract.RevocationOptions{}) },
        "DeleteByUser": func() error {
            _, deleteErr := store.DeleteByUser(newTokenStoreRuntime(), invalid)

            return deleteErr
        },
    } {
        refusal := call()
        if nil == refusal || false == strings.Contains(refusal.Error(), "must be valid UTF-8") || true == exception.AnswersUnavailable(refusal) {
            t.Fatalf("expected %s to refuse the identifier unmarked by name before any round trip, got %v", door, refusal)
        }
    }
}

func TestRedisTokenStore_ANonUtf8DeviceIdentifierIsRefusedNamingTheField(t *testing.T) {
    store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{})

    for field, call := range map[string]func() error{
        "Claims.DeviceIdentifier": func() error {
            return store.Put(newTokenStoreRuntime(), "token", securitycontract.Claims{UserIdentifier: "alice", DeviceIdentifier: "phone\xff"}, bound.UnlimitedDuration())
        },
        "RevocationOptions.DeviceIdentifier": func() error {
            return store.RevokeBefore(newTokenStoreRuntime(), "alice", time.Now(), securitycontract.RevocationOptions{DeviceIdentifier: "phone\xff"})
        },
    } {
        refusal := call()
        if nil == refusal || false == strings.Contains(refusal.Error(), "device identifier must be valid UTF-8") || true == exception.AnswersUnavailable(refusal) {
            t.Fatalf("expected %s refused unmarked before any round trip, got %v", field, refusal)
        }

        refusalContext := exception.FromError(refusal).Context()
        if field != refusalContext["field"] || 6 != refusalContext["deviceLength"] {
            t.Fatalf("expected the refusal to name %s and the length, never the bytes, got %v", field, refusalContext)
        }
    }
}

func TestRedisTokenStore_AnIdentifierPastMaxIdentifierBytesIsRefusedNamingTheFieldAndTheLength(t *testing.T) {
    oversized := strings.Repeat("a", 256)
    store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{})

    for field, call := range map[string]func() error{
        "Claims.UserIdentifier": func() error {
            return store.Put(newTokenStoreRuntime(), "token", securitycontract.Claims{UserIdentifier: oversized}, bound.UnlimitedDuration())
        },
        "Claims.DeviceIdentifier": func() error {
            return store.Put(newTokenStoreRuntime(), "token", securitycontract.Claims{UserIdentifier: "alice", DeviceIdentifier: oversized}, bound.UnlimitedDuration())
        },
        "userIdentifier": func() error {
            return store.RevokeBefore(newTokenStoreRuntime(), oversized, time.Now(), securitycontract.RevocationOptions{})
        },
        "RevocationOptions.DeviceIdentifier": func() error {
            return store.RevokeBefore(newTokenStoreRuntime(), "alice", time.Now(), securitycontract.RevocationOptions{DeviceIdentifier: oversized})
        },
    } {
        refusal := call()
        if nil == refusal || true == exception.AnswersUnavailable(refusal) {
            t.Fatalf("expected %s of 256 bytes refused unmarked before any round trip, got %v", field, refusal)
        }

        refusalContext := exception.FromError(refusal).Context()
        if field != refusalContext["field"] || 256 != refusalContext["identifierLength"] || 255 != refusalContext["maxIdentifierBytes"] || "TokenStoreConfig.MaxIdentifierBytes" != refusalContext["limit"] {
            t.Fatalf("expected the refusal to name %s, its length and the limit, never the bytes, got %v", field, refusalContext)
        }

        if true == strings.Contains(refusal.Error(), oversized) {
            t.Fatalf("expected the refusal never to carry the identifier, got %v", refusal)
        }
    }

    for _, testCase := range []struct {
        config             TokenStoreConfig
        maxIdentifierBytes int
    }{
        {config: TokenStoreConfig{}, maxIdentifierBytes: 255},
        {config: TokenStoreConfig{MaxIdentifierBytes: bound.Of(4)}, maxIdentifierBytes: 4},
        {config: TokenStoreConfig{MaxIdentifierBytes: bound.UnlimitedCount()}, maxIdentifierBytes: 0},
    } {
        if resolved := NewTokenStore(newTestConnection(&stubClient{}), testCase.config); testCase.maxIdentifierBytes != resolved.maxIdentifierBytes {
            t.Fatalf("expected the bound %d resolved, got %d", testCase.maxIdentifierBytes, resolved.maxIdentifierBytes)
        }
    }

    bounded := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{MaxIdentifierBytes: bound.Of(4)})
    refusal := bounded.RevokeBefore(newTokenStoreRuntime(), "abcde", time.Now(), securitycontract.RevocationOptions{})
    if nil == refusal || 4 != exception.FromError(refusal).Context()["maxIdentifierBytes"] {
        t.Fatalf("expected the configured bound applied, got %v", refusal)
    }
}

func TestRefuseOversizedIdentifier_AdmitsTheBoundItselfAndAnyLengthWhenUnlimited(t *testing.T) {
    if refusal := refuseOversizedIdentifier(strings.Repeat("a", 255), claimsUserField, 255); nil != refusal {
        t.Fatalf("expected an identifier of 255 bytes admitted under a bound of 255, got %v", refusal)
    }

    if refusal := refuseOversizedIdentifier(strings.Repeat("a", 256), claimsUserField, 255); nil == refusal {
        t.Fatalf("expected an identifier of 256 bytes refused under a bound of 255")
    }

    if refusal := refuseOversizedIdentifier(strings.Repeat("a", 1<<16), claimsDeviceField, 0); nil != refusal {
        t.Fatalf("expected the unlimited bound to admit any length, got %v", refusal)
    }
}

func TestRedisTokenStore_RevocationEpochAnswersTheUsersBoundaryForADeviceNoWriteAccepts(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:epochunwritabledevice"})
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("wanda")).Build())

    instant := time.Now().Add(-time.Minute).Truncate(time.Millisecond)
    if revokeErr := store.RevokeBefore(newTokenStoreRuntime(), "wanda", instant, securitycontract.RevocationOptions{}); nil != revokeErr {
        t.Fatalf("revoke: %v", revokeErr)
    }

    for _, device := range []string{"phone\xff", strings.Repeat("a", 256)} {
        epoch, epochErr := store.RevocationEpoch(newTokenStoreRuntime(), "wanda", securitycontract.RevocationOptions{DeviceIdentifier: device})
        if nil != epochErr {
            t.Fatalf("expected the read to answer for a device no write accepts, got %v", epochErr)
        }

        if false == epoch.Equal(instant) {
            t.Fatalf("expected the user's boundary %v, got %v", instant, epoch)
        }
    }
}

/* json stores the device as U+FFFD, so a boundary under the raw bytes would never meet the token */
func TestRedisTokenStore_ANonUtf8DeviceCanNotBeAcceptedAndThenEscapeItsRevocation(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:nonutf8device"})
    defer store.DeleteByUser(newTokenStoreRuntime(), "vera")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("vera")).Build())

    putErr := store.Put(newTokenStoreRuntime(), "token-vera", securitycontract.Claims{UserIdentifier: "vera", DeviceIdentifier: "phone\xff"}, bound.UnlimitedDuration())
    revokeErr := store.RevokeBefore(newTokenStoreRuntime(), "vera", time.Now().Add(time.Second), securitycontract.RevocationOptions{DeviceIdentifier: "phone\xff"})
    _, found, _ := store.Lookup(newTokenStoreRuntime(), "token-vera")

    if nil == putErr && nil == revokeErr && true == found {
        t.Fatalf("a device revocation answered success and its token still resolves")
    }

    if nil == putErr || nil == revokeErr {
        t.Fatalf("expected both the put and the revocation of a non-UTF-8 device refused, got put=%v revoke=%v", putErr, revokeErr)
    }
}

func TestRedisTokenStore_DeleteOverAnUnreachableStoreAnswersAMarkedError(t *testing.T) {
    store := NewTokenStore(newTestConnection(closedTestClient(t)), TokenStoreConfig{Prefix: "melody:token:test:unreachable"})

    deleteErr := store.Delete(newTokenStoreRuntime(), "t")
    if nil == deleteErr || false == strings.Contains(deleteErr.Error(), "redis token store delete failed") {
        t.Fatalf("expected the delete failure, got %v", deleteErr)
    }

    if false == exception.AnswersUnavailable(deleteErr) {
        t.Fatalf("expected the delete failure marked as an infrastructure failure, got %v", deleteErr)
    }
}

func TestRedisTokenStore_EveryWriteDoorAnswersAMarkedErrorOverAnUnreachableStore(t *testing.T) {
    store := NewTokenStore(newTestConnection(closedTestClient(t)), TokenStoreConfig{Prefix: "melody:token:test:unreachable"})
    runtimeInstance := newTokenStoreRuntime()

    for door, call := range map[string]func() error{
        "Put": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u"}, bound.UnlimitedDuration())
        },
        "Delete": func() error { return store.Delete(runtimeInstance, "t") },
        "DeleteByUser": func() error {
            _, walkErr := store.DeleteByUser(runtimeInstance, "u")

            return walkErr
        },
        "PurgeExpired": func() error {
            _, walkErr := store.PurgeExpired(runtimeInstance)

            return walkErr
        },
        "RevokeBefore": func() error { return store.RevokeBefore(runtimeInstance, "u", time.Now(), securitycontract.RevocationOptions{}) },
    } {
        if doorErr := call(); false == exception.AnswersUnavailable(doorErr) {
            t.Fatalf("expected %s over an unreachable store to answer a marked error, got %v", door, doorErr)
        }
    }
}

func TestRedisTokenStore_AWriteIsCancelledWithTheRuntimesContext(t *testing.T) {
    client, commands := newCommandCountingClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:cancelled"})

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance := &contextOnlyRuntime{ctx: cancelledContext}

    for door, call := range map[string]func() error{
        "Put": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u"}, bound.UnlimitedDuration())
        },
        "Delete": func() error { return store.Delete(runtimeInstance, "t") },
        "DeleteByUser": func() error {
            _, walkErr := store.DeleteByUser(runtimeInstance, "u")

            return walkErr
        },
        "PurgeExpired": func() error {
            _, walkErr := store.PurgeExpired(runtimeInstance)

            return walkErr
        },
        "RevokeBefore": func() error { return store.RevokeBefore(runtimeInstance, "u", time.Now(), securitycontract.RevocationOptions{}) },
    } {
        before := commands.Load()
        doorErr := call()

        if false == exception.AnswersUnavailable(doorErr) || false == errors.Is(doorErr, context.Canceled) {
            t.Fatalf("expected %s under a cancelled context to answer a marked error carrying the cancellation, got %v", door, doorErr)
        }

        if before != commands.Load() {
            t.Fatalf("expected %s under a cancelled context to make no round trip, got %d", door, commands.Load()-before)
        }
    }
}

func TestRedisTokenStore_AWriteInFlightEndsAtTheRuntimesDeadline(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{CallTimeout: bound.Of(10 * time.Second)})

    tight, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
    defer cancel()

    started := time.Now()
    deleteErr := awaitOutcome(t, boundProbeBudget, func() error {
        return store.Delete(&contextOnlyRuntime{ctx: tight}, "token")
    })

    if elapsed := time.Since(started); time.Second < elapsed {
        t.Fatalf("expected the round trip ended at the caller's deadline, it ran %s: %v", elapsed, deleteErr)
    }

    if false == errors.Is(deleteErr, context.DeadlineExceeded) || false == exception.AnswersUnavailable(deleteErr) || nil != exceptionContextOf(deleteErr)["bound"] {
        t.Fatalf("expected a marked failure of the caller's deadline, naming no bound of the store, got %v %v", deleteErr, exceptionContextOf(deleteErr))
    }
}

type lapsedDeadlineContext struct {
    context.Context
}

func (instance lapsedDeadlineContext) Deadline() (time.Time, bool) {
    return time.Now().Add(-time.Second), true
}

func TestRedisTokenStore_ACallersPassedDeadlineIsNotTheCallTimeout(t *testing.T) {
    store := NewTokenStore(newTestConnection(newTokenStoreClient(t)), TokenStoreConfig{Prefix: "melody:token:test:lapsed"})

    deleteErr := store.Delete(&contextOnlyRuntime{ctx: lapsedDeadlineContext{Context: context.Background()}}, "token")
    if false == errors.Is(deleteErr, context.DeadlineExceeded) || false == exception.AnswersUnavailable(deleteErr) || nil != exceptionContextOf(deleteErr)["bound"] {
        t.Fatalf("expected the caller's deadline answered marked, naming no bound of the store, got %v %v", deleteErr, exceptionContextOf(deleteErr))
    }
}

func TestRedisTokenStore_ARoundTripCutByTheCallTimeoutNamesTheBound(t *testing.T) {
    store, _ := newWedgedTokenStore(t, TokenStoreConfig{})

    deleteErr := awaitOutcome(t, boundProbeBudget, func() error {
        return store.Delete(newTokenStoreRuntime(), "token")
    })

    requireDeadlineExceeded(t, deleteErr)

    if false == exception.AnswersUnavailable(deleteErr) || "TokenStoreConfig.CallTimeout" != exceptionContextOf(deleteErr)["bound"] {
        t.Fatalf("expected a marked failure naming the call timeout, got %v %v", deleteErr, exceptionContextOf(deleteErr))
    }
}

type countdownContext struct {
    context.Context
    remaining atomic.Int64
}

func (instance *countdownContext) Err() error {
    if 0 > instance.remaining.Add(-1) {
        return context.Canceled
    }

    return nil
}

func TestRedisTokenStore_DeleteByUserAnswersWhatItRemovedBeforeTheFailure(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:partial", ScanCount: bound.Of(1)})

    for _, tokenString := range []string{"partial-1", "partial-2"} {
        if putErr := store.Put(newTokenStoreRuntime(), tokenString, securitycontract.Claims{UserIdentifier: "pat"}, bound.UnlimitedDuration()); nil != putErr {
            t.Fatalf("put: %v", putErr)
        }
    }
    defer mustDeleteByUser(t, store, "pat")

    cutContext := &countdownContext{Context: context.Background()}
    cutContext.remaining.Store(2)

    removed, deleteErr := store.DeleteByUser(&contextOnlyRuntime{ctx: cutContext}, "pat")
    if 1 != removed || false == exception.AnswersUnavailable(deleteErr) {
        t.Fatalf("expected the one token removed before the cut beside a marked error, got %d %v", removed, deleteErr)
    }
}

func TestRedisTokenStore_DeleteByUserAnswersWhatItRemovedBeforeABatchFailed(t *testing.T) {
    client, storeGate := dialGated(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:wedge:" + t.Name(), ScanCount: bound.Of(1), CallTimeout: bound.Of(50 * time.Millisecond)})

    for _, tokenString := range []string{"batch-1", "batch-2"} {
        if putErr := store.Put(newTokenStoreRuntime(), tokenString, securitycontract.Claims{UserIdentifier: "bart"}, bound.UnlimitedDuration()); nil != putErr {
            t.Fatalf("put: %v", putErr)
        }
    }

    storeGate.WedgeIntegerRepliesAfter(1)

    var removed int
    deleteErr := awaitOutcome(t, boundProbeBudget, func() error {
        var walkErr error
        removed, walkErr = store.DeleteByUser(newTokenStoreRuntime(), "bart")

        return walkErr
    })

    if 1 != removed || false == exception.AnswersUnavailable(deleteErr) {
        t.Fatalf("expected the one token removed before the wedged batch beside a marked error, got %d %v", removed, deleteErr)
    }
}

func TestRedisTokenStore_ArgumentRefusalsAreUnmarkedErrors(t *testing.T) {
    store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{})
    runtimeInstance := newTokenStoreRuntime()

    for refusal, call := range map[string]func() error{
        "ttl absent": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u"}, bound.Duration{})
        },
        "empty user":   func() error { return store.RevokeBefore(runtimeInstance, "", time.Now(), securitycontract.RevocationOptions{}) },
        "zero instant": func() error { return store.RevokeBefore(runtimeInstance, "u", time.Time{}, securitycontract.RevocationOptions{}) },
        "non-UTF-8 user": func() error {
            _, deleteErr := store.DeleteByUser(runtimeInstance, "u\xff")

            return deleteErr
        },
    } {
        if refusalErr := call(); nil == refusalErr || true == exception.IsInfrastructureFailure(refusalErr) {
            t.Fatalf("expected the %s refusal to be an unmarked error, got %v", refusal, refusalErr)
        }
    }
}

func TestTokenStores_RefuseANilRuntimeAtEveryDoor(t *testing.T) {
    stores := map[string]securitycontract.EpochRevocableTokenStore{
        "redis":     NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{}),
        "in-memory": melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}),
    }

    var typedNil *contextOnlyRuntime
    runtimes := map[string]containercontract.Runtime{
        "nil":           nil,
        "typed nil":     typedNil,
        "nil context":   &contextOnlyRuntime{},
    }

    for storeName, store := range stores {
        for runtimeName, runtimeInstance := range runtimes {
            doors := map[string]func() error{
                "Lookup": func() error {
                    _, _, lookupErr := store.Lookup(runtimeInstance, "t")

                    return lookupErr
                },
                "Put": func() error {
                    return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u"}, bound.UnlimitedDuration())
                },
                "Delete": func() error { return store.Delete(runtimeInstance, "t") },
                "DeleteByUser": func() error {
                    _, deleteErr := store.DeleteByUser(runtimeInstance, "u")

                    return deleteErr
                },
                "PurgeExpired": func() error {
                    _, purgeErr := store.PurgeExpired(runtimeInstance)

                    return purgeErr
                },
                "RevokeBefore": func() error { return store.RevokeBefore(runtimeInstance, "u", time.Now(), securitycontract.RevocationOptions{}) },
                "RevocationEpoch": func() error {
                    _, epochErr := store.RevocationEpoch(runtimeInstance, "u", securitycontract.RevocationOptions{})

                    return epochErr
                },
            }

            for door, call := range doors {
                doorErr := call()
                if "runtimeInstance" != exceptionContextOf(doorErr)["argument"] || true == exception.IsInfrastructureFailure(doorErr) {
                    t.Fatalf("expected the %s store's %s to refuse a %s runtime unmarked, naming the argument, got %v", storeName, door, runtimeName, doorErr)
                }
            }
        }
    }
}

func TestRedisTokenStore_LookupMarksAFailedRoundTripAndLeavesACorruptPayloadUnmarked(t *testing.T) {
    unreachable := NewTokenStore(newTestConnection(closedTestClient(t)), TokenStoreConfig{Prefix: "melody:token:test:lookupfailure"})
    if _, _, lookupErr := unreachable.Lookup(newTokenStoreRuntime(), "t"); false == exception.AnswersUnavailable(lookupErr) {
        t.Fatalf("expected a failed lookup round trip marked, got %v", lookupErr)
    }

    if _, epochErr := unreachable.RevocationEpoch(newTokenStoreRuntime(), "u", securitycontract.RevocationOptions{}); false == exception.AnswersUnavailable(epochErr) {
        t.Fatalf("expected a failed epoch round trip marked, got %v", epochErr)
    }

    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:lookupcorrupt"})
    tokenKey := store.tokenKey("corrupt")
    defer client.Do(context.Background(), client.B().Del().Key(tokenKey).Build())

    if setErr := client.Do(context.Background(), client.B().Set().Key(tokenKey).Value("not json").Build()).Error(); nil != setErr {
        t.Fatalf("planting the corrupt payload: %v", setErr)
    }

    _, _, lookupErr := store.Lookup(newTokenStoreRuntime(), "corrupt")
    if nil == lookupErr || true == exception.IsInfrastructureFailure(lookupErr) {
        t.Fatalf("expected a corrupt payload to be an unmarked error, got %v", lookupErr)
    }
}

func TestRedisTokenStore_NoKeyAndNoIndexMemberCarriesTheToken(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:digest"})

    if putErr := store.Put(newTokenStoreRuntime(), "secret-token", securitycontract.Claims{UserIdentifier: "dora"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }
    defer mustDeleteByUser(t, store, "dora")

    keys, scanErr := client.Do(context.Background(), client.B().Keys().Pattern(escapeRedisGlobMeta(store.keyspace())+"*").Build()).AsStrSlice()
    if nil != scanErr || 0 == len(keys) {
        t.Fatalf("expected the namespace to hold keys, got %v %v", keys, scanErr)
    }

    members, membersErr := client.Do(context.Background(), client.B().Smembers().Key(store.userKey("dora")).Build()).AsStrSlice()
    if nil != membersErr || 1 != len(members) {
        t.Fatalf("expected one index member, got %v %v", members, membersErr)
    }

    for _, element := range append(keys, members...) {
        if true == strings.Contains(element, "secret-token") {
            t.Fatalf("expected no key or index member to carry the token, got %q", element)
        }
    }
}

func TestRedisTokenStore_TheTokenKeyIsTheHexSha256OfTheToken(t *testing.T) {
    store := NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{Prefix: "melody:token"})

    if "{melody:token}:token:930bbdc51b6aed5c2a5678fd6e28dee7a05e8a4b643cfc0b4427c3efb86c0d94" != store.tokenKey("secret-token") {
        t.Fatalf("expected the key to be the hex SHA-256 of the token, got %q", store.tokenKey("secret-token"))
    }
}

func TestRedisTokenStore_ATokenUnderItsVerbatimKeyIsNotFound(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:verbatim"})
    verbatimKey := store.keyspace() + ":token:verbatim-token"
    defer client.Do(context.Background(), client.B().Del().Key(verbatimKey).Build())

    if setErr := client.Do(context.Background(), client.B().Set().Key(verbatimKey).Value(`{"UserIdentifier":"vera","Roles":["ROLE_USER"]}`).Build()).Error(); nil != setErr {
        t.Fatalf("planting the verbatim key: %v", setErr)
    }

    if _, found, lookupErr := store.Lookup(newTokenStoreRuntime(), "verbatim-token"); true == found || nil != lookupErr {
        t.Fatalf("expected a token stored under its verbatim key to be refused, got found=%v err=%v", found, lookupErr)
    }
}

func TestRedisTokenStore_DeleteByUserRemovesAVerbatimKeyLeftByAnEarlierRelease(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:verbatimreclaim"})
    verbatimKey := store.keyspace() + ":token:earlier-release-token"
    indexKey := store.userKey("walt")
    defer client.Do(context.Background(), client.B().Del().Key(verbatimKey).Build())
    defer client.Do(context.Background(), client.B().Del().Key(indexKey).Build())

    if setErr := client.Do(context.Background(), client.B().Set().Key(verbatimKey).Value(`{"UserIdentifier":"walt","Roles":["ROLE_USER"]}`).Build()).Error(); nil != setErr {
        t.Fatalf("planting the verbatim key: %v", setErr)
    }

    if addErr := client.Do(context.Background(), client.B().Sadd().Key(indexKey).Member(verbatimKey).Build()).Error(); nil != addErr {
        t.Fatalf("planting the index member: %v", addErr)
    }

    if removed := mustDeleteByUser(t, store, "walt"); 1 != removed {
        t.Fatalf("expected the earlier release's PascalCase value removed, got %d", removed)
    }

    if exists, _ := client.Do(context.Background(), client.B().Exists().Key(verbatimKey).Build()).AsInt64(); 0 != exists {
        t.Fatalf("expected the verbatim key gone")
    }
}

func mustDeleteByUser(t *testing.T, store *RedisTokenStore, userIdentifier string) int {
    t.Helper()

    removed, deleteErr := store.DeleteByUser(newTokenStoreRuntime(), userIdentifier)
    if nil != deleteErr {
        t.Fatalf("delete by user: %v", deleteErr)
    }

    return removed
}

func TestTokenStores_RefuseADoneContextAtEveryDoorMarked(t *testing.T) {
    stores := map[string]securitycontract.EpochRevocableTokenStore{
        "redis":     NewTokenStore(newTestConnection(&stubClient{}), TokenStoreConfig{}),
        "in-memory": melodysecurity.NewInMemoryTokenStore(melodysecurity.InMemoryTokenStoreConfig{}),
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    runtimeInstance := &contextOnlyRuntime{ctx: cancelledContext}

    for storeName, store := range stores {
        doors := map[string]func() error{
            "Lookup": func() error {
                _, _, lookupErr := store.Lookup(runtimeInstance, "t")

                return lookupErr
            },
            "Put": func() error {
                return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u"}, bound.UnlimitedDuration())
            },
            "Delete": func() error { return store.Delete(runtimeInstance, "t") },
            "DeleteByUser": func() error {
                _, deleteErr := store.DeleteByUser(runtimeInstance, "u")

                return deleteErr
            },
            "PurgeExpired": func() error {
                _, purgeErr := store.PurgeExpired(runtimeInstance)

                return purgeErr
            },
            "RevokeBefore": func() error { return store.RevokeBefore(runtimeInstance, "u", time.Now(), securitycontract.RevocationOptions{}) },
            "RevocationEpoch": func() error {
                _, epochErr := store.RevocationEpoch(runtimeInstance, "u", securitycontract.RevocationOptions{})

                return epochErr
            },
        }

        for door, call := range doors {
            doorErr := call()
            if false == exception.AnswersUnavailable(doorErr) || false == errors.Is(doorErr, context.Canceled) {
                t.Fatalf("expected the %s store's %s to refuse a done context marked, carrying its error, got %v", storeName, door, doorErr)
            }
        }
    }
}

func plantUndecodableRecord(t *testing.T, client redisclient.Client, store *RedisTokenStore, tokenString string, userIdentifier string) string {
    t.Helper()

    tokenKey := store.tokenKey(tokenString)
    if setErr := client.Do(context.Background(), client.B().Set().Key(tokenKey).Value("not json").Build()).Error(); nil != setErr {
        t.Fatalf("planting the undecodable record: %v", setErr)
    }

    if "" != userIdentifier {
        if addErr := client.Do(context.Background(), client.B().Sadd().Key(store.userKey(userIdentifier)).Member(tokenKey).Build()).Error(); nil != addErr {
            t.Fatalf("indexing the undecodable record: %v", addErr)
        }
    }

    return tokenKey
}

func keyExists(t *testing.T, client redisclient.Client, key string) bool {
    t.Helper()

    exists, existsErr := client.Do(context.Background(), client.B().Exists().Key(key).Build()).AsInt64()
    if nil != existsErr {
        t.Fatalf("exists: %v", existsErr)
    }

    return 1 == exists
}

func TestRedisTokenStore_DeleteRemovesAnUndecodableRecord(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:undecodable:delete"})
    tokenKey := plantUndecodableRecord(t, client, store, "corrupt", "")
    defer client.Do(context.Background(), client.B().Del().Key(tokenKey).Build())

    if deleteErr := store.Delete(newTokenStoreRuntime(), "corrupt"); nil != deleteErr {
        t.Fatalf("expected the undecodable record deleted, got %v", deleteErr)
    }

    if true == keyExists(t, client, tokenKey) {
        t.Fatalf("expected the undecodable record gone")
    }
}

func TestRedisTokenStore_PutOverAnUndecodableRecordReplacesIt(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:undecodable:put"})
    tokenKey := plantUndecodableRecord(t, client, store, "corrupt", "")
    defer client.Do(context.Background(), client.B().Del().Key(tokenKey).Build())
    defer mustDeleteByUser(t, store, "replacer")

    if putErr := store.Put(newTokenStoreRuntime(), "corrupt", securitycontract.Claims{UserIdentifier: "replacer"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected the put over the undecodable record admitted, got %v", putErr)
    }

    if claims, found, lookupErr := store.Lookup(newTokenStoreRuntime(), "corrupt"); nil != lookupErr || false == found || "replacer" != claims.UserIdentifier {
        t.Fatalf("expected the replacing claims, got %+v %t %v", claims, found, lookupErr)
    }
}

func TestRedisTokenStore_DeleteByUserIsNotBlockedByAnUndecodableMember(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:undecodable:byuser"})

    if putErr := store.Put(newTokenStoreRuntime(), "sound", securitycontract.Claims{UserIdentifier: "uma"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    tokenKey := plantUndecodableRecord(t, client, store, "corrupt", "uma")
    defer client.Do(context.Background(), client.B().Del().Key(tokenKey).Build())

    removed, deleteErr := store.DeleteByUser(newTokenStoreRuntime(), "uma")
    if nil != deleteErr || 2 != removed {
        t.Fatalf("expected both records removed, got %d %v", removed, deleteErr)
    }

    if true == keyExists(t, client, tokenKey) || true == keyExists(t, client, store.userKey("uma")) {
        t.Fatalf("expected the undecodable record and the index gone")
    }
}

func TestRedisTokenStore_PurgeExpiredAnswersWhatItPrunedBeforeTheFailure(t *testing.T) {
    client, storeGate := dialGated(t)
    store := NewTokenStore(newTestConnection(client), TokenStoreConfig{Prefix: "melody:token:test:wedge:" + t.Name(), ScanCount: bound.Of(1), CallTimeout: bound.Of(50 * time.Millisecond)})

    for _, tokenString := range []string{"lapsed-1", "lapsed-2"} {
        if putErr := store.Put(newTokenStoreRuntime(), tokenString, securitycontract.Claims{UserIdentifier: "petra"}, bound.UnlimitedDuration()); nil != putErr {
            t.Fatalf("put: %v", putErr)
        }

        if delErr := client.Do(context.Background(), client.B().Del().Key(store.tokenKey(tokenString)).Build()).Error(); nil != delErr {
            t.Fatalf("dropping the token record: %v", delErr)
        }
    }
    cleanupClient := newTokenStoreClient(t)
    defer cleanupClient.Do(context.Background(), cleanupClient.B().Del().Key(store.userKey("petra")).Build())

    storeGate.WedgeIntegerRepliesAfter(1)

    var pruned int
    purgeErr := awaitOutcome(t, boundProbeBudget, func() error {
        var walkErr error
        pruned, walkErr = store.PurgeExpired(newTokenStoreRuntime())

        return walkErr
    })

    if 1 != pruned || false == exception.AnswersUnavailable(purgeErr) {
        t.Fatalf("expected the one member pruned before the wedged batch beside a marked error, got %d %v", pruned, purgeErr)
    }
}

type answeredDeadlineContext struct {
    context.Context
    deadline time.Time
}

func (instance answeredDeadlineContext) Deadline() (time.Time, bool) {
    return instance.deadline, true
}

func TestStoreFailure_NamesTheCallTimeoutOnlyWhenTheCallsOwnDeadlinePassed(t *testing.T) {
    clientDeadline := fmt.Errorf("client read: %w", context.DeadlineExceeded)

    liveCall := answeredDeadlineContext{Context: context.Background(), deadline: time.Now().Add(time.Hour)}
    if nil != exceptionContextOf(storeFailure("m", nil, context.Background(), liveCall, clientDeadline))["bound"] {
        t.Fatalf("expected a deadline the client answered before the call's own to name no bound")
    }

    lapsedCall := answeredDeadlineContext{Context: context.Background(), deadline: time.Now().Add(-time.Second)}
    if "TokenStoreConfig.CallTimeout" != exceptionContextOf(storeFailure("m", nil, context.Background(), lapsedCall, clientDeadline))["bound"] {
        t.Fatalf("expected the call's passed deadline to name the call timeout")
    }
}

func TestStoreFailure_ACancelledCallerIsNamedOnce(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    failure := storeFailure("m", nil, cancelledContext, cancelledContext, context.Canceled)
    if "context canceled" != exceptionContextOf(failure)["cause"] {
        t.Fatalf("expected the caller's cancellation named once, got %q %v", failure.Error(), exceptionContextOf(failure))
    }

    wrapped := storeFailure("m", nil, cancelledContext, nil, errors.New("connection reset"))
    if false == errors.Is(wrapped, context.Canceled) || false == exception.AnswersUnavailable(wrapped) {
        t.Fatalf("expected the caller's cancellation in the chain of a failed round trip, got %v", wrapped)
    }
}
