package session

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* ErrSessionDeleted is the cause SaveSession carries for a session deleted while the request holding it ran; the session ended, the storage did not fail. */
var ErrSessionDeleted = errors.New("session was deleted")

/* ErrSessionUnavailable is in a firewall door's refusal chain when the request's session could not be loaded, answered 503 with reason session_unavailable. */
var ErrSessionUnavailable = errors.New("session unavailable")

/* ErrSessionRotated is the cause SaveSession carries, beside ErrSessionDeleted, for an id a rotation retired; the browser cookie is kept. */
var ErrSessionRotated = errors.New("session was rotated away")

/* a fixed number, since a map of locks would need a lock of its own to grow and prune */
const sessionStripeCount = 256

/* Manager holds the tombstones of deleted and rotated-away ids beside the storage it writes through. */
type Manager struct {
    storage sessioncontract.Storage
    clock              clockcontract.Clock
    ttl                bound.Duration
    ownsStorage        bool
    tombstoneRetention time.Duration
    persistenceTimeout bound.Resolved[time.Duration]
    /* one slot per stripe rather than a mutex, so the wait can be cut by the caller's context */
    sessionStripes [sessionStripeCount]chan struct{}
    /* held for a map read or write only, never across the storage */
    tombstoneMutex sync.Mutex
    deletedAtById  map[string]time.Time
    rotatedAwayIds map[string]bool
    buriedInOrder []tombstone
}

type tombstone struct {
    sessionId string
    deletedAt time.Time
}

/* ManagerConfig is what a session manager declares beyond its storage; the zero value is the default. */
type ManagerConfig struct {
    /* Ttl is the lifetime of a stored session since its last write: 24h by default, at least 1s, unlimited for sessions that never lapse. */
    Ttl bound.Duration

    /* TombstoneRetention is how long a deleted id keeps refusing a write-back from a request that loaded it before the delete: 5m by default, at least 1s, unlimited refused. */
    TombstoneRetention bound.Duration

    /* PersistenceTimeout bounds the end-of-request save or delete and the read after a failed removal: 5s by default, at least 1ms, unlimited for no bound; the bound is cooperative. */
    PersistenceTimeout bound.Duration

    /* Clock is where every instant of the tombstone record is read; nil is the system clock. */
    Clock clockcontract.Clock

    /* OwnsStorage closes the storage with the manager; off, CloseWithContext leaves it open for whoever built it. */
    OwnsStorage bool
}

var managerTtlRule = bound.Rule[time.Duration]{
    Name:              "ManagerConfig.Ttl",
    Default:           24 * time.Hour,
    Minimum:           bound.TtlMinimum,
    UnlimitedAdmitted: true,
}

var managerTombstoneRetentionRule = bound.Rule[time.Duration]{
    Name:    "ManagerConfig.TombstoneRetention",
    Default: 5 * time.Minute,
    Minimum: bound.TtlMinimum,
}

/* DefaultPersistenceTimeout bounds the end-of-request persistence when ManagerConfig.PersistenceTimeout is left out or a manager answers an absent bound. */
const DefaultPersistenceTimeout = 5 * time.Second

/* PersistenceTimeoutRule resolves ManagerConfig.PersistenceTimeout and judges every manager's PersistenceTimeout. */
var PersistenceTimeoutRule = bound.Rule[time.Duration]{
    Name:              "ManagerConfig.PersistenceTimeout",
    Default:           DefaultPersistenceTimeout,
    Minimum:           bound.TimeoutMinimum,
    UnlimitedAdmitted: true,
}

