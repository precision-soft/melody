package rueidis

import (
    "context"
    "strconv"
    "strings"
    "testing"
    "time"

    melodyclock "github.com/precision-soft/melody/v3/clock"
    securitycontract "github.com/precision-soft/melody/v3/security/contract"
    redisclient "github.com/redis/rueidis"
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

func TestWithTokenStoreScanCount_SetsField(t *testing.T) {
    store := &RedisTokenStore{}
    WithTokenStoreScanCount(512)(store)

    if 512 != store.scanCount {
        t.Fatalf("expected scan count 512, got %d", store.scanCount)
    }
}

func TestNewTokenStore_DefaultScanCount(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client)

    if defaultTokenStoreScanCount != store.scanCount {
        t.Fatalf("expected default scan count %d, got %d", defaultTokenStoreScanCount, store.scanCount)
    }
}

func TestNewTokenStore_ScanCountOverride(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStoreScanCount(512))

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

/* only the token key ever carried PX; the per-user index set carried no expiry at all, so a user who logs in and out leaves an ever-growing set of dead member names behind for as long as the Redis lives — nothing sweeps it unless PurgeExpired is scheduled, and nothing here schedules it. */
func TestRedisTokenStore_PutGivesTheUserIndexAnExpiryThatOutlivesItsToken(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:indexttl"))

    store.PutWithTtl("token-indexed", securitycontract.Claims{UserIdentifier: "helen"}, 30*time.Second)
    defer store.DeleteByUser("helen")

    tokenPttl := tokenStorePttl(t, client, store.tokenKey("token-indexed"))
    indexPttl := tokenStorePttl(t, client, store.userKey("helen"))

    if 0 >= indexPttl {
        t.Fatalf("expected the user index to carry an expiry, got pttl %d", indexPttl)
    }

    if indexPttl <= tokenPttl {
        t.Fatalf("expected the user index (pttl %d) to outlive its token (pttl %d), or a live token would become unrevocable", indexPttl, tokenPttl)
    }
}

/* the index set is how DeleteByUser finds a user's tokens, so its expiry may only ever be raised: a shorter-lived token added later must not pull the deadline in front of a token that lives longer. */
func TestRedisTokenStore_PutRaisesButNeverShortensTheUserIndexExpiry(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:indexraise"))

    store.PutWithTtl("token-long", securitycontract.Claims{UserIdentifier: "ivan"}, 5*time.Minute)
    defer store.DeleteByUser("ivan")

    afterLong := tokenStorePttl(t, client, store.userKey("ivan"))

    store.PutWithTtl("token-short", securitycontract.Claims{UserIdentifier: "ivan"}, 2*time.Second)

    afterShort := tokenStorePttl(t, client, store.userKey("ivan"))
    if 4*time.Minute.Milliseconds() > afterShort {
        t.Fatalf("expected the index to keep the long-lived deadline (was %d), got %d", afterLong, afterShort)
    }

    store.PutWithTtl("token-longer", securitycontract.Claims{UserIdentifier: "ivan"}, 30*time.Minute)

    if afterLonger := tokenStorePttl(t, client, store.userKey("ivan")); 25*time.Minute.Milliseconds() > afterLonger {
        t.Fatalf("expected a longer-lived token to raise the index deadline, got %d", afterLonger)
    }
}

