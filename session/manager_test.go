package session

import (
    "errors"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal/testhelper"
)

type nilMapStorage struct{}

func (instance *nilMapStorage) Load(sessionId string) (map[string]any, bool, error) {
    return nil, true, nil
}

func (instance *nilMapStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    return nil
}

func (instance *nilMapStorage) Delete(sessionId string) error {
    return nil
}

func (instance *nilMapStorage) Close() error {
    return nil
}

func TestNewManager_PanicsWhenStorageIsNil(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewManager(nil, time.Minute)
    }, "session storage is nil")
}

func TestManager_Session_ReturnsNilWhenIdEmpty(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    value := manager.Session("")
    if nil != value {
        t.Fatalf("expected nil")
    }
}

func TestManager_Session_ReturnsNilWhenNotFound(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    value := manager.Session("0123456789abcdef0123456789abcdef")
    if nil != value {
        t.Fatalf("expected nil")
    }
}

func TestManager_Session_ReturnsNilWhenIdIsMalformed(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, time.Minute)

    if nil != manager.Session("abc") {
        t.Fatalf("expected nil for too-short id")
    }

    if nil != manager.Session("0123456789ABCDEF0123456789ABCDEF") {
        t.Fatalf("expected nil for uppercase hex id")
    }

    if nil != manager.Session("0123456789abcdef0123456789abcdeg") {
        t.Fatalf("expected nil for non-hex id")
    }

    tooLong := "0123456789abcdef0123456789abcdef0"
    if nil != manager.Session(tooLong) {
        t.Fatalf("expected nil for too-long id")
    }
}

func TestManager_Session_NormalizesNilValuesMap(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, time.Minute)

    sessionInstance := manager.Session("0123456789abcdef0123456789abcdef")
    if nil == sessionInstance {
        t.Fatalf("expected session")
    }

    err := func() (returnedErr error) {
        defer func() {
            recoveredValue := recover()
            if nil != recoveredValue {
                returnedErr = exception.NewError("unexpected panic", nil, nil)
            }
        }()

        sessionInstance.Set("k", "v")
        return nil
    }()
    if nil != err {
        t.Fatalf("expected no panic")
    }

    if "v" != sessionInstance.String("k") {
        t.Fatalf("expected stored value")
    }
}

func TestManager_NewSession_HasId(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    if "" == sessionInstance.Id() {
        t.Fatalf("expected id")
    }
}

func TestManager_NewSession_GeneratesUniqueId(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    s1 := manager.NewSession()
    s2 := manager.NewSession()

    if "" == s1.Id() || "" == s2.Id() {
        t.Fatalf("expected ids")
    }

    if s1.Id() == s2.Id() {
        t.Fatalf("expected unique ids")
    }
}

func TestManager_SaveAndLoad_RoundTrip(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()

    sessionInstance.Set("a", "b")

    if false == sessionInstance.IsModified() {
        t.Fatalf("expected modified")
    }

    err := manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    loaded := manager.Session(sessionInstance.Id())
    if nil == loaded {
        t.Fatalf("expected loaded session")
    }

    if "b" != loaded.String("a") {
        t.Fatalf("unexpected value")
    }
}