/* NewManager panics on a nil storage and on a field below its minimum, by name. */
func NewManager(storage sessioncontract.Storage, config ManagerConfig) *Manager {
    if true == internal.IsNilInterface(storage) {
        exception.Panic(exception.NewError("session storage is nil", nil, nil))
    }

    ttl, ttlErr := bound.Resolve(config.Ttl, managerTtlRule)
    if nil != ttlErr {
        exception.Panic(exception.FromError(ttlErr))
    }

    tombstoneRetention, tombstoneRetentionErr := bound.Resolve(config.TombstoneRetention, managerTombstoneRetentionRule)
    if nil != tombstoneRetentionErr {
        exception.Panic(exception.FromError(tombstoneRetentionErr))
    }

    persistenceTimeout, persistenceTimeoutErr := bound.Resolve(config.PersistenceTimeout, PersistenceTimeoutRule)
    if nil != persistenceTimeoutErr {
        exception.Panic(exception.FromError(persistenceTimeoutErr))
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    manager := &Manager{
        storage:            storage,
        clock:              clockInstance,
        ttl:                ttl.Limit(),
        ownsStorage:        config.OwnsStorage,
        tombstoneRetention: tombstoneRetention.Value,
        persistenceTimeout: persistenceTimeout,
        deletedAtById:      make(map[string]time.Time),
        rotatedAwayIds:     make(map[string]bool),
    }

    for index := range manager.sessionStripes {
        manager.sessionStripes[index] = make(chan struct{}, 1)
    }

    return manager
}

/* PersistenceTimeout answers the bound resolved at construction. */
func (instance *Manager) PersistenceTimeout() bound.Duration {
    return instance.persistenceTimeout.Limit()
}

/* ResolvePersistenceTimeout judges a declared bound by PersistenceTimeoutRule: absent is the default, a refused bound an error naming the field. */
func ResolvePersistenceTimeout(manager sessioncontract.Manager) (bound.Resolved[time.Duration], error) {
    if true == internal.IsNilInterface(manager) {
        return bound.Resolved[time.Duration]{}, exception.NewError("session manager is nil", nil, nil)
    }

    return bound.Resolve(manager.PersistenceTimeout(), PersistenceTimeoutRule)
}

/* Session answers the stored session, nil when the id names none; a storage outage carries exception.MarkInfrastructureFailure. */
func (instance *Manager) Session(ctx context.Context, sessionId string) (sessioncontract.Session, error) {
    if nilErr := sessioncontract.RefuseNilContext(ctx); nil != nilErr {
        return nil, nilErr
    }

    if false == isValidSessionId(sessionId) {
        return nil, nil
    }

    if nil != ctx.Err() {
        return nil, contextEndedError(ctx, "Session", sessionId)
    }

    data, exists, err := instance.storage.Load(ctx, sessionId)
    if nil != err {
        return nil, exception.NewError(
            "could not load the session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            exception.MarkInfrastructureFailure(err),
        )
    }

    if false == exists {
        return nil, nil
    }

    if nil == data {
        data = make(map[string]any)
    }

    return &Session{
        id:       sessionId,
        values:   data,
        modified: false,
        cleared:  false,
    }, nil
}

/* NewSession answers a session under an id the storage does not hold; a probe outage is marked as an infrastructure failure, and 128 held ids in a row is an unmarked error. */
func (instance *Manager) NewSession(ctx context.Context) (sessioncontract.Session, error) {
    if nilErr := sessioncontract.RefuseNilContext(ctx); nil != nilErr {
        return nil, nilErr
    }

    sessionId, mintErr := instance.uniqueSessionId(ctx, "NewSession")
    if nil != mintErr {
        return nil, mintErr
    }

    return &Session{
        id:       sessionId,
        values:   make(map[string]any),
        modified: false,
        cleared:  false,
        minted:   true,
    }, nil
}

/* RegenerateSession answers the values under a fresh id, marked modified, removes the previous entry and latches the session passed in cleared; a buried id is refused before anything is stored. */
func (instance *Manager) RegenerateSession(ctx context.Context, sessionInstance sessioncontract.Session) (sessioncontract.Session, error) {
    if nilErr := sessioncontract.RefuseNilContext(ctx); nil != nilErr {
        return nil, nilErr
    }

    if true == internal.IsNilInterface(sessionInstance) {
        return nil, exception.NewError("session is nil in regenerate session", nil, nil)
    }

    /* refused before the mint probe, so no id is minted for an identity the storage cannot hold */
    if refusalErr := refuseUnavailableSession(sessionInstance, "RegenerateSession"); nil != refusalErr {
        return nil, refusalErr
    }

    previousId := sessionInstance.Id()
    if false == isValidSessionId(previousId) {
        return nil, exception.NewError(
            "session id is invalid in regenerate session",
            map[string]any{
                "sessionRef": sessionIdLogReference(previousId),
            },
            nil,
        )
    }

    values := sessionInstance.All()

    /* minted before the previous entry is removed, so a probe outage leaves the session in use intact */
    rotatedId, mintErr := instance.uniqueSessionId(ctx, "RegenerateSession")
    if nil != mintErr {
        return nil, exception.NewError(
            "session id could not be minted in regenerate session",
            map[string]any{
                "sessionRef": sessionIdLogReference(previousId),
            },
            mintErr,
        )
    }

    /* a session this manager minted and never stored has no entry to remove and no copy to bury */
    concreteSession, isConcrete := sessionInstance.(*Session)
    if false == isConcrete || false == concreteSession.isMinted() {
        /* checked, removed and buried in one critical section, so a request that loaded it can neither write it back nor rotate it again */
        deleteErr := instance.deleteSessionRecordingCause(ctx, previousId, true)
        if nil != deleteErr {
            return nil, deleteErr
        }
    }

    /* latched cleared only once the entry is gone, so a failed delete leaves a usable session */
    sessionInstance.Clear()

    return &Session{
        id:       rotatedId,
        values:   values,
        modified: true,
        cleared:  false,
        minted:   true,
    }, nil
}

/* SaveSession stores the session under its id, holding the id's stripe across the storage's Save; a context that ends during the wait answers an infrastructure failure. */
func (instance *Manager) SaveSession(ctx context.Context, sessionInstance sessioncontract.Session) error {
    if nilErr := sessioncontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    /* a typed-nil session would panic in the snapshot below, inside the response path's recovery defer */
    if true == internal.IsNilInterface(sessionInstance) {
        return exception.NewError("session is nil in save session", nil, nil)
    }

    if refusalErr := refuseUnavailableSession(sessionInstance, "SaveSession"); nil != refusalErr {
        return refusalErr
    }

    /* one snapshot pairs the branch decision with the values it acts on, so a concurrent Clear cannot land between the reads */
    values, sessionModified, sessionCleared := Snapshot(sessionInstance)

    if true == sessionCleared {
        /* a session this manager minted and never stored has no entry to remove and no copy to bury */
        if concreteSession, isConcrete := sessionInstance.(*Session); true == isConcrete && true == concreteSession.isMinted() {
            return nil
        }

        return instance.DeleteSession(ctx, sessionInstance.Id())
    }

    if false == sessionModified {
        return nil
    }

    sessionId := sessionInstance.Id()
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in save session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            nil,
        )
    }

    /* Storage.Save is a blind upsert, so the tombstone check and the write are one critical section */
    release, stripeErr := instance.acquireSessionStripe(ctx, "SaveSession", sessionId)
    if nil != stripeErr {
        return stripeErr
    }
    defer release()

    if refusalErr := instance.buriedIdRefusal(sessionId, "saved again"); nil != refusalErr {
        return refusalErr
    }

    saveErr := instance.storage.Save(ctx, sessionId, values, instance.ttl)

    /* once a save was attempted the id may be stored, so a later rotation removes and buries it */
    if concreteSession, isConcrete := sessionInstance.(*Session); true == isConcrete {
        concreteSession.markStored()
    }

    if nil != saveErr {
        return exception.NewError(
            "could not save the session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            exception.MarkInfrastructureFailure(saveErr),
        )
    }

    return nil
}

