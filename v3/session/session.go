package session

import (
    "crypto/rand"
    "encoding/hex"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

type Session struct {
    id       string
    mutex    sync.RWMutex
    values   map[string]any
    modified bool
    cleared  bool
}

func (instance *Session) Id() string {
    return instance.id
}

/* Get hands out a copy at the depth All copies at: a live nested value mutated in place would change the session without Set marking it modified, so the change would never persist. Read, mutate the copy, Set it back. */
func (instance *Session) Get(key string) any {
    instance.mutex.RLock()
    value, exists := instance.values[key]
    instance.mutex.RUnlock()

    if false == exists {
        return nil
    }

    return internal.CopyAnyValue(value)
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

/* Set stores its own deep copy, so a caller still writing to the value it handed over cannot race the copy the response path makes. */
func (instance *Session) Set(key string, value any) {
    ownedValue := internal.CopyAnyValue(value)

    instance.mutex.Lock()
    instance.values[key] = ownedValue
    instance.modified = true
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

/* Clear ends the session, and the ending latches: a later Set puts a value back and marks it modified but cannot make the session live again, so the response path deletes it rather than saving it under its id. A usable session after clearing comes from the manager. A Clear is guaranteed effective only before the handler returns, since the response path decides from one Snapshot. */
func (instance *Session) Clear() {
    instance.mutex.Lock()
    instance.values = make(map[string]any)
    instance.modified = true
    instance.cleared = true
    instance.mutex.Unlock()
}

/* All hands out a deep copy, the depth both storages copy at, so mutating it cannot change the live session without Set. */
func (instance *Session) All() map[string]any {
    instance.mutex.RLock()
    result := internal.CopyAnyMap(instance.values)
    instance.mutex.RUnlock()

    return result
}

/* Snapshot reads the values, the modified flag and the cleared flag under one lock acquisition, so a concurrent Clear cannot land between them. */
func (instance *Session) Snapshot() (map[string]any, bool, bool) {
    instance.mutex.RLock()
    values := internal.CopyAnyMap(instance.values)
    modified := instance.modified
    cleared := instance.cleared
    instance.mutex.RUnlock()

    return values, modified, cleared
}

func (instance *Session) IsModified() bool {
    instance.mutex.RLock()
    value := instance.modified
    instance.mutex.RUnlock()

    return value
}

func (instance *Session) IsCleared() bool {
    instance.mutex.RLock()
    value := instance.cleared
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