func TestManager_SaveSession_ReturnsErrorWhenSessionNil(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    err := manager.SaveSession(nil)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestManager_SaveSession_ReturnsErrorWhenSessionIdEmpty(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    sessionInstance := &Session{
        id:       "",
        values:   map[string]any{"a": "b"},
        modified: true,
        cleared:  false,
    }

    err := manager.SaveSession(sessionInstance)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestManager_DeleteSession_RemovesSession(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("a", "b")

    err := manager.SaveSession(sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    err = manager.DeleteSession(sessionInstance.Id())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    loaded := manager.Session(sessionInstance.Id())
    if nil != loaded {
        t.Fatalf("expected nil session after delete")
    }
}

func TestManager_DeleteSession_ReturnsErrorWhenIdEmpty(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    err := manager.DeleteSession("")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestManager_DeleteSession_ReturnsErrorWhenIdIsMalformed(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    err := manager.DeleteSession("not-a-valid-hex-id")
    if nil == err {
        t.Fatalf("expected error for malformed id")
    }
}

func TestManager_RegenerateSession_MintsANewIdAndDropsTheOldEntry(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    original := manager.NewSession()
    original.Set("userId", "u-1")

    saveErr := manager.SaveSession(original)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    originalId := original.Id()

    rotated, regenerateErr := manager.RegenerateSession(original)
    if nil != regenerateErr {
        t.Fatalf("unexpected error: %v", regenerateErr)
    }

    if originalId == rotated.Id() {
        t.Fatalf("expected a fresh session id after the rotation")
    }

    if "u-1" != rotated.String("userId") {
        t.Fatalf("expected the values to survive the rotation, got %q", rotated.String("userId"))
    }

    if false == rotated.IsModified() {
        t.Fatalf("expected the rotated session to be modified so the response path stores it")
    }

    if nil != manager.Session(originalId) {
        t.Fatalf("expected the pre-rotation entry to be gone from storage")
    }
}

func TestManager_RegenerateSession_MarksTheAbandonedSessionCleared(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), 30*time.Minute)

    original := manager.NewSession()
    original.Set("userId", "u-1")

    saveErr := manager.SaveSession(original)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    rotated, regenerateErr := manager.RegenerateSession(original)
    if nil != regenerateErr {
        t.Fatalf("unexpected error: %v", regenerateErr)
    }

    if false == original.IsCleared() {
        t.Fatalf("expected the abandoned session to be marked cleared so a forgotten republish logs the client out")
    }

    if true == rotated.IsCleared() {
        t.Fatalf("expected the rotated session to be live")
    }

    if "u-1" != rotated.String("userId") {
        t.Fatalf("expected clearing the abandoned session to leave the carried-over values alone, got %q", rotated.String("userId"))
    }
}

func TestManager_RegenerateSession_ReturnsErrorWhenSessionIsNil(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    rotated, err := manager.RegenerateSession(nil)
    if nil == err {
        t.Fatalf("expected error")
    }

    if nil != rotated {
        t.Fatalf("expected no session when the rotation failed")
    }
}

func TestManager_RegenerateSession_ReturnsErrorWhenIdIsMalformed(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    sessionInstance := &Session{
        id:       "not-a-valid-hex-id",
        values:   map[string]any{"a": "b"},
        modified: false,
        cleared:  false,
    }

    rotated, err := manager.RegenerateSession(sessionInstance)
    if nil == err {
        t.Fatalf("expected error for a malformed id")
    }

    if nil != rotated {
        t.Fatalf("expected no session when the rotation failed")
    }

    if "not-a-valid-hex-id" != sessionInstance.Id() {
        t.Fatalf("expected the original session to be left untouched by a failed rotation")
    }
}

func TestManager_RegenerateSession_AbandonmentSurvivesALaterWriteToTheOriginal(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), 30*time.Minute)

    original := manager.NewSession()
    original.Set("userId", "anonymous")

    saveErr := manager.SaveSession(original)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    originalId := original.Id()

    rotated, regenerateErr := manager.RegenerateSession(original)
    if nil != regenerateErr {
        t.Fatalf("unexpected error: %v", regenerateErr)
    }

    original.Set("userId", "u-1")

    if false == original.IsCleared() {
        t.Fatalf("expected a write to the abandoned session to leave it cleared, so the rotation cannot be undone")
    }

    if nil != manager.SaveSession(original) {
        t.Fatalf("unexpected error saving the abandoned session")
    }

    if nil != manager.Session(originalId) {
        t.Fatalf("expected the rotated-away id to stay gone from storage instead of being re-created under the authenticated identity")
    }

    if true == rotated.IsCleared() {
        t.Fatalf("expected the rotated session to stay live")
    }

    rotated.Set("userId", "u-1")

    if nil != manager.SaveSession(rotated) {
        t.Fatalf("unexpected error saving the rotated session")
    }

    reloaded := manager.Session(rotated.Id())
    if nil == reloaded {
        t.Fatalf("expected the rotated session to be storable")
    }

    if "u-1" != reloaded.String("userId") {
        t.Fatalf("expected the rotated session to carry the identity, got %q", reloaded.String("userId"))
    }
}

/* @info A typed nil session — the zero value of a *Session variable, carried in the interface — is not equal to nil, so a `nil ==` guard lets it through and the first method call on it dereferences nil. SaveSession must refuse it the way RegenerateSession already does, with an error the caller can act on rather than a panic. */
func TestManager_SaveSessionRefusesATypedNilSession(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    var typedNil *Session

    err := manager.SaveSession(typedNil)
    if nil == err {
        t.Fatalf("expected a typed nil session to be refused")
    }

    if "session is nil in save session" != err.Error() {
        t.Fatalf("expected the nil-session error, got %q", err.Error())
    }
}

