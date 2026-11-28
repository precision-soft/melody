package session

import (
    "context"
    "errors"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

type nilMapStorage struct{}

func (instance *nilMapStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, true, nil
}

func (instance *nilMapStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return nil
}

func (instance *nilMapStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return nil
}

func (instance *nilMapStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

type ttlRecordingStorage struct {
    savedTtl bound.Duration
}

func (instance *ttlRecordingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *ttlRecordingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    instance.savedTtl = ttl

    return nil
}

func (instance *ttlRecordingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return nil
}

func (instance *ttlRecordingStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

type probeFailingStorage struct{}

var errProbeStorageUnavailable = exception.NewError("the probe storage is unavailable", nil, nil)

func (instance *probeFailingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, errProbeStorageUnavailable
}

func (instance *probeFailingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return nil
}

func (instance *probeFailingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return nil
}

func (instance *probeFailingStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

type writeFailingStorage struct{}

var errWriteStorageUnavailable = exception.NewError("the write storage is unavailable", nil, nil)

func (instance *writeFailingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *writeFailingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return errWriteStorageUnavailable
}

func (instance *writeFailingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return errWriteStorageUnavailable
}

func (instance *writeFailingStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func assertMarkedStorageError(t *testing.T, err error, storageErr error) {
    t.Helper()

    if nil == err {
        t.Fatalf("expected the storage error, got nil")
    }

    if false == exception.ChainIs(err, storageErr) {
        t.Fatalf("expected the storage error in the chain, got %v", err)
    }

    if false == exception.IsInfrastructureFailure(err) {
        t.Fatalf("expected the storage error marked as an infrastructure failure, got %v", err)
    }
}

func TestManager_NewSession_TheCollisionExhaustionIsNotMarked(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance, mintErr := manager.NewSession(sessionRuntime())
    if nil != sessionInstance {
        t.Fatalf("expected no session when every candidate id is taken")
    }

    if nil == mintErr || "could not generate unique session id" != mintErr.Error() {
        t.Fatalf("expected the exhaustion error, got %v", mintErr)
    }

    if true == exception.IsInfrastructureFailure(mintErr) {
        t.Fatalf("expected the exhaustion error unmarked, since no storage failed")
    }
}

func TestManager_NewSessionAnswersTheStorageErrorMarked(t *testing.T) {
    manager := NewManager(&probeFailingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance, mintErr := manager.NewSession(sessionRuntime())
    if nil != sessionInstance {
        t.Fatalf("expected no session when the storage probe fails")
    }

    assertMarkedStorageError(t, mintErr, errProbeStorageUnavailable)
}

func TestManager_SaveAndDeleteAnswerTheStorageErrorMarked(t *testing.T) {
    manager := NewManager(&writeFailingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
    sessionInstance.Set("user", "alice")

    assertMarkedStorageError(t, manager.SaveSession(sessionRuntime(), sessionInstance), errWriteStorageUnavailable)
    assertMarkedStorageError(t, manager.DeleteSession(sessionRuntime(), sessionInstance.Id()), errWriteStorageUnavailable)
}

type deleteFailingStorage struct {
    held map[string]map[string]any
}

func (instance *deleteFailingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    data, exists := instance.held[sessionId]

    return data, exists, nil
}

func (instance *deleteFailingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    if nil == instance.held {
        instance.held = make(map[string]map[string]any)
    }

    instance.held[sessionId] = data

    return nil
}

func (instance *deleteFailingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return exception.NewError("the probe storage refuses to delete", nil, nil)
}

func (instance *deleteFailingStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func TestManager_SessionAnswersTheStorageErrorMarked(t *testing.T) {
    manager := NewManager(&probeFailingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance, loadErr := manager.Session(sessionRuntime(), "0123456789abcdef0123456789abcdef")
    if nil != sessionInstance {
        t.Fatalf("expected no session when the storage cannot answer")
    }

    assertMarkedStorageError(t, loadErr, errProbeStorageUnavailable)
}

func TestManager_SessionAnswersNoErrorForAnIdTheStorageDoesNotHold(t *testing.T) {
    manager := NewManager(&ttlRecordingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance, loadErr := manager.Session(sessionRuntime(), "0123456789abcdef0123456789abcdef")
    if nil != sessionInstance || nil != loadErr {
        t.Fatalf("expected neither a session nor an error for an id the storage does not hold, got %v, %v", sessionInstance, loadErr)
    }
}

func TestManager_Session_AcceptsAStoredSessionWithNoValues(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := mustSession(manager.Session(sessionRuntime(), "0123456789abcdef0123456789abcdef"))
    if nil == sessionInstance {
        t.Fatalf("expected a session for an id the storage reports as existing")
    }

    sessionInstance.Set("k", "v")

    if "v" != sessionInstance.String("k") {
        t.Fatalf("expected the session to be writable, got %q", sessionInstance.String("k"))
    }
}

func TestManager_RegenerateSession_KeepsTheOriginalWhenTheDeleteFails(t *testing.T) {
    manager := NewManager(&deleteFailingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
    sessionInstance.Set("user", "alice")

    if saveErr := manager.SaveSession(sessionRuntime(), sessionInstance); nil != saveErr {
        t.Fatalf("unexpected error storing the session: %v", saveErr)
    }

    rotated, rotateErr := manager.RegenerateSession(sessionRuntime(), sessionInstance)
    if nil == rotateErr {
        t.Fatalf("expected the rotation to fail when the previous entry cannot be removed")
    }

    if nil != rotated {
        t.Fatalf("expected no rotated session when the rotation failed")
    }

    if true == sessionInstance.IsCleared() {
        t.Fatalf("expected the original session to stay usable when the rotation failed")
    }

    if saveErr := manager.SaveSession(sessionRuntime(), sessionInstance); nil != saveErr {
        t.Fatalf("expected the session whose entry could not be removed to stay saveable, got %s", saveErr.Error())
    }
}

func TestNewManager_PanicsWhenStorageIsNil(t *testing.T) {
    testhelper.AssertPanicsWithError(t, func() {
        _ = NewManager(nil, ManagerConfig{Ttl: bound.Of(time.Minute)})
    }, "session storage is nil")
}

func TestManager_Session_ReturnsNilWhenIdEmpty(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    value := mustSession(manager.Session(sessionRuntime(), ""))
    if nil != value {
        t.Fatalf("expected nil")
    }
}

func TestManager_Session_ReturnsNilWhenNotFound(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    value := mustSession(manager.Session(sessionRuntime(), "0123456789abcdef0123456789abcdef"))
    if nil != value {
        t.Fatalf("expected nil")
    }
}

func TestManager_Session_ReturnsNilWhenIdIsMalformed(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    if nil != mustSession(manager.Session(sessionRuntime(), "abc")) {
        t.Fatalf("expected nil for too-short id")
    }

    if nil != mustSession(manager.Session(sessionRuntime(), "0123456789ABCDEF0123456789ABCDEF")) {
        t.Fatalf("expected nil for uppercase hex id")
    }

    if nil != mustSession(manager.Session(sessionRuntime(), "0123456789abcdef0123456789abcdeg")) {
        t.Fatalf("expected nil for non-hex id")
    }

    tooLong := "0123456789abcdef0123456789abcdef0"
    if nil != mustSession(manager.Session(sessionRuntime(), tooLong)) {
        t.Fatalf("expected nil for too-long id")
    }
}

func TestManager_Session_NormalizesNilValuesMap(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := mustSession(manager.Session(sessionRuntime(), "0123456789abcdef0123456789abcdef"))
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
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
    if "" == sessionInstance.Id() {
        t.Fatalf("expected id")
    }
}

func TestManager_NewSession_GeneratesUniqueId(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    s1 := mustSession(manager.NewSession(sessionRuntime()))
    s2 := mustSession(manager.NewSession(sessionRuntime()))

    if "" == s1.Id() || "" == s2.Id() {
        t.Fatalf("expected ids")
    }

    if s1.Id() == s2.Id() {
        t.Fatalf("expected unique ids")
    }
}

func TestManager_SaveAndLoad_RoundTrip(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))

    sessionInstance.Set("a", "b")

    if false == sessionInstance.IsModified() {
        t.Fatalf("expected modified")
    }

    err := manager.SaveSession(sessionRuntime(), sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    loaded := mustSession(manager.Session(sessionRuntime(), sessionInstance.Id()))
    if nil == loaded {
        t.Fatalf("expected loaded session")
    }

    if "b" != loaded.String("a") {
        t.Fatalf("unexpected value")
    }
}

func TestManager_SaveSession_ReturnsErrorWhenSessionNil(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    err := manager.SaveSession(sessionRuntime(), nil)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestManager_SaveSession_ReturnsErrorWhenSessionIdEmpty(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := &Session{
        id:       "",
        values:   map[string]any{"a": "b"},
        modified: true,
        cleared:  false,
    }

    err := manager.SaveSession(sessionRuntime(), sessionInstance)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestManager_DeleteSession_RemovesSession(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
    sessionInstance.Set("a", "b")

    err := manager.SaveSession(sessionRuntime(), sessionInstance)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    err = manager.DeleteSession(sessionRuntime(), sessionInstance.Id())
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    loaded := mustSession(manager.Session(sessionRuntime(), sessionInstance.Id()))
    if nil != loaded {
        t.Fatalf("expected nil session after delete")
    }
}

func TestManager_DeleteSession_ReturnsErrorWhenIdEmpty(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    err := manager.DeleteSession(sessionRuntime(), "")
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestManager_DeleteSession_ReturnsErrorWhenIdIsMalformed(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    err := manager.DeleteSession(sessionRuntime(), "not-a-valid-hex-id")
    if nil == err {
        t.Fatalf("expected error for malformed id")
    }
}

func TestManager_RegenerateSession_MintsANewIdAndDropsTheOldEntry(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    original := mustSession(manager.NewSession(sessionRuntime()))
    original.Set("userId", "u-1")

    saveErr := manager.SaveSession(sessionRuntime(), original)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    originalId := original.Id()

    rotated, regenerateErr := manager.RegenerateSession(sessionRuntime(), original)
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

    if nil != mustSession(manager.Session(sessionRuntime(), originalId)) {
        t.Fatalf("expected the pre-rotation entry to be gone from storage")
    }
}

func TestManager_RegenerateSession_MarksTheAbandonedSessionCleared(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    original := mustSession(manager.NewSession(sessionRuntime()))
    original.Set("userId", "u-1")

    saveErr := manager.SaveSession(sessionRuntime(), original)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    rotated, regenerateErr := manager.RegenerateSession(sessionRuntime(), original)
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
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    rotated, err := manager.RegenerateSession(sessionRuntime(), nil)
    if nil == err {
        t.Fatalf("expected error")
    }

    if nil != rotated {
        t.Fatalf("expected no session when the rotation failed")
    }
}

func TestManager_RegenerateSession_ReturnsErrorWhenIdIsMalformed(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := &Session{
        id:       "not-a-valid-hex-id",
        values:   map[string]any{"a": "b"},
        modified: false,
        cleared:  false,
    }

    rotated, err := manager.RegenerateSession(sessionRuntime(), sessionInstance)
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
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    original := mustSession(manager.NewSession(sessionRuntime()))
    original.Set("userId", "anonymous")

    saveErr := manager.SaveSession(sessionRuntime(), original)
    if nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    originalId := original.Id()

    rotated, regenerateErr := manager.RegenerateSession(sessionRuntime(), original)
    if nil != regenerateErr {
        t.Fatalf("unexpected error: %v", regenerateErr)
    }

    original.Set("userId", "u-1")

    if false == original.IsCleared() {
        t.Fatalf("expected a write to the abandoned session to leave it cleared, so the rotation cannot be undone")
    }

    if nil != manager.SaveSession(sessionRuntime(), original) {
        t.Fatalf("unexpected error saving the abandoned session")
    }

    if nil != mustSession(manager.Session(sessionRuntime(), originalId)) {
        t.Fatalf("expected the rotated-away id to stay gone from storage instead of being re-created under the authenticated identity")
    }

    if true == rotated.IsCleared() {
        t.Fatalf("expected the rotated session to stay live")
    }

    rotated.Set("userId", "u-1")

    if nil != manager.SaveSession(sessionRuntime(), rotated) {
        t.Fatalf("unexpected error saving the rotated session")
    }

    reloaded := mustSession(manager.Session(sessionRuntime(), rotated.Id()))
    if nil == reloaded {
        t.Fatalf("expected the rotated session to be storable")
    }

    if "u-1" != reloaded.String("userId") {
        t.Fatalf("expected the rotated session to carry the identity, got %q", reloaded.String("userId"))
    }
}

func TestManager_SaveSessionRefusesATypedNilSession(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    var typedNil *Session

    err := manager.SaveSession(sessionRuntime(), typedNil)
    if nil == err {
        t.Fatalf("expected a typed nil session to be refused")
    }

    if "session is nil in save session" != err.Error() {
        t.Fatalf("expected the nil-session error, got %q", err.Error())
    }
}

func TestManager_SaveSessionRefusesAnIdTheDeletePathWouldRefuse(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    foreign := &foreignIdSession{id: "abc", modified: true}

    err := manager.SaveSession(sessionRuntime(), foreign)
    if nil == err {
        t.Fatalf("expected an id the delete path refuses to be refused by the save path too")
    }

    if _, exists, _ := storage.Load(sessionRuntime(), "abc"); true == exists {
        t.Fatalf("expected nothing to be stored under an id that can never be read back")
    }
}

func TestNewManager_ATtlBelowOneSecondIsRefusedByName(t *testing.T) {
    for _, refusedTtl := range []time.Duration{-time.Hour, 0, 500 * time.Millisecond, time.Second - time.Nanosecond} {
        assertSessionFieldPanic(t, "the limit is below its minimum", "ManagerConfig.Ttl", func() {
            _ = NewManager(&nilMapStorage{}, ManagerConfig{Ttl: bound.Of(refusedTtl)})
        })
    }
}

func TestNewManager_AnAbsentTtlIsADayAndUnlimitedIsNoExpiry(t *testing.T) {
    defaultManager := NewManager(&nilMapStorage{}, ManagerConfig{})
    if bound.Of(24*time.Hour) != defaultManager.ttl {
        t.Fatalf("expected an absent ttl to be 24h, got %s", defaultManager.ttl)
    }

    unlimitedManager := NewManager(&nilMapStorage{}, ManagerConfig{Ttl: bound.UnlimitedDuration()})
    if false == unlimitedManager.ttl.IsUnlimited() {
        t.Fatalf("expected an unlimited ttl to be handed to the storage as unlimited, got %s", unlimitedManager.ttl)
    }
}

func TestManager_HandsTheStorageItsTtlAsDeclared(t *testing.T) {
    for _, row := range []struct {
        name   string
        config ManagerConfig
        check  func(ttl bound.Duration) bool
    }{
        {"absent is the day default", ManagerConfig{}, func(ttl bound.Duration) bool { return bound.Of(24*time.Hour) == ttl }},
        {"unlimited stays unlimited", ManagerConfig{Ttl: bound.UnlimitedDuration()}, func(ttl bound.Duration) bool { return true == ttl.IsUnlimited() }},
    } {
        t.Run(row.name, func(t *testing.T) {
            storage := &ttlRecordingStorage{}
            manager := NewManager(storage, row.config)

            created := mustSession(manager.NewSession(sessionRuntime()))
            created.Set("userId", "u-1")
            if saveErr := manager.SaveSession(sessionRuntime(), created); nil != saveErr {
                t.Fatalf("unexpected error: %v", saveErr)
            }

            if true == storage.savedTtl.IsAbsent() || false == row.check(storage.savedTtl) {
                t.Fatalf("unexpected ttl handed to the storage: %s", storage.savedTtl)
            }
        })
    }
}

func TestNewManager_AcceptsExactlyOneSecond(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Second)})
    if nil == manager {
        t.Fatalf("expected a one-second ttl to be accepted")
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
func (instance *foreignIdSession) Snapshot() (map[string]any, bool, bool) {
    return instance.All(), instance.modified, instance.cleared
}

func TestManager_ADeletedSessionCannotBeSavedBackByAnInFlightRequest(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    victim := mustSession(manager.NewSession(sessionRuntime()))
    victim.Set("userId", "u-42")

    if nil != manager.SaveSession(sessionRuntime(), victim) {
        t.Fatalf("unexpected error seeding the session")
    }

    sessionId := victim.Id()

    concurrentView := mustSession(manager.Session(sessionRuntime(), sessionId))
    if nil == concurrentView {
        t.Fatalf("expected the concurrent request to load the session")
    }

    victim.Clear()
    if nil != manager.SaveSession(sessionRuntime(), victim) {
        t.Fatalf("unexpected error deleting the session on logout")
    }

    if _, exists, _ := storage.Load(sessionRuntime(), sessionId); true == exists {
        t.Fatalf("expected the logout to have removed the session")
    }

    concurrentView.Set("lastSeen", "now")

    saveErr := manager.SaveSession(sessionRuntime(), concurrentView)
    if nil == saveErr {
        t.Fatalf("expected the in-flight request to be refused the write")
    }

    if false == errors.Is(saveErr, ErrSessionDeleted) {
        t.Fatalf("expected the refusal to carry ErrSessionDeleted, got %v", saveErr)
    }

    if _, exists, _ := storage.Load(sessionRuntime(), sessionId); true == exists {
        t.Fatalf("expected the deleted session to stay deleted rather than being resurrected under the same id")
    }
}

func TestManager_ARotatedAwayIdCannotBeSavedBackByAnInFlightRequest(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    original := mustSession(manager.NewSession(sessionRuntime()))
    original.Set("userId", "u-42")

    if nil != manager.SaveSession(sessionRuntime(), original) {
        t.Fatalf("unexpected error seeding the session")
    }

    previousId := original.Id()

    concurrentView := mustSession(manager.Session(sessionRuntime(), previousId))
    if nil == concurrentView {
        t.Fatalf("expected the concurrent request to load the session")
    }

    if _, err := manager.RegenerateSession(sessionRuntime(), original); nil != err {
        t.Fatalf("unexpected error rotating the session: %v", err)
    }

    concurrentView.Set("lastSeen", "now")

    saveErr := manager.SaveSession(sessionRuntime(), concurrentView)
    if nil == saveErr {
        t.Fatalf("expected the in-flight request to be refused the write to the rotated-away id")
    }

    if false == errors.Is(saveErr, ErrSessionDeleted) {
        t.Fatalf("expected the refusal to carry ErrSessionDeleted, got %v", saveErr)
    }

    if _, exists, _ := storage.Load(sessionRuntime(), previousId); true == exists {
        t.Fatalf("expected the rotated-away id to stay gone")
    }
}

func TestManager_TombstonesArePrunedByTheRetentionWindow(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    staleId := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    freshId := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

    manager.buryTombstoneAt(staleId, time.Now().Add(-managerTombstoneRetentionRule.Default-time.Minute))

    if true == manager.isTombstoned(staleId) {
        t.Fatalf("expected a tombstone older than the retention window to have lapsed")
    }

    manager.buryTombstone(freshId)

    manager.tombstoneMutex.Lock()
    _, staleStillHeld := manager.deletedAtById[staleId]
    entryCount := len(manager.deletedAtById)
    manager.tombstoneMutex.Unlock()

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

func TestManager_ClosePassesThroughOnlyForAnOwnedStorage(t *testing.T) {
    injected := &closeCountingStorage{}
    if nil != NewManager(injected, ManagerConfig{Ttl: bound.Of(time.Minute)}).CloseWithContext(t.Context()) {
        t.Fatalf("unexpected error closing the manager")
    }

    if 0 != injected.closeCount {
        t.Fatalf("expected an injected storage to be left open, it was closed %d time(s)", injected.closeCount)
    }

    owned := &closeCountingStorage{}
    if nil != NewManager(owned, ManagerConfig{Ttl: bound.Of(time.Minute), OwnsStorage: true}).CloseWithContext(t.Context()) {
        t.Fatalf("unexpected error closing the owning manager")
    }

    if 1 != owned.closeCount {
        t.Fatalf("expected an owned storage to be closed exactly once, got %d", owned.closeCount)
    }
}

type closeCountingStorage struct {
    closeCount int
}

func (instance *closeCountingStorage) Load(containercontract.Runtime, string) (map[string]any, bool, error) {
    return nil, false, nil
}

func (instance *closeCountingStorage) Save(containercontract.Runtime, string, map[string]any, bound.Duration) error {
    return nil
}

func (instance *closeCountingStorage) Delete(containercontract.Runtime, string) error { return nil }

func (instance *closeCountingStorage) CloseWithContext(closeContext context.Context) error {
    instance.closeCount++

    return nil
}

func TestManager_ConcurrentSavesDeletesAndRotationsAreRaceFree(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Millisecond)})
    defer storage.CloseWithContext(context.Background())

    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := make([]string, 0, 16)
    for index := 0; index < 16; index++ {
        sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
        sessionInstance.Set("index", index)

        if nil != manager.SaveSession(sessionRuntime(), sessionInstance) {
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

            if loaded := mustSession(manager.Session(sessionRuntime(), sessionId)); nil != loaded {
                loaded.Set("touched", true)
                _ = manager.SaveSession(sessionRuntime(), loaded)
            }
        }()

        go func() {
            defer waitGroup.Done()

            _ = manager.DeleteSession(sessionRuntime(), sessionId)
        }()

        go func() {
            defer waitGroup.Done()

            if loaded := mustSession(manager.Session(sessionRuntime(), sessionId)); nil != loaded {
                _, _ = manager.RegenerateSession(sessionRuntime(), loaded)
            }
        }()

        cutContext, cancel := context.WithCancel(context.Background())
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()

            _ = manager.DeleteSession(sessionRuntimeUnder(cutContext), sessionId)
            if loaded, _ := manager.Session(sessionRuntimeUnder(cutContext), sessionId); nil != loaded {
                loaded.Set("cut", true)
                _ = manager.SaveSession(sessionRuntimeUnder(cutContext), loaded)
            }
        }()

        go func() {
            defer waitGroup.Done()

            cancel()
        }()
    }

    waitGroup.Wait()

    for _, sessionId := range seeded {
        if true == manager.isTombstoned(sessionId) {
            if _, exists, _ := storage.Load(sessionRuntime(), sessionId); true == exists {
                t.Fatalf("expected a buried id to stay gone, %q is live again", sessionId)
            }
        }
    }
}

func TestInMemoryStorage_CloseConcurrentWithLoadAndSaveIsRaceFree(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{CleanupInterval: bound.Of(time.Millisecond)})

    var waitGroup sync.WaitGroup

    for index := 0; index < 8; index++ {
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()

            _ = storage.Save(sessionRuntime(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"k": "v"}, bound.Of(time.Minute))
        }()

        go func() {
            defer waitGroup.Done()

            _, _, _ = storage.Load(sessionRuntime(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
        }()
    }

    if nil != storage.CloseWithContext(t.Context()) {
        t.Fatalf("unexpected error closing the storage")
    }

    waitGroup.Wait()

    if _, _, err := storage.Load(sessionRuntime(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); nil == err {
        t.Fatalf("expected the closed storage to refuse a load")
    }
}

type blockingSaveStorage struct {
    inner       *InMemoryStorage
    enteredSave chan struct{}
    releaseSave chan struct{}
    enterOnce   sync.Once
}

func (instance *blockingSaveStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *blockingSaveStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    instance.enterOnce.Do(
        func() {
            close(instance.enteredSave)
            <-instance.releaseSave
        },
    )

    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *blockingSaveStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *blockingSaveStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func TestManager_ADeleteCannotInterleaveBetweenTheTombstoneCheckAndTheWrite(t *testing.T) {
    inner := NewInMemoryStorage(InMemoryStorageConfig{})
    defer inner.CloseWithContext(context.Background())

    storage := &blockingSaveStorage{
        inner:       inner,
        enteredSave: make(chan struct{}),
        releaseSave: make(chan struct{}),
    }

    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := mustSession(manager.NewSession(sessionRuntime()))
    sessionInstance.Set("userId", "u-42")

    sessionId := sessionInstance.Id()

    saveDone := make(chan error, 1)
    go func() {
        saveDone <- manager.SaveSession(sessionRuntime(), sessionInstance)
    }()

    select {
    case <-storage.enteredSave:
    case <-time.After(5 * time.Second):
        t.Fatalf("the save never reached the storage")
    }

    go func() {
        time.Sleep(50 * time.Millisecond)
        close(storage.releaseSave)
    }()

    if nil != manager.DeleteSession(sessionRuntime(), sessionId) {
        t.Fatalf("unexpected error deleting the session")
    }

    <-saveDone

    if _, exists, _ := inner.Load(sessionRuntime(), sessionId); true == exists {
        t.Fatalf("expected the deleted session to stay deleted: a delete interleaved between the tombstone check and the write, and the write resurrected it")
    }
}

type heldSaveStorage struct {
    inner       *InMemoryStorage
    heldId      string
    enteredSave chan string
    releaseSave chan struct{}
}

func (instance *heldSaveStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *heldSaveStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    instance.enteredSave <- sessionId

    if sessionId == instance.heldId {
        <-instance.releaseSave
    }

    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *heldSaveStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *heldSaveStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func TestManager_ASaveDoesNotWaitOnTheSaveOfAnotherSession(t *testing.T) {
    inner := NewInMemoryStorage(InMemoryStorageConfig{})
    defer inner.CloseWithContext(context.Background())

    storage := &heldSaveStorage{
        inner:       inner,
        enteredSave: make(chan string, 4),
        releaseSave: make(chan struct{}),
    }

    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    heldSession := mustSession(manager.NewSession(sessionRuntime()))
    heldSession.Set("userId", "u-1")

    otherSession := mustSession(manager.NewSession(sessionRuntime()))
    for attempt := 0; manager.sessionStripeOf(heldSession.Id()) == manager.sessionStripeOf(otherSession.Id()); attempt++ {
        if 64 < attempt {
            t.Fatalf("could not mint two session ids on different locks")
        }

        otherSession = mustSession(manager.NewSession(sessionRuntime()))
    }
    otherSession.Set("userId", "u-2")

    storage.heldId = heldSession.Id()

    heldDone := make(chan error, 1)
    go func() {
        heldDone <- manager.SaveSession(sessionRuntime(), heldSession)
    }()

    select {
    case arrived := <-storage.enteredSave:
        if heldSession.Id() != arrived {
            t.Fatalf("expected the held save to reach the storage first, got %q", arrived)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("the held save never reached the storage")
    }

    otherDone := make(chan error, 1)
    go func() {
        otherDone <- manager.SaveSession(sessionRuntime(), otherSession)
    }()

    select {
    case saveErr := <-otherDone:
        if nil != saveErr {
            t.Fatalf("unexpected error saving the unrelated session: %v", saveErr)
        }
    case <-time.After(5 * time.Second):
        close(storage.releaseSave)
        t.Fatalf("an unrelated session waited on a save that was still inside the storage")
    }

    close(storage.releaseSave)

    if saveErr := <-heldDone; nil != saveErr {
        t.Fatalf("unexpected error saving the held session: %v", saveErr)
    }
}

func TestManager_TheBurialPruningStopsAtTheFirstLivingTombstone(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    lapsedId := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    livingId := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    freshId := "cccccccccccccccccccccccccccccccc"

    manager.buryTombstoneAt(lapsedId, time.Now().Add(-managerTombstoneRetentionRule.Default-time.Minute))
    manager.buryTombstoneAt(livingId, time.Now().Add(-time.Minute))

    manager.buryTombstone(freshId)

    if true == manager.isTombstoned(lapsedId) {
        t.Fatalf("expected the lapsed tombstone to be gone")
    }

    if false == manager.isTombstoned(livingId) {
        t.Fatalf("expected the tombstone still inside the window to be held")
    }

    if false == manager.isTombstoned(freshId) {
        t.Fatalf("expected the fresh tombstone to be held")
    }

    manager.tombstoneMutex.Lock()
    entryCount := len(manager.deletedAtById)
    queuedCount := len(manager.buriedInOrder)
    manager.tombstoneMutex.Unlock()

    if 2 != entryCount {
        t.Fatalf("expected two tombstones to remain, got %d", entryCount)
    }

    if 2 != queuedCount {
        t.Fatalf("expected the lapsed burial to leave the queue, got %d queued", queuedCount)
    }
}

func TestManager_AReburiedTombstoneSurvivesThePruningOfItsEarlierBurial(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    reburiedId := "dddddddddddddddddddddddddddddddd"
    freshId := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

    now := time.Now()

    manager.buryTombstoneAt(reburiedId, now.Add(-managerTombstoneRetentionRule.Default-time.Minute))
    manager.buryTombstoneAt(reburiedId, now.Add(-managerTombstoneRetentionRule.Default+time.Minute))

    manager.buryTombstoneAt(freshId, now)

    if false == manager.isTombstoned(reburiedId) {
        t.Fatalf("expected the re-burial to keep refusing write-backs after its earlier burial lapsed")
    }
}

func TestNewManager_ATombstoneRetentionBelowOneSecondOrUnlimitedIsRefusedByName(t *testing.T) {
    for _, refusedRetention := range []time.Duration{0, -time.Second, time.Second - time.Nanosecond} {
        assertSessionFieldPanic(t, "the limit is below its minimum", "ManagerConfig.TombstoneRetention", func() {
            _ = NewManager(&nilMapStorage{}, ManagerConfig{TombstoneRetention: bound.Of(refusedRetention)})
        })
    }

    assertSessionFieldPanic(t, "the limit does not admit unlimited", "ManagerConfig.TombstoneRetention", func() {
        _ = NewManager(&nilMapStorage{}, ManagerConfig{TombstoneRetention: bound.UnlimitedDuration()})
    })
}

func TestNewManager_AnAbsentTombstoneRetentionIsFiveMinutes(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{})
    if 5*time.Minute != manager.tombstoneRetention {
        t.Fatalf("expected an absent retention to be 5m, got %s", manager.tombstoneRetention)
    }
}

func TestNewManager_ATombstoneRetentionSizesTheRefusalWindow(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute), TombstoneRetention: bound.Of(time.Hour)})

    buriedId := "dddddddddddddddddddddddddddddddd"

    manager.buryTombstoneAt(buriedId, time.Now().Add(-managerTombstoneRetentionRule.Default-time.Minute))

    if false == manager.isTombstoned(buriedId) {
        t.Fatalf("expected a burial older than the default window to still refuse under the sized one")
    }

    manager.buryTombstoneAt(buriedId, time.Now().Add(-time.Hour-time.Minute))

    if true == manager.isTombstoned(buriedId) {
        t.Fatalf("expected a burial older than the sized window to have lapsed")
    }
}

type snapshotClearedSession struct {
    foreignIdSession
}

func (instance *snapshotClearedSession) Id() string { return "1234567890abcdef1234567890abcdef" }

func (instance *snapshotClearedSession) Snapshot() (map[string]any, bool, bool) {
    return map[string]any{}, true, true
}

func TestSaveSession_TheBranchDecisionFollowsTheSnapshotNotTheAccessors(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.UnlimitedDuration(), OwnsStorage: true})
    defer func() { _ = manager.CloseWithContext(context.Background()) }()

    sessionInstance := &snapshotClearedSession{}
    sessionInstance.modified = true
    sessionInstance.cleared = false

    if saveErr := manager.SaveSession(sessionRuntime(), sessionInstance); nil != saveErr {
        t.Fatalf("expected the delete branch to answer nil, got %v", saveErr)
    }

    if _, exists, _ := storage.Load(sessionRuntime(), sessionInstance.Id()); true == exists {
        t.Fatalf("expected the snapshot's cleared flag to have taken the delete branch")
    }
}

func TestManager_SaveSession_RefusalNamesTheSessionByAOneWayReferenceNotTheRawId(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    rawId := "not-a-valid-session-id"

    err := manager.SaveSession(sessionRuntime(), &foreignIdSession{id: rawId, modified: true})
    if nil == err {
        t.Fatalf("expected the malformed id to be refused")
    }

    contextProvider, ok := err.(*exception.Error)
    if false == ok {
        t.Fatalf("expected a melody error carrying context, got %T", err)
    }

    sessionReference := contextProvider.Context()["sessionReference"]

    if rawId == sessionReference {
        t.Fatalf("the raw session id must not travel in the refusal context, got %v", sessionReference)
    }

    if sessionIdLogReference(rawId) != sessionReference {
        t.Fatalf("expected the truncated digest of the id, got %v", sessionReference)
    }

    reference, ok := sessionReference.(string)
    if false == ok || 16 != len(reference) {
        t.Fatalf("expected a sixteen character reference, got %v", sessionReference)
    }
}

func TestNewManager_TheRetentionWindowRefusesUpToItsBoundaryButNotAtIt(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2020, time.March, 1, 12, 0, 0, 0, time.UTC))

    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute), TombstoneRetention: bound.Of(time.Hour), Clock: frozenClock})
    defer func() { _ = manager.CloseWithContext(context.Background()) }()

    buriedId := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

    manager.buryTombstone(buriedId)

    frozenClock.Advance(time.Hour - time.Nanosecond)

    if false == manager.isTombstoned(buriedId) {
        t.Fatalf("expected a burial one nanosecond inside the window to still refuse the write-back")
    }

    frozenClock.Advance(time.Nanosecond)

    if true == manager.isTombstoned(buriedId) {
        t.Fatalf("expected the burial to have lapsed at the instant the window names, which the comparison excludes")
    }
}

func TestNewManager_ANilClockIsTheSystemClock(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{})
    if _, isSystemClock := manager.clock.(*clock.SystemClock); false == isSystemClock {
        t.Fatalf("expected a nil clock to be the system clock, got %T", manager.clock)
    }
}

func TestManager_SaveSession_NamesARotationApartFromADeletion(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    rotatedAway := mustSession(manager.NewSession(sessionRuntime()))
    rotatedAway.Set("userId", "u-1")
    if saveErr := manager.SaveSession(sessionRuntime(), rotatedAway); nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    rotatedAwayId := rotatedAway.Id()

    if _, regenerateErr := manager.RegenerateSession(sessionRuntime(), rotatedAway); nil != regenerateErr {
        t.Fatalf("unexpected error: %v", regenerateErr)
    }

    rotationRefusal := manager.SaveSession(sessionRuntime(), &Session{id: rotatedAwayId, values: map[string]any{}, modified: true})
    if false == errors.Is(rotationRefusal, ErrSessionRotated) {
        t.Fatalf("expected the refusal of a rotated-away id to carry ErrSessionRotated, got %v", rotationRefusal)
    }

    if false == errors.Is(rotationRefusal, ErrSessionDeleted) {
        t.Fatalf("expected the refusal of a rotated-away id to carry ErrSessionDeleted beside it, got %v", rotationRefusal)
    }

    deleted := mustSession(manager.NewSession(sessionRuntime()))
    deleted.Set("userId", "u-2")
    if saveErr := manager.SaveSession(sessionRuntime(), deleted); nil != saveErr {
        t.Fatalf("unexpected error: %v", saveErr)
    }

    deletedId := deleted.Id()

    if deleteErr := manager.DeleteSession(sessionRuntime(), deletedId); nil != deleteErr {
        t.Fatalf("unexpected error: %v", deleteErr)
    }

    deletionRefusal := manager.SaveSession(sessionRuntime(), &Session{id: deletedId, values: map[string]any{}, modified: true})
    if false == errors.Is(deletionRefusal, ErrSessionDeleted) {
        t.Fatalf("expected the refusal of a deleted id to carry ErrSessionDeleted, got %v", deletionRefusal)
    }

    if true == errors.Is(deletionRefusal, ErrSessionRotated) {
        t.Fatalf("expected the refusal of a deleted id NOT to read as a rotation")
    }
}

type removingFailingStorage struct {
    inner *InMemoryStorage
}

func (instance *removingFailingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *removingFailingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *removingFailingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    _ = instance.inner.Delete(runtimeInstance, sessionId)

    return exception.NewError("the probe storage removed the entry and failed to flush", nil, nil)
}

func (instance *removingFailingStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

type keepingFailingStorage struct {
    inner     *InMemoryStorage
    loadFails bool
}

func (instance *keepingFailingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    if true == instance.loadFails {
        return nil, false, exception.NewError("the probe storage cannot read", nil, nil)
    }

    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *keepingFailingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *keepingFailingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return exception.NewError("the probe storage refuses to delete", nil, nil)
}

func (instance *keepingFailingStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func tombstoneCountOf(manager *Manager) int {
    manager.tombstoneMutex.Lock()
    defer manager.tombstoneMutex.Unlock()

    return len(manager.deletedAtById)
}

func seededSessionOf(t *testing.T, manager *Manager) sessioncontract.Session {
    t.Helper()

    seeded := mustSession(manager.NewSession(sessionRuntime()))
    seeded.Set("userId", "u-7")

    if saveErr := manager.SaveSession(sessionRuntime(), seeded); nil != saveErr {
        t.Fatalf("unexpected error seeding the session: %v", saveErr)
    }

    return seeded
}

func TestManager_RegenerateSession_RefusesAnIdALogoutBuried(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := seededSessionOf(t, manager)
    loadedBeforeTheLogout := mustSession(manager.Session(sessionRuntime(), seeded.Id()))

    if deleteErr := manager.DeleteSession(sessionRuntime(), seeded.Id()); nil != deleteErr {
        t.Fatalf("unexpected error on the logout: %v", deleteErr)
    }

    rotated, rotateErr := manager.RegenerateSession(sessionRuntime(), loadedBeforeTheLogout)
    if nil == rotateErr {
        t.Fatalf("expected the rotation of a buried id to be refused, got the fresh id %q", rotated.Id())
    }

    if nil != rotated {
        t.Fatalf("expected no rotated session for a buried id")
    }

    if false == errors.Is(rotateErr, ErrSessionDeleted) {
        t.Fatalf("expected the refusal to carry ErrSessionDeleted, got %v", rotateErr)
    }

    if true == errors.Is(rotateErr, ErrSessionRotated) {
        t.Fatalf("expected a logout's refusal not to read as a rotation, got %v", rotateErr)
    }
}

func TestManager_RegenerateSession_RefusesTheSecondRotationOfOneId(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := seededSessionOf(t, manager)
    firstCopy := mustSession(manager.Session(sessionRuntime(), seeded.Id()))
    secondCopy := mustSession(manager.Session(sessionRuntime(), seeded.Id()))

    if _, rotateErr := manager.RegenerateSession(sessionRuntime(), firstCopy); nil != rotateErr {
        t.Fatalf("unexpected error on the first rotation: %v", rotateErr)
    }

    rotated, rotateErr := manager.RegenerateSession(sessionRuntime(), secondCopy)
    if nil == rotateErr {
        t.Fatalf("expected the second rotation of one id to be refused, got the fresh id %q", rotated.Id())
    }

    if false == errors.Is(rotateErr, ErrSessionDeleted) || false == errors.Is(rotateErr, ErrSessionRotated) {
        t.Fatalf("expected the refusal to carry ErrSessionDeleted and ErrSessionRotated, got %v", rotateErr)
    }
}

func TestManager_DeleteSession_BuriesAnEntryTheStorageRemovedBeforeFailing(t *testing.T) {
    storage := &removingFailingStorage{inner: NewInMemoryStorage(InMemoryStorageConfig{})}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := seededSessionOf(t, manager)
    inFlight := mustSession(manager.Session(sessionRuntime(), seeded.Id()))

    if deleteErr := manager.DeleteSession(sessionRuntime(), seeded.Id()); nil == deleteErr {
        t.Fatalf("expected the storage's failure to reach the caller")
    }

    inFlight.Set("lastSeen", "now")

    saveErr := manager.SaveSession(sessionRuntime(), inFlight)
    if false == errors.Is(saveErr, ErrSessionDeleted) {
        t.Fatalf("expected the write-back of a removed entry to be refused with ErrSessionDeleted, got %v", saveErr)
    }

    if _, exists, _ := storage.inner.Load(sessionRuntime(), seeded.Id()); true == exists {
        t.Fatalf("expected the removed entry to stay removed")
    }
}

func TestManager_DeleteSession_LeavesAnEntryTheStorageKeptUnburied(t *testing.T) {
    storage := &keepingFailingStorage{inner: NewInMemoryStorage(InMemoryStorageConfig{})}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := seededSessionOf(t, manager)

    if deleteErr := manager.DeleteSession(sessionRuntime(), seeded.Id()); nil == deleteErr {
        t.Fatalf("expected the storage's refusal to reach the caller")
    }

    seeded.Set("lastSeen", "now")

    if saveErr := manager.SaveSession(sessionRuntime(), seeded); nil != saveErr {
        t.Fatalf("expected a session whose entry is still held to stay saveable, got %v", saveErr)
    }

    if 0 != tombstoneCountOf(manager) {
        t.Fatalf("expected no tombstone over a kept entry")
    }
}

func TestManager_DeleteSession_LeavesAnEntryItCannotReReadUnburied(t *testing.T) {
    storage := &keepingFailingStorage{inner: NewInMemoryStorage(InMemoryStorageConfig{})}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := seededSessionOf(t, manager)

    reads := 0
    readingStorage := &readCountingStorage{inner: storage, onRead: func() {
        reads = reads + 1
        if 1 < reads {
            storage.loadFails = true
        }
    }}
    manager.storage = readingStorage

    if deleteErr := manager.DeleteSession(sessionRuntime(), seeded.Id()); nil == deleteErr {
        t.Fatalf("expected the storage's refusal to reach the caller")
    }

    if 0 != tombstoneCountOf(manager) {
        t.Fatalf("expected no tombstone when the storage cannot say the entry went")
    }
}

type readCountingStorage struct {
    inner  sessioncontract.Storage
    onRead func()
}

func (instance *readCountingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    instance.onRead()

    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *readCountingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *readCountingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *readCountingStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func TestManager_DeleteSession_BuriesAnIdItIsHandedForTheWindow(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    if deleteErr := manager.DeleteSession(sessionRuntime(), "0123456789abcdef0123456789abcdef"); nil != deleteErr {
        t.Fatalf("unexpected error deleting an absent id: %v", deleteErr)
    }

    if 1 != tombstoneCountOf(manager) {
        t.Fatalf("expected one record for the id the delete was handed, got %d", tombstoneCountOf(manager))
    }
}

func TestManager_ALogoutOfAnEntryThatVanishedUnderneathStillBuriesIt(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := seededSessionOf(t, manager)
    inFlight := mustSession(manager.Session(sessionRuntime(), seeded.Id()))

    if deleteErr := storage.Delete(sessionRuntime(), seeded.Id()); nil != deleteErr {
        t.Fatalf("unexpected error evicting the entry: %v", deleteErr)
    }

    if deleteErr := manager.DeleteSession(sessionRuntime(), seeded.Id()); nil != deleteErr {
        t.Fatalf("unexpected error on the logout: %v", deleteErr)
    }

    inFlight.Set("lastSeen", "now")

    if saveErr := manager.SaveSession(sessionRuntime(), inFlight); false == errors.Is(saveErr, ErrSessionDeleted) {
        t.Fatalf("expected the write-back of the ended session refused with ErrSessionDeleted, got %v", saveErr)
    }

    if nil != mustSession(manager.Session(sessionRuntime(), seeded.Id())) {
        t.Fatalf("expected the ended session to stay absent from the storage")
    }
}

type deleteCountingStorage struct {
    inner   *InMemoryStorage
    deletes int
}

func (instance *deleteCountingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *deleteCountingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *deleteCountingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    instance.deletes++

    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *deleteCountingStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(context.Background())
}

func TestManager_ARotationOfAMintedSessionLeavesNoRecord(t *testing.T) {
    storage := &deleteCountingStorage{inner: NewInMemoryStorage(InMemoryStorageConfig{})}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    minted := mustSession(manager.NewSession(sessionRuntime()))
    minted.Set("userId", "u-1")

    rotated, rotateErr := manager.RegenerateSession(sessionRuntime(), minted)
    if nil != rotateErr {
        t.Fatalf("unexpected error rotating a fresh session: %v", rotateErr)
    }

    if minted.Id() == rotated.Id() || "u-1" != rotated.String("userId") {
        t.Fatalf("expected a fresh id carrying the values, got %q", rotated.Id())
    }

    if _, rotateErr := manager.RegenerateSession(sessionRuntime(), rotated); nil != rotateErr {
        t.Fatalf("unexpected error rotating the rotated session again: %v", rotateErr)
    }

    if 0 != storage.deletes || 0 != tombstoneCountOf(manager) {
        t.Fatalf("expected no removal and no record for a minted id, got %d removals and %d records", storage.deletes, tombstoneCountOf(manager))
    }
}

func TestManager_ASaveOfAClearedMintedSessionLeavesNoRecord(t *testing.T) {
    storage := &deleteCountingStorage{inner: NewInMemoryStorage(InMemoryStorageConfig{})}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    original := mustSession(manager.NewSession(sessionRuntime()))
    original.Set("userId", "u-1")

    if _, rotateErr := manager.RegenerateSession(sessionRuntime(), original); nil != rotateErr {
        t.Fatalf("unexpected error rotating a fresh session: %v", rotateErr)
    }

    if saveErr := manager.SaveSession(sessionRuntime(), original); nil != saveErr {
        t.Fatalf("unexpected error saving the rotated-away original: %v", saveErr)
    }

    cleared := mustSession(manager.NewSession(sessionRuntime()))
    cleared.Clear()

    if saveErr := manager.SaveSession(sessionRuntime(), cleared); nil != saveErr {
        t.Fatalf("unexpected error saving a cleared fresh session: %v", saveErr)
    }

    if 0 != storage.deletes || 0 != tombstoneCountOf(manager) {
        t.Fatalf("expected no removal and no record for a cleared minted id, got %d removals and %d records", storage.deletes, tombstoneCountOf(manager))
    }

    stored := mustSession(manager.NewSession(sessionRuntime()))
    stored.Set("userId", "u-2")
    if saveErr := manager.SaveSession(sessionRuntime(), stored); nil != saveErr {
        t.Fatalf("unexpected error saving: %v", saveErr)
    }

    stored.Clear()
    if saveErr := manager.SaveSession(sessionRuntime(), stored); nil != saveErr {
        t.Fatalf("unexpected error saving the cleared stored session: %v", saveErr)
    }

    if 1 != storage.deletes || 1 != tombstoneCountOf(manager) {
        t.Fatalf("expected the stored id removed and buried, got %d removals and %d records", storage.deletes, tombstoneCountOf(manager))
    }
}

func TestManager_ARotationOfASavedSessionBuriesTheRetiredId(t *testing.T) {
    storage := &deleteCountingStorage{inner: NewInMemoryStorage(InMemoryStorageConfig{})}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    saved := mustSession(manager.NewSession(sessionRuntime()))
    saved.Set("userId", "u-1")

    if saveErr := manager.SaveSession(sessionRuntime(), saved); nil != saveErr {
        t.Fatalf("unexpected error saving: %v", saveErr)
    }

    if _, rotateErr := manager.RegenerateSession(sessionRuntime(), saved); nil != rotateErr {
        t.Fatalf("unexpected error rotating: %v", rotateErr)
    }

    if 1 != storage.deletes || 1 != tombstoneCountOf(manager) {
        t.Fatalf("expected the retired id removed and buried, got %d removals and %d records", storage.deletes, tombstoneCountOf(manager))
    }
}

func TestManager_ARotationOfALoadedSessionBuriesTheRetiredId(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    seeded := seededSessionOf(t, manager)
    loaded := mustSession(manager.Session(sessionRuntime(), seeded.Id()))

    if _, rotateErr := manager.RegenerateSession(sessionRuntime(), loaded); nil != rotateErr {
        t.Fatalf("unexpected error rotating: %v", rotateErr)
    }

    if 1 != tombstoneCountOf(manager) || nil != mustSession(manager.Session(sessionRuntime(), seeded.Id())) {
        t.Fatalf("expected the retired id removed and buried, got %d records", tombstoneCountOf(manager))
    }
}

func TestManager_ALapsedRotationBurialLeavesNoRotationMarkBehind(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    rotatedId := "cccccccccccccccccccccccccccccccc"
    manager.buryRotationTombstoneAt(rotatedId, time.Now().Add(-managerTombstoneRetentionRule.Default-time.Minute))

    manager.buryTombstone("dddddddddddddddddddddddddddddddd")

    manager.tombstoneMutex.Lock()
    markCount := len(manager.rotatedAwayIds)
    manager.tombstoneMutex.Unlock()

    if 0 != markCount {
        t.Fatalf("expected the rotation mark pruned with its lapsed burial, got %d marks", markCount)
    }
}

func TestManager_SaveSession_AnIdDeletedAfterItsRotationReadsAsDeleted(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(30*time.Minute)})

    sessionId := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
    manager.buryRotationTombstoneAt(sessionId, time.Now().Add(-time.Second))
    manager.buryTombstoneAt(sessionId, time.Now())

    refusal := manager.SaveSession(sessionRuntime(), &Session{id: sessionId, values: map[string]any{}, modified: true})
    if false == errors.Is(refusal, ErrSessionDeleted) {
        t.Fatalf("expected the refusal to carry ErrSessionDeleted, got %v", refusal)
    }

    if true == errors.Is(refusal, ErrSessionRotated) {
        t.Fatalf("expected the deletion after the rotation to name the deletion, not the rotation")
    }
}

func TestManager_SaveSessionReadsAForeignSessionThroughItsSnapshot(t *testing.T) {
    storage := NewInMemoryStorage(InMemoryStorageConfig{})
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionId := mustSession(manager.NewSession(sessionRuntime())).Id()

    foreign := &foreignSession{id: sessionId, values: map[string]any{"user": "ada"}, modified: true}

    saveErr := manager.SaveSession(sessionRuntime(), foreign)
    if nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    storedValues, exists, _ := storage.Load(sessionRuntime(), sessionId)
    if false == exists || "ada" != storedValues["user"] {
        t.Fatalf("expected the session's values read through its snapshot to be stored, got %v %t", storedValues, exists)
    }

    foreign.Clear()

    saveErr = manager.SaveSession(sessionRuntime(), foreign)
    if nil != saveErr {
        t.Fatalf("unexpected save error on the cleared session: %v", saveErr)
    }

    if _, exists, _ = storage.Load(sessionRuntime(), sessionId); true == exists {
        t.Fatalf("expected a session cleared through its accessors to be deleted")
    }
}

func TestManager_RegenerateSession_ReturnsTheStorageFailureOfTheIdProbe(t *testing.T) {
    manager := NewManager(&probeFailingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    sessionInstance := &Session{
        id:     strings.Repeat("ab", 16),
        values: map[string]any{"a": "b"},
    }

    rotated, err := manager.RegenerateSession(sessionRuntime(), sessionInstance)
    if nil != rotated || nil == err {
        t.Fatalf("expected the probe failure to be returned, got session %v and error %v", rotated, err)
    }

    melodyErr, isMelodyErr := err.(*exception.Error)
    if false == isMelodyErr || "session id could not be minted in regenerate session" != melodyErr.Message() {
        t.Fatalf("expected the refusal to name the door, got %v", err)
    }

    if _, hasReference := melodyErr.Context()["sessionReference"]; false == hasReference {
        t.Fatalf("expected the refusal to carry the session reference, got %v", melodyErr.Context())
    }

    if _, hasRetiredKey := melodyErr.Context()["sessionRef"]; true == hasRetiredKey {
        t.Fatalf("expected the reference under sessionReference only, got %v", melodyErr.Context())
    }

    if false == strings.Contains(err.Error(), "the probe storage is unavailable") && nil == errors.Unwrap(err) {
        t.Fatalf("expected the storage failure to travel as the cause, got %v", err)
    }
}

func TestManager_InvalidIdRefusalsOfRegenerateAndDeleteCarryTheReference(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(InMemoryStorageConfig{}), ManagerConfig{Ttl: bound.Of(time.Minute)})

    _, regenerateErr := manager.RegenerateSession(sessionRuntime(), &Session{id: "not-a-valid-hex-id", values: map[string]any{}})
    deleteErr := manager.DeleteSession(sessionRuntime(), "not-a-valid-hex-id")

    for name, err := range map[string]error{"regenerate": regenerateErr, "delete": deleteErr} {
        melodyErr, isMelodyErr := err.(*exception.Error)
        if false == isMelodyErr {
            t.Fatalf("%s: expected an exception error, got %v", name, err)
        }

        if _, hasReference := melodyErr.Context()["sessionReference"]; false == hasReference {
            t.Fatalf("%s: expected the refusal to carry the session reference, got %v", name, melodyErr.Context())
        }
    }
}

func TestManager_RegenerateSessionAnswersAStorageOutageMarked(t *testing.T) {
    probeManager := NewManager(&probeFailingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    _, probeErr := probeManager.RegenerateSession(sessionRuntime(), &Session{id: "0123456789abcdef0123456789abcdef", values: map[string]any{}})
    assertMarkedStorageError(t, probeErr, errProbeStorageUnavailable)

    writeManager := NewManager(&writeFailingStorage{}, ManagerConfig{Ttl: bound.Of(time.Minute)})

    _, deleteErr := writeManager.RegenerateSession(sessionRuntime(), &Session{id: "0123456789abcdef0123456789abcdef", values: map[string]any{}})
    assertMarkedStorageError(t, deleteErr, errWriteStorageUnavailable)
}

type storageCallCountingStorage struct {
    loadCalls   int
    saveCalls   int
    deleteCalls int
}

func (instance *storageCallCountingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    instance.loadCalls++

    return nil, false, nil
}

func (instance *storageCallCountingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    instance.saveCalls++

    return nil
}

func (instance *storageCallCountingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    instance.deleteCalls++

    return nil
}

func (instance *storageCallCountingStorage) CloseWithContext(closeContext context.Context) error {
    return nil
}

func requireUnavailableSessionRefusal(t *testing.T, refusalErr error, cause error) {
    t.Helper()

    if nil == refusalErr || false == exception.ChainIs(refusalErr, ErrSessionUnavailable) || false == exception.ChainIs(refusalErr, cause) {
        t.Fatalf("expected the refusal to carry ErrSessionUnavailable and the outage, got %v", refusalErr)
    }

    if false == exception.IsInfrastructureFailure(refusalErr) {
        t.Fatalf("expected the refusal marked as an infrastructure failure, got %v", refusalErr)
    }
}

func TestManager_SaveSessionRefusesAnUnavailableSession(t *testing.T) {
    storage := &storageCallCountingStorage{}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Hour)})

    cause := errors.New("session store is down")
    unavailable := NewUnavailableSession(cause)
    unavailable.Set("cart", "one line")

    requireUnavailableSessionRefusal(t, manager.SaveSession(sessionRuntime(), unavailable), cause)

    unavailable.Clear()
    requireUnavailableSessionRefusal(t, manager.SaveSession(sessionRuntime(), unavailable), cause)

    if 0 != storage.saveCalls || 0 != storage.deleteCalls || 0 != storage.loadCalls {
        t.Fatalf("expected no storage call, got load=%d save=%d delete=%d", storage.loadCalls, storage.saveCalls, storage.deleteCalls)
    }
}

