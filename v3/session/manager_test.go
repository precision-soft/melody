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
    testhelper.AssertPanics(t, func() {
        _ = NewManager(nil, time.Minute)
    })
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
        if expected != isValidSessionId(value) {
            t.Fatalf("isValidSessionId(%q) = %v, want %v", value, !expected, expected)
        }
    }
}

func TestSession_AllReturnsCopy(t *testing.T) {
    sessionInstance := &Session{
        id:       "id",
        values:   map[string]any{"a": "b"},
        modified: false,
        cleared:  false,
    }

    all := sessionInstance.All()
    all["a"] = "changed"

    if "b" != sessionInstance.values["a"].(string) {
        t.Fatalf("expected isolation")
    }
}

func TestSession_DeleteMarksModifiedOnlyWhenKeyExists(t *testing.T) {
    sessionInstance := &Session{
        id:       "id",
        values:   map[string]any{},
        modified: false,
        cleared:  false,
    }

    sessionInstance.Delete("missing")
    if true == sessionInstance.IsModified() {
        t.Fatalf("expected not modified")
    }

    sessionInstance.Set("a", "b")
    if false == sessionInstance.IsModified() {
        t.Fatalf("expected modified")
    }
}

var _ = exception.NewError