/* a token with no expiry is revocable forever, so the set that indexes it must never expire either */
func TestRedisTokenStore_NonExpiringTokenKeepsTheUserIndexPersistent(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:indexpersist"))

    store.PutWithTtl("token-expiring", securitycontract.Claims{UserIdentifier: "judy"}, 10*time.Second)
    store.Put("token-forever", securitycontract.Claims{UserIdentifier: "judy"})
    defer store.DeleteByUser("judy")

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

/* Redis runs a script with every other client blocked, so reading a whole index set inside one script makes a revocation stall the entire server for as long as that user's token history is: measured at ~4.9ms over 1k tokens, ~224ms over 100k and around two seconds over a million. The walk has to be incremental, and the batch handed to any one script has to be bounded by this side, because SSCAN treats its count as a hint and can return more. */
func TestRedisTokenStore_DeleteByUserRevokesInBoundedBatches(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:batched"),
        WithTokenStoreScanCount(10),
    )

    tokenCount := 250
    for index := 0; index < tokenCount; index++ {
        store.Put("batched-token-"+strconv.Itoa(index), securitycontract.Claims{UserIdentifier: "karl"})
    }

    /* another user's live token must survive a revocation that walks past it */
    store.Put("batched-token-other", securitycontract.Claims{UserIdentifier: "lena"})
    defer store.DeleteByUser("lena")

    before := sscanCallCount(t, client)

    removed := store.DeleteByUser("karl")
    if tokenCount != removed {
        t.Fatalf("expected %d tokens removed, got %d", tokenCount, removed)
    }

    /* one script over the whole set needs no cursor at all; a bounded walk needs one round trip per batch */
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

/* the purge reads the same sets a revocation does and has to hold the server for no longer, so it walks them the same way. It is in fact the operation that meets the biggest ones: it exists because dead members accumulate inside an index that is still alive, so the set it is handed is the whole history of an account that never stopped logging in — read into a single script, that is a multi-second freeze of every other client of that Redis. */
func TestRedisTokenStore_PurgeExpiredPrunesInBoundedBatches(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:purgebatched"),
        WithTokenStoreScanCount(10),
    )

    defer store.DeleteByUser("mona")

    staleCount := 250
    for index := 0; index < staleCount; index++ {
        tokenString := "purge-batched-token-" + strconv.Itoa(index)

        store.Put(tokenString, securitycontract.Claims{UserIdentifier: "mona"})

        /* dropping the token key from underneath the index leaves exactly the state a purge exists for: a set that is still alive holding members whose tokens are gone */
        if deleteErr := client.Do(
            context.Background(),
            client.B().Del().Key(store.tokenKey(tokenString)).Build(),
        ).Error(); nil != deleteErr {
            t.Fatalf("del: %v", deleteErr)
        }
    }

    store.Put("purge-batched-token-live", securitycontract.Claims{UserIdentifier: "mona"})

    before := sscanCallCount(t, client)

    if pruned := store.PurgeExpired(); staleCount != pruned {
        t.Fatalf("expected %d stale index members pruned, got %d", staleCount, pruned)
    }

    /* one script over the whole set needs no cursor at all; a bounded walk needs one round trip per batch */
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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokekeeps"))
    runtimeInstance := newTokenStoreRuntime()
    store.Put("token-revoked", securitycontract.Claims{UserIdentifier: "nina", Roles: []string{"ROLE_USER"}})
    defer store.DeleteByUser("nina")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("nina")).Build())

    if _, found, _ := store.Lookup(runtimeInstance, "token-revoked"); false == found {
        t.Fatalf("expected the token to resolve before it was revoked")
    }

    store.RevokeBefore("nina", "", time.Now().Add(time.Second))

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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokelater"))
    runtimeInstance := newTokenStoreRuntime()

    boundary := time.Now()
    store.RevokeBefore("owen", "", boundary)
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("owen")).Build())

    store.Put("token-fresh", securitycontract.Claims{UserIdentifier: "owen"})
    defer store.DeleteByUser("owen")

    if _, found, lookupErr := store.Lookup(runtimeInstance, "token-fresh"); false == found || nil != lookupErr {
        t.Fatalf("expected a token issued after the boundary to resolve, got found=%v err=%v", found, lookupErr)
    }

    published, epochErr := store.RevocationEpoch(runtimeInstance, "owen", "")
    if nil != epochErr {
        t.Fatalf("revocation epoch: %v", epochErr)
    }

    if false == published.Equal(boundary) {
        t.Fatalf("expected the published boundary to be %v, got %v", boundary, published)
    }
}

func TestRedisTokenStore_RevocationEpochOnlyEverMovesForward(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokemonotonic"))
    runtimeInstance := newTokenStoreRuntime()

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("pete")).Build())

    later := time.Now().Add(time.Hour)
    earlier := later.Add(-time.Minute)

    store.RevokeBefore("pete", "", later)
    store.RevokeBefore("pete", "", earlier)

    published, epochErr := store.RevocationEpoch(runtimeInstance, "pete", "")
    if nil != epochErr {
        t.Fatalf("revocation epoch: %v", epochErr)
    }

    if false == published.Equal(later) {
        t.Fatalf("expected the later boundary %v to stand, got %v", later, published)
    }

    store.Put("token-between", securitycontract.Claims{UserIdentifier: "pete"})
    defer store.DeleteByUser("pete")

    if _, found, _ := store.Lookup(runtimeInstance, "token-between"); true == found {
        t.Fatalf("a token stamped before the standing boundary resolved, so the boundary was lowered")
    }
}

func TestRedisTokenStore_RevocationEpochRaisesByASingleNanosecond(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokenanos"))
    runtimeInstance := newTokenStoreRuntime()

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("quinn")).Build())

    earlier := time.Now()
    later := earlier.Add(time.Nanosecond)

    store.RevokeBefore("quinn", "", earlier)
    store.RevokeBefore("quinn", "", later)

    published, epochErr := store.RevocationEpoch(runtimeInstance, "quinn", "")
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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokedevice"))
    runtimeInstance := newTokenStoreRuntime()

    store.Put("token-phone", securitycontract.Claims{UserIdentifier: "rita", DeviceIdentifier: "phone"})
    store.Put("token-laptop", securitycontract.Claims{UserIdentifier: "rita", DeviceIdentifier: "laptop"})
    defer store.DeleteByUser("rita")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("rita")).Build())

    store.RevokeBefore("rita", "phone", time.Now().Add(time.Second))

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

