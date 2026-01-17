package session

import (
	"time"

	"github.com/precision-soft/melody/exception"
	"github.com/precision-soft/melody/internal"
	sessioncontract "github.com/precision-soft/melody/session/contract"
)

type Manager struct {
	storage sessioncontract.Storage
	ttl     time.Duration
}

func NewManager(storage sessioncontract.Storage, ttl time.Duration) *Manager {
	if true == internal.IsNilInterface(storage) {
		exception.Panic(exception.NewError("session storage is nil", nil, nil))
	}

	return &Manager{
		storage: storage,
		ttl:     ttl,
	}
}

func (instance *Manager) Session(sessionId string) sessioncontract.Session {
	if "" == sessionId {
		return nil
	}

	data, exists, err := instance.storage.Load(sessionId)
	if nil != err {
		exception.Panic(exception.FromError(err))
	}

	if false == exists {
		return nil
	}

	if nil == data {
		data = make(map[string]any)
	}

	return &Session{
		id:       sessionId,
		values:   data,
		modified: false,
		cleared:  false,
	}
}

func (instance *Manager) NewSession() sessioncontract.Session {
	maxAttempts := 128

	for attempt := 0; attempt < maxAttempts; attempt++ {
		newId := generateSessionId()

		_, exists, err := instance.storage.Load(newId)
		if nil != err {
			exception.Panic(exception.FromError(err))
		}

		if true == exists {
			continue
		}

		return &Session{
			id:       newId,
			values:   make(map[string]any),
			modified: false,
			cleared:  false,
		}
	}

	exception.Panic(
		exception.NewError(
			"could not generate unique session id",
			map[string]any{
				"attempts": maxAttempts,
			},
			nil,
		),
	)

	return nil
}

func (instance *Manager) SaveSession(sessionInstance sessioncontract.Session) error {
	if nil == sessionInstance {
		return exception.NewError("session is nil in save session", nil, nil)
	}

	if true == sessionInstance.IsCleared() {
		return instance.DeleteSession(sessionInstance.Id())
	}

	if false == sessionInstance.IsModified() {
		return nil
	}

	sessionId := sessionInstance.Id()
	if "" == sessionId {
		return exception.NewError("session id is required in save session", nil, nil)
	}

	return instance.storage.Save(sessionId, sessionInstance.All(), instance.ttl)
}

func (instance *Manager) DeleteSession(sessionId string) error {
	if "" == sessionId {
		return exception.NewError("session id is required in delete session", nil, nil)
	}

	return instance.storage.Delete(sessionId)
}

func (instance *Manager) Close() error {
	return instance.storage.Close()
}

var _ sessioncontract.Manager = (*Manager)(nil)