/* DeleteSession removes the session and buries its id under the id's stripe; after a failed removal one bounded read, past the caller's cancellation, decides the burial. */
func (instance *Manager) DeleteSession(ctx context.Context, sessionId string) error {
    if nilErr := sessioncontract.RefuseNilContext(ctx); nil != nilErr {
        return nilErr
    }

    return instance.deleteSessionRecordingCause(ctx, sessionId, false)
}

func (instance *Manager) deleteSessionRecordingCause(ctx context.Context, sessionId string, rotated bool) error {
    if false == isValidSessionId(sessionId) {
        return exception.NewError(
            "session id is invalid in delete session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            nil,
        )
    }

    door := "DeleteSession"
    if true == rotated {
        door = "RegenerateSession"
    }

    release, stripeErr := instance.acquireSessionStripe(ctx, door, sessionId)
    if nil != stripeErr {
        return stripeErr
    }
    defer release()

    /* a copy loaded before a logout or a rotation cannot mint a fresh id carrying the identity the burial ended */
    if true == rotated {
        if refusalErr := instance.buriedIdRefusal(sessionId, "rotated"); nil != refusalErr {
            return refusalErr
        }
    }

    deleteErr := instance.storage.Delete(ctx, sessionId)

    /* a burial over a surviving entry would read as a logout on the next save, so a failed removal is buried only when a fresh read says the entry went */
    removed := nil == deleteErr
    if nil != deleteErr {
        /* outlives the context that may have cut the removal, and is bounded, since the stripe is held across it */
        removed = instance.entryWentAfterFailedRemoval(ctx, sessionId)
    }

    /* every id gone after the removal is buried, a lapsed or evicted one too, so a held copy cannot write the ended session back */
    if true == removed {
        if true == rotated {
            instance.buryRotationTombstone(sessionId)
        } else {
            instance.buryTombstone(sessionId)
        }
    }

    if nil != deleteErr {
        return exception.NewError(
            "could not delete the session",
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            exception.MarkInfrastructureFailure(deleteErr),
        )
    }

    return nil
}

