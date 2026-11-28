package security

import (
    "context"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

func tokenStoreRuntime() containercontract.Runtime {
    c := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRequest(context.Background(), c.NewScope())
}

func TestInMemoryTokenStore_PreservesOriginatingActor(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})

    store.Put(newTestRuntime(), "opaque-actor", securitycontract.Claims{
        UserIdentifier: "billing-service",
        Roles:          []string{"ROLE_SERVICE"},
        OriginatingActor: &securitycontract.ActorData{
            Identifier: "client-42",
            Type:       securitycontract.ActorTypeApiClient,
            Roles:      []string{"ROLE_CLIENT"},
            Attributes: map[string]string{"region": "eu"},
        },
    }, bound.UnlimitedDuration())

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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    store.Put(newTestRuntime(), "short-lived", securitycontract.Claims{UserIdentifier: "user-1"}, bound.Of(30*time.Second))

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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put(newTestRuntime(), "token-a", securitycontract.Claims{UserIdentifier: "user-1"}, bound.UnlimitedDuration())
    store.Put(newTestRuntime(), "token-b", securitycontract.Claims{UserIdentifier: "user-1"}, bound.UnlimitedDuration())
    store.Put(newTestRuntime(), "token-c", securitycontract.Claims{UserIdentifier: "user-2"}, bound.UnlimitedDuration())

    runtimeInstance := testRuntime()

    if removed, _ := store.DeleteByUser(newTestRuntime(), "user-1"); 2 != removed {
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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    store.Put(newTestRuntime(), "short", securitycontract.Claims{UserIdentifier: "user-1"}, bound.Of(30*time.Second))
    store.Put(newTestRuntime(), "forever", securitycontract.Claims{UserIdentifier: "user-2"}, bound.UnlimitedDuration())

    frozen.Advance(time.Minute)

    if purged, _ := store.PurgeExpired(newTestRuntime()); 1 != purged {
        t.Fatalf("expected exactly one expired entry purged, got %d", purged)
    }

    if _, found, _ := store.Lookup(testRuntime(), "forever"); false == found {
        t.Fatalf("expected the non-expiring token to survive the purge")
    }
}

func TestInMemoryTokenStore_LookupReturnsIsolatedClaims(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put(newTestRuntime(), "opaque-iso", securitycontract.Claims{
        UserIdentifier: "user-1",
        Roles:          []string{"ROLE_USER"},
        Scope:          map[string]any{"company": "acme"},
    }, bound.UnlimitedDuration())

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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok", securitycontract.Claims{
        UserIdentifier: "u1",
        Attributes: map[string]any{
            "meta": map[string]any{"x": 1},
        },
    }, bound.UnlimitedDuration())

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

func TestInMemoryTokenStore_LookupDeepCopiesImpersonatorSubtree(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok-imp", securitycontract.Claims{
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
    }, bound.UnlimitedDuration())

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

func TestInMemoryTokenStore_PutDeepCopiesImpersonatorSubtree(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
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

    store.Put(newTestRuntime(), "tok-imp-put", original, bound.UnlimitedDuration())

    original.OriginatingActor.Impersonator.Roles[0] = "ROLE_PWNED"

    stored, found, err := store.Lookup(rt, "tok-imp-put")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    if "ROLE_ADMIN" != stored.OriginatingActor.Impersonator.Roles[0] {
        t.Fatalf("mutating the caller's impersonator after Put corrupted the stored entry, got %v", stored.OriginatingActor.Impersonator.Roles)
    }
}

func TestInMemoryTokenStore_CyclicImpersonatorChainTerminates(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    rt := tokenStoreRuntime()

    actor := &securitycontract.ActorData{
        Identifier: "client-42",
        Type:       securitycontract.ActorTypeUser,
        Roles:      []string{"ROLE_USER"},
    }
    actor.Impersonator = actor

    store.Put(newTestRuntime(), "tok-cycle", securitycontract.Claims{
        UserIdentifier:   "billing-service",
        OriginatingActor: actor,
    }, bound.UnlimitedDuration())

    stored, found, err := store.Lookup(rt, "tok-cycle")
    if nil != err || false == found {
        t.Fatalf("lookup failed: found=%v err=%v", found, err)
    }

    if nil == stored.OriginatingActor || "client-42" != stored.OriginatingActor.Identifier {
        t.Fatalf("expected the cyclic actor to round-trip its top link, got %+v", stored.OriginatingActor)
    }
}

func TestInMemoryTokenStore_PutDeepCopiesNestedScopeMap(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    rt := tokenStoreRuntime()

    original := securitycontract.Claims{
        UserIdentifier: "u1",
        Scope: map[string]any{
            "ns": map[string]any{"read": true},
        },
    }

    store.Put(newTestRuntime(), "tok", original, bound.UnlimitedDuration())

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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())

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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1"}, bound.UnlimitedDuration())

    frozen.Advance(time.Minute)
    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    if _, found, err := store.Lookup(rt, "tok"); true == found || nil != err {
        t.Fatalf("expected the revoked token to stop resolving, got found=%v err=%v", found, err)
    }

    if removed, _ := store.DeleteByUser(newTestRuntime(), "u1"); 1 != removed {
        t.Fatalf("the entry is already gone, so the miss proves it was DELETED rather than that the boundary refused it; DeleteByUser reported %d", removed)
    }
}