func TestManager_RegenerateSessionRefusesAnUnavailableSession(t *testing.T) {
    storage := &storageCallCountingStorage{}
    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Hour)})

    cause := errors.New("session store is down")

    rotated, refusalErr := manager.RegenerateSession(sessionRuntime(), NewUnavailableSession(cause))
    if nil != rotated {
        t.Fatalf("expected no rotated session, got %v", rotated)
    }

    requireUnavailableSessionRefusal(t, refusalErr, cause)

    if 0 != storage.loadCalls || 0 != storage.deleteCalls {
        t.Fatalf("expected no probe and no delete, got load=%d delete=%d", storage.loadCalls, storage.deleteCalls)
    }
}

type doneObservingContext struct {
    context.Context
    observeOnce sync.Once
    observed    chan struct{}
}

func newDoneObservingContext(parent context.Context) *doneObservingContext {
    return &doneObservingContext{Context: parent, observed: make(chan struct{})}
}

func (instance *doneObservingContext) Done() <-chan struct{} {
    instance.observeOnce.Do(func() { close(instance.observed) })

    return instance.Context.Done()
}

func awaitSignal(t *testing.T, signal <-chan struct{}, what string) {
    t.Helper()

    select {
    case <-signal:
    case <-time.After(5 * time.Second):
        t.Fatalf("timed out waiting for %s", what)
    }
}

