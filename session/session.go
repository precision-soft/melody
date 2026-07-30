package session

import (
    "crypto/rand"
    "encoding/hex"
    "sync"

    "github.com/precision-soft/melody/exception"
    "github.com/precision-soft/melody/internal"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

type Session struct {
    id        string
    mutex     sync.RWMutex
    values    map[string]any
    modified  bool
    cleared   bool
    abandoned bool
}

func (instance *Session) Id() string {
    return instance.id
}

func (instance *Session) Get(key string) any {
    instance.mutex.RLock()
    value, exists := instance.values[key]
    instance.mutex.RUnlock()

    if false == exists {
        return nil
    }

    return value
}

func (instance *Session) String(key string) string {
    value := instance.Get(key)
    if nil == value {
        return ""
    }

    stringValue, ok := value.(string)
    if false == ok {
        return ""
    }

    return stringValue
}

func (instance *Session) Set(key string, value any) {
    instance.mutex.Lock()
    instance.values[key] = value
    instance.modified = true
    instance.cleared = false
    instance.mutex.Unlock()
}

func (instance *Session) Has(key string) bool {
    instance.mutex.RLock()
    _, exists := instance.values[key]
    instance.mutex.RUnlock()

    return exists
}

func (instance *Session) Delete(key string) {
    instance.mutex.Lock()
    _, exists := instance.values[key]
    if true == exists {
        delete(instance.values, key)
        instance.modified = true
    }
    instance.mutex.Unlock()
}

/* Clear ends the session, and the ending latches: a later Set puts a value back and marks the session modified, but it cannot make the session look live again. Without the latch a logout handler that clears the session and is followed by anything writing to the same object — a middleware or an event listener leaving a farewell message — had the response path take the save branch instead of the delete branch, so the values were overwritten but the pre-logout id stayed alive in the storage and was re-issued to the browser under the same cookie. A caller that wants a usable session after clearing one asks the manager for a new session. */
func (instance *Session) Clear() {
    instance.mutex.Lock()
    instance.values = make(map[string]any)
    instance.modified = true
    instance.cleared = true
    instance.abandoned = true
    instance.mutex.Unlock()
}

/* All hands out a copy that reaches all the way down, the depth both storages already copy at. A copy of only the top level would hand the caller the very map or slice a nested value holds, so mutating it would change the live session without passing through Set — the session would not be marked modified and the change would never be persisted, while a caller that mutates it after the response path has handed the same value to the storage races the copy the storage makes. */
func (instance *Session) All() map[string]any {
    instance.mutex.RLock()
    result := internal.CopyAnyMap(instance.values)
    instance.mutex.RUnlock()

    return result
}

func (instance *Session) IsModified() bool {
    instance.mutex.RLock()
    value := instance.modified
    instance.mutex.RUnlock()

    return value
}

func (instance *Session) IsCleared() bool {
    instance.mutex.RLock()
    value := instance.cleared || instance.abandoned
    instance.mutex.RUnlock()

    return value
}

var _ sessioncontract.Session = (*Session)(nil)

func generateSessionId() string {
    bytes := make([]byte, 16)

    readCount, err := rand.Read(bytes)
    if nil != err {
        exception.Panic(
            exception.NewError("could not generate session id", nil, err),
        )
    }

    if 16 != readCount {
        exception.Panic(
            exception.NewError("generated invalid session id", nil, nil),
        )
    }

    return hex.EncodeToString(bytes)
}

func isValidSessionId(sessionId string) bool {
    if 32 != len(sessionId) {
        return false
    }

    for index := 0; index < len(sessionId); index++ {
        character := sessionId[index]

        isLowerHex := 'a' <= character && 'f' >= character
        isDigit := '0' <= character && '9' >= character

        if false == isLowerHex && false == isDigit {
            return false
        }
    }

    return true
}
