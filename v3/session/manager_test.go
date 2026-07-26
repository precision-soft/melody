package session

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal/testhelper"
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

func TestManager_NewSession_HasId(t *testing.T) {
    storage := NewInMemoryStorage()
    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()
    if "" == sessionInstance.Id() {
        t.Fatalf("expected id")
    }
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

func TestIsValidSessionId(t *testing.T) {
    cases := map[string]bool{
        "":                                  false,
        "abc":                               false,
        "0123456789abcdef0123456789abcdef":  true,
        "0123456789ABCDEF0123456789ABCDEF":  false,
        "0123456789abcdef0123456789abcdeg":  false,
        "0123456789abcdef0123456789abcde ":  false,
        "0123456789abcdef0123456789abcde":   false,
        "0123456789abcdef0123456789abcdef0": false,
    }

    for value, expected := range cases {
        if result := isValidSessionId(value); expected != result {
            t.Fatalf("isValidSessionId(%q) = %v, want %v", value, result, expected)
        }
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

func TestManager_RegenerateSession_CarriesASharedValueOverAsTheSameHandle(t *testing.T) {
    storage := NewInMemoryStorage()
    defer storage.Close()

    manager := NewManager(storage, 30*time.Minute)

    sessionInstance := manager.NewSession()

    handle := &sharedCounter{count: 1}
    sessionInstance.SetShared("counter", handle)

    saveErr := manager.SaveSession(sessionInstance)
    if nil != saveErr {
        t.Fatalf("expected the session to be saved, got %v", saveErr)
    }

    rotatedSession, regenerateErr := manager.RegenerateSession(sessionInstance)
    if nil != regenerateErr {
        t.Fatalf("expected the session to rotate, got %v", regenerateErr)
    }

    if handle != rotatedSession.Get("counter") {
        t.Fatalf("expected the rotated session to carry the very handle over")
    }

    saveErr = manager.SaveSession(rotatedSession)
    if nil != saveErr {
        t.Fatalf("expected the rotated session to be saved, got %v", saveErr)
    }

    reloadedSession := manager.Session(rotatedSession.Id())
    if nil == reloadedSession {
        t.Fatalf("expected the rotated session to load")
    }

    if handle != reloadedSession.Get("counter") {
        t.Fatalf("expected the shared value to stay shared through rotation and storage")
    }
}