func awaitDoor(t *testing.T, entered <-chan string, door string) {
    t.Helper()

    select {
    case reached := <-entered:
        if door != reached {
            t.Fatalf("expected the storage to be entered at %s, got %s", door, reached)
        }
    case <-time.After(5 * time.Second):
        t.Fatalf("timed out waiting for the storage's %s", door)
    }
}

func awaitError(t *testing.T, result <-chan error) error {
    t.Helper()

    select {
    case err := <-result:
        return err
    case <-time.After(5 * time.Second):
        t.Fatalf("timed out waiting for the door to return")
    }

    return nil
}

func requireContextEnded(t *testing.T, err error, cause error) {
    t.Helper()

    if nil == err || false == exception.ChainIs(err, cause) {
        t.Fatalf("expected an error carrying %v, got %v", cause, err)
    }

    if false == exception.IsInfrastructureFailure(err) || false == exception.AnswersUnavailable(err) {
        t.Fatalf("expected the error marked as an infrastructure failure, got %v", err)
    }
}

func storedTestSession(sessionId string) *Session {
    return &Session{id: sessionId, values: map[string]any{"userId": "u-1"}, modified: true}
}

func TestManager_EveryDoorRefusesAnAbsentRuntime(t *testing.T) {
    storage := &storageCallCountingStorage{}
    manager := NewManager(storage, ManagerConfig{})

    for _, refused := range refusedSessionRuntimes() {
        refusals := []error{}

        _, sessionErr := manager.Session(refused, "malformed")
        refusals = append(refusals, sessionErr)

        _, newErr := manager.NewSession(refused)
        refusals = append(refusals, newErr)

        refusals = append(refusals, manager.SaveSession(refused, nil))
        refusals = append(refusals, manager.DeleteSession(refused, "malformed"))

        _, regenerateErr := manager.RegenerateSession(refused, nil)
        refusals = append(refusals, regenerateErr)

        for index, refusal := range refusals {
            requireErrorMessage(t, refusal, refusedRuntimeMessage)

            if true == exception.IsInfrastructureFailure(refusal) {
                t.Fatalf("expected door %d's refusal unmarked, got %v", index, refusal)
            }
        }
    }

    if 0 != storage.loadCalls+storage.saveCalls+storage.deleteCalls {
        t.Fatalf("expected no storage call, got %d loads, %d saves and %d deletes", storage.loadCalls, storage.saveCalls, storage.deleteCalls)
    }
}

