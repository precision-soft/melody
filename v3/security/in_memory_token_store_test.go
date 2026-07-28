package security

import (
    "context"
    "sync"
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

/* the originating actor must survive the store's claim clone (Put + Lookup), otherwise the opaque-token path silently drops F1 propagation. */
func TestInMemoryTokenStore_PreservesOriginatingActor(t *testing.T) {
    store := NewInMemoryTokenStore()

    store.Put("opaque-actor", securitycontract.Claims{
        UserIdentifier: "billing-service",
        Roles:          []string{"ROLE_SERVICE"},
        OriginatingActor: &securitycontract.ActorData{
            Identifier: "client-42",
            Type:       securitycontract.ActorTypeApiClient,
            Roles:      []string{"ROLE_CLIENT"},
            Attributes: map[string]string{"region": "eu"},
        },
    })

    claims, found, lookupErr := store.Lookup(testRuntime(), "opaque-actor")
    if nil != lookupErr || false == found {
        t.Fatalf("expected the token to be found: found=%v err=%v", found, lookupErr)
    }

    if nil == claims.OriginatingActor {
        t.Fatal("expected the originating actor to survive the store round-trip")
    }

    if "client-42" != claims.OriginatingActor.Identifier || "eu" != claims.OriginatingActor.Attributes["region"] {
        t.Fatalf("unexpected actor after round-trip: %+v", claims.OriginatingActor)
    }

    token := NewAuthenticatedTokenFromClaims(claims)
    if _, present := token.OnBehalfOf(); false == present {
        t.Fatal("expected the rebuilt token to carry the actor")
    }
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

/* the impersonator subtree carried on an originating actor must be deep-copied by the store clone, otherwise a Lookup caller (or a caller mutating its claims after Put) corrupts the stored impersonator's roles and concurrent Lookups race on the shared *ActorData. */
func TestInMemoryTokenStore_LookupDeepCopiesImpersonatorSubtree(t *testing.T) {
    store := NewInMemoryTokenStore()
    rt := tokenStoreRuntime()

    store.Put("tok-imp", securitycontract.Claims{
        UserIdentifier: "billing-service",
        OriginatingActor: &securitycontract.ActorData{
            Identifier: "client-42",
            Type:       securitycontract.ActorTypeUser,
            Impersonator: &securitycontract.ActorData{
                Identifier: "admin-7",
                Type:       securitycontract.ActorTypeUser,
                Roles:      []string{"ROLE_ADMIN"},
                Attributes: map[string]string{"region": "eu"},
            },
        },
    })

    first, found, err := store.Lookup(rt, "tok-imp")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    first.OriginatingActor.Impersonator.Roles[0] = "ROLE_PWNED"
    first.OriginatingActor.Impersonator.Attributes["region"] = "evil"

    second, _, _ := store.Lookup(rt, "tok-imp")
    if "ROLE_ADMIN" != second.OriginatingActor.Impersonator.Roles[0] {
        t.Fatalf("mutating a returned impersonator role corrupted the stored entry, got %v", second.OriginatingActor.Impersonator.Roles)
    }
    if "eu" != second.OriginatingActor.Impersonator.Attributes["region"] {
        t.Fatalf("mutating a returned impersonator attribute corrupted the stored entry, got %v", second.OriginatingActor.Impersonator.Attributes)
    }
}

/* mutating the caller's nested impersonator after Put must not reach the stored entry. */
func TestInMemoryTokenStore_PutDeepCopiesImpersonatorSubtree(t *testing.T) {
    store := NewInMemoryTokenStore()
    rt := tokenStoreRuntime()

    original := securitycontract.Claims{
        UserIdentifier: "billing-service",
        OriginatingActor: &securitycontract.ActorData{
            Identifier: "client-42",
            Type:       securitycontract.ActorTypeUser,
            Impersonator: &securitycontract.ActorData{
                Identifier: "admin-7",
                Type:       securitycontract.ActorTypeUser,
                Roles:      []string{"ROLE_ADMIN"},
            },
        },
    }

    store.Put("tok-imp-put", original)

    original.OriginatingActor.Impersonator.Roles[0] = "ROLE_PWNED"

    stored, found, err := store.Lookup(rt, "tok-imp-put")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    if "ROLE_ADMIN" != stored.OriginatingActor.Impersonator.Roles[0] {
        t.Fatalf("mutating the caller's impersonator after Put corrupted the stored entry, got %v", stored.OriginatingActor.Impersonator.Roles)
    }
}

/* @important a cyclic impersonator chain (reachable in-process through the exported ActorData.Impersonator field) must terminate via the depth bound rather than recurse until the goroutine stack overflows — a fatal error no recover() can catch. The test completing (Put and Lookup both return) is the assertion; without the bound cloneActorData would SIGSEGV. */
func TestInMemoryTokenStore_CyclicImpersonatorChainTerminates(t *testing.T) {
    store := NewInMemoryTokenStore()
    rt := tokenStoreRuntime()

    actor := &securitycontract.ActorData{
        Identifier: "client-42",
        Type:       securitycontract.ActorTypeUser,
        Roles:      []string{"ROLE_USER"},
    }
    actor.Impersonator = actor

    store.Put("tok-cycle", securitycontract.Claims{
        UserIdentifier:   "billing-service",
        OriginatingActor: actor,
    })

    stored, found, err := store.Lookup(rt, "tok-cycle")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    if nil == stored.OriginatingActor || "client-42" != stored.OriginatingActor.Identifier {
        t.Fatalf("expected the cyclic actor to round-trip its top link, got %+v", stored.OriginatingActor)
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

func TestInMemoryTokenStore_ClonedClaimsCarryEveryField(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    store.Put("tok", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"})

    claims, found, err := store.Lookup(rt, "tok")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    if "phone" != claims.DeviceIdentifier {
        t.Fatalf("the device identifier did not survive the clone, got %q", claims.DeviceIdentifier)
    }

    if false == claims.IssuedAt.Equal(frozen.Now()) {
        t.Fatalf("the issue instant did not survive the clone, got %v", claims.IssuedAt)
    }
}

func TestInMemoryTokenStore_RevokeBeforeRejectsAnOlderTokenWithoutDeletingIt(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    store.Put("tok", securitycontract.Claims{UserIdentifier: "u1"})

    frozen.Advance(time.Minute)
    store.RevokeBefore("u1", "", frozen.Now())

    if _, found, err := store.Lookup(rt, "tok"); true == found || nil != err {
        t.Fatalf("expected the revoked token to stop resolving, got found=%v err=%v", found, err)
    }

    if removed := store.DeleteByUser("u1"); 1 != removed {
        t.Fatalf("the entry is already gone, so the miss proves it was DELETED rather than that the boundary refused it; DeleteByUser reported %d", removed)
    }
}

func TestInMemoryTokenStore_RevocationEpochOnlyEverMovesForward(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    later := time.Unix(5000, 0)
    store.RevokeBefore("u1", "", later)
    store.RevokeBefore("u1", "", later.Add(-time.Minute))

    published, err := store.RevocationEpoch(rt, "u1", "")
    if nil != err {
        t.Fatalf("revocation epoch: %v", err)
    }

    if false == published.Equal(later) {
        t.Fatalf("expected the later boundary %v to stand, got %v", later, published)
    }
}

func TestInMemoryTokenStore_DeviceRevocationSparesAnotherDeviceOfTheSameUser(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    store.Put("tok-phone", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"})
    store.Put("tok-laptop", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "laptop"})

    frozen.Advance(time.Minute)
    store.RevokeBefore("u1", "phone", frozen.Now())

    if _, found, _ := store.Lookup(rt, "tok-phone"); true == found {
        t.Fatalf("expected the revoked device's token to stop resolving")
    }

    if _, found, err := store.Lookup(rt, "tok-laptop"); false == found || nil != err {
        t.Fatalf("revoking one device ended another device's token, got found=%v err=%v", found, err)
    }
}

func TestInMemoryTokenStore_UserRevocationCoversEveryDevice(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    store.Put("tok-phone", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"})
    store.Put("tok-laptop", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "laptop"})

    frozen.Advance(time.Minute)
    store.RevokeBefore("u1", "", frozen.Now())

    for _, tokenString := range []string{"tok-phone", "tok-laptop"} {
        if _, found, _ := store.Lookup(rt, tokenString); true == found {
            t.Fatalf("a user-wide revocation left %q alive", tokenString)
        }
    }
}

func TestInMemoryTokenStore_PutAlwaysStampsTheIssueInstant(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    store.RevokeBefore("u1", "", time.Unix(900, 0))
    store.Put("tok", securitycontract.Claims{UserIdentifier: "u1", IssuedAt: time.Unix(500, 0)})

    claims, found, err := store.Lookup(rt, "tok")
    if false == found || nil != err {
        t.Fatalf("the caller's stale issue instant was kept, so the refreshed token was born already revoked: found=%v err=%v", found, err)
    }

    if false == claims.IssuedAt.Equal(frozen.Now()) {
        t.Fatalf("expected the store's clock to stamp %v, got %v", frozen.Now(), claims.IssuedAt)
    }
}

func TestInMemoryTokenStore_PurgeExpiredKeepsTheBoundaryWhileATokenRemains(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    store.PutWithTtl("tok-short", securitycontract.Claims{UserIdentifier: "u1"}, time.Second)
    store.Put("tok-long", securitycontract.Claims{UserIdentifier: "u1"})

    frozen.Advance(time.Minute)
    store.RevokeBefore("u1", "", frozen.Now())

    store.PurgeExpired()

    published, err := store.RevocationEpoch(rt, "u1", "")
    if nil != err {
        t.Fatalf("revocation epoch: %v", err)
    }

    if true == published.IsZero() {
        t.Fatalf("the purge dropped a boundary while a token it refuses is still held, so that token would resolve again")
    }
}

func TestInMemoryTokenStore_PurgeExpiredDropsBoundariesOfUsersWithNoTokens(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStoreWithClock(frozen)
    rt := tokenStoreRuntime()

    store.PutWithTtl("tok", securitycontract.Claims{UserIdentifier: "u1"}, time.Second)

    frozen.Advance(time.Minute)
    store.RevokeBefore("u1", "", frozen.Now())

    store.PurgeExpired()

    published, err := store.RevocationEpoch(rt, "u1", "")
    if nil != err {
        t.Fatalf("revocation epoch: %v", err)
    }

    if false == published.IsZero() {
        t.Fatalf("expected the boundary of a user holding no token to be dropped, got %v", published)
    }
}

func TestInMemoryTokenStore_ConcurrentRevokeAndLookupIsRaceFree(t *testing.T) {
    store := NewInMemoryTokenStore()
    rt := tokenStoreRuntime()

    store.Put("tok", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"})

    waitGroup := sync.WaitGroup{}

    for worker := 0; worker < 8; worker++ {
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()

            for round := 0; round < 200; round++ {
                store.RevokeBefore("u1", "phone", time.Now())
            }
        }()

        go func() {
            defer waitGroup.Done()

            for round := 0; round < 200; round++ {
                _, _, _ = store.Lookup(rt, "tok")
            }
        }()
    }

    waitGroup.Wait()
}