func TestInMemoryTokenStore_RevocationEpochOnlyEverMovesForward(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    later := time.Unix(5000, 0)
    store.RevokeBefore(newTestRuntime(), "u1", later, securitycontract.RevocationOptions{})
    store.RevokeBefore(newTestRuntime(), "u1", later.Add(-time.Minute), securitycontract.RevocationOptions{})

    published, err := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{})
    if nil != err {
        t.Fatalf("revocation epoch: %v", err)
    }

    if false == published.Equal(later) {
        t.Fatalf("expected the later boundary %v to stand, got %v", later, published)
    }
}

func TestInMemoryTokenStore_DeviceRevocationSparesAnotherDeviceOfTheSameUser(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok-phone", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())
    store.Put(newTestRuntime(), "tok-laptop", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "laptop"}, bound.UnlimitedDuration())

    frozen.Advance(time.Minute)
    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{DeviceIdentifier: "phone"})

    if _, found, _ := store.Lookup(rt, "tok-phone"); true == found {
        t.Fatalf("expected the revoked device's token to stop resolving")
    }

    if _, found, err := store.Lookup(rt, "tok-laptop"); false == found || nil != err {
        t.Fatalf("revoking one device ended another device's token, got found=%v err=%v", found, err)
    }
}

func TestInMemoryTokenStore_RevocationEpochReadsTheLaterOfTheUserAndTheDevice(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    userInstant := time.Unix(1000, 0)
    deviceInstant := time.Unix(2000, 0)

    store.RevokeBefore(newTestRuntime(), "u1", userInstant, securitycontract.RevocationOptions{})
    store.RevokeBefore(newTestRuntime(), "u1", deviceInstant, securitycontract.RevocationOptions{DeviceIdentifier: "phone"})

    if epoch, _ := store.RevocationEpoch(newTestRuntime(), "u1", securitycontract.RevocationOptions{DeviceIdentifier: "phone"}); false == deviceInstant.Equal(epoch) {
        t.Fatalf("expected the device's later boundary, got %v", epoch)
    }

    if epoch, _ := store.RevocationEpoch(newTestRuntime(), "u1", securitycontract.RevocationOptions{}); false == userInstant.Equal(epoch) {
        t.Fatalf("expected the whole user's boundary when the device is left out, got %v", epoch)
    }

    if epoch, _ := store.RevocationEpoch(newTestRuntime(), "u1", securitycontract.RevocationOptions{DeviceIdentifier: "laptop"}); false == userInstant.Equal(epoch) {
        t.Fatalf("expected another device to read the user's boundary alone, got %v", epoch)
    }
}

func TestInMemoryTokenStore_UserRevocationCoversEveryDevice(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok-phone", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())
    store.Put(newTestRuntime(), "tok-laptop", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "laptop"}, bound.UnlimitedDuration())

    frozen.Advance(time.Minute)
    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    for _, tokenString := range []string{"tok-phone", "tok-laptop"} {
        if _, found, _ := store.Lookup(rt, tokenString); true == found {
            t.Fatalf("a user-wide revocation left %q alive", tokenString)
        }
    }
}

func TestInMemoryTokenStore_PutAlwaysStampsTheIssueInstant(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.RevokeBefore(newTestRuntime(), "u1", time.Unix(900, 0), securitycontract.RevocationOptions{})
    store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1", IssuedAt: time.Unix(500, 0)}, bound.UnlimitedDuration())

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
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok-short", securitycontract.Claims{UserIdentifier: "u1"}, bound.Of(time.Second))
    store.Put(newTestRuntime(), "tok-long", securitycontract.Claims{UserIdentifier: "u1"}, bound.UnlimitedDuration())

    frozen.Advance(time.Minute)
    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    store.PurgeExpired(newTestRuntime())

    published, err := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{})
    if nil != err {
        t.Fatalf("revocation epoch: %v", err)
    }

    if true == published.IsZero() {
        t.Fatalf("the purge dropped a boundary while a token it refuses is still held, so that token would resolve again")
    }
}

func TestInMemoryTokenStore_PurgeExpiredKeepsTheBoundaryOfAUserWithNoStoredTokens(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1"}, bound.Of(time.Second))

    frozen.Advance(time.Minute)
    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    frozen.Advance(time.Minute)
    store.PurgeExpired(newTestRuntime())

    published, err := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{})
    if nil != err {
        t.Fatalf("revocation epoch: %v", err)
    }

    if true == published.IsZero() {
        t.Fatal("the purge dropped the boundary of a user holding no stored tokens, so every outstanding stateless JWT of that user was silently un-revoked")
    }
}

func TestInMemoryTokenStore_PurgeExpiredHonorsTheRevocationEpochRetentionWindow(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen, RevocationEpochRetention: bound.Of(time.Hour)})
    rt := tokenStoreRuntime()

    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    frozen.Advance(30 * time.Minute)
    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{DeviceIdentifier: "phone"})

    frozen.Advance(45 * time.Minute)
    store.PurgeExpired(newTestRuntime())

    userBoundary, userErr := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{})
    if nil != userErr {
        t.Fatalf("revocation epoch: %v", userErr)
    }

    if false == userBoundary.IsZero() {
        t.Fatalf("expected the user boundary published beyond the retention window to be purged, got %v", userBoundary)
    }

    deviceBoundary, deviceErr := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{DeviceIdentifier: "phone"})
    if nil != deviceErr {
        t.Fatalf("revocation epoch: %v", deviceErr)
    }

    if true == deviceBoundary.IsZero() {
        t.Fatal("the purge dropped a device boundary still inside the retention window")
    }
}