func TestManager_ADoneContextNeverReachesTheStorage(t *testing.T) {
    storage := &storageCallCountingStorage{}
    manager := NewManager(storage, ManagerConfig{})
    sessionId := generateSessionId()

    _, sessionErr := manager.Session(sessionRuntimeUnder(cancelledContext()), sessionId)
    requireContextEnded(t, sessionErr, context.Canceled)

    _, newErr := manager.NewSession(sessionRuntimeUnder(cancelledContext()))
    requireContextEnded(t, newErr, context.Canceled)

    requireContextEnded(t, manager.SaveSession(sessionRuntimeUnder(cancelledContext()), storedTestSession(sessionId)), context.Canceled)

    requireContextEnded(t, manager.DeleteSession(sessionRuntimeUnder(cancelledContext()), sessionId), context.Canceled)

    rotated := storedTestSession(sessionId)
    rotated.modified = false
    _, regenerateErr := manager.RegenerateSession(sessionRuntimeUnder(cancelledContext()), rotated)
    requireContextEnded(t, regenerateErr, context.Canceled)

    if true == rotated.IsCleared() {
        t.Fatalf("expected the session handed to a cut rotation not to be cleared")
    }

    if 0 != storage.loadCalls+storage.saveCalls+storage.deleteCalls {
        t.Fatalf("expected no storage call, got %d loads, %d saves and %d deletes", storage.loadCalls, storage.saveCalls, storage.deleteCalls)
    }
}

