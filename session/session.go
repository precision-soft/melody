package session

import (
    "crypto/rand"
    "encoding/hex"
    "sync"

    "github.com/precision-soft/melody/exception"
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

/* sharedValue is the envelope a SetShared write leaves in the values map. The storage layer defends the values it holds by deep-copying them, which is what keeps a session read on one request from writing into another's data — and which would also dissolve a handle that was shared on purpose. The envelope survives that copy because it has nothing copyable to offer: the deep copy walks exported fields only and returns a struct with none of them exactly as it found it, so the handle inside arrives as itself.

It is unexported and carries no exported field, so the only way one enters a values map is SetShared. That is what lets a storage which cannot represent a handle recognise the values it must refuse rather than persist a lookalike of. */
type sharedValue struct {
    value any
}

func (instance *Session) Id() string {
    return instance.id
}

/* Get returns a copy for a value stored with Set — a write through it is lost; a handle that must stay the one value needs SetShared. */
func (instance *Session) Get(key string) any {
    instance.mutex.RLock()
    value, exists := instance.values[key]
    instance.mutex.RUnlock()

    if false == exists {
        return nil
    }

    return unwrapSharedValue(value)
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

/* SetShared stores the value itself where Set stores something the storage layer is free to copy: Get hands this very handle back, and every reader of the session reaches the one value. Set stays the default because a copy is the semantics that cannot be got wrong by accident — nobody writes through it and expects the write to land somewhere else. SetShared is for what a copy would break, a value whose identity is the point rather than its contents.

There is no reading counterpart on purpose. The write decides the semantics and Get honours whichever the value was stored with, so a GetShared could only restate an intent it has no power to change.

A storage that serialises cannot carry a handle across a round trip. The file storage refuses to save a session holding one instead of writing out something that would load back as a plain copy. */
func (instance *Session) SetShared(key string, value any) {
    instance.mutex.Lock()
    instance.values[key] = sharedValue{
        value: value,
    }
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

func (instance *Session) Clear() {
    instance.mutex.Lock()
    instance.values = make(map[string]any)
    instance.modified = true
    instance.cleared = true
    instance.mutex.Unlock()
}

/* abandon is Clear with a latch: Set lifts the cleared flag, the latch nothing lifts. A session whose id the manager already deleted must never look live again, or the response path would save it back under that id and re-issue it. */
func (instance *Session) abandon() {
    instance.mutex.Lock()
    instance.values = make(map[string]any)
    instance.modified = true
    instance.cleared = true
    instance.abandoned = true
    instance.mutex.Unlock()
}

/* All reads session data, so it answers what Get answers for every key: a shared value comes out as the handle, not as the envelope it is filed under. The envelope is an implementation of the storage path and no caller of All is on it. */
func (instance *Session) All() map[string]any {
    instance.mutex.RLock()
    result := make(map[string]any, len(instance.values))
    for key, value := range instance.values {
        result[key] = unwrapSharedValue(value)
    }
    instance.mutex.RUnlock()

    return result
}

/* storedValues is the values map as the storage layer has to receive it, envelopes intact, so that a save followed by a load returns a shared value shared instead of quietly demoting it to a copy. Rotation carries the values over through here for the same reason: a session that survives a regenerated id keeps the handles it was holding. */
func (instance *Session) storedValues() map[string]any {
    instance.mutex.RLock()
    result := make(map[string]any, len(instance.values))
    for key, value := range instance.values {
        result[key] = value
    }
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

func unwrapSharedValue(value any) any {
    shared, isShared := value.(sharedValue)
    if false == isShared {
        return value
    }

    return shared.value
}

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