func TestRedisTokenStore_UserRevocationCoversEveryDevice(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokeuserwide"))
    runtimeInstance := newTokenStoreRuntime()

    store.Put("token-one", securitycontract.Claims{UserIdentifier: "sam", DeviceIdentifier: "phone"})
    store.Put("token-two", securitycontract.Claims{UserIdentifier: "sam", DeviceIdentifier: "laptop"})
    defer store.DeleteByUser("sam")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("sam")).Build())

    store.RevokeBefore("sam", "", time.Now().Add(time.Second))

    for _, tokenString := range []string{"token-one", "token-two"} {
        if _, found, _ := store.Lookup(runtimeInstance, tokenString); true == found {
            t.Fatalf("a user-wide revocation left %q alive", tokenString)
        }
    }
}

func TestRedisTokenStore_ADeviceNamedUserCannotWriteTheUserWideBoundary(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokefieldclash"))
    runtimeInstance := newTokenStoreRuntime()

    store.Put("token-other", securitycontract.Claims{UserIdentifier: "tess", DeviceIdentifier: "laptop"})
    defer store.DeleteByUser("tess")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("tess")).Build())

    store.RevokeBefore("tess", revocationUserField, time.Now().Add(time.Hour))

    if _, found, lookupErr := store.Lookup(runtimeInstance, "token-other"); false == found || nil != lookupErr {
        t.Fatalf("a device named %q revoked the whole account, got found=%v err=%v", revocationUserField, found, lookupErr)
    }
}

func TestRedisTokenStore_LookupRejectsAnUnstampedTokenOnlyOnceABoundaryExists(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokeunstamped"))
    runtimeInstance := newTokenStoreRuntime()

    tokenKey := store.tokenKey("token-legacy")
    defer client.Do(context.Background(), client.B().Del().Key(tokenKey).Build())
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("uma")).Build())
    if setErr := client.Do(
        context.Background(),
        client.B().Set().Key(tokenKey).Value(`{"UserIdentifier":"uma","Roles":["ROLE_USER"]}`).Build(),
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

    store.RevokeBefore("uma", "", time.Now())

    if _, found, _ := store.Lookup(runtimeInstance, "token-legacy"); true == found {
        t.Fatalf("an unstamped token survived a revocation, so it can never be revoked at all")
    }
}

func TestRedisTokenStore_PutStampsTheIssueInstantFromTheInjectedClock(t *testing.T) {
    client := newTokenStoreClient(t)
    frozen := melodyclock.NewFrozenClock(time.Unix(1_700_000_000, 0))
    store := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:revokestamp"),
        WithTokenStoreClock(frozen),
    )
    runtimeInstance := newTokenStoreRuntime()

    stale := time.Unix(1_600_000_000, 0)
    store.Put("token-stamped", securitycontract.Claims{UserIdentifier: "vera", IssuedAt: stale})
    defer store.DeleteByUser("vera")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("vera")).Build())

    claims, found, lookupErr := store.Lookup(runtimeInstance, "token-stamped")
    if false == found || nil != lookupErr {
        t.Fatalf("lookup: found=%v err=%v", found, lookupErr)
    }

    if false == claims.IssuedAt.Equal(frozen.Now()) {
        t.Fatalf("expected the store's clock to stamp %v, got %v", frozen.Now(), claims.IssuedAt)
    }
    store.RevokeBefore("vera", "", time.Unix(1_650_000_000, 0))

    if _, found, _ := store.Lookup(runtimeInstance, "token-stamped"); false == found {
        t.Fatalf("the caller's stale issue instant was kept, so the token was born already revoked")
    }
}

func TestRedisTokenStore_RevocationEpochOutlivesTheTokenItRefuses(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:revokeoutlives"),
        WithRevocationEpochRetention(time.Second),
    )

    store.PutWithTtl("token-long", securitycontract.Claims{UserIdentifier: "wade"}, time.Hour)
    defer store.DeleteByUser("wade")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("wade")).Build())

    store.RevokeBefore("wade", "", time.Now())

    tokenPttl := tokenStorePttl(t, client, store.tokenKey("token-long"))
    epochPttl := tokenStorePttl(t, client, store.epochKey("wade"))

    if epochPttl <= tokenPttl {
        t.Fatalf("the boundary expires at or before the token it refuses (epoch pttl %d, token pttl %d), so the token would come back to life", epochPttl, tokenPttl)
    }
}

