package session

import (
	"testing"
	"time"
)

func TestInMemoryStorageAndManager(t *testing.T) {
	storage := NewInMemoryStorage()
	manager := NewManager(storage, time.Minute)

	sessionInstance := manager.NewSession()
	if "" == sessionInstance.Id() {
		t.Fatalf("expected session id")
	}

	err := manager.SaveSession(sessionInstance)
	if nil != err {
		t.Fatalf("expected no error")
	}

	loaded := manager.Session(sessionInstance.Id())
	if nil != loaded {
		t.Fatalf("expected session not to be persisted without modifications")
	}

	sessionInstance.Set("key", "value")

	err = manager.SaveSession(sessionInstance)
	if nil != err {
		t.Fatalf("save error: %v", err)
	}

	loaded = manager.Session(sessionInstance.Id())
	if nil == loaded {
		t.Fatalf("expected loaded session")
	}

	if "value" != loaded.String("key") {
		t.Fatalf("expected stored value")
	}

	sessionInstance.Clear()

	err = manager.SaveSession(sessionInstance)
	if nil != err {
		t.Fatalf("clear commit error: %v", err)
	}

	deleted := manager.Session(sessionInstance.Id())
	if nil != deleted {
		t.Fatalf("expected session to be deleted after clear")
	}
}

func TestInMemoryStorage_Delete_RemovesSession(t *testing.T) {
	storage := NewInMemoryStorage()
	manager := NewManager(storage, time.Minute)

	sessionInstance := manager.NewSession()
	sessionInstance.Set("a", "b")

	err := manager.SaveSession(sessionInstance)
	if nil != err {
		t.Fatalf("unexpected error")
	}

	err = manager.DeleteSession(sessionInstance.Id())
	if nil != err {
		t.Fatalf("unexpected error")
	}

	loaded := manager.Session(sessionInstance.Id())
	if nil != loaded {
		t.Fatalf("expected nil after delete")
	}
}

func TestInMemoryStorage_Close_DoesNotError(t *testing.T) {
	storage := NewInMemoryStorage()
	manager := NewManager(storage, time.Minute)

	err := manager.Close()
	if nil != err {
		t.Fatalf("unexpected error")
	}
}

func TestNewInMemoryStorage_DefaultCleanupIntervalIsOneMinute(t *testing.T) {
	storage := NewInMemoryStorage()
	defer func() {
		closeErr := storage.Close()
		if nil != closeErr {
			t.Fatalf("unexpected close error: %v", closeErr)
		}
	}()

	if time.Minute != storage.cleanupInterval {
		t.Fatalf("expected default cleanup interval to be one minute")
	}
}

func TestNewInMemoryStorageWithCleanupInterval_SetsInterval(t *testing.T) {
	storage := NewInMemoryStorageWithCleanupInterval(250 * time.Millisecond)
	defer func() {
		closeErr := storage.Close()
		if nil != closeErr {
			t.Fatalf("unexpected close error: %v", closeErr)
		}
	}()

	if 250*time.Millisecond != storage.cleanupInterval {
		t.Fatalf("expected cleanup interval to be set")
	}
}

func TestNewInMemoryStorageWithCleanupInterval_PanicsWhenIntervalIsZeroOrNegative(t *testing.T) {
	defer func() {
		recovered := recover()
		if nil == recovered {
			t.Fatalf("expected constructor to panic for invalid interval")
		}
	}()

	_ = NewInMemoryStorageWithCleanupInterval(0)
}