func TestManager_TheArgumentsAreJudgedBeforeADoneContext(t *testing.T) {
    storage := &storageCallCountingStorage{}
    manager := NewManager(storage, ManagerConfig{})

    if loaded, loadErr := manager.Session(sessionRuntimeUnder(cancelledContext()), "malformed"); nil != loaded || nil != loadErr {
        t.Fatalf("expected a malformed id to answer nothing, got %v and %v", loaded, loadErr)
    }

    requireErrorMessage(t, manager.SaveSession(sessionRuntimeUnder(cancelledContext()), nil), "session is nil in save session")

    cause := errors.New("session store is down")
    unavailableErr := manager.SaveSession(sessionRuntimeUnder(cancelledContext()), NewUnavailableSession(cause))
    if false == exception.ChainIs(unavailableErr, ErrSessionUnavailable) || true == exception.ChainIs(unavailableErr, context.Canceled) {
        t.Fatalf("expected the unavailable refusal, got %v", unavailableErr)
    }

    requireErrorMessage(t, manager.DeleteSession(sessionRuntimeUnder(cancelledContext()), "malformed"), "session id is invalid in delete session")

    unmodified := storedTestSession(generateSessionId())
    unmodified.modified = false
    if saveErr := manager.SaveSession(sessionRuntimeUnder(cancelledContext()), unmodified); nil != saveErr {
        t.Fatalf("expected an unmodified session to be a no-op, got %v", saveErr)
    }

    minted := &Session{id: generateSessionId(), values: map[string]any{}, minted: true}
    minted.Clear()
    if saveErr := manager.SaveSession(sessionRuntimeUnder(cancelledContext()), minted); nil != saveErr {
        t.Fatalf("expected a minted cleared session to be a no-op, got %v", saveErr)
    }

    if 0 != storage.loadCalls+storage.saveCalls+storage.deleteCalls {
        t.Fatalf("expected no storage call, got %d loads, %d saves and %d deletes", storage.loadCalls, storage.saveCalls, storage.deleteCalls)
    }
}