/* the session lock is held */
func (instance *Manager) buriedIdRefusal(sessionId string, attempt string) error {
    tombstoned, rotatedAway := instance.tombstoneStateOf(sessionId)
    if false == tombstoned {
        return nil
    }

    if true == rotatedAway {
        return exception.NewError(
            "session was rotated away and cannot be "+attempt,
            map[string]any{
                "sessionRef": sessionIdLogReference(sessionId),
            },
            errors.Join(ErrSessionDeleted, ErrSessionRotated),
        )
    }

    return exception.NewError(
        "session was deleted and cannot be "+attempt,
        map[string]any{
            "sessionRef": sessionIdLogReference(sessionId),
        },
        ErrSessionDeleted,
    )
}

func sessionIdLogReference(sessionId string) string {
    digest := sha256.Sum256([]byte(sessionId))

    return hex.EncodeToString(digest[:])[:16]
}

func contextEndedError(ctx context.Context, door string, sessionId string) error {
    errorContext := map[string]any{
        "door": door,
    }

    if "" != sessionId {
        errorContext["sessionRef"] = sessionIdLogReference(sessionId)
    }

    return exception.NewError(
        "the session operation was cut by its context",
        errorContext,
        exception.MarkInfrastructureFailure(ctx.Err()),
    )
}

func (instance *Manager) acquireSessionStripe(ctx context.Context, door string, sessionId string) (func(), error) {
    /* asked first, since a select with both cases ready picks either */
    if nil != ctx.Err() {
        return nil, contextEndedError(ctx, door, sessionId)
    }

    stripe := instance.sessionStripeOf(sessionId)

    select {
    case stripe <- struct{}{}:
        /* the slot and the ended context can be ready together, and the select may have picked the slot */
        if nil != ctx.Err() {
            endedErr := contextEndedError(ctx, door, sessionId)
            <-stripe

            return nil, endedErr
        }

        return func() { <-stripe }, nil
    case <-ctx.Done():
        return nil, contextEndedError(ctx, door, sessionId)
    }
}

func (instance *Manager) entryWentAfterFailedRemoval(ctx context.Context, sessionId string) bool {
    burialContext, releaseBurialContext := instance.burialContext(ctx)
    defer releaseBurialContext()

    _, stillHeld, readErr := instance.storage.Load(burialContext, sessionId)

    return nil == readErr && false == stillHeld
}

func (instance *Manager) burialContext(ctx context.Context) (context.Context, context.CancelFunc) {
    detached := context.WithoutCancel(ctx)

    if true == instance.persistenceTimeout.Unlimited {
        return detached, func() {}
    }

    return context.WithTimeout(detached, instance.persistenceTimeout.Value)
}

