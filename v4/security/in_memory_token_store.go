package security

import (
    "errors"
    "math"
    "sync"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
    securitycontract "github.com/precision-soft/melody/v4/security/contract"
)

/* InMemoryTokenStoreConfig is what the in-memory store declares at construction; the zero value is the default. */
type InMemoryTokenStoreConfig struct {
    /* MaxEntries bounds the tokens stored at once: at the ceiling, after the lapsed and the revoked ones are swept, Put refuses a token it does not already hold with ErrTokenStoreFull, marked as an infrastructure failure, so a flood never evicts a live token. 100,000 by default, at least 1, unlimited for a store bounded by its tokens' lifetimes alone. RevokeBefore is never refused at the ceiling: the boundaries are bounded by the users, and a refused revocation would be a bypass. */
    MaxEntries bound.Count

    /* RevocationEpochRetention is how long PurgeExpired keeps a revocation boundary after its instant, so it must cover the longest lifetime of a token issued before it: 7 days by default, at least 1s, unlimited for boundaries kept until the process exits, the safe reading when token lifetimes are not bounded, since a boundary dropped early is a revocation bypass. */
    RevocationEpochRetention bound.Duration

    /* SweepInterval is how often a write (Put, Delete, DeleteByUser, RevokeBefore) first drops the lapsed and the revoked stored tokens, so a store nothing purges by hand does not grow without end: 1m by default, at least 1s, since each sweep walks every stored token under the store's lock, unlimited leaving it to PurgeExpired. The sweep drops no revocation boundary: only PurgeExpired applies RevocationEpochRetention. */
    SweepInterval bound.Duration

    /* Clock is where the issue stamps and the expiries are read; nil is the system clock. */
    Clock clockcontract.Clock
}

/* ErrTokenStoreFull is in the chain of a Put the in-memory store refused at InMemoryTokenStoreConfig.MaxEntries. */
var ErrTokenStoreFull = errors.New("the token store is full")