type plantedContextKey struct{}

func TestManager_HandsTheStorageTheCallersContext(t *testing.T) {
    storage := newContextRecordingStorage()
    defer storage.CloseWithContext(context.Background())

    manager := NewManager(storage, ManagerConfig{})
    ctx := context.WithValue(context.Background(), plantedContextKey{}, "planted")

    created := mustSession(manager.NewSession(sessionRuntimeUnder(ctx)))
    created.Set("userId", "u-1")
    if saveErr := manager.SaveSession(sessionRuntimeUnder(ctx), created); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if nil == mustSession(manager.Session(sessionRuntimeUnder(ctx), created.Id())) {
        t.Fatalf("expected the saved session to load")
    }

    if deleteErr := manager.DeleteSession(sessionRuntimeUnder(ctx), created.Id()); nil != deleteErr {
        t.Fatalf("unexpected delete error: %v", deleteErr)
    }

    cleared := mustSession(manager.NewSession(sessionRuntimeUnder(ctx)))
    cleared.Set("userId", "u-2")
    if saveErr := manager.SaveSession(sessionRuntimeUnder(ctx), cleared); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    clearedCopy := mustSession(manager.Session(sessionRuntimeUnder(ctx), cleared.Id()))
    clearedCopy.Clear()
    if saveErr := manager.SaveSession(sessionRuntimeUnder(ctx), clearedCopy); nil != saveErr {
        t.Fatalf("unexpected save error of the cleared session: %v", saveErr)
    }

    if 2 != len(storage.recorded("Delete")) {
        t.Fatalf("expected the cleared session removed through the storage's Delete, got %d deletes", len(storage.recorded("Delete")))
    }

    for _, door := range []string{"Load", "Save", "Delete"} {
        received := storage.recorded(door)
        if 0 == len(received) {
            t.Fatalf("expected the storage's %s to be called", door)
        }

        for _, receivedContext := range received {
            if "planted" != receivedContext.Value(plantedContextKey{}) {
                t.Fatalf("expected the storage's %s to receive the caller's context", door)
            }
        }
    }

    if 2 > len(storage.recorded("Load")) {
        t.Fatalf("expected both the mint probe and the read to reach Load")
    }
}

