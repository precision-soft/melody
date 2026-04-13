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

func TestFileStorage_Save_NestedMap_DeepCopied(t *testing.T) {
	fileInstance, err := os.CreateTemp("", "melody_session_nested_*.json")
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

	nestedMap := map[string]any{
		"inner": "original",
	}
	data := map[string]any{
		"nested": nestedMap,
	}

	saveErr := storage.Save("nested_test", data, 0)
	if nil != saveErr {
		t.Fatalf("unexpected save error: %s", saveErr.Error())
	}

	nestedMap["inner"] = "mutated"

	loadedData, exists, loadErr := storage.Load("nested_test")
	if nil != loadErr {
		t.Fatalf("unexpected load error: %s", loadErr.Error())
	}

	if false == exists {
		t.Fatalf("expected session to exist")
	}

	loadedNested, ok := loadedData["nested"].(map[string]any)
	if false == ok {
		t.Fatalf("expected nested map in loaded data")
	}

	if "original" != loadedNested["inner"].(string) {
		t.Fatalf("expected deep copy to protect stored data from external mutation, got: %v", loadedNested["inner"])
	}

	loadedNested["inner"] = "loaded_mutation"

	loadedData2, _, _ := storage.Load("nested_test")
	loadedNested2, _ := loadedData2["nested"].(map[string]any)

	if "loaded_mutation" == loadedNested2["inner"].(string) {
		t.Fatalf("expected Load to return independent copy, not shared reference")
	}
}

func TestCopyAnyMap_NilInput_ReturnsEmptyMap(t *testing.T) {
	result := copyAnyMap(nil)
	if nil == result {
		t.Fatalf("expected non-nil result for nil input")
	}
	if 0 != len(result) {
		t.Fatalf("expected empty map")
	}
}

func TestCopyAnyMap_NestedMap_IsDeepCopied(t *testing.T) {
	original := map[string]any{
		"level1": map[string]any{
			"level2": "value",
		},
		"simple": "text",
	}

	copied := copyAnyMap(original)

	level1, ok := copied["level1"].(map[string]any)
	if false == ok {
		t.Fatalf("expected nested map to be preserved")
	}

	if "value" != level1["level2"].(string) {
		t.Fatalf("expected nested value to be copied")
	}

	originalLevel1 := original["level1"].(map[string]any)
	originalLevel1["level2"] = "mutated"

	if "mutated" == level1["level2"].(string) {
		t.Fatalf("expected deep copy to isolate nested map")
	}
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