func TestInMemoryTokenStore_KeepsARevocationBoundarySevenDaysByDefault(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    frozen.Advance(7*24*time.Hour - time.Minute)
    store.PurgeExpired(newTestRuntime())

    if kept, _ := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{}); true == kept.IsZero() {
        t.Fatal("the default retention dropped a boundary younger than seven days")
    }

    frozen.Advance(2 * time.Minute)
    store.PurgeExpired(newTestRuntime())

    if dropped, _ := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{}); false == dropped.IsZero() {
        t.Fatalf("expected the default retention to drop a boundary older than seven days, got %v", dropped)
    }
}

func TestInMemoryTokenStore_AnUnlimitedRetentionKeepsTheBoundary(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen, RevocationEpochRetention: bound.UnlimitedDuration()})
    rt := tokenStoreRuntime()

    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    frozen.Advance(365 * 24 * time.Hour)
    store.PurgeExpired(newTestRuntime())

    if kept, _ := store.RevocationEpoch(rt, "u1", securitycontract.RevocationOptions{}); true == kept.IsZero() {
        t.Fatal("an unlimited retention dropped the boundary")
    }
}

func TestNewInMemoryTokenStore_RefusesARetentionUnderASecond(t *testing.T) {
    expectPanicNamingField(t, "InMemoryTokenStoreConfig.RevocationEpochRetention", func() {
        NewInMemoryTokenStore(InMemoryTokenStoreConfig{RevocationEpochRetention: bound.Of(500 * time.Millisecond)})
    })
}

func TestNewInMemoryTokenStore_ANilClockIsTheSystemClock(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    before := time.Now()

    store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1"}, bound.UnlimitedDuration())

    claims, found, _ := store.Lookup(tokenStoreRuntime(), "tok")
    if false == found || true == claims.IssuedAt.Before(before.Add(-time.Second)) || true == claims.IssuedAt.After(time.Now().Add(time.Second)) {
        t.Fatalf("expected the issue stamp from the system clock, got %v (found=%t)", claims.IssuedAt, found)
    }
}

func TestInMemoryTokenStore_PutRefusesAnAbsentTtlAndOneUnderASecond(t *testing.T) {
    for _, ttl := range []bound.Duration{{}, bound.Of(time.Second - time.Nanosecond), bound.Of(time.Duration(0)), bound.Of(-time.Second)} {
        putErr := NewInMemoryTokenStore(InMemoryTokenStoreConfig{}).Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1"}, ttl)
        expectErrorNamingField(t, putErr, securitycontract.TokenTtlRule().Name)

        if true == exception.AnswersUnavailable(putErr) {
            t.Fatalf("expected a refused ttl to be an unmarked error, got %v", putErr)
        }
    }
}

type gateClock struct {
    inner    clockcontract.Clock
    entered  chan struct{}
    release  chan struct{}
    gateOnce sync.Once
}

func (instance *gateClock) Now() time.Time {
    instance.gateOnce.Do(func() {
        close(instance.entered)
        <-instance.release
    })

    return instance.inner.Now()
}

func (instance *gateClock) NewTicker(interval time.Duration) clockcontract.Ticker {
    return instance.inner.NewTicker(interval)
}

func (instance *gateClock) NewTimer(duration time.Duration) clockcontract.Timer {
    return clock.NewSystemClock().NewTimer(duration)
}

func TestInMemoryTokenStore_PutStampsAndInsertsAtomicallyAgainstRevokeBefore(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    gate := &gateClock{inner: frozen, entered: make(chan struct{}), release: make(chan struct{})}
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: gate})

    putDone := make(chan struct{})
    go func() {
        store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1"}, bound.UnlimitedDuration())
        close(putDone)
    }()

    <-gate.entered

    revokeDone := make(chan struct{})
    go func() {
        store.RevokeBefore(newTestRuntime(), "u1", frozen.Now().Add(time.Second), securitycontract.RevocationOptions{})
        close(revokeDone)
    }()

    select {
    case <-revokeDone:
        t.Fatal("RevokeBefore completed while put held its clock read, so a boundary could still be published between the stamp and the insert")
    case <-time.After(50 * time.Millisecond):
    }

    close(gate.release)
    <-putDone
    <-revokeDone
}

func TestInMemoryTokenStore_ConcurrentRevokeAndLookupIsRaceFree(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())

    waitGroup := sync.WaitGroup{}

    for worker := 0; worker < 8; worker++ {
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()

            for round := 0; round < 200; round++ {
                store.RevokeBefore(newTestRuntime(), "u1", time.Now(), securitycontract.RevocationOptions{DeviceIdentifier: "phone"})
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

/* stamped exactly at the boundary: a minute away, a comparison written either way answers the same */
func TestInMemoryTokenStore_ATokenIssuedExactlyAtTheRevocationBoundaryIsRevoked(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen})
    rt := tokenStoreRuntime()

    store.Put(newTestRuntime(), "tok", securitycontract.Claims{UserIdentifier: "u1"}, bound.UnlimitedDuration())

    store.RevokeBefore(newTestRuntime(), "u1", frozen.Now(), securitycontract.RevocationOptions{})

    if _, found, err := store.Lookup(rt, "tok"); true == found || nil != err {
        t.Fatalf("expected a token stamped at the boundary instant to be revoked, got found=%v err=%v", found, err)
    }

    frozen.Advance(time.Nanosecond)
    store.Put(newTestRuntime(), "fresh", securitycontract.Claims{UserIdentifier: "u1"}, bound.UnlimitedDuration())

    if _, found, err := store.Lookup(rt, "fresh"); false == found || nil != err {
        t.Fatalf("expected a token stamped one unit after the boundary to survive it, got found=%v err=%v", found, err)
    }
}