/* @info The save path holds the session id to the same standard the load and delete paths hold it to. An id those two refuse would be stored under a key Session can never read back and DeleteSession can never remove, so the save would report success over an entry that is unreachable for the rest of the process — and the later clear would log a delete failure the manager itself manufactured. */
func TestManager_SaveSessionRefusesAnIdTheDeletePathWouldRefuse(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, time.Minute)

    foreign := &foreignIdSession{id: "abc", modified: true}

    err := manager.SaveSession(foreign)
    if nil == err {
        t.Fatalf("expected an id the delete path refuses to be refused by the save path too")
    }

    if _, exists, _ := storage.Load("abc"); true == exists {
        t.Fatalf("expected nothing to be stored under an id that can never be read back")
    }
}

/* @info A negative lifetime reads as "already lapsed", but both storages test `0 < ttl` and store an entry with no expiry at all when it is false — so the value that asks for the shortest possible session would produce the one that never ends. The constructor refuses it instead, the same answer the configuration path gives; zero keeps its meaning of no expiry. */
func TestNewManager_RefusesANegativeTtl(t *testing.T) {
    testhelper.AssertPanicsWithError(
        t,
        func() {
            NewManager(NewInMemoryStorage(), -time.Hour)
        },
        "session ttl must be zero or positive",
    )
}

/* @info Zero stays a supported choice and must not be caught by the guard above. */
func TestNewManager_AcceptsAZeroTtlAsNoExpiry(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), 0)
    if nil == manager {
        t.Fatalf("expected a zero ttl to be accepted as no expiry")
    }
}

type foreignIdSession struct {
    id       string
    modified bool
    cleared  bool
}

func (instance *foreignIdSession) Id() string           { return instance.id }
func (instance *foreignIdSession) Get(string) any       { return nil }
func (instance *foreignIdSession) String(string) string { return "" }
func (instance *foreignIdSession) Set(string, any)      {}
func (instance *foreignIdSession) Has(string) bool      { return false }
func (instance *foreignIdSession) Delete(string)        {}
func (instance *foreignIdSession) Clear()               { instance.cleared = true }
func (instance *foreignIdSession) All() map[string]any  { return map[string]any{"a": 1} }
func (instance *foreignIdSession) IsModified() bool     { return instance.modified }
func (instance *foreignIdSession) IsCleared() bool      { return instance.cleared }

/* @info A session deleted while a request was in flight must not be written back. Storage.Save is a blind upsert, so without the tombstone a request that loaded the session before a logout deleted it re-creates the entry when its own handler finishes, with the identity intact — and the window is as long as a request takes, which is long enough for someone holding a stolen cookie to keep a revoked session alive by repeating a slow one. */
func TestManager_ADeletedSessionCannotBeSavedBackByAnInFlightRequest(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, time.Minute)

    victim := manager.NewSession()
    victim.Set("userId", "u-42")

    if nil != manager.SaveSession(victim) {
        t.Fatalf("unexpected error seeding the session")
    }

    sessionId := victim.Id()

    /* a second request loaded the same session before the logout ran */
    concurrentView := manager.Session(sessionId)
    if nil == concurrentView {
        t.Fatalf("expected the concurrent request to load the session")
    }

    victim.Clear()
    if nil != manager.SaveSession(victim) {
        t.Fatalf("unexpected error deleting the session on logout")
    }

    if _, exists, _ := storage.Load(sessionId); true == exists {
        t.Fatalf("expected the logout to have removed the session")
    }

    concurrentView.Set("lastSeen", "now")

    saveErr := manager.SaveSession(concurrentView)
    if nil == saveErr {
        t.Fatalf("expected the in-flight request to be refused the write")
    }

    if false == errors.Is(saveErr, ErrSessionDeleted) {
        t.Fatalf("expected the refusal to carry ErrSessionDeleted, got %v", saveErr)
    }

    if _, exists, _ := storage.Load(sessionId); true == exists {
        t.Fatalf("expected the deleted session to stay deleted rather than being resurrected under the same id")
    }
}