var inMemoryTokenStoreMaxEntriesRule = bound.Rule[int]{
    Name:              "InMemoryTokenStoreConfig.MaxEntries",
    Default:           100_000,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

var inMemoryTokenStoreRevocationEpochRetentionRule = bound.Rule[time.Duration]{
    Name:              "InMemoryTokenStoreConfig.RevocationEpochRetention",
    Default:           7 * 24 * time.Hour,
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

var inMemoryTokenStoreSweepIntervalRule = bound.Rule[time.Duration]{
    Name:              "InMemoryTokenStoreConfig.SweepInterval",
    Default:           time.Minute,
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

/* NewInMemoryTokenStore resolves the configuration, panicking on a field below its minimum by name, and builds the store. */
func NewInMemoryTokenStore(config InMemoryTokenStoreConfig) *InMemoryTokenStore {
    maxEntries, maxEntriesErr := bound.Resolve(config.MaxEntries, inMemoryTokenStoreMaxEntriesRule)
    if nil != maxEntriesErr {
        exception.Panic(exception.FromError(maxEntriesErr))
    }

    retention, retentionErr := bound.Resolve(config.RevocationEpochRetention, inMemoryTokenStoreRevocationEpochRetentionRule)
    if nil != retentionErr {
        exception.Panic(exception.FromError(retentionErr))
    }

    sweepInterval, sweepIntervalErr := bound.Resolve(config.SweepInterval, inMemoryTokenStoreSweepIntervalRule)
    if nil != sweepIntervalErr {
        exception.Panic(exception.FromError(sweepIntervalErr))
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    /* 0 is the unlimited retention and the unlimited ceiling */
    return &InMemoryTokenStore{
        clock:                    clockInstance,
        maxEntries:               maxEntries.Value,
        entriesByToken:           make(map[string]tokenEntry),
        tokensByUser:             make(map[string]map[string]struct{}),
        epochsByUser:             make(map[string]map[string]time.Time),
        revocationEpochRetention: retention.Value,
        sweepInterval:            sweepInterval.Value,
        sweepDisabled:            sweepInterval.Unlimited,
    }
}

type InMemoryTokenStore struct {
    clock                    clockcontract.Clock
    maxEntries               int
    mutex                    sync.RWMutex
    entriesByToken           map[string]tokenEntry
    tokensByUser             map[string]map[string]struct{}
    epochsByUser             map[string]map[string]time.Time
    revocationEpochRetention time.Duration
    sweepInterval            time.Duration
    sweepDisabled            bool
    lastSweep                time.Time

    /* so a store at its ceiling does not walk every entry on each refused Put */
    earliestExpiry              time.Time
    boundaryPublishedSinceSweep bool
}

/* the instants a revocation boundary admits are those the redis store can encode in Unix nanoseconds */
var (
    revocationInstantLowerBound = time.Unix(0, 0)
    revocationInstantUpperBound = time.Unix(0, math.MaxInt64)
)

func refuseNonUtf8TokenStoreUser(userIdentifier string) error {
    if false == utf8.ValidString(userIdentifier) {
        return exception.NewError(
            "token store user identifier must be valid UTF-8",
            map[string]any{"userLength": len(userIdentifier)},
            nil,
        )
    }

    return nil
}

func refuseDoneTokenStoreContext(runtimeInstance runtimecontract.Runtime, door string) error {
    contextErr := runtimeInstance.Context().Err()
    if nil == contextErr {
        return nil
    }

    return exception.NewError(
        "token store call refused, the caller's context is done",
        exceptioncontract.Context{"door": door},
        exception.MarkInfrastructureFailure(contextErr),
    )
}

const (
    revocationUserField = "user"

    revocationDeviceFieldPrefix = "device:"
)

func revocationField(deviceIdentifier string) string {
    if "" == deviceIdentifier {
        return revocationUserField
    }

    return revocationDeviceFieldPrefix + deviceIdentifier
}

type tokenEntry struct {
    claims    securitycontract.Claims
    expiresAt time.Time
}

/* Put stores the claims under the ttl, refusing an absent one and one under a second by the ttl's name; unlimited stores with no expiry. The user identifier must be valid UTF-8, as the redis store asks. */
func (instance *InMemoryTokenStore) Put(
    runtimeInstance runtimecontract.Runtime,
    tokenString string,
    claims securitycontract.Claims,
    ttl bound.Duration,
) error {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return runtimeErr
    }

    expiry, err := securitycontract.ResolveTokenTtl(ttl)
    if nil != err {
        return err
    }

    if utf8Err := refuseNonUtf8TokenStoreUser(claims.UserIdentifier); nil != utf8Err {
        return utf8Err
    }

    if doneErr := refuseDoneTokenStoreContext(runtimeInstance, "Put"); nil != doneErr {
        return doneErr
    }

    return instance.put(tokenString, claims, expiry)
}

func (instance *InMemoryTokenStore) Delete(runtimeInstance runtimecontract.Runtime, tokenString string) error {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return runtimeErr
    }

    if doneErr := refuseDoneTokenStoreContext(runtimeInstance, "Delete"); nil != doneErr {
        return doneErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.sweepWhenDueLocked(instance.clock.Now())

    if entry, found := instance.entriesByToken[tokenString]; true == found {
        instance.unindexLocked(entry.claims.UserIdentifier, tokenString)
        delete(instance.entriesByToken, tokenString)
    }

    return nil
}

func (instance *InMemoryTokenStore) DeleteByUser(runtimeInstance runtimecontract.Runtime, userIdentifier string) (int, error) {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return 0, runtimeErr
    }

    if utf8Err := refuseNonUtf8TokenStoreUser(userIdentifier); nil != utf8Err {
        return 0, utf8Err
    }

    if doneErr := refuseDoneTokenStoreContext(runtimeInstance, "DeleteByUser"); nil != doneErr {
        return 0, doneErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.sweepWhenDueLocked(instance.clock.Now())

    tokens, exists := instance.tokensByUser[userIdentifier]
    if false == exists {
        return 0, nil
    }

    removed := 0
    for tokenString := range tokens {
        delete(instance.entriesByToken, tokenString)
        removed++
    }

    delete(instance.tokensByUser, userIdentifier)

    return removed, nil
}

func (instance *InMemoryTokenStore) PurgeExpired(runtimeInstance runtimecontract.Runtime) (int, error) {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return 0, runtimeErr
    }

    if doneErr := refuseDoneTokenStoreContext(runtimeInstance, "PurgeExpired"); nil != doneErr {
        return 0, doneErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    now := instance.clock.Now()
    purged := instance.purgeStoredTokensLocked(now)
    instance.purgeBoundariesLocked(now)

    return purged, nil
}

func (instance *InMemoryTokenStore) sweepWhenDueLocked(now time.Time) {
    if true == instance.sweepDisabled {
        return
    }

    /* building the store reads no clock, so its first write sweeps over nothing and starts the interval */
    if elapsed := now.Sub(instance.lastSweep); 0 <= elapsed && elapsed < instance.sweepInterval {
        return
    }

    instance.purgeStoredTokensLocked(now)
}

func (instance *InMemoryTokenStore) purgeStoredTokensLocked(now time.Time) int {
    instance.lastSweep = now
    instance.boundaryPublishedSinceSweep = false

    purged := 0
    for tokenString, entry := range instance.entriesByToken {
        if false == entry.expiresAt.IsZero() && true == now.After(entry.expiresAt) {
            instance.unindexLocked(entry.claims.UserIdentifier, tokenString)
            delete(instance.entriesByToken, tokenString)
            purged++
        }
    }

    /* a boundary dropped below must not hand a revoked token back */
    for tokenString, entry := range instance.entriesByToken {
        if true == instance.isRevokedLocked(entry.claims) {
            instance.unindexLocked(entry.claims.UserIdentifier, tokenString)
            delete(instance.entriesByToken, tokenString)
            purged++
        }
    }

    earliestExpiry := time.Time{}
    for _, entry := range instance.entriesByToken {
        if false == entry.expiresAt.IsZero() && (true == earliestExpiry.IsZero() || true == entry.expiresAt.Before(earliestExpiry)) {
            earliestExpiry = entry.expiresAt
        }
    }
    instance.earliestExpiry = earliestExpiry

    return purged
}

func (instance *InMemoryTokenStore) sweepCanFreeLocked(now time.Time) bool {
    if true == instance.boundaryPublishedSinceSweep {
        return true
    }

    return false == instance.earliestExpiry.IsZero() && true == now.After(instance.earliestExpiry)
}

func (instance *InMemoryTokenStore) purgeBoundariesLocked(now time.Time) {
    /* a stateless JWT is checked against a boundary without being stored */
    if 0 < instance.revocationEpochRetention {
        horizon := now.Add(-instance.revocationEpochRetention)
        for userIdentifier, boundaries := range instance.epochsByUser {
            for field, instant := range boundaries {
                if true == instant.Before(horizon) {
                    delete(boundaries, field)
                }
            }

            if 0 == len(boundaries) {
                delete(instance.epochsByUser, userIdentifier)
            }
        }
    }
}

func cloneClaims(claims securitycontract.Claims) securitycontract.Claims {
    cloned := claims

    if nil != claims.Roles {
        cloned.Roles = append([]string{}, claims.Roles...)
    }

    if nil != claims.Scope {
        cloned.Scope = internal.CopyAnyMap(claims.Scope)
    }

    if nil != claims.Attributes {
        cloned.Attributes = internal.CopyAnyMap(claims.Attributes)
    }

    cloned.OriginatingActor = cloneActorData(claims.OriginatingActor)

    return cloned
}

/* a cyclic ActorData would recurse into a stack overflow no recover catches */
const maxActorImpersonationDepth = 10000

func cloneActorData(actor *securitycontract.ActorData) *securitycontract.ActorData {
    return cloneActorDataAtDepth(actor, 0)
}

func cloneActorDataAtDepth(actor *securitycontract.ActorData, depth int) *securitycontract.ActorData {
    if nil == actor {
        return nil
    }

    actorCopy := *actor
    if nil != actor.Roles {
        actorCopy.Roles = append([]string{}, actor.Roles...)
    }
    if nil != actor.Attributes {
        actorCopy.Attributes = internal.CopyStringMap(actor.Attributes)
    }

    if depth >= maxActorImpersonationDepth {
        actorCopy.Impersonator = nil

        return &actorCopy
    }

    actorCopy.Impersonator = cloneActorDataAtDepth(actor.Impersonator, depth+1)

    return &actorCopy
}

func (instance *InMemoryTokenStore) Lookup(
    runtimeInstance runtimecontract.Runtime,
    tokenString string,
) (securitycontract.Claims, bool, error) {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return securitycontract.Claims{}, false, runtimeErr
    }

    if doneErr := refuseDoneTokenStoreContext(runtimeInstance, "Lookup"); nil != doneErr {
        return securitycontract.Claims{}, false, doneErr
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    entry, found := instance.entriesByToken[tokenString]
    if false == found {
        return securitycontract.Claims{}, false, nil
    }

    if false == entry.expiresAt.IsZero() && true == instance.clock.Now().After(entry.expiresAt) {
        return securitycontract.Claims{}, false, nil
    }

    if true == instance.isRevokedLocked(entry.claims) {
        return securitycontract.Claims{}, false, nil
    }

    return cloneClaims(entry.claims), true, nil
}

func (instance *InMemoryTokenStore) RevokeBefore(
    runtimeInstance runtimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
    instant time.Time,
) error {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return runtimeErr
    }

    if "" == userIdentifier {
        return exception.NewError("token store revocation needs a user identifier", nil, nil)
    }

    if utf8Err := refuseNonUtf8TokenStoreUser(userIdentifier); nil != utf8Err {
        return utf8Err
    }

    if true == instant.IsZero() {
        return exception.NewError(
            "token store revocation instant is zero",
            map[string]any{"user": userIdentifier},
            nil,
        )
    }

    if true == instant.Before(revocationInstantLowerBound) || true == instant.After(revocationInstantUpperBound) {
        return exception.NewError(
            "token store revocation instant is not representable",
            map[string]any{"user": userIdentifier, "instant": instant.String()},
            nil,
        )
    }

    if doneErr := refuseDoneTokenStoreContext(runtimeInstance, "RevokeBefore"); nil != doneErr {
        return doneErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.sweepWhenDueLocked(instance.clock.Now())

    boundaries, exists := instance.epochsByUser[userIdentifier]
    if false == exists {
        boundaries = make(map[string]time.Time)
        instance.epochsByUser[userIdentifier] = boundaries
    }

    field := revocationField(deviceIdentifier)
    if published, found := boundaries[field]; true == found && false == instant.After(published) {
        return nil
    }

    boundaries[field] = instant
    if 0 < len(instance.tokensByUser[userIdentifier]) {
        instance.boundaryPublishedSinceSweep = true
    }

    return nil
}

func (instance *InMemoryTokenStore) RevocationEpoch(
    runtimeInstance runtimecontract.Runtime,
    userIdentifier string,
    deviceIdentifier string,
) (time.Time, error) {
    if runtimeErr := runtimecontract.RefuseNilRuntime(runtimeInstance); nil != runtimeErr {
        return time.Time{}, runtimeErr
    }

    if doneErr := refuseDoneTokenStoreContext(runtimeInstance, "RevocationEpoch"); nil != doneErr {
        return time.Time{}, doneErr
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.revocationEpochLocked(userIdentifier, deviceIdentifier), nil
}

func (instance *InMemoryTokenStore) revocationEpochLocked(userIdentifier string, deviceIdentifier string) time.Time {
    boundaries, exists := instance.epochsByUser[userIdentifier]
    if false == exists {
        return time.Time{}
    }

    effective := boundaries[revocationUserField]

    if "" != deviceIdentifier {
        if device, found := boundaries[revocationDeviceFieldPrefix+deviceIdentifier]; true == found && true == device.After(effective) {
            effective = device
        }
    }

    return effective
}

func (instance *InMemoryTokenStore) isRevokedLocked(claims securitycontract.Claims) bool {
    epoch := instance.revocationEpochLocked(claims.UserIdentifier, claims.DeviceIdentifier)
    if true == epoch.IsZero() {
        return false
    }

    return false == claims.IssuedAt.After(epoch)
}

func (instance *InMemoryTokenStore) put(tokenString string, claims securitycontract.Claims, ttl time.Duration) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    /* stamped under the insert's lock, so a RevokeBefore cannot land between the stamp and the insert */
    now := instance.clock.Now()

    instance.sweepWhenDueLocked(now)

    claims.IssuedAt = now

    expiresAt := time.Time{}
    if 0 < ttl {
        expiresAt = now.Add(ttl)
    }

    existing, found := instance.entriesByToken[tokenString]
    if false == found {
        if fullErr := instance.refuseAtTheCeilingLocked(now); nil != fullErr {
            return fullErr
        }
    }

    if true == found && existing.claims.UserIdentifier != claims.UserIdentifier {
        instance.unindexLocked(existing.claims.UserIdentifier, tokenString)
    }

    instance.entriesByToken[tokenString] = tokenEntry{claims: cloneClaims(claims), expiresAt: expiresAt}
    instance.indexLocked(claims.UserIdentifier, tokenString)

    /* a token born under a future boundary is already refused, so the ceiling's sweep can free it */
    if true == instance.isRevokedLocked(claims) {
        instance.boundaryPublishedSinceSweep = true
    }

    if false == expiresAt.IsZero() && (true == instance.earliestExpiry.IsZero() || true == expiresAt.Before(instance.earliestExpiry)) {
        instance.earliestExpiry = expiresAt
    }

    return nil
}

func (instance *InMemoryTokenStore) refuseAtTheCeilingLocked(now time.Time) error {
    if 0 == instance.maxEntries || len(instance.entriesByToken) < instance.maxEntries {
        return nil
    }

    if true == instance.sweepCanFreeLocked(now) {
        instance.purgeStoredTokensLocked(now)
    }

    if len(instance.entriesByToken) < instance.maxEntries {
        return nil
    }

    return exception.NewError(
        "the token store is full",
        exceptioncontract.Context{
            "field":      inMemoryTokenStoreMaxEntriesRule.Name,
            "maxEntries": instance.maxEntries,
        },
        exception.MarkInfrastructureFailure(ErrTokenStoreFull),
    )
}

func (instance *InMemoryTokenStore) indexLocked(userIdentifier string, tokenString string) {
    tokens, exists := instance.tokensByUser[userIdentifier]
    if false == exists {
        tokens = make(map[string]struct{})
        instance.tokensByUser[userIdentifier] = tokens
    }

    tokens[tokenString] = struct{}{}
}

func (instance *InMemoryTokenStore) unindexLocked(userIdentifier string, tokenString string) {
    tokens, exists := instance.tokensByUser[userIdentifier]
    if false == exists {
        return
    }

    delete(tokens, tokenString)
    if 0 == len(tokens) {
        delete(instance.tokensByUser, userIdentifier)
    }
}

var _ securitycontract.RevocableTokenStore = (*InMemoryTokenStore)(nil)
var _ securitycontract.EpochRevocableTokenStore = (*InMemoryTokenStore)(nil)