func TestInMemoryTokenStore_PutForAnotherUserLeavesThePreviousUsersIndex(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    store.Put(newTestRuntime(), "shared", securitycontract.Claims{UserIdentifier: "alice"}, bound.UnlimitedDuration())
    store.Put(newTestRuntime(), "shared", securitycontract.Claims{UserIdentifier: "bob"}, bound.UnlimitedDuration())

    if removed, _ := store.DeleteByUser(newTestRuntime(), "alice"); 0 != removed {
        t.Fatalf("expected alice to hold no token, removed %d", removed)
    }

    claims, found, err := store.Lookup(testRuntime(), "shared")
    if nil != err || false == found || "bob" != claims.UserIdentifier {
        t.Fatalf("expected bob's token kept, got %+v found=%v err=%v", claims, found, err)
    }
}

func TestInMemoryTokenStore_PurgeExpiredKeepsARevokedTokenRevokedOnceItsBoundaryIsDropped(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Now())
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozenClock})

    store.Put(newTestRuntime(), "revoked", securitycontract.Claims{UserIdentifier: "user"}, bound.UnlimitedDuration())
    store.Put(newTestRuntime(), "other-device", securitycontract.Claims{UserIdentifier: "device-user", DeviceIdentifier: "phone"}, bound.UnlimitedDuration())

    frozenClock.Advance(time.Second)
    store.RevokeBefore(newTestRuntime(), "user", frozenClock.Now(), securitycontract.RevocationOptions{})
    store.RevokeBefore(newTestRuntime(), "device-user", frozenClock.Now(), securitycontract.RevocationOptions{DeviceIdentifier: "tablet"})

    frozenClock.Advance(time.Second)
    store.Put(newTestRuntime(), "after", securitycontract.Claims{UserIdentifier: "user"}, bound.UnlimitedDuration())

    frozenClock.Advance(8 * 24 * time.Hour)
    if purged, _ := store.PurgeExpired(newTestRuntime()); 1 != purged {
        t.Fatalf("expected the revoked token alone purged, got %d", purged)
    }

    for token, wanted := range map[string]bool{"revoked": false, "after": true, "other-device": true} {
        if _, found, _ := store.Lookup(tokenStoreRuntime(), token); wanted != found {
            t.Fatalf("token %q: expected found %t after the purge", token, wanted)
        }
    }
}

func TestInMemoryTokenStore_AWriteSweepsTheLapsedEntriesOnceTheIntervalPassed(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen, SweepInterval: bound.Of(time.Minute)})

    store.Put(newTestRuntime(), "lapsing", securitycontract.Claims{UserIdentifier: "u1"}, bound.Of(time.Second))

    frozen.Advance(30 * time.Second)
    store.Put(newTestRuntime(), "second", securitycontract.Claims{UserIdentifier: "u2"}, bound.Of(time.Hour))
    if _, held := store.entriesByToken["lapsing"]; false == held {
        t.Fatal("expected no sweep before the interval passed")
    }

    frozen.Advance(31 * time.Second)
    store.Put(newTestRuntime(), "third", securitycontract.Claims{UserIdentifier: "u3"}, bound.Of(time.Hour))
    if _, held := store.entriesByToken["lapsing"]; true == held {
        t.Fatal("expected the lapsed entry swept by the write after the interval")
    }

    if _, indexed := store.tokensByUser["u1"]; true == indexed {
        t.Fatal("expected the lapsed entry's user index swept with it")
    }
}

func TestInMemoryTokenStore_AnUnlimitedSweepIntervalLeavesThePurgeToTheCaller(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen, SweepInterval: bound.UnlimitedDuration()})

    store.Put(newTestRuntime(), "lapsing", securitycontract.Claims{UserIdentifier: "u1"}, bound.Of(time.Second))
    frozen.Advance(time.Hour)
    store.Put(newTestRuntime(), "second", securitycontract.Claims{UserIdentifier: "u2"}, bound.Of(time.Hour))

    if _, held := store.entriesByToken["lapsing"]; false == held {
        t.Fatal("expected no sweep when the interval is unlimited")
    }
}

func TestNewInMemoryTokenStore_RefusesASweepIntervalUnderAMillisecond(t *testing.T) {
    expectPanicNamingField(t, "InMemoryTokenStoreConfig.SweepInterval", func() {
        NewInMemoryTokenStore(InMemoryTokenStoreConfig{SweepInterval: bound.Of(time.Microsecond)})
    })
}

func TestInMemoryTokenStore_ARevocationAloneSweepsTheLapsedTokensAndKeepsEveryBoundary(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen, SweepInterval: bound.Of(time.Minute), RevocationEpochRetention: bound.Of(time.Second)})

    store.Put(newTestRuntime(), "lapsing", securitycontract.Claims{UserIdentifier: "u1"}, bound.Of(time.Second))
    store.RevokeBefore(newTestRuntime(), "u2", frozen.Now(), securitycontract.RevocationOptions{})

    frozen.Advance(2 * time.Minute)
    store.RevokeBefore(newTestRuntime(), "u3", frozen.Now(), securitycontract.RevocationOptions{})

    if _, held := store.entriesByToken["lapsing"]; true == held {
        t.Fatal("expected a revocation to sweep the lapsed token")
    }

    if _, kept := store.epochsByUser["u2"]; false == kept {
        t.Fatal("expected the sweep to keep a boundary past its retention, which only PurgeExpired drops")
    }

    store.PurgeExpired(newTestRuntime())
    if _, kept := store.epochsByUser["u2"]; true == kept {
        t.Fatal("expected PurgeExpired to drop the boundary past its retention")
    }
}