/* @info A rotated-away id is buried for the same reason a deleted one is: a request holding the session under the previous id would otherwise write it back and re-create the very id the rotation exists to retire. */
func TestManager_ARotatedAwayIdCannotBeSavedBackByAnInFlightRequest(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, time.Minute)

    original := manager.NewSession()
    original.Set("userId", "u-42")

    if nil != manager.SaveSession(original) {
        t.Fatalf("unexpected error seeding the session")
    }

    previousId := original.Id()

    concurrentView := manager.Session(previousId)
    if nil == concurrentView {
        t.Fatalf("expected the concurrent request to load the session")
    }

    if _, err := manager.RegenerateSession(original); nil != err {
        t.Fatalf("unexpected error rotating the session: %v", err)
    }

    concurrentView.Set("lastSeen", "now")

    if nil == manager.SaveSession(concurrentView) {
        t.Fatalf("expected the in-flight request to be refused the write to the rotated-away id")
    }

    if _, exists, _ := storage.Load(previousId); true == exists {
        t.Fatalf("expected the rotated-away id to stay gone")
    }
}

/* @info The tombstone map is bounded by the retention window, not by the number of sessions ever deleted: each burial prunes the entries that have outlived it, so nothing has to sweep the map on a timer. */
func TestManager_TombstonesArePrunedByTheRetentionWindow(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), time.Minute)

    staleId := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    freshId := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

    manager.mutex.Lock()
    manager.deletedAtById[staleId] = time.Now().Add(-TombstoneRetention - time.Minute)
    manager.mutex.Unlock()

    if true == manager.isTombstoned(staleId) {
        t.Fatalf("expected a tombstone older than the retention window to have lapsed")
    }

    manager.buryTombstone(freshId)

    manager.mutex.Lock()
    _, staleStillHeld := manager.deletedAtById[staleId]
    entryCount := len(manager.deletedAtById)
    manager.mutex.Unlock()

    if true == staleStillHeld {
        t.Fatalf("expected the lapsed tombstone to be pruned by the next burial")
    }

    if 1 != entryCount {
        t.Fatalf("expected only the fresh tombstone to remain, got %d entries", entryCount)
    }

    if false == manager.isTombstoned(freshId) {
        t.Fatalf("expected the fresh tombstone to be held")
    }
}

/* @info A manager built by NewManager does not own the storage it was handed, so closing it leaves the storage open — the storage is a service the container closes itself, and closing it twice is what a storage wrapping a connection reports as a failure on the second call. NewManagerOwningStorage is the constructor for the caller that built both by hand and wants one Close to end both. */
func TestManager_ClosePassesThroughOnlyForAnOwnedStorage(t *testing.T) {
    injected := &closeCountingStorage{}
    if nil != NewManager(injected, time.Minute).Close() {
        t.Fatalf("unexpected error closing the manager")
    }

    if 0 != injected.closeCount {
        t.Fatalf("expected an injected storage to be left open, it was closed %d time(s)", injected.closeCount)
    }

    owned := &closeCountingStorage{}
    if nil != NewManagerOwningStorage(owned, time.Minute).Close() {
        t.Fatalf("unexpected error closing the owning manager")
    }

    if 1 != owned.closeCount {
        t.Fatalf("expected an owned storage to be closed exactly once, got %d", owned.closeCount)
    }
}

type closeCountingStorage struct {
    closeCount int
}

func (instance *closeCountingStorage) Load(string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *closeCountingStorage) Save(string, map[string]any, time.Duration) error {
    return nil
}

func (instance *closeCountingStorage) Delete(string) error { return nil }

func (instance *closeCountingStorage) Close() error {
    instance.closeCount++

    return nil
}