func TestRedisTokenStore_NonExpiringTokenKeepsTheRevocationEpochPersistent(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokepersist"))

    store.Put("token-forever", securitycontract.Claims{UserIdentifier: "xena"})
    defer store.DeleteByUser("xena")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("xena")).Build())

    store.RevokeBefore("xena", "", time.Now())

    if epochPttl := tokenStorePttl(t, client, store.epochKey("xena")); -1 != epochPttl {
        t.Fatalf("expected the boundary of a non-expiring token to be persistent, got pttl %d", epochPttl)
    }
}

func TestRedisTokenStore_RevokeBeforeGivesABrandNewEpochHashAnExpiry(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:revokenewexpiry"),
        WithRevocationEpochRetention(time.Hour),
    )

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("yuri")).Build())
    store.RevokeBefore("yuri", "", time.Now())

    if epochPttl := tokenStorePttl(t, client, store.epochKey("yuri")); 0 >= epochPttl {
        t.Fatalf("expected a new boundary to carry the retention floor, got pttl %d", epochPttl)
    }
}

func TestRedisTokenStore_LookupReportsACorruptPayloadRatherThanFailingTheScript(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokecorrupt"))
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
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokebadinstant"))

    for name, instant := range map[string]time.Time{
        "zero":          {},
        "before-unix":   time.Unix(-1, 0),
        "past-int64-ns": time.Unix(1<<40, 0),
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("expected a panic for the %s instant", name)
                }
            }()

            store.RevokeBefore("zack", "", instant)
        }()
    }
}

func TestRedisTokenStore_RevokeBeforeRefusesAnEmptyUserIdentifier(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokenouser"))

    defer func() {
        if nil == recover() {
            t.Fatalf("expected a panic for an empty user identifier")
        }
    }()

    store.RevokeBefore("", "", time.Now())
}

func TestRedisTokenStore_PurgeExpiredDoesNotWalkTheEpochKeys(t *testing.T) {
    client := newTokenStoreClient(t)
    store := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:revokepurgescan"))

    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("abby")).Build())

    store.RevokeBefore("abby", "", time.Now())

    store.PurgeExpired()

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

    issuingNode := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:skew"), WithTokenStoreClock(issuingNodeClock))
    unbounded := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:skew"), WithTokenStoreClock(revokingNodeClock))
    bounded := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:skew"),
        WithTokenStoreClock(revokingNodeClock),
        WithTokenStoreMaximumClockSkew(10*time.Second),
    )

    defer issuingNode.DeleteByUser("compromised")
    defer client.Do(context.Background(), client.B().Del().Key(issuingNode.epochKey("compromised")).Build())

    issuingNode.Put("attacker-token", securitycontract.Claims{UserIdentifier: "compromised", Roles: []string{"ROLE_USER"}})

    revokingNodeClock.Advance(5 * time.Second)
    unbounded.RevokeBefore("compromised", "", revokingNodeClock.Now())

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

    store := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:skewcontrol"),
        WithTokenStoreClock(nodeClock),
        WithTokenStoreMaximumClockSkew(10*time.Second),
    )

    defer store.DeleteByUser("owen")
    defer client.Do(context.Background(), client.B().Del().Key(store.epochKey("owen")).Build())

    store.RevokeBefore("owen", "", nodeClock.Now())

    nodeClock.Advance(30 * time.Second)
    store.Put("fresh-token", securitycontract.Claims{UserIdentifier: "owen"})

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

    issuingNode := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:skewwiden"), WithTokenStoreClock(issuingNodeClock))
    unbounded := NewTokenStore(client, WithTokenStorePrefix("melody:token:test:skewwiden"), WithTokenStoreClock(revokingNodeClock))
    bounded := NewTokenStore(
        client,
        WithTokenStorePrefix("melody:token:test:skewwiden"),
        WithTokenStoreClock(revokingNodeClock),
        WithTokenStoreMaximumClockSkew(10*time.Second),
    )

    defer issuingNode.DeleteByUser("compromised")
    defer client.Do(context.Background(), client.B().Del().Key(issuingNode.epochKey("compromised")).Build())

    issuingNode.Put("attacker-token", securitycontract.Claims{UserIdentifier: "compromised", Roles: []string{"ROLE_USER"}})

    revokingNodeClock.Advance(5 * time.Second)
    unbounded.RevokeBefore("compromised", "", revokingNodeClock.Now())

    if _, found, _ := unbounded.Lookup(runtimeInstance, "attacker-token"); false == found {
        t.Fatalf("the unbounded store refused the token, so the bounded half below cannot show the boundary being widened")
    }

    if _, found, _ := bounded.Lookup(runtimeInstance, "attacker-token"); true == found {
        t.Fatalf("the stamp sits inside the verifying node's own tolerance, so only widening the boundary can refuse it — and it was honoured")
    }
}