func TestInMemoryTokenStore_ADeleteSweepsTheLapsedTokens(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen, SweepInterval: bound.Of(time.Minute)})

    store.Put(newTestRuntime(), "lapsing", securitycontract.Claims{UserIdentifier: "u1"}, bound.Of(time.Second))
    store.Put(newTestRuntime(), "other", securitycontract.Claims{UserIdentifier: "u2"}, bound.Of(time.Second))

    frozen.Advance(2 * time.Minute)
    store.Delete(newTestRuntime(), "absent")
    if _, held := store.entriesByToken["lapsing"]; true == held {
        t.Fatal("expected a Delete to sweep")
    }

    store.Put(newTestRuntime(), "third", securitycontract.Claims{UserIdentifier: "u3"}, bound.Of(time.Second))
    frozen.Advance(2 * time.Minute)
    store.DeleteByUser(newTestRuntime(), "nobody")
    if _, held := store.entriesByToken["third"]; true == held {
        t.Fatal("expected a DeleteByUser to sweep")
    }
}

func TestInMemoryTokenStore_AClockSteppedBackSweepsOnTheNextWrite(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(100000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{Clock: frozen, SweepInterval: bound.Of(time.Minute)})

    store.Put(newTestRuntime(), "first", securitycontract.Claims{UserIdentifier: "u1"}, bound.Of(time.Second))
    store.entriesByToken["lapsed"] = tokenEntry{claims: securitycontract.Claims{UserIdentifier: "u9"}, expiresAt: time.Unix(10, 0)}

    frozen.TravelTo(time.Unix(50000, 0))
    store.Put(newTestRuntime(), "second", securitycontract.Claims{UserIdentifier: "u2"}, bound.Of(time.Hour))

    if _, held := store.entriesByToken["lapsed"]; true == held {
        t.Fatal("expected a write after the clock stepped back to sweep")
    }
}

func TestNewInMemoryTokenStore_RefusesASweepIntervalUnderASecond(t *testing.T) {
    expectPanicNamingField(t, "InMemoryTokenStoreConfig.SweepInterval", func() {
        NewInMemoryTokenStore(InMemoryTokenStoreConfig{SweepInterval: bound.Of(500 * time.Millisecond)})
    })
}

func fillTokenStore(t *testing.T, store *InMemoryTokenStore, ttl bound.Duration, tokens ...string) {
    t.Helper()

    for _, tokenString := range tokens {
        if putErr := store.Put(newTestRuntime(), tokenString, securitycontract.Claims{UserIdentifier: "u-" + tokenString}, ttl); nil != putErr {
            t.Fatalf("put %s: %v", tokenString, putErr)
        }
    }
}

func TestInMemoryTokenStore_PutAtTheCeilingIsRefusedMarkedAndFull(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(2)})
    fillTokenStore(t, store, bound.UnlimitedDuration(), "a", "b")

    putErr := store.Put(newTestRuntime(), "c", securitycontract.Claims{UserIdentifier: "u-c"}, bound.UnlimitedDuration())
    if false == exception.AnswersUnavailable(putErr) || false == exception.ChainIs(putErr, ErrTokenStoreFull) {
        t.Fatalf("expected a marked refusal carrying ErrTokenStoreFull, got %v", putErr)
    }

    if refusalContext := exception.LogContext(putErr, exception.LogContextOptions{}); "InMemoryTokenStoreConfig.MaxEntries" != refusalContext["field"] || 2 != refusalContext["maxEntries"] {
        t.Fatalf("expected the refusal to name the field and the ceiling, got %v", refusalContext)
    }
}

func TestInMemoryTokenStore_AnOverwriteAtTheCeilingIsAdmitted(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(2)})
    fillTokenStore(t, store, bound.UnlimitedDuration(), "a", "b")

    if putErr := store.Put(newTestRuntime(), "a", securitycontract.Claims{UserIdentifier: "u-other"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected an overwrite at the ceiling to be admitted, got %v", putErr)
    }

    if claims, found, _ := store.Lookup(newTestRuntime(), "a"); false == found || "u-other" != claims.UserIdentifier {
        t.Fatalf("expected the overwritten claims, got %+v (found=%t)", claims, found)
    }
}

func TestInMemoryTokenStore_TheCeilingSweepsLapsedAndRevokedTokensBeforeRefusing(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(2), SweepInterval: bound.UnlimitedDuration(), Clock: frozen})
    fillTokenStore(t, store, bound.Of(time.Second), "lapsing")
    fillTokenStore(t, store, bound.UnlimitedDuration(), "kept")

    frozen.Advance(2 * time.Second)

    if putErr := store.Put(newTestRuntime(), "after-lapse", securitycontract.Claims{UserIdentifier: "u-after"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected the lapsed token swept to make room, got %v", putErr)
    }

    revoking := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(1), SweepInterval: bound.UnlimitedDuration(), Clock: frozen})
    fillTokenStore(t, revoking, bound.UnlimitedDuration(), "revoked")

    frozen.Advance(time.Second)

    if revokeErr := revoking.RevokeBefore(newTestRuntime(), "u-revoked", frozen.Now(), securitycontract.RevocationOptions{}); nil != revokeErr {
        t.Fatalf("revoke: %v", revokeErr)
    }

    if putErr := revoking.Put(newTestRuntime(), "after-revocation", securitycontract.Claims{UserIdentifier: "u-after"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected the revoked token swept to make room, got %v", putErr)
    }
}

