package session

import (
	"sync"
	"time"

	"github.com/precision-soft/melody/v2/exception"
)

func NewInMemoryStorage() *InMemoryStorage {
	return NewInMemoryStorageWithCleanupInterval(time.Minute)
}

func NewInMemoryStorageWithCleanupInterval(cleanupInterval time.Duration) *InMemoryStorage {
	if 0 >= cleanupInterval {
		exception.Panic(
			exception.NewError("cleanup interval must be greater than zero", nil, nil),
		)
	}

	storage := &InMemoryStorage{
		sessions:        make(map[string]inMemorySessionEntry),
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
		cleanupDone:     make(chan struct{}),
	}

	go storage.cleanupLoop()

	return storage
}

type InMemoryStorage struct {
	mutex           sync.RWMutex
	sessions        map[string]inMemorySessionEntry
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	cleanupDone     chan struct{}
	stopCleanupOnce sync.Once
}

type inMemorySessionEntry struct {
	data      map[string]any
	expiresAt *time.Time
}

func (instance *InMemoryStorage) Load(sessionId string) (map[string]any, bool, error) {
	if "" == sessionId {
		return nil, false, exception.NewError("session id is required in load session", nil, nil)
	}

	now := time.Now()

	instance.mutex.RLock()
	entry, exists := instance.sessions[sessionId]
	instance.mutex.RUnlock()

	if false == exists {
		return nil, false, nil
	}

	if nil != entry.expiresAt && true == entry.expiresAt.Before(now) {
		instance.mutex.Lock()
		delete(instance.sessions, sessionId)
		instance.mutex.Unlock()

		return nil, false, nil
	}

	result := make(map[string]any, len(entry.data))
	for key, value := range entry.data {
		result[key] = value
	}

	return result, true, nil
}

func (instance *InMemoryStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
	if "" == sessionId {
		return exception.NewError("session id is required in save session", nil, nil)
	}

	copyValue := make(map[string]any, len(data))
	for key, value := range data {
		copyValue[key] = value
	}

	var expiresAt *time.Time
	if 0 < ttl {
		expiration := time.Now().Add(ttl)
		expiresAt = &expiration
	}

	instance.mutex.Lock()
	instance.sessions[sessionId] = inMemorySessionEntry{
		data:      copyValue,
		expiresAt: expiresAt,
	}
	instance.mutex.Unlock()

	return nil
}

func (instance *InMemoryStorage) Delete(sessionId string) error {
	if "" == sessionId {
		return exception.NewError("session id is required in delete session", nil, nil)
	}

	instance.mutex.Lock()
	delete(instance.sessions, sessionId)
	instance.mutex.Unlock()

	return nil
}

func (instance *InMemoryStorage) Clear() error {
	instance.mutex.Lock()
	instance.sessions = make(map[string]inMemorySessionEntry)
	instance.mutex.Unlock()

	return nil
}

func (instance *InMemoryStorage) Close() error {
	instance.stopCleanupOnce.Do(
		func() {
			close(instance.stopCleanup)
		},
	)

	<-instance.cleanupDone

	return nil
}

func (instance *InMemoryStorage) cleanupLoop() {
	defer close(instance.cleanupDone)

	ticker := time.NewTicker(instance.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			instance.cleanupExpired()
		case <-instance.stopCleanup:
			return
		}
	}
}

func (instance *InMemoryStorage) cleanupExpired() {
	now := time.Now()

	instance.mutex.Lock()
	for sessionId, entry := range instance.sessions {
		if nil == entry.expiresAt {
			continue
		}

		if true == entry.expiresAt.Before(now) {
			delete(instance.sessions, sessionId)
		}
	}
	instance.mutex.Unlock()
}