func (instance *Manager) sessionStripeOf(sessionId string) chan struct{} {
    hash := uint32(2166136261)

    for index := 0; index < len(sessionId); index++ {
        hash = hash ^ uint32(sessionId[index])
        hash = hash * 16777619
    }

    return instance.sessionStripes[hash%sessionStripeCount]
}

/* CloseWithContext closes the storage the manager owns under the teardown's deadline. */
func (instance *Manager) CloseWithContext(closeContext context.Context) error {
    if false == instance.ownsStorage {
        return nil
    }

    return instance.storage.CloseWithContext(closeContext)
}

func (instance *Manager) buryTombstone(sessionId string) {
    instance.buryTombstoneAt(sessionId, instance.clock.Now())
}

func (instance *Manager) buryRotationTombstone(sessionId string) {
    instance.buryRotationTombstoneAt(sessionId, instance.clock.Now())
}

/* every burial is held for the same window, so the lapsed ones are a prefix of the queue */
func (instance *Manager) buryTombstoneAt(sessionId string, deletedAt time.Time) {
    instance.buryTombstoneRecordingCause(sessionId, deletedAt, false)
}

func (instance *Manager) buryRotationTombstoneAt(sessionId string, deletedAt time.Time) {
    instance.buryTombstoneRecordingCause(sessionId, deletedAt, true)
}

func (instance *Manager) buryTombstoneRecordingCause(sessionId string, deletedAt time.Time, rotated bool) {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    instance.pruneLapsedTombstonesLocked(deletedAt)

    instance.deletedAtById[sessionId] = deletedAt

    /* an id buried again inside its window takes the cause of the current burial */
    if true == rotated {
        instance.rotatedAwayIds[sessionId] = true
    } else {
        delete(instance.rotatedAwayIds, sessionId)
    }

    instance.buriedInOrder = append(
        instance.buriedInOrder,
        tombstone{sessionId: sessionId, deletedAt: deletedAt},
    )
}

func (instance *Manager) pruneLapsedTombstonesLocked(now time.Time) {
    lapsedUntil := 0

    for _, buried := range instance.buriedInOrder {
        if instance.tombstoneRetention > now.Sub(buried.deletedAt) {
            break
        }

        /* an id buried twice has two queue entries and one record, so the record is removed only for the burial that wrote it */
        deletedAt, exists := instance.deletedAtById[buried.sessionId]
        if true == exists && true == deletedAt.Equal(buried.deletedAt) {
            delete(instance.deletedAtById, buried.sessionId)
            delete(instance.rotatedAwayIds, buried.sessionId)
        }

        lapsedUntil = lapsedUntil + 1
    }

    if 0 < lapsedUntil {
        instance.buriedInOrder = instance.buriedInOrder[lapsedUntil:]
    }
}

func (instance *Manager) tombstoneStateOf(sessionId string) (bool, bool) {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    if false == instance.isTombstonedLocked(sessionId) {
        return false, false
    }

    return true, instance.wasRotatedAwayLocked(sessionId)
}

/* meaningful only for an id isTombstonedLocked reported; the record lock is held */
func (instance *Manager) wasRotatedAwayLocked(sessionId string) bool {
    return instance.rotatedAwayIds[sessionId]
}

func (instance *Manager) isTombstonedLocked(sessionId string) bool {
    deletedAt, exists := instance.deletedAtById[sessionId]
    if false == exists {
        return false
    }

    return instance.tombstoneRetention > instance.clock.Now().Sub(deletedAt)
}

func (instance *Manager) uniqueSessionId(ctx context.Context, door string) (string, error) {
    maxAttempts := 128

    for attempt := 0; attempt < maxAttempts; attempt++ {
        if nil != ctx.Err() {
            return "", contextEndedError(ctx, door, "")
        }

        candidateId := generateSessionId()

        _, exists, err := instance.storage.Load(ctx, candidateId)
        if nil != err {
            return "", exception.NewError("could not probe a session id", nil, exception.MarkInfrastructureFailure(err))
        }

        if true == exists {
            continue
        }

        return candidateId, nil
    }

    return "", exception.NewError(
        "could not generate unique session id",
        map[string]any{
            "attempts": maxAttempts,
        },
        nil,
    )
}

var _ sessioncontract.Manager = (*Manager)(nil)