func TestInMemoryTokenStore_ARefusedPutEvictsNothing(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(2)})
    fillTokenStore(t, store, bound.UnlimitedDuration(), "a", "b")

    if putErr := store.Put(newTestRuntime(), "c", securitycontract.Claims{UserIdentifier: "u-c"}, bound.UnlimitedDuration()); nil == putErr {
        t.Fatalf("expected the put at the ceiling refused")
    }

    for _, tokenString := range []string{"a", "b"} {
        if _, found, _ := store.Lookup(newTestRuntime(), tokenString); false == found {
            t.Fatalf("expected %s kept after a refused put", tokenString)
        }
    }

    if _, found, _ := store.Lookup(newTestRuntime(), "c"); true == found {
        t.Fatalf("expected the refused token absent")
    }
}

func TestInMemoryTokenStore_RevokeBeforeAtTheCeilingIsAdmitted(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(1), Clock: frozen})
    fillTokenStore(t, store, bound.UnlimitedDuration(), "a")

    if revokeErr := store.RevokeBefore(newTestRuntime(), "someone-else", frozen.Now(), securitycontract.RevocationOptions{}); nil != revokeErr {
        t.Fatalf("expected a revocation at the ceiling admitted, got %v", revokeErr)
    }

    if epoch, _ := store.RevocationEpoch(newTestRuntime(), "someone-else", securitycontract.RevocationOptions{}); false == epoch.Equal(frozen.Now()) {
        t.Fatalf("expected the boundary published, got %v", epoch)
    }
}

func TestInMemoryTokenStore_MaxEntriesDefaultsToAHundredThousand(t *testing.T) {
    if maxEntries := NewInMemoryTokenStore(InMemoryTokenStoreConfig{}).maxEntries; 100_000 != maxEntries {
        t.Fatalf("expected 100,000 by default, got %d", maxEntries)
    }
}

func TestInMemoryTokenStore_UnlimitedMaxEntriesIsTheDoor(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.UnlimitedCount(), SweepInterval: bound.UnlimitedDuration()})
    runtimeInstance := newTestRuntime()

    for index := 0; index <= 100_000; index++ {
        if putErr := store.Put(runtimeInstance, strconv.Itoa(index), securitycontract.Claims{UserIdentifier: "u"}, bound.UnlimitedDuration()); nil != putErr {
            t.Fatalf("expected an unlimited store to admit token %d, got %v", index, putErr)
        }
    }
}

func TestInMemoryTokenStore_MaxEntriesBelowOneIsRefusedAtConstruction(t *testing.T) {
    expectPanicNamingField(t, "InMemoryTokenStoreConfig.MaxEntries", func() {
        NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(0)})
    })
}

func TestInMemoryTokenStore_AFullStoreDoesNotWalkEveryEntryPerRefusedPut(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(2), SweepInterval: bound.UnlimitedDuration(), Clock: frozen})
    fillTokenStore(t, store, bound.Of(time.Hour), "a")
    fillTokenStore(t, store, bound.UnlimitedDuration(), "b")

    for range 2 {
        frozen.Advance(time.Minute)

        if putErr := store.Put(newTestRuntime(), "c", securitycontract.Claims{UserIdentifier: "u-c"}, bound.UnlimitedDuration()); nil == putErr {
            t.Fatalf("expected the put at the ceiling refused")
        }

        if false == store.lastSweep.IsZero() {
            t.Fatalf("expected no sweep while nothing could have lapsed, got one at %v", store.lastSweep)
        }
    }
}

func TestInMemoryTokenStore_ArgumentRefusalsAreUnmarkedErrors(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    runtimeInstance := newTestRuntime()

    for refusal, call := range map[string]func() error{
        "ttl absent": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u"}, bound.Duration{})
        },
        "empty user":   func() error { return store.RevokeBefore(runtimeInstance, "", time.Now(), securitycontract.RevocationOptions{}) },
        "zero instant": func() error { return store.RevokeBefore(runtimeInstance, "u", time.Time{}, securitycontract.RevocationOptions{}) },
        "non-UTF-8 user at Put": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "\xff"}, bound.UnlimitedDuration())
        },
        "non-UTF-8 user at DeleteByUser": func() error {
            _, deleteErr := store.DeleteByUser(runtimeInstance, "\xff")

            return deleteErr
        },
        "non-UTF-8 user at RevokeBefore": func() error { return store.RevokeBefore(runtimeInstance, "\xff", time.Now(), securitycontract.RevocationOptions{}) },
        "instant before 1970": func() error {
            return store.RevokeBefore(runtimeInstance, "u", time.Date(1960, time.January, 1, 0, 0, 0, 0, time.UTC), securitycontract.RevocationOptions{})
        },
        "instant past 2262": func() error {
            return store.RevokeBefore(runtimeInstance, "u", time.Date(2300, time.January, 1, 0, 0, 0, 0, time.UTC), securitycontract.RevocationOptions{})
        },
    } {
        if refusalErr := call(); nil == refusalErr || true == exception.IsInfrastructureFailure(refusalErr) {
            t.Fatalf("expected the %s refusal to be an unmarked error, got %v", refusal, refusalErr)
        }
    }
}

