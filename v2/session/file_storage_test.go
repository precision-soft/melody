package session

import (
	"os"
	"testing"
	"time"
)

func TestFileStorage_Close_DoesNotCloseInjectedFile(t *testing.T) {
	fileInstance, err := os.CreateTemp("", "melody_session_injected_*.json")
	if nil != err {
		t.Fatalf("unexpected create temp error: %s", err.Error())
	}

	defer func() {
		_ = fileInstance.Close()
		_ = os.Remove(fileInstance.Name())
	}()

	storage, err := NewFileStorageFromFile(fileInstance)
	if nil != err {
		t.Fatalf("unexpected storage error: %s", err.Error())
	}

	closeErr := storage.Close()
	if nil != closeErr {
		t.Fatalf("unexpected close error: %s", closeErr.Error())
	}

	_, writeErr := fileInstance.WriteString("x")
	if nil != writeErr {
		t.Fatalf("expected injected file to remain open, got write error: %s", writeErr.Error())
	}
}

func TestFileStorage_Close_ClosesOwnedFile(t *testing.T) {
	fileInstance, err := os.CreateTemp("", "melody_session_owned_*.json")
	if nil != err {
		t.Fatalf("unexpected create temp error: %s", err.Error())
	}

	path := fileInstance.Name()

	_ = fileInstance.Close()
	_ = os.Remove(path)

	storage, err := NewFileStorageFromPath(path)
	if nil != err {
		t.Fatalf("unexpected storage error: %s", err.Error())
	}

	saveErr := storage.Save(
		"abc",
		map[string]any{"k": "v"},
		2*time.Second,
	)
	if nil != saveErr {
		t.Fatalf("unexpected save error: %s", saveErr.Error())
	}

	closeErr := storage.Close()
	if nil != closeErr {
		t.Fatalf("unexpected close error: %s", closeErr.Error())
	}

	_ = os.Remove(path)
}

func TestFileStorage_Save_PersistsAcrossInstances_ByPath(t *testing.T) {
	fileInstance, err := os.CreateTemp("", "melody_session_persist_path_*.json")
	if nil != err {
		t.Fatalf("unexpected create temp error: %s", err.Error())
	}

	path := fileInstance.Name()

	_ = fileInstance.Close()
	_ = os.Remove(path)

	storage1, err := NewFileStorageFromPath(path)
	if nil != err {
		t.Fatalf("unexpected storage error: %s", err.Error())
	}

	saveErr := storage1.Save(
		"abc",
		map[string]any{"k": "v"},
		0,
	)
	if nil != saveErr {
		t.Fatalf("unexpected save error: %s", saveErr.Error())
	}

	loadAfterSaveData, loadAfterSaveExists, loadAfterSaveErr := storage1.Load("abc")
	if nil != loadAfterSaveErr {
		t.Fatalf("unexpected load error: %s", loadAfterSaveErr.Error())
	}

	if false == loadAfterSaveExists {
		t.Fatalf("expected session to exist after save")
	}

	if "v" != loadAfterSaveData["k"].(string) {
		t.Fatalf("expected saved value")
	}

	closeErr := storage1.Close()
	if nil != closeErr {
		t.Fatalf("unexpected close error: %s", closeErr.Error())
	}

	storage2, err := NewFileStorageFromPath(path)
	if nil != err {
		t.Fatalf("unexpected storage error: %s", err.Error())
	}

	data, exists, loadErr := storage2.Load("abc")
	if nil != loadErr {
		t.Fatalf("unexpected load error: %s", loadErr.Error())
	}

	if false == exists {
		t.Fatalf("expected session to exist after reload")
	}

	if "v" != data["k"].(string) {
		t.Fatalf("expected persisted value")
	}

	_ = storage2.Close()
	_ = os.Remove(path)
}

func TestFileStorage_Save_PersistsAcrossInstances_ByInjectedFile(t *testing.T) {
	fileInstance, err := os.CreateTemp("", "melody_session_persist_injected_*.json")
	if nil != err {
		t.Fatalf("unexpected create temp error: %s", err.Error())
	}

	defer func() {
		_ = fileInstance.Close()
		_ = os.Remove(fileInstance.Name())
	}()

	storage1, err := NewFileStorageFromFile(fileInstance)
	if nil != err {
		t.Fatalf("unexpected storage error: %s", err.Error())
	}

	saveErr := storage1.Save(
		"abc",
		map[string]any{"k": "v"},
		0,
	)
	if nil != saveErr {
		t.Fatalf("unexpected save error: %s", saveErr.Error())
	}

	storage2, err := NewFileStorageFromFile(fileInstance)
	if nil != err {
		t.Fatalf("unexpected storage error: %s", err.Error())
	}

	data, exists, loadErr := storage2.Load("abc")
	if nil != loadErr {
		t.Fatalf("unexpected load error: %s", loadErr.Error())
	}

	if false == exists {
		t.Fatalf("expected session to exist after reload")
	}

	if "v" != data["k"].(string) {
		t.Fatalf("expected persisted value")
	}
}