/* @info The manager became shared mutable state when it started remembering deleted ids, so the paths that read and write that record run concurrently here: saves, deletes and rotations against overlapping ids, alongside the storage's own cleanup. Run under -race. */
func TestManager_ConcurrentSavesDeletesAndRotationsAreRaceFree(t *testing.T) {
    storage := NewInMemoryStorageWithCleanupInterval(time.Millisecond)
    defer storage.Close()

    manager := NewManager(storage, time.Minute)

    seeded := make([]string, 0, 16)
    for index := 0; index < 16; index++ {
        sessionInstance := manager.NewSession()
        sessionInstance.Set("index", index)

        if nil != manager.SaveSession(sessionInstance) {
            t.Fatalf("unexpected error seeding session %d", index)
        }

        seeded = append(seeded, sessionInstance.Id())
    }

    var waitGroup sync.WaitGroup

    for index := 0; index < 16; index++ {
        sessionId := seeded[index]

        waitGroup.Add(3)

        go func() {
            defer waitGroup.Done()

            if loaded := manager.Session(sessionId); nil != loaded {
                loaded.Set("touched", true)
                _ = manager.SaveSession(loaded)
            }
        }()

        go func() {
            defer waitGroup.Done()

            _ = manager.DeleteSession(sessionId)
        }()

        go func() {
            defer waitGroup.Done()

            if loaded := manager.Session(sessionId); nil != loaded {
                _, _ = manager.RegenerateSession(loaded)
            }
        }()
    }

    waitGroup.Wait()

    /* whatever the interleaving, a deleted id must not be live again */
    for _, sessionId := range seeded {
        if true == manager.isTombstoned(sessionId) {
            if _, exists, _ := storage.Load(sessionId); true == exists {
                t.Fatalf("expected a buried id to stay gone, %q is live again", sessionId)
            }
        }
    }
}

/* @info Close racing Load and Save must answer, not corrupt: after Close every operation reports the storage closed and none of them touches the map the stopped cleanup goroutine used to walk. Run under -race. */
func TestInMemoryStorage_CloseConcurrentWithLoadAndSaveIsRaceFree(t *testing.T) {
    storage := NewInMemoryStorageWithCleanupInterval(time.Millisecond)

    var waitGroup sync.WaitGroup

    for index := 0; index < 8; index++ {
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()

            _ = storage.Save("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"k": "v"}, time.Minute)
        }()

        go func() {
            defer waitGroup.Done()

            _, _, _ = storage.Load("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
        }()
    }

    if nil != storage.Close() {
        t.Fatalf("unexpected error closing the storage")
    }

    waitGroup.Wait()

    if _, _, err := storage.Load("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); nil == err {
        t.Fatalf("expected the closed storage to refuse a load")
    }
}

type blockingSaveStorage struct {
    inner        *InMemoryStorage
    enteredSave  chan struct{}
    releaseSave  chan struct{}
    enterOnce    sync.Once
}

func (instance *blockingSaveStorage) Load(sessionId string) (map[string]any, bool, error) {
    return instance.inner.Load(sessionId)
}

func (instance *blockingSaveStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    instance.enterOnce.Do(
        func() {
            close(instance.enteredSave)
            <-instance.releaseSave
        },
    )

    return instance.inner.Save(sessionId, data, ttl)
}

func (instance *blockingSaveStorage) Delete(sessionId string) error {
    return instance.inner.Delete(sessionId)
}

func (instance *blockingSaveStorage) Close() error { return instance.inner.Close() }

/* @info The tombstone is only worth anything if the record is read and the storage written inside one critical section. Reading it and then writing outside the lock leaves the same resurrection in miniature: a delete lands between the two and the write that follows re-creates the session. This sequences that interleaving deterministically — a save is held open inside the storage while a delete is attempted — so the test fails for any arrangement where the delete can get in between. */
func TestManager_ADeleteCannotInterleaveBetweenTheTombstoneCheckAndTheWrite(t *testing.T) {
    inner := NewInMemoryStorage()
    defer inner.Close()

    storage := &blockingSaveStorage{
        inner:       inner,
        enteredSave: make(chan struct{}),
        releaseSave: make(chan struct{}),
    }

    manager := NewManager(storage, time.Minute)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("userId", "u-42")

    sessionId := sessionInstance.Id()

    saveDone := make(chan error, 1)
    go func() {
        saveDone <- manager.SaveSession(sessionInstance)
    }()

    select {
    case <-storage.enteredSave:
    case <-time.After(5 * time.Second):
        t.Fatalf("the save never reached the storage")
    }

    /* let the held save finish shortly, so a delete that must wait for the lock is not deadlocked by this test */
    go func() {
        time.Sleep(50 * time.Millisecond)
        close(storage.releaseSave)
    }()

    if nil != manager.DeleteSession(sessionId) {
        t.Fatalf("unexpected error deleting the session")
    }

    <-saveDone

    if _, exists, _ := inner.Load(sessionId); true == exists {
        t.Fatalf("expected the deleted session to stay deleted: a delete interleaved between the tombstone check and the write, and the write resurrected it")
    }
}