func TestInMemoryTokenStore_ANonUtf8DeviceIdentifierIsRefusedNamingTheField(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    runtimeInstance := newTestRuntime()

    for field, call := range map[string]func() error{
        "Claims.DeviceIdentifier": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u", DeviceIdentifier: "phone\xff"}, bound.UnlimitedDuration())
        },
        "RevocationOptions.DeviceIdentifier": func() error {
            return store.RevokeBefore(runtimeInstance, "u", time.Now(), securitycontract.RevocationOptions{DeviceIdentifier: "phone\xff"})
        },
    } {
        refusal := call()
        if nil == refusal || false == strings.Contains(refusal.Error(), "device identifier must be valid UTF-8") || true == exception.IsInfrastructureFailure(refusal) {
            t.Fatalf("expected %s refused unmarked as not UTF-8, got %v", field, refusal)
        }

        refusalContext := exception.FromError(refusal).Context()
        if field != refusalContext["field"] || 6 != refusalContext["deviceLength"] {
            t.Fatalf("expected the refusal to name %s and the length, never the bytes, got %v", field, refusalContext)
        }
    }

    if _, found, _ := store.Lookup(runtimeInstance, "t"); true == found {
        t.Fatalf("expected the refused put to store nothing")
    }

    if epoch, _ := store.RevocationEpoch(runtimeInstance, "u", securitycontract.RevocationOptions{DeviceIdentifier: "phone\xff"}); false == epoch.IsZero() {
        t.Fatalf("expected the refused revocation to publish no boundary, got %v", epoch)
    }
}

func TestInMemoryTokenStore_AnIdentifierPastMaxIdentifierBytesIsRefusedNamingTheFieldAndTheLength(t *testing.T) {
    runtimeInstance := newTestRuntime()
    oversized := strings.Repeat("a", 256)
    longest := strings.Repeat("a", 255)

    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})

    for field, call := range map[string]func() error{
        "Claims.UserIdentifier": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: oversized}, bound.UnlimitedDuration())
        },
        "Claims.DeviceIdentifier": func() error {
            return store.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "u", DeviceIdentifier: oversized}, bound.UnlimitedDuration())
        },
        "userIdentifier": func() error {
            return store.RevokeBefore(runtimeInstance, oversized, time.Now(), securitycontract.RevocationOptions{})
        },
        "RevocationOptions.DeviceIdentifier": func() error {
            return store.RevokeBefore(runtimeInstance, "u", time.Now(), securitycontract.RevocationOptions{DeviceIdentifier: oversized})
        },
    } {
        refusal := call()
        if nil == refusal || true == exception.IsInfrastructureFailure(refusal) {
            t.Fatalf("expected %s of 256 bytes refused unmarked, got %v", field, refusal)
        }

        refusalContext := exception.FromError(refusal).Context()
        if field != refusalContext["field"] || 256 != refusalContext["identifierLength"] || 255 != refusalContext["maxIdentifierBytes"] || "InMemoryTokenStoreConfig.MaxIdentifierBytes" != refusalContext["limit"] {
            t.Fatalf("expected the refusal to name %s, its length and the limit, never the bytes, got %v", field, refusalContext)
        }

        if true == strings.Contains(refusal.Error(), oversized) {
            t.Fatalf("expected the refusal never to carry the identifier, got %v", refusal)
        }
    }

    if _, found, _ := store.Lookup(runtimeInstance, "t"); true == found {
        t.Fatalf("expected the refused put to store nothing")
    }

    if putErr := store.Put(runtimeInstance, "kept", securitycontract.Claims{UserIdentifier: longest, DeviceIdentifier: longest}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected identifiers of 255 bytes admitted, got %v", putErr)
    }

    if revokeErr := store.RevokeBefore(runtimeInstance, longest, time.Now(), securitycontract.RevocationOptions{DeviceIdentifier: longest}); nil != revokeErr {
        t.Fatalf("expected identifiers of 255 bytes admitted at the revocation, got %v", revokeErr)
    }

    bounded := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxIdentifierBytes: bound.Of(4)})
    if putErr := bounded.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "abcd", DeviceIdentifier: "abcd"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected identifiers at the configured bound admitted, got %v", putErr)
    }

    refusal := bounded.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: "abcde"}, bound.UnlimitedDuration())
    if nil == refusal || 4 != exception.FromError(refusal).Context()["maxIdentifierBytes"] {
        t.Fatalf("expected the configured bound applied, got %v", refusal)
    }

    unlimited := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxIdentifierBytes: bound.UnlimitedCount()})
    if putErr := unlimited.Put(runtimeInstance, "t", securitycontract.Claims{UserIdentifier: oversized + oversized, DeviceIdentifier: oversized}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected an unlimited bound to admit any length, got %v", putErr)
    }
}

func TestInMemoryTokenStore_RevocationEpochAnswersTheUsersBoundaryForADeviceNoWriteAccepts(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    runtimeInstance := newTestRuntime()

    instant := time.Now().Add(-time.Minute)
    if revokeErr := store.RevokeBefore(runtimeInstance, "u", instant, securitycontract.RevocationOptions{}); nil != revokeErr {
        t.Fatalf("revoke: %v", revokeErr)
    }

    for _, device := range []string{"phone\xff", strings.Repeat("a", 256)} {
        epoch, epochErr := store.RevocationEpoch(runtimeInstance, "u", securitycontract.RevocationOptions{DeviceIdentifier: device})
        if nil != epochErr {
            t.Fatalf("expected the read to answer for a device no write accepts, got %v", epochErr)
        }

        if false == epoch.Equal(instant) {
            t.Fatalf("expected the user's boundary %v, got %v", instant, epoch)
        }
    }
}