func TestManager_ALoadBlockedInTheStorageEndsWithItsContext(t *testing.T) {
    storage := newContextBlockingStorage()
    storage.blockLoad = true
    manager := NewManager(storage, ManagerConfig{})

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    result := make(chan error, 1)
    go func() {
        _, loadErr := manager.Session(sessionRuntimeUnder(ctx), generateSessionId())
        result <- loadErr
    }()

    awaitDoor(t, storage.entered, "Load")
    cancel()

    requireContextEnded(t, awaitError(t, result), context.Canceled)
}

func holdStripeWithAHungSave(t *testing.T, manager *Manager, storage *contextBlockingStorage, sessionId string) func() {
    t.Helper()

    holderContext, releaseHolder := context.WithCancel(context.Background())
    holderDone := make(chan struct{})

    go func() {
        defer close(holderDone)
        _ = manager.SaveSession(sessionRuntimeUnder(holderContext), storedTestSession(sessionId))
    }()

    awaitDoor(t, storage.entered, "Save")

    return func() {
        releaseHolder()
        awaitSignal(t, holderDone, "the holder to leave")
    }
}

func TestManager_AStripeHeldByAHungStorageCutsTheSecondCallerAtItsContext(t *testing.T) {
    storage := newContextBlockingStorage()
    storage.blockSave = true
    manager := NewManager(storage, ManagerConfig{})
    sessionId := generateSessionId()

    release := holdStripeWithAHungSave(t, manager, storage, sessionId)

    waiting, cancel := context.WithCancel(context.Background())
    defer cancel()
    observed := newDoneObservingContext(waiting)

    result := make(chan error, 1)
    go func() {
        result <- manager.DeleteSession(sessionRuntimeUnder(observed), sessionId)
    }()

    awaitSignal(t, observed.observed, "the delete to wait for the stripe")
    cancel()

    cutErr := awaitError(t, result)
    requireContextEnded(t, cutErr, context.Canceled)
    requireErrorMessage(t, cutErr, "the session operation was cut by its context")

    if "DeleteSession" != exception.LogContext(cutErr, exception.LogContextOptions{})["door"] {
        t.Fatalf("expected the cut to name its door, got %v", exception.LogContext(cutErr, exception.LogContextOptions{}))
    }

    release()

    for _, door := range storage.recordedDoors() {
        if "Delete" == door {
            t.Fatalf("expected a cut wait never to reach the storage's Delete")
        }
    }

    if true == manager.isTombstoned(sessionId) {
        t.Fatalf("expected a cut delete to bury nothing")
    }

    storage.blockSave = false
    if saveErr := manager.SaveSession(sessionRuntime(), storedTestSession(sessionId)); nil != saveErr {
        t.Fatalf("expected a fresh copy of the id to save after a cut delete, got %v", saveErr)
    }
}

func TestManager_AStripeHeldByAHungStorageCutsASecondSaveAtItsDeadline(t *testing.T) {
    storage := newContextBlockingStorage()
    storage.blockSave = true
    manager := NewManager(storage, ManagerConfig{})
    sessionId := generateSessionId()

    release := holdStripeWithAHungSave(t, manager, storage, sessionId)
    defer release()

    deadlineContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
    defer cancel()

    second := &Session{id: sessionId, values: map[string]any{"userId": "u-2"}, modified: true, minted: true}
    requireContextEnded(t, manager.SaveSession(sessionRuntimeUnder(deadlineContext), second), context.DeadlineExceeded)

    if false == second.isMinted() {
        t.Fatalf("expected a save cut before the storage not to mark the session stored")
    }
}

func TestManager_ACutWaitLeavesTheStripeFree(t *testing.T) {
    storage := newContextBlockingStorage()
    storage.blockSave = true
    manager := NewManager(storage, ManagerConfig{})
    sessionId := generateSessionId()

    release := holdStripeWithAHungSave(t, manager, storage, sessionId)

    waiting, cancel := context.WithCancel(context.Background())
    observed := newDoneObservingContext(waiting)

    result := make(chan error, 1)
    go func() {
        result <- manager.SaveSession(sessionRuntimeUnder(observed), storedTestSession(sessionId))
    }()

    awaitSignal(t, observed.observed, "the save to wait for the stripe")
    cancel()
    requireContextEnded(t, awaitError(t, result), context.Canceled)

    release()

    storage.blockSave = false
    boundedContext, cancelBounded := context.WithTimeout(context.Background(), time.Second)
    defer cancelBounded()

    if saveErr := manager.SaveSession(sessionRuntimeUnder(boundedContext), storedTestSession(sessionId)); nil != saveErr {
        t.Fatalf("expected a third caller to take the stripe at once, got %v", saveErr)
    }
}

type burialProbeStorage struct {
    inner               *InMemoryStorage
    removeThenBlock     bool
    failDelete          bool
    blockArmedLoad      bool
    panicArmedLoad      bool
    armed               atomic.Bool
    deleteEntered       chan struct{}
    mutex               sync.Mutex
    armedLoadContexts   []context.Context
    armedLoadRuntimes   []containercontract.Runtime
    armedLoadErrAtEntry []error
}

func newBurialProbeStorage() *burialProbeStorage {
    return &burialProbeStorage{
        inner:         NewInMemoryStorage(InMemoryStorageConfig{}),
        deleteEntered: make(chan struct{}, 1),
    }
}

func (instance *burialProbeStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    ctx := runtimeInstance.Context()
    if true == instance.armed.Load() {
        instance.mutex.Lock()
        instance.armedLoadContexts = append(instance.armedLoadContexts, ctx)
        instance.armedLoadRuntimes = append(instance.armedLoadRuntimes, runtimeInstance)
        instance.armedLoadErrAtEntry = append(instance.armedLoadErrAtEntry, ctx.Err())
        instance.mutex.Unlock()

        if true == instance.panicArmedLoad {
            panic("the burial read panicked")
        }

        if true == instance.blockArmedLoad {
            <-ctx.Done()

            return nil, false, ctx.Err()
        }
    }

    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *burialProbeStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *burialProbeStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    ctx := runtimeInstance.Context()
    instance.deleteEntered <- struct{}{}

    if true == instance.failDelete {
        return errors.New("the removal failed")
    }

    if removeErr := instance.inner.Delete(sessionRuntime(), sessionId); nil != removeErr {
        return removeErr
    }

    if true == instance.removeThenBlock {
        <-ctx.Done()

        return ctx.Err()
    }

    return nil
}

func (instance *burialProbeStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(closeContext)
}

func (instance *burialProbeStorage) recordedLoads() ([]context.Context, []error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]context.Context{}, instance.armedLoadContexts...), append([]error{}, instance.armedLoadErrAtEntry...)
}

func seedBurialProbe(t *testing.T, manager *Manager, storage *burialProbeStorage) (string, sessioncontract.Session) {
    t.Helper()

    seeded := mustSession(manager.NewSession(sessionRuntime()))
    seeded.Set("userId", "u-1")
    if saveErr := manager.SaveSession(sessionRuntime(), seeded); nil != saveErr {
        t.Fatalf("unexpected seed error: %v", saveErr)
    }

    copyLoaded := mustSession(manager.Session(sessionRuntime(), seeded.Id()))
    storage.armed.Store(true)

    return seeded.Id(), copyLoaded
}

func TestManager_TheBurialReadOutlivesTheContextThatCutTheRemoval(t *testing.T) {
    storage := newBurialProbeStorage()
    storage.removeThenBlock = true
    manager := NewManager(storage, ManagerConfig{})
    sessionId, copyLoaded := seedBurialProbe(t, manager, storage)

    ctx, cancel := context.WithCancel(context.WithValue(context.Background(), plantedContextKey{}, "planted"))
    defer cancel()

    callerScope := container.NewContainer(container.ContainerConfig{}).NewScope()
    callerRuntime := runtime.NewRequest(ctx, callerScope)

    result := make(chan error, 1)
    go func() {
        result <- manager.DeleteSession(callerRuntime, sessionId)
    }()

    awaitSignal(t, storage.deleteEntered, "the removal")
    cancel()

    requireContextEnded(t, awaitError(t, result), context.Canceled)

    storage.mutex.Lock()
    burialRuntime := storage.armedLoadRuntimes[0]
    storage.mutex.Unlock()
    if callerScope != burialRuntime.Scope() || containercontract.KindRequest != burialRuntime.Kind() {
        t.Fatalf("expected the burial read under the caller's scope and kind, got %s", burialRuntime.Kind())
    }

    received, errorsAtEntry := storage.recordedLoads()
    if 1 != len(received) || nil != errorsAtEntry[0] {
        t.Fatalf("expected one burial read under a live context, got %d reads and %v", len(received), errorsAtEntry)
    }

    if "planted" != received[0].Value(plantedContextKey{}) {
        t.Fatalf("expected the burial read to carry the caller's values")
    }

    copyLoaded.Set("userId", "u-2")
    if saveErr := manager.SaveSession(sessionRuntime(), copyLoaded); false == exception.ChainIs(saveErr, ErrSessionDeleted) {
        t.Fatalf("expected the removed id buried, got %v", saveErr)
    }
}

func TestManager_TheBurialReadContextIsReleasedOnceTheReadAnswers(t *testing.T) {
    storage := newBurialProbeStorage()
    storage.failDelete = true
    manager := NewManager(storage, ManagerConfig{StorageTimeout: bound.Of(time.Hour)})
    sessionId, _ := seedBurialProbe(t, manager, storage)

    if deleteErr := manager.DeleteSession(sessionRuntime(), sessionId); nil == deleteErr {
        t.Fatalf("expected the failed removal to be answered")
    }

    received, _ := storage.recordedLoads()
    if 1 != len(received) {
        t.Fatalf("expected one burial read, got %d", len(received))
    }

    if false == errors.Is(received[0].Err(), context.Canceled) {
        t.Fatalf("expected the burial read's context released, got %v", received[0].Err())
    }
}

