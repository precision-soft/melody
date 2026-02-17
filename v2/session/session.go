package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/precision-soft/melody/v2/exception"
	sessioncontract "github.com/precision-soft/melody/v2/session/contract"
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

func (instance *Session) Clear() {
	instance.mutex.Lock()
	instance.values = make(map[string]any)
	instance.modified = true
	instance.cleared = true
	instance.mutex.Unlock()
}

func (instance *Session) All() map[string]any {
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