func TestInMemoryTokenStore_AMaxIdentifierBytesBelowOnePanicsNamingTheField(t *testing.T) {
    expectPanicNamingField(t, "InMemoryTokenStoreConfig.MaxIdentifierBytes", func() {
        NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxIdentifierBytes: bound.Of(0)})
    })
}

func TestInMemoryTokenStore_RefusesADoneContextAtEveryDoorMarked(t *testing.T) {
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{})
    fillTokenStore(t, store, bound.UnlimitedDuration(), "kept")

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    cancelledRuntime := runtime.NewRequest(cancelledContext, serviceContainer.NewScope())

    for door, call := range map[string]func() error{
        "Lookup": func() error {
            _, _, lookupErr := store.Lookup(cancelledRuntime, "kept")

            return lookupErr
        },
        "Put": func() error {
            return store.Put(cancelledRuntime, "t", securitycontract.Claims{UserIdentifier: "u"}, bound.UnlimitedDuration())
        },
        "Delete": func() error { return store.Delete(cancelledRuntime, "kept") },
        "DeleteByUser": func() error {
            _, deleteErr := store.DeleteByUser(cancelledRuntime, "u-kept")

            return deleteErr
        },
        "PurgeExpired": func() error {
            _, purgeErr := store.PurgeExpired(cancelledRuntime)

            return purgeErr
        },
        "RevokeBefore": func() error { return store.RevokeBefore(cancelledRuntime, "u-kept", time.Now(), securitycontract.RevocationOptions{}) },
        "RevocationEpoch": func() error {
            _, epochErr := store.RevocationEpoch(cancelledRuntime, "u-kept", securitycontract.RevocationOptions{})

            return epochErr
        },
    } {
        doorErr := call()
        if false == exception.AnswersUnavailable(doorErr) || false == exception.ChainIs(doorErr, context.Canceled) {
            t.Fatalf("expected %s to refuse a done context marked, carrying its error, got %v", door, doorErr)
        }
    }

    if claims, found, _ := store.Lookup(newTestRuntime(), "kept"); false == found || "u-kept" != claims.UserIdentifier {
        t.Fatalf("expected the refused calls to change nothing, got %+v (found=%t)", claims, found)
    }

    if epoch, _ := store.RevocationEpoch(newTestRuntime(), "u-kept", securitycontract.RevocationOptions{}); false == epoch.IsZero() {
        t.Fatalf("expected no boundary published by a refused revocation, got %v", epoch)
    }
}

func TestInMemoryTokenStore_TheCeilingSweepsAShortTokenStoredAfterALongOne(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(2), SweepInterval: bound.UnlimitedDuration(), Clock: frozen})
    fillTokenStore(t, store, bound.Of(time.Hour), "long")
    fillTokenStore(t, store, bound.Of(time.Second), "short")

    frozen.Advance(2 * time.Second)

    if putErr := store.Put(newTestRuntime(), "after-lapse", securitycontract.Claims{UserIdentifier: "u-after"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected the lapsed short token swept to make room, got %v", putErr)
    }
}

func TestInMemoryTokenStore_TheCeilingSweepsATokenBornUnderAFutureBoundary(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(1), SweepInterval: bound.UnlimitedDuration(), Clock: frozen})

    if revokeErr := store.RevokeBefore(newTestRuntime(), "u-born", frozen.Now().Add(time.Hour), securitycontract.RevocationOptions{}); nil != revokeErr {
        t.Fatalf("revoke: %v", revokeErr)
    }

    if _, purgeErr := store.PurgeExpired(newTestRuntime()); nil != purgeErr {
        t.Fatalf("purge: %v", purgeErr)
    }

    fillTokenStore(t, store, bound.UnlimitedDuration(), "born")

    if _, found, _ := store.Lookup(newTestRuntime(), "born"); true == found {
        t.Fatalf("expected the token born under the future boundary refused")
    }

    if putErr := store.Put(newTestRuntime(), "other", securitycontract.Claims{UserIdentifier: "u-other"}, bound.UnlimitedDuration()); nil != putErr {
        t.Fatalf("expected the token revoked at birth swept to make room, got %v", putErr)
    }
}

func TestInMemoryTokenStore_ABoundaryOfAUserWithNoStoredTokenForcesNoWalk(t *testing.T) {
    frozen := clock.NewFrozenClock(time.Unix(1000, 0))
    store := NewInMemoryTokenStore(InMemoryTokenStoreConfig{MaxEntries: bound.Of(2), SweepInterval: bound.UnlimitedDuration(), Clock: frozen})
    fillTokenStore(t, store, bound.UnlimitedDuration(), "a", "b")

    for range 2 {
        frozen.Advance(time.Minute)

        if revokeErr := store.RevokeBefore(newTestRuntime(), "nobody", frozen.Now(), securitycontract.RevocationOptions{}); nil != revokeErr {
            t.Fatalf("revoke: %v", revokeErr)
        }

        if putErr := store.Put(newTestRuntime(), "c", securitycontract.Claims{UserIdentifier: "u-c"}, bound.UnlimitedDuration()); nil == putErr {
            t.Fatalf("expected the put at the ceiling refused")
        }

        if false == store.lastSweep.IsZero() {
            t.Fatalf("expected no sweep for a boundary that frees nothing, got one at %v", store.lastSweep)
        }
    }
}