func TestManager_TheBurialReadIsBoundedByTheStorageTimeout(t *testing.T) {
    storage := newBurialProbeStorage()
    storage.failDelete = true
    storage.blockArmedLoad = true
    manager := NewManager(storage, ManagerConfig{StorageTimeout: bound.Of(50 * time.Millisecond)})
    sessionId, _ := seedBurialProbe(t, manager, storage)

    start := time.Now()
    result := make(chan error, 1)
    go func() {
        result <- manager.DeleteSession(sessionRuntime(), sessionId)
    }()

    if deleteErr := awaitError(t, result); nil == deleteErr {
        t.Fatalf("expected the failed removal to be answered")
    }

    received, _ := storage.recordedLoads()
    if 1 != len(received) {
        t.Fatalf("expected one burial read, got %d", len(received))
    }

    deadline, hasDeadline := received[0].Deadline()
    if false == hasDeadline {
        t.Fatalf("expected the burial read bounded by a deadline")
    }

    if time.Second < deadline.Sub(start) {
        t.Fatalf("expected the burial read bounded by the configured 50ms, got %s", deadline.Sub(start))
    }

    if true == manager.isTombstoned(sessionId) {
        t.Fatalf("expected a burial read that timed out to leave the id unburied")
    }
}

func TestManager_APanickingBurialReadLeavesNoTimerArmed(t *testing.T) {
    storage := newBurialProbeStorage()
    storage.failDelete = true
    storage.panicArmedLoad = true
    manager := NewManager(storage, ManagerConfig{StorageTimeout: bound.Of(time.Hour)})
    sessionId, _ := seedBurialProbe(t, manager, storage)

    func() {
        defer func() { _ = recover() }()

        _ = manager.DeleteSession(sessionRuntime(), sessionId)
    }()

    received, _ := storage.recordedLoads()
    if 1 != len(received) || false == errors.Is(received[0].Err(), context.Canceled) {
        t.Fatalf("expected the burial read's context released through the panic, got %d reads", len(received))
    }
}

type stripeHeldEndsContext struct {
    context.Context
    stripe chan struct{}
}

func (instance *stripeHeldEndsContext) Err() error {
    if 0 < len(instance.stripe) {
        return context.Canceled
    }

    return nil
}

func TestManager_AContextEndedAsTheStripeIsWonReachesNoStorage(t *testing.T) {
    storage := &storageCallCountingStorage{}
    manager := NewManager(storage, ManagerConfig{})
    sessionId := generateSessionId()
    stripe := manager.sessionStripeOf(sessionId)

    saveErr := manager.SaveSession(sessionRuntimeUnder(&stripeHeldEndsContext{Context: context.Background(), stripe: stripe}), storedTestSession(sessionId))
    requireContextEnded(t, saveErr, context.Canceled)

    if calls := storage.loadCalls + storage.saveCalls + storage.deleteCalls; 0 != calls || 0 != len(stripe) {
        t.Fatalf("expected no storage call and a free stripe, got %d calls and %d held", calls, len(stripe))
    }
}

func TestManager_AnUnlimitedStorageTimeoutGivesTheBurialReadNoDeadline(t *testing.T) {
    storage := newBurialProbeStorage()
    storage.failDelete = true
    manager := NewManager(storage, ManagerConfig{StorageTimeout: bound.UnlimitedDuration()})
    sessionId, _ := seedBurialProbe(t, manager, storage)

    if deleteErr := manager.DeleteSession(sessionRuntime(), sessionId); nil == deleteErr {
        t.Fatalf("expected the failed removal to be answered")
    }

    received, _ := storage.recordedLoads()
    if 1 != len(received) {
        t.Fatalf("expected one burial read, got %d", len(received))
    }

    if _, hasDeadline := received[0].Deadline(); true == hasDeadline {
        t.Fatalf("expected an unlimited bound to give the burial read no deadline")
    }
}

func TestNewManager_PanicsOnAStorageTimeoutBelowTheMinimum(t *testing.T) {
    defer func() {
        recoveredErr, isError := recover().(error)
        if false == isError || "ManagerConfig.StorageTimeout" != exception.LogContext(recoveredErr, exception.LogContextOptions{})["field"] {
            t.Fatalf("expected the refusal to name ManagerConfig.StorageTimeout, got %v", recoveredErr)
        }
    }()

    _ = NewManager(&nilMapStorage{}, ManagerConfig{StorageTimeout: bound.Of(time.Microsecond)})
}

func TestNewManager_DefaultsTheStorageTimeoutToFiveSeconds(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{})

    if bound.Of(5*time.Second) != manager.StorageTimeout() || 5*time.Second != DefaultStorageTimeout {
        t.Fatalf("expected the default storage timeout to be 5s, got %s", manager.StorageTimeout())
    }
}

func TestNewManager_AdmitsAnUnlimitedStorageTimeout(t *testing.T) {
    manager := NewManager(&nilMapStorage{}, ManagerConfig{StorageTimeout: bound.UnlimitedDuration()})

    if false == manager.StorageTimeout().IsUnlimited() {
        t.Fatalf("expected an unlimited storage timeout, got %s", manager.StorageTimeout())
    }
}

type declaredBoundManager struct {
    *Manager
    declared bound.Duration
}

func (instance *declaredBoundManager) StorageTimeout() bound.Duration {
    return instance.declared
}

func TestResolveStorageTimeout_JudgesEveryManagerByTheRule(t *testing.T) {
    own := NewManager(&nilMapStorage{}, ManagerConfig{StorageTimeout: bound.Of(2 * time.Second)})
    if resolved, err := ResolveStorageTimeout(own); nil != err || 2*time.Second != resolved.Value {
        t.Fatalf("expected the manager's own bound, got %v and %v", resolved, err)
    }

    absent := &declaredBoundManager{Manager: own}
    if resolved, err := ResolveStorageTimeout(absent); nil != err || DefaultStorageTimeout != resolved.Value {
        t.Fatalf("expected an absent bound to be the default, got %v and %v", resolved, err)
    }

    below := &declaredBoundManager{Manager: own, declared: bound.Of(time.Microsecond)}
    if _, err := ResolveStorageTimeout(below); nil == err || "ManagerConfig.StorageTimeout" != exception.LogContext(err, exception.LogContextOptions{})["field"] {
        t.Fatalf("expected a bound below the minimum refused naming the field, got %v", err)
    }

    unlimited := &declaredBoundManager{Manager: own, declared: bound.UnlimitedDuration()}
    if resolved, err := ResolveStorageTimeout(unlimited); nil != err || false == resolved.Unlimited {
        t.Fatalf("expected unlimited admitted, got %v and %v", resolved, err)
    }

    var nilManager *Manager
    if _, err := ResolveStorageTimeout(nilManager); nil == err {
        t.Fatalf("expected a nil manager refused")
    }
}

type cancellingProbeStorage struct {
    *InMemoryStorage
    cancelAfterLoad atomic.Pointer[context.CancelFunc]
    deleteCalls     atomic.Int32
}

func (instance *cancellingProbeStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    data, exists, err := instance.InMemoryStorage.Load(runtimeInstance, sessionId)

    if cancel := instance.cancelAfterLoad.Swap(nil); nil != cancel {
        (*cancel)()
    }

    return data, exists, err
}

func (instance *cancellingProbeStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    instance.deleteCalls.Add(1)

    return instance.InMemoryStorage.Delete(runtimeInstance, sessionId)
}

func TestManager_RegenerateSessionCutBeforeTheRemovalLeavesTheSessionIntact(t *testing.T) {
    storage := &cancellingProbeStorage{InMemoryStorage: NewInMemoryStorage(InMemoryStorageConfig{})}
    manager := NewManager(storage, ManagerConfig{})

    seeded := mustSession(manager.NewSession(sessionRuntime()))
    seeded.Set("userId", "u-1")
    if saveErr := manager.SaveSession(sessionRuntime(), seeded); nil != saveErr {
        t.Fatalf("unexpected seed error: %v", saveErr)
    }

    loaded := mustSession(manager.Session(sessionRuntime(), seeded.Id()))

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    storage.cancelAfterLoad.Store(&cancel)

    _, regenerateErr := manager.RegenerateSession(sessionRuntimeUnder(ctx), loaded)
    requireContextEnded(t, regenerateErr, context.Canceled)

    if true == loaded.IsCleared() {
        t.Fatalf("expected the session of a cut rotation not to be cleared")
    }

    if nil == mustSession(manager.Session(sessionRuntime(), seeded.Id())) {
        t.Fatalf("expected the stored entry to survive a cut rotation")
    }

    if 0 != storage.deleteCalls.Load() || true == manager.isTombstoned(seeded.Id()) {
        t.Fatalf("expected a cut rotation to remove and bury nothing")
    }
}

/* scopelessSessionRuntime carries a context and no scope, which only a runtime of another implementation can */
type scopelessSessionRuntime struct {
    containercontract.Runtime
}

func (instance scopelessSessionRuntime) Context() context.Context {
    return context.Background()
}

func (instance scopelessSessionRuntime) Scope() containercontract.Scope {
    return nil
}

func TestManager_EveryDoorRefusesARuntimeWithoutAScope(t *testing.T) {
    storage := &storageCallCountingStorage{}
    manager := NewManager(storage, ManagerConfig{})
    scopeless := scopelessSessionRuntime{}

    _, sessionErr := manager.Session(scopeless, "malformed")
    _, newErr := manager.NewSession(scopeless)
    _, regenerateErr := manager.RegenerateSession(scopeless, nil)

    for _, refusal := range []error{sessionErr, newErr, manager.SaveSession(scopeless, nil), manager.DeleteSession(scopeless, "malformed"), regenerateErr} {
        requireErrorMessage(t, refusal, "the runtime carries no scope")
    }

    if 0 != storage.loadCalls+storage.saveCalls+storage.deleteCalls {
        t.Fatalf("expected no storage call under a scopeless runtime")
    }
}

func TestContextEndedError_NamesTheSessionReferenceUnderItsOneKey(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    endedErr := contextEndedError(ctx, "Manager.SaveSession", "session-123")
    errorContext := exception.FromError(endedErr).Context()

    if sessionIdLogReference("session-123") != errorContext["sessionReference"] {
        t.Fatalf("expected the reference of the session, got %v", errorContext)
    }

    if _, hasRetiredKey := errorContext["sessionRef"]; true == hasRetiredKey {
        t.Fatalf("expected the reference under sessionReference only, got %v", errorContext)
    }

    if _, named := exception.FromError(contextEndedError(ctx, "Manager.SaveSession", "")).Context()["sessionReference"]; true == named {
        t.Fatal("expected no reference without a session id")
    }
}
