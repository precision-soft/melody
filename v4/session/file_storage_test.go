package session

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "math"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/exception"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
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

    storage, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    closeErr := storage.CloseWithContext(t.Context())
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

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    saveErr := storage.Save(
        context.Background(),
        "abc",
        map[string]any{"k": "v"},
        bound.Of(2*time.Second),
    )
    if nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    closeErr := storage.CloseWithContext(t.Context())
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

    storage1, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    saveErr := storage1.Save(
        context.Background(),
        "abc",
        map[string]any{"k": "v"},
        bound.UnlimitedDuration(),
    )
    if nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    loadAfterSaveData, loadAfterSaveExists, loadAfterSaveErr := storage1.Load(context.Background(), "abc")
    if nil != loadAfterSaveErr {
        t.Fatalf("unexpected load error: %s", loadAfterSaveErr.Error())
    }

    if false == loadAfterSaveExists {
        t.Fatalf("expected session to exist after save")
    }

    if "v" != loadAfterSaveData["k"].(string) {
        t.Fatalf("expected saved value")
    }

    closeErr := storage1.CloseWithContext(t.Context())
    if nil != closeErr {
        t.Fatalf("unexpected close error: %s", closeErr.Error())
    }

    storage2, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    data, exists, loadErr := storage2.Load(context.Background(), "abc")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if false == exists {
        t.Fatalf("expected session to exist after reload")
    }

    if "v" != data["k"].(string) {
        t.Fatalf("expected persisted value")
    }

    _ = storage2.CloseWithContext(t.Context())
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

    storage1, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    saveErr := storage1.Save(
        context.Background(),
        "abc",
        map[string]any{"k": "v"},
        bound.UnlimitedDuration(),
    )
    if nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    storage2, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    data, exists, loadErr := storage2.Load(context.Background(), "abc")
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

func expireStoredEntry(t *testing.T, storage *FileStorage, sessionId string) {
    t.Helper()

    storage.mutex.Lock()
    defer storage.mutex.Unlock()

    entry, exists := storage.sessionById[sessionId]
    if false == exists {
        t.Fatalf("expected %q to be stored before its expiry is rewound", sessionId)
    }

    entry.ExpiresAt = time.Now().Add(-time.Hour).UnixNano()
    storage.sessionById[sessionId] = entry
}

func TestFileStorage_Load_ExpiredEntryIsDeleted(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path, JournalDisabled: true})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.CloseWithContext(context.Background())

    if saveErr := storage.Save(context.Background(), "expired", map[string]any{"k": "v"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if saveErr := storage.Save(context.Background(), "live", map[string]any{"k": "live"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    expireStoredEntry(t, storage, "expired")

    data, exists, loadErr := storage.Load(context.Background(), "expired")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if true == exists {
        t.Fatalf("expected expired entry to be reported as absent, got data=%v", data)
    }

    if nil != data {
        t.Fatalf("expected nil data for expired entry, got %v", data)
    }

    if _, stillStored := storage.sessionById["expired"]; true == stillStored {
        t.Fatalf("expected the expired entry to be dropped from the in-memory state")
    }

    liveData, liveExists, liveErr := storage.Load(context.Background(), "live")
    if nil != liveErr {
        t.Fatalf("unexpected live load error: %s", liveErr.Error())
    }
    if false == liveExists || "live" != liveData["k"] {
        t.Fatalf("expected the unexpired sibling to survive, got exists=%v data=%v", liveExists, liveData)
    }

    storage2, err := NewFileStorage(FileStorageConfig{Path: path, JournalDisabled: true})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage2.CloseWithContext(context.Background())

    _, existsAfterReload, loadAfterReloadErr := storage2.Load(context.Background(), "expired")
    if nil != loadAfterReloadErr {
        t.Fatalf("unexpected reload error: %s", loadAfterReloadErr.Error())
    }

    if true == existsAfterReload {
        t.Fatalf("expected expired entry to be persisted as removed")
    }

    _, liveExistsAfterReload, liveReloadErr := storage2.Load(context.Background(), "live")
    if nil != liveReloadErr {
        t.Fatalf("unexpected live reload error: %s", liveReloadErr.Error())
    }

    if false == liveExistsAfterReload {
        t.Fatalf("expected the unexpired sibling to survive the rewrite")
    }
}

func TestFileStorage_Save_PurgesEntriesThatLapsedWithoutBeingLoaded(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path, JournalDisabled: true})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.CloseWithContext(context.Background())

    if saveErr := storage.Save(context.Background(), "lapsed", map[string]any{"k": "v"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if saveErr := storage.Save(context.Background(), "keeper", map[string]any{"k": "keeper"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    expireStoredEntry(t, storage, "lapsed")

    if saveErr := storage.Save(context.Background(), "keeper", map[string]any{"k": "keeper"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if _, stillStored := storage.sessionById["lapsed"]; true == stillStored {
        t.Fatalf("expected the lapsed entry to be purged from the in-memory state")
    }

    reader, err := NewFileStorage(FileStorageConfig{Path: path, JournalDisabled: true})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer reader.CloseWithContext(context.Background())

    if _, stillPersisted := reader.sessionById["lapsed"]; true == stillPersisted {
        t.Fatalf("expected the lapsed entry to be purged from the snapshot")
    }

    if _, keeperPersisted := reader.sessionById["keeper"]; false == keeperPersisted {
        t.Fatalf("expected the unexpired entry to survive the purge")
    }
}

func TestFileStorage_Delete_RefusesAnEmptySessionId(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.CloseWithContext(context.Background())

    deleteErr := storage.Delete(context.Background(), "")
    if nil == deleteErr {
        t.Fatalf("expected an empty session id to be refused")
    }

    if "session id is required in delete session" != deleteErr.Error() {
        t.Fatalf("expected the empty id refusal, got %q", deleteErr.Error())
    }
}

func TestFileStorage_Delete_AfterCloseReturnsError(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save(context.Background(), "abc", map[string]any{"k": "v"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    _ = storage.CloseWithContext(t.Context())

    deleteErr := storage.Delete(context.Background(), "abc")
    if nil == deleteErr {
        t.Fatalf("expected delete after close to error")
    }

    if "session storage is closed" != deleteErr.Error() {
        t.Fatalf("expected the closed refusal, got %q", deleteErr.Error())
    }
}

func TestFileStorage_Delete_RemovesTheEntryAndSkipsTheFlushForAnAbsentId(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save(context.Background(), "deleted", map[string]any{"k": "v"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if saveErr := storage.Save(context.Background(), "kept", map[string]any{"k": "kept"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if deleteErr := storage.Delete(context.Background(), "deleted"); nil != deleteErr {
        t.Fatalf("unexpected delete error: %s", deleteErr.Error())
    }

    if _, stillStored := storage.sessionById["deleted"]; true == stillStored {
        t.Fatalf("expected the deleted entry to be gone from the in-memory state")
    }

    _, exists, loadErr := storage.Load(context.Background(), "deleted")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }
    if true == exists {
        t.Fatalf("expected the deleted session to be absent")
    }

    _ = storage.CloseWithContext(t.Context())

    reader, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer reader.CloseWithContext(context.Background())

    if _, stillPersisted := reader.sessionById["deleted"]; true == stillPersisted {
        t.Fatalf("expected the deleted entry to be gone from the snapshot")
    }
    if _, keptPersisted := reader.sessionById["kept"]; false == keptPersisted {
        t.Fatalf("expected the untouched entry to survive the delete")
    }

    readOnlyPath := filepath.Join(directory, "read_only.json")
    if writeErr := os.WriteFile(readOnlyPath, []byte("{}\n"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the read-only storage file: %s", writeErr.Error())
    }

    unwritableStorage := newFileStorageOverAReadOnlyHandle(t, readOnlyPath)

    if deleteErr := unwritableStorage.Delete(context.Background(), "never-stored"); nil != deleteErr {
        t.Fatalf("expected deleting an absent id to answer success without flushing, got %s", deleteErr.Error())
    }
}

func TestFileStorage_Delete_RestoresTheEntryWhenTheFlushFails(t *testing.T) {
    fileInstance, err := os.CreateTemp("", "melody_session_delete_rollback_*.json")
    if nil != err {
        t.Fatalf("unexpected create temp error: %s", err.Error())
    }

    path := fileInstance.Name()

    defer func() {
        _ = fileInstance.Close()
        _ = os.Remove(path)
    }()

    storage, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save(context.Background(), "doomed", map[string]any{"k": "v"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if closeErr := fileInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected error closing the handle: %s", closeErr.Error())
    }

    deleteErr := storage.Delete(context.Background(), "doomed")
    if nil == deleteErr {
        t.Fatalf("expected the delete to fail when its flush cannot write")
    }

    if _, restored := storage.sessionById["doomed"]; false == restored {
        t.Fatalf("expected the entry to be restored after a failed flush, since the file still carries it")
    }
}

func TestFileStorage_Close_IsIdempotent(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if err := storage.CloseWithContext(t.Context()); nil != err {
        t.Fatalf("unexpected first close error: %s", err.Error())
    }

    if err := storage.CloseWithContext(t.Context()); nil != err {
        t.Fatalf("unexpected second close error: %s", err.Error())
    }
}

func TestNewFileStorage_RefusesAPathWhoseDirectoryCannotBeCreated(t *testing.T) {
    directory := t.TempDir()

    blockingFilePath := filepath.Join(directory, "blocking")
    if writeErr := os.WriteFile(blockingFilePath, []byte("x"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the blocking file: %s", writeErr.Error())
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: filepath.Join(blockingFilePath, "nested", "session.json")})
    if nil == storageErr {
        t.Fatalf("expected a path under a regular file to be refused")
    }

    if nil != storage {
        t.Fatalf("expected no storage for a path whose directory cannot be created")
    }

    if "failed to create session storage directory" != storageErr.Error() {
        t.Fatalf("expected the directory refusal, got %q", storageErr.Error())
    }
}

func TestFileStorage_Load_AfterCloseReturnsError(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save(context.Background(), "abc", map[string]any{"k": "v"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    _ = storage.CloseWithContext(t.Context())

    _, exists, loadErr := storage.Load(context.Background(), "abc")
    if nil == loadErr {
        t.Fatalf("expected load after close to error")
    }

    if true == exists {
        t.Fatalf("expected a closed storage to answer no session")
    }

    if "session storage is closed" != loadErr.Error() {
        t.Fatalf("expected the closed refusal, got %q", loadErr.Error())
    }
}

func TestReadSessionFileAtPath_ReportsAnOpenFailureThatIsNotAMissingFile(t *testing.T) {
    directory := t.TempDir()

    blockingFilePath := filepath.Join(directory, "blocking")
    if writeErr := os.WriteFile(blockingFilePath, []byte("x"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the blocking file: %s", writeErr.Error())
    }

    decoded, readErr := readSessionFileAtPath(filepath.Join(blockingFilePath, "session.json"))
    if nil == readErr {
        t.Fatalf("expected a path under a regular file to be reported")
    }

    if nil != decoded {
        t.Fatalf("expected no decoded snapshot on an open failure")
    }

    if "failed to open session storage file" != readErr.Error() {
        t.Fatalf("expected the open refusal, got %q", readErr.Error())
    }

    empty, missingErr := readSessionFileAtPath(filepath.Join(directory, "never-written.json"))
    if nil != missingErr {
        t.Fatalf("expected a missing file to answer an empty store, got %s", missingErr.Error())
    }
    if 0 != len(empty) {
        t.Fatalf("expected an empty store for a missing file, got %d entries", len(empty))
    }
}

func TestFileStorage_FlushLocked_RefusesWhenTheInjectedHandleIsGone(t *testing.T) {
    fileInstance, err := os.CreateTemp("", "melody_session_handle_gone_*.json")
    if nil != err {
        t.Fatalf("unexpected create temp error: %s", err.Error())
    }

    defer func() {
        _ = fileInstance.Close()
        _ = os.Remove(fileInstance.Name())
    }()

    storage, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    storage.file = nil

    saveErr := storage.Save(context.Background(), "abc", map[string]any{"k": "v"}, bound.Of(time.Hour))
    if nil == saveErr {
        t.Fatalf("expected the flush to refuse when there is no handle to write through")
    }

    if "session storage file is nil" != saveErr.Error() {
        t.Fatalf("expected the missing handle refusal, got %q", saveErr.Error())
    }
}

func TestFileStorage_Close_ReportsAFailingCloseOfTheOwnedFile(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    ownedFile, createErr := os.Create(filepath.Join(directory, "owned.json"))
    if nil != createErr {
        t.Fatalf("unexpected create error: %s", createErr.Error())
    }

    if closeErr := ownedFile.Close(); nil != closeErr {
        t.Fatalf("unexpected error closing the handle: %s", closeErr.Error())
    }

    storage.file = ownedFile

    closeErr := storage.CloseWithContext(t.Context())
    if nil == closeErr {
        t.Fatalf("expected the failing close of the owned file to be reported")
    }

    if "failed to close session storage file" != closeErr.Error() {
        t.Fatalf("expected the close refusal, got %q", closeErr.Error())
    }
}

func TestFileStorage_Save_AfterCloseReturnsError(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    _ = storage.CloseWithContext(t.Context())

    saveErr := storage.Save(context.Background(), "k", map[string]any{"v": 1}, bound.Of(time.Minute))
    if nil == saveErr {
        t.Fatalf("expected save after close to error")
    }
}

func TestFileStorage_AtomicWrite_DoesNotLeaveTempFiles(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.CloseWithContext(context.Background())

    for iteration := 0; iteration < 5; iteration++ {
        saveErr := storage.Save(
            context.Background(),
            "s"+strconv.Itoa(iteration),
            map[string]any{"iteration": iteration},
            bound.Of(time.Minute),
        )
        if nil != saveErr {
            t.Fatalf("unexpected save error: %s", saveErr.Error())
        }
    }

    entries, err := os.ReadDir(directory)
    if nil != err {
        t.Fatalf("unexpected readdir error: %s", err.Error())
    }

    for _, entry := range entries {
        name := entry.Name()
        if "session.json" == name || "session.json.journal" == name {
            continue
        }

        t.Fatalf("unexpected leftover file in session directory: %s", name)
    }
}

func TestWriteSessionFileAtomically_RemovesTheTempFileWhenTheReplaceFails(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    if mkdirErr := os.MkdirAll(filepath.Join(path, "occupant"), 0755); nil != mkdirErr {
        t.Fatalf("unexpected error seeding the destination: %s", mkdirErr.Error())
    }

    writeErr := writeSessionFileAtomically(path, map[string]fileSessionEntry{
        "abc": {Data: map[string]any{"k": "v"}, ExpiresAt: 0},
    })
    if nil == writeErr {
        t.Fatalf("expected the replace to fail when the destination cannot be replaced")
    }

    if "failed to replace session storage file" != writeErr.Error() {
        t.Fatalf("expected the replace refusal, got %q", writeErr.Error())
    }

    entries, readErr := os.ReadDir(directory)
    if nil != readErr {
        t.Fatalf("unexpected readdir error: %s", readErr.Error())
    }

    for _, entry := range entries {
        if "session.json" == entry.Name() {
            continue
        }

        t.Fatalf("a failed write left %s behind, so every failed write leaks a full copy of the sessions", entry.Name())
    }
}

func TestWriteSessionFileAtomically_RefusesWhenTheDirectoryCannotBeCreated(t *testing.T) {
    directory := t.TempDir()

    blockingFilePath := filepath.Join(directory, "blocking")
    if writeErr := os.WriteFile(blockingFilePath, []byte("x"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the blocking file: %s", writeErr.Error())
    }

    writeErr := writeSessionFileAtomically(filepath.Join(blockingFilePath, "nested", "session.json"), map[string]fileSessionEntry{})
    if nil == writeErr {
        t.Fatalf("expected a directory under a regular file to be refused")
    }

    if "failed to create session storage directory" != writeErr.Error() {
        t.Fatalf("expected the directory refusal, got %q", writeErr.Error())
    }
}

func TestNewFileStorage_RefusesAHandleThatCannotBeSeeked(t *testing.T) {
    fileInstance, err := os.CreateTemp("", "melody_session_unseekable_*.json")
    if nil != err {
        t.Fatalf("unexpected create temp error: %s", err.Error())
    }

    path := fileInstance.Name()

    defer func() {
        _ = os.Remove(path)
    }()

    if closeErr := fileInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected close error: %s", closeErr.Error())
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil == storageErr {
        t.Fatalf("expected a closed handle to be refused")
    }

    if nil != storage {
        t.Fatalf("expected no storage over a handle that cannot be read")
    }

    if "failed to seek session storage file" != storageErr.Error() {
        t.Fatalf("expected the seek refusal, got %q", storageErr.Error())
    }
}

func TestNewFileStorage_RefusesAHandleOpenedForAppending(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    if writeErr := os.WriteFile(path, []byte("{}"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the storage file: %s", writeErr.Error())
    }

    fileInstance, openErr := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0644)
    if nil != openErr {
        t.Fatalf("unexpected open error: %s", openErr.Error())
    }

    defer func() {
        _ = fileInstance.Close()
    }()

    storage, storageErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil == storageErr {
        t.Fatalf("expected an appending handle to be refused")
    }

    if nil != storage {
        t.Fatalf("expected no storage over an appending handle")
    }

    if "session storage file is opened for appending" != storageErr.Error() {
        t.Fatalf("expected the append refusal, got %q", storageErr.Error())
    }
}

func TestFileStorage_InPlaceWrite_RefusesToLandAnywhereButTheStartOfTheFile(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    if writeErr := os.WriteFile(path, []byte("{}"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the storage file: %s", writeErr.Error())
    }

    fileInstance, openErr := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0644)
    if nil != openErr {
        t.Fatalf("unexpected open error: %s", openErr.Error())
    }

    defer func() {
        _ = fileInstance.Close()
    }()

    writeErr := writeSessionFileInPlace(
        fileInstance,
        map[string]fileSessionEntry{
            "session-1": {
                Data:      map[string]any{"userId": 1},
                ExpiresAt: 0,
            },
        },
    )
    if nil == writeErr {
        t.Fatalf("expected the in-place write to refuse an appending handle")
    }

    if "failed to write session storage file" != writeErr.Error() {
        t.Fatalf("expected the write refusal, got %q", writeErr.Error())
    }

    content, readErr := os.ReadFile(path)
    if nil != readErr {
        t.Fatalf("unexpected read error: %s", readErr.Error())
    }

    if "{}" != string(content) {
        t.Fatalf("expected the refused write to leave the document untouched, got %q", string(content))
    }
}

func TestNewFileStorage_RefusesAFileThatIsNotASnapshot(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    if writeErr := os.WriteFile(path, []byte("not json at all"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the storage file: %s", writeErr.Error())
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path})
    if nil == storageErr {
        t.Fatalf("expected an undecodable snapshot to be refused")
    }

    if nil != storage {
        t.Fatalf("expected no storage over an undecodable snapshot")
    }

    if "failed to decode session storage file" != storageErr.Error() {
        t.Fatalf("expected the decode refusal, got %q", storageErr.Error())
    }
}

func TestNewFileStorage_RefusesAPathThatNamesNoFile(t *testing.T) {
    for _, path := range []string{".", "./"} {
        storage, storageErr := NewFileStorage(FileStorageConfig{Path: path})
        if nil == storageErr {
            t.Fatalf("expected path %q to be refused", path)
        }

        if nil != storage {
            t.Fatalf("expected no storage for path %q", path)
        }

        if "invalid session storage path" != storageErr.Error() {
            t.Fatalf("expected the path refusal for %q, got %q", path, storageErr.Error())
        }
    }
}

func TestNewFileStorage_ANilClockIsTheSystemClockAndAnInjectedOneIsKeptOnBothBranches(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC))

    for _, injected := range []bool{false, true} {
        configs := []FileStorageConfig{
            {Path: filepath.Join(t.TempDir(), "session.json")},
            {File: openSeededSessionFile(t, os.O_RDWR)},
        }

        for _, config := range configs {
            if true == injected {
                config.Clock = frozenClock
            }

            storage, storageErr := NewFileStorage(config)
            if nil != storageErr {
                t.Fatalf("unexpected storage error: %v", storageErr)
            }

            if true == injected && frozenClock != storage.clock {
                t.Fatalf("expected the injected clock to be kept, got %T", storage.clock)
            }

            if _, isSystemClock := storage.clock.(*clock.SystemClock); false == injected && false == isSystemClock {
                t.Fatalf("expected a nil clock to be the system clock, got %T", storage.clock)
            }

            _ = storage.CloseWithContext(t.Context())
        }
    }
}

func TestNewFileStorage_RefusesAConfigurationNamingNeitherAPathNorAFile(t *testing.T) {
    storage, storageErr := NewFileStorage(FileStorageConfig{})
    if nil == storageErr || nil != storage {
        t.Fatalf("expected an empty configuration to be refused, got %v", storageErr)
    }

    if "session file storage declares neither a path nor a file" != storageErr.Error() {
        t.Fatalf("expected the neither refusal, got %q", storageErr.Error())
    }
}

func TestNewFileStorage_RefusesAConfigurationNamingBothAPathAndAFile(t *testing.T) {
    path := filepath.Join(t.TempDir(), "session.json")
    fileInstance := openSeededSessionFile(t, os.O_RDWR)

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path, File: fileInstance})
    if nil == storageErr || nil != storage {
        t.Fatalf("expected a configuration naming both to be refused, got %v", storageErr)
    }

    if "session file storage declares both a path and a file" != storageErr.Error() {
        t.Fatalf("expected the both refusal, got %q", storageErr.Error())
    }

    if path != exception.LogContext(storageErr, exception.LogContextConfig{})["path"] || fileInstance.Name() != exception.LogContext(storageErr, exception.LogContextConfig{})["file"] {
        t.Fatalf("expected the refusal to name the path and the file, got %v", exception.LogContext(storageErr, exception.LogContextConfig{}))
    }

    if _, statErr := os.Stat(path); false == os.IsNotExist(statErr) {
        t.Fatalf("expected nothing to be written at the path, got %v", statErr)
    }
}

func TestNewFileStorage_RefusesAWriteOnlyHandleAtTheRead(t *testing.T) {
    fileInstance := openSeededSessionFile(t, os.O_WRONLY)

    storage, storageErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil == storageErr {
        t.Fatalf("expected a write-only handle to be refused")
    }

    if nil != storage {
        t.Fatalf("expected no storage over a write-only handle")
    }

    if "session storage file is opened read-only" == storageErr.Error() {
        t.Fatalf("expected the write-only handle to pass the mode check and be refused at the read")
    }
}

func TestNewFileStorage_AcceptsAReadWriteHandleAndSaves(t *testing.T) {
    fileInstance := openSeededSessionFile(t, os.O_RDWR)

    storage, storageErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != storageErr {
        t.Fatalf("unexpected error over a read-write handle: %s", storageErr.Error())
    }

    if saveErr := storage.Save(context.Background(), "sid-1", map[string]any{"key": "value"}, bound.UnlimitedDuration()); nil != saveErr {
        t.Fatalf("unexpected save error over a read-write handle: %s", saveErr.Error())
    }
}

func openSeededSessionFile(t *testing.T, flag int) *os.File {
    t.Helper()

    path := filepath.Join(t.TempDir(), "session.json")

    if writeErr := os.WriteFile(path, []byte("{}"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the storage file: %s", writeErr.Error())
    }

    fileInstance, openErr := os.OpenFile(path, flag, 0644)
    if nil != openErr {
        t.Fatalf("unexpected open error: %s", openErr.Error())
    }

    t.Cleanup(func() {
        _ = fileInstance.Close()
    })

    return fileInstance
}

func newFileStorageOverAReadOnlyHandle(t *testing.T, path string) *FileStorage {
    t.Helper()

    readWriteHandle, openErr := os.OpenFile(path, os.O_RDWR, 0644)
    if nil != openErr {
        t.Fatalf("unexpected error opening the storage file: %s", openErr.Error())
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{File: readWriteHandle})
    if nil != storageErr {
        t.Fatalf("unexpected error constructing the storage: %s", storageErr.Error())
    }

    _ = readWriteHandle.Close()

    readOnlyHandle, openErr := os.OpenFile(path, os.O_RDONLY, 0644)
    if nil != openErr {
        t.Fatalf("unexpected error opening the storage file read-only: %s", openErr.Error())
    }

    t.Cleanup(func() {
        _ = readOnlyHandle.Close()
    })

    storage.file = readOnlyHandle

    return storage
}

func TestFileStorage_ConcurrentLoadSaveIsRaceFree(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.CloseWithContext(context.Background())

    sessionId := "concurrent-session"

    if saveErr := storage.Save(context.Background(), sessionId, map[string]any{"counter": 0}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    var waitGroup sync.WaitGroup
    iterations := 20

    for writerIndex := 0; writerIndex < 4; writerIndex++ {
        waitGroup.Add(1)
        go func(writerId int) {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                _ = storage.Save(
                    context.Background(),
                    sessionId,
                    map[string]any{
                        "counter": index,
                        "worker":  strconv.Itoa(writerId),
                        "nested": map[string]any{
                            "value": index,
                        },
                    },
                    bound.Of(time.Minute),
                )
            }
        }(writerIndex)
    }

    for readerIndex := 0; readerIndex < 4; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                loaded, _, loadErr := storage.Load(context.Background(), sessionId)
                if nil != loadErr {
                    t.Errorf("load error: %v", loadErr)
                    return
                }
                for key := range loaded {
                    _ = loaded[key]
                }
            }
        }()
    }

    waitGroup.Wait()
}

func TestFileStorage_Save_FailedEncodeDoesNotDestroyPersistedSessions(t *testing.T) {
    fileInstance, err := os.CreateTemp("", "melody_session_dataloss_*.json")
    if nil != err {
        t.Fatalf("unexpected create temp error: %s", err.Error())
    }

    defer func() {
        _ = fileInstance.Close()
        _ = os.Remove(fileInstance.Name())
    }()

    storage, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    saveErr := storage.Save(context.Background(), "keep", map[string]any{"k": "v"}, bound.UnlimitedDuration())
    if nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    info, statErr := os.Stat(fileInstance.Name())
    if nil != statErr {
        t.Fatalf("unexpected stat error: %s", statErr.Error())
    }
    if 0 == info.Size() {
        t.Fatalf("expected the persisted session file to be non-empty after a successful save")
    }

    badSaveErr := storage.Save(context.Background(), "bad", map[string]any{"ch": make(chan int)}, bound.UnlimitedDuration())
    if nil == badSaveErr {
        t.Fatalf("expected a non-marshalable session value to fail the save")
    }

    info, statErr = os.Stat(fileInstance.Name())
    if nil != statErr {
        t.Fatalf("unexpected stat error: %s", statErr.Error())
    }
    if 0 == info.Size() {
        t.Fatalf("a failed save truncated the session file to 0 bytes, destroying the previously-persisted sessions")
    }

    reader, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    data, exists, loadErr := reader.Load(context.Background(), "keep")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }
    if false == exists {
        t.Fatalf("the previously-persisted \"keep\" session was lost from disk after an unrelated failed save")
    }
    if "v" != data["k"].(string) {
        t.Fatalf("the previously-persisted session value was corrupted after a failed save")
    }
}

func TestFileStorage_Save_RollsBackInMemoryEntryWhenFlushFails(t *testing.T) {
    fileInstance, err := os.CreateTemp("", "melody_session_rollback_*.json")
    if nil != err {
        t.Fatalf("unexpected create temp error: %s", err.Error())
    }

    defer func() {
        _ = fileInstance.Close()
        _ = os.Remove(fileInstance.Name())
    }()

    storage, err := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save(context.Background(), "existing", map[string]any{"v": "old"}, bound.UnlimitedDuration()); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if newErr := storage.Save(context.Background(), "fresh", map[string]any{"ch": make(chan int)}, bound.UnlimitedDuration()); nil == newErr {
        t.Fatalf("expected the non-marshalable save to fail")
    }
    if _, exists, loadErr := storage.Load(context.Background(), "fresh"); nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    } else if true == exists {
        t.Fatalf("a failed Save of a new id must not be observable via Load")
    }

    if updErr := storage.Save(context.Background(), "existing", map[string]any{"ch": make(chan int)}, bound.UnlimitedDuration()); nil == updErr {
        t.Fatalf("expected the non-marshalable update to fail")
    }
    data, exists, loadErr := storage.Load(context.Background(), "existing")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }
    if false == exists || "old" != data["v"].(string) {
        t.Fatalf("a failed update must restore the previous in-memory value, got exists=%v data=%v", exists, data)
    }
}

func TestFileStorage_Save_KeepsTheNewEntryWhenTheFlushFailsAfterThePersist(t *testing.T) {
    characterDevice, openErr := os.OpenFile(refusingCharacterDevice, os.O_RDWR, 0)
    if nil != openErr {
        t.Fatalf("unexpected open error: %s", openErr.Error())
    }

    defer func() {
        _ = characterDevice.Close()
    }()

    storage, storageErr := NewFileStorage(FileStorageConfig{File: characterDevice})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %s", storageErr.Error())
    }

    saveErr := storage.Save(context.Background(), "kept", map[string]any{"v": "new"}, bound.UnlimitedDuration())
    if nil == saveErr {
        t.Fatalf("expected the truncation refusal to be reported")
    }

    if false == errors.Is(saveErr, errSessionStoragePersistedDespiteFlushFailure) {
        t.Fatalf("expected the failure to carry the persisted-despite-flush mark, got %s", saveErr.Error())
    }

    data, exists, loadErr := storage.Load(context.Background(), "kept")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if false == exists || "new" != data["v"].(string) {
        t.Fatalf("expected the entry persisted before the failed flush to stay, got exists=%v data=%v", exists, data)
    }
}

func TestFileStorage_Delete_KeepsTheEntryDeletedWhenTheFlushFailsAfterThePersist(t *testing.T) {
    characterDevice, openErr := os.OpenFile(refusingCharacterDevice, os.O_RDWR, 0)
    if nil != openErr {
        t.Fatalf("unexpected open error: %s", openErr.Error())
    }

    defer func() {
        _ = characterDevice.Close()
    }()

    storage, storageErr := NewFileStorage(FileStorageConfig{File: characterDevice})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %s", storageErr.Error())
    }

    if saveErr := storage.Save(context.Background(), "gone", map[string]any{"v": "old"}, bound.UnlimitedDuration()); false == errors.Is(saveErr, errSessionStoragePersistedDespiteFlushFailure) {
        t.Fatalf("expected the seeding save to fail only past the persist, got %v", saveErr)
    }

    deleteErr := storage.Delete(context.Background(), "gone")
    if nil == deleteErr {
        t.Fatalf("expected the truncation refusal to be reported")
    }

    if false == errors.Is(deleteErr, errSessionStoragePersistedDespiteFlushFailure) {
        t.Fatalf("expected the failure to carry the persisted-despite-flush mark, got %s", deleteErr.Error())
    }

    if _, exists, loadErr := storage.Load(context.Background(), "gone"); nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    } else if true == exists {
        t.Fatalf("expected the entry deleted before the failed flush to stay deleted")
    }
}

func TestFileStorage_Save_TtlBeyondYear2262IsKeptNotPurged(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.CloseWithContext(context.Background())

    if saveErr := storage.Save(context.Background(), "forever", map[string]any{"k": "v"}, bound.Of(time.Duration(math.MaxInt64))); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    data, exists, loadErr := storage.Load(context.Background(), "forever")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }
    if false == exists {
        t.Fatalf("expected the session saved with a very large ttl to be kept, not purged as already expired")
    }
    if "v" != data["k"].(string) {
        t.Fatalf("expected the persisted session data to survive, got %v", data)
    }

    reader, err := NewFileStorage(FileStorageConfig{Path: path})
    if nil != err {
        t.Fatalf("unexpected reader storage error: %s", err.Error())
    }
    defer reader.CloseWithContext(context.Background())

    reloadData, reloadExists, reloadErr := reader.Load(context.Background(), "forever")
    if nil != reloadErr {
        t.Fatalf("unexpected reload error: %s", reloadErr.Error())
    }
    if false == reloadExists || "v" != reloadData["k"].(string) {
        t.Fatalf("expected the session to persist across instances, got exists=%v data=%v", reloadExists, reloadData)
    }
}

func TestFileStorage_LoadingAnExpiredSessionRemovesItFromTheFile(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "sessions.json")

    storage, newErr := NewFileStorage(FileStorageConfig{Path: path, JournalDisabled: true})
    if nil != newErr {
        t.Fatalf("unexpected storage error: %v", newErr)
    }

    saveErr := storage.Save(context.Background(), "expired", map[string]any{"user": "alice"}, bound.Of(time.Hour))
    if nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    saveErr = storage.Save(context.Background(), "live", map[string]any{"user": "bob"}, bound.Of(time.Hour))
    if nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    agedEntry := storage.sessionById["expired"]
    agedEntry.ExpiresAt = time.Now().Add(-time.Hour).UnixNano()
    storage.sessionById["expired"] = agedEntry

    _, exists, loadErr := storage.Load(context.Background(), "expired")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %v", loadErr)
    }

    if true == exists {
        t.Fatalf("expected the expired session not to be handed back")
    }

    if _, stillThere := storage.sessionById["expired"]; true == stillThere {
        t.Fatalf("expected the expired session to be gone from the map")
    }

    reopened, reopenErr := NewFileStorage(FileStorageConfig{Path: path, JournalDisabled: true})
    if nil != reopenErr {
        t.Fatalf("unexpected reopen error: %v", reopenErr)
    }

    if _, persisted := reopened.sessionById["expired"]; true == persisted {
        t.Fatalf("expected the expired session to be gone from the file")
    }

    if _, persisted := reopened.sessionById["live"]; false == persisted {
        t.Fatalf("expected the live session to survive the purge")
    }
}

func TestFileStorage_LoadOfALapsedEntryAnswersAbsentWhenTheFlushCannotWrite(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "sessions.json")

    if err := os.WriteFile(path, []byte("{}\n"), 0644); nil != err {
        t.Fatalf("unexpected error seeding the storage file: %v", err)
    }

    storage := newFileStorageOverAReadOnlyHandle(t, path)

    sessionId := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

    storage.sessionById[sessionId] = fileSessionEntry{
        Data:      map[string]any{"userId": "u-1"},
        ExpiresAt: time.Now().Add(-time.Hour).UnixNano(),
    }

    data, exists, loadErr := storage.Load(context.Background(), sessionId)
    if nil != loadErr {
        t.Fatalf("expected a lapsed entry to answer absent rather than surface the housekeeping failure, got %v", loadErr)
    }

    if true == exists || nil != data {
        t.Fatalf("expected the lapsed entry to be reported absent")
    }

    manager := NewManager(storage, ManagerConfig{Ttl: bound.Of(time.Minute)})

    if nil != mustSession(manager.Session(context.Background(), sessionId)) {
        t.Fatalf("expected the manager to answer no session instead of panicking")
    }
}

func TestFileStorage_InPlaceWrite_AShrinkingSnapshotLeavesNoTailBehind(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    fileInstance, createErr := os.Create(path)
    if nil != createErr {
        t.Fatalf("unexpected create error: %v", createErr)
    }
    defer fileInstance.Close()

    storage, storageErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }

    for index := 0; index < 12; index++ {
        sessionId := strings.Repeat(strconv.Itoa(index%10), 32)

        if saveErr := storage.Save(context.Background(), sessionId, map[string]any{"padding": strings.Repeat("x", 64)}, bound.UnlimitedDuration()); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    large, largeErr := os.ReadFile(path)
    if nil != largeErr {
        t.Fatalf("unexpected read error: %v", largeErr)
    }

    for index := 0; index < 12; index++ {
        sessionId := strings.Repeat(strconv.Itoa(index%10), 32)

        if deleteErr := storage.Delete(context.Background(), sessionId); nil != deleteErr {
            t.Fatalf("unexpected delete error: %v", deleteErr)
        }
    }

    small, smallErr := os.ReadFile(path)
    if nil != smallErr {
        t.Fatalf("unexpected read error: %v", smallErr)
    }

    if len(small) >= len(large) {
        t.Fatalf("expected the file to shrink with the snapshot, went from %d to %d bytes", len(large), len(small))
    }

    reopened, reopenErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != reopenErr {
        t.Fatalf("the shrunk file did not decode, so a tail of the previous document survived: %v", reopenErr)
    }

    if 0 != len(reopened.sessionById) {
        t.Fatalf("expected an empty snapshot after every session was deleted, got %d", len(reopened.sessionById))
    }
}

func TestNewFileStorage_SweepsTheOrphanTemporaryFiles(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    orphanPath := filepath.Join(directory, "session.json.123456.tmp")
    if writeErr := os.WriteFile(orphanPath, []byte("{}"), 0600); nil != writeErr {
        t.Fatalf("could not plant the orphan: %v", writeErr)
    }

    unrelatedPath := filepath.Join(directory, "other.json.123456.tmp")
    if writeErr := os.WriteFile(unrelatedPath, []byte("{}"), 0600); nil != writeErr {
        t.Fatalf("could not plant the unrelated file: %v", writeErr)
    }

    backupPath := filepath.Join(directory, "session.json.bak")
    if writeErr := os.WriteFile(backupPath, []byte("{}"), 0600); nil != writeErr {
        t.Fatalf("could not plant the backup: %v", writeErr)
    }

    directoryPath := filepath.Join(directory, "session.json.654321.tmp")
    if mkdirErr := os.Mkdir(directoryPath, 0700); nil != mkdirErr {
        t.Fatalf("could not plant the directory: %v", mkdirErr)
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path})
    if nil != storageErr {
        t.Fatalf("unexpected construction error: %v", storageErr)
    }
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    if _, statErr := os.Stat(orphanPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the orphan temp file swept away, stat answered %v", statErr)
    }

    for _, survivor := range []string{unrelatedPath, backupPath, directoryPath} {
        if _, statErr := os.Stat(survivor); nil != statErr {
            t.Fatalf("%s, outside this storage's temp files, was touched: %v", filepath.Base(survivor), statErr)
        }
    }
}

func TestSyncSessionDirectory_AnswersTheDirectoryItCouldNotOpen(t *testing.T) {
    missing := filepath.Join(t.TempDir(), "absent")

    syncErr := syncSessionDirectory(missing)
    if nil == syncErr {
        t.Fatal("expected the missing directory to be refused")
    }

    if false == strings.Contains(syncErr.Error(), "fsync") {
        t.Fatalf("expected the refusal to name the fsync step, got %v", syncErr)
    }

    if false == errors.Is(syncErr, errSessionStoragePersistedDespiteFlushFailure) {
        t.Fatalf("expected the post-rename refusal to carry the persisted-despite-flush mark, got %s", syncErr.Error())
    }
}

func TestSyncSessionDirectory_MarksTheRefusedFlushAsPersisted(t *testing.T) {
    syncErr := syncSessionDirectory(refusingCharacterDevice)
    if nil == syncErr {
        t.Fatal("expected the character device to refuse the flush")
    }

    if false == errors.Is(syncErr, errSessionStoragePersistedDespiteFlushFailure) {
        t.Fatalf("expected the post-rename refusal to carry the persisted-despite-flush mark, got %s", syncErr.Error())
    }
}

func TestSyncSessionDirectory_AcceptsARealDirectory(t *testing.T) {
    if syncErr := syncSessionDirectory(t.TempDir()); nil != syncErr {
        t.Fatalf("unexpected error over a real directory: %v", syncErr)
    }
}

func TestFileStorage_ExpiryFollowsTheInjectedClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC))

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json"), Clock: frozenClock})
    if nil != storageErr {
        t.Fatalf("storage error: %v", storageErr)
    }
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    saveErr := storage.Save(context.Background(), "session-1", map[string]any{"user": "editor"}, bound.Of(time.Hour))
    if nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    _, exists, loadErr := storage.Load(context.Background(), "session-1")
    if nil != loadErr || false == exists {
        t.Fatalf("expected the fresh session, got exists=%v err=%v", exists, loadErr)
    }

    frozenClock.Advance(2 * time.Hour)

    _, exists, loadErr = storage.Load(context.Background(), "session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if true == exists {
        t.Fatalf("expected the session to lapse once the injected clock passed its ttl")
    }
}

func TestFileStorage_ThePurgeReadsTheInjectedClockNotTheWallClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))

    storagePath := filepath.Join(t.TempDir(), "sessions.json")

    storage, newErr := NewFileStorage(FileStorageConfig{Path: storagePath, Clock: frozenClock})
    if nil != newErr {
        t.Fatalf("unexpected storage error: %v", newErr)
    }
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if saveErr := storage.Save(context.Background(), "session-1", map[string]any{"user": "editor"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), "session-2", map[string]any{"user": "admin"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    _, exists, loadErr := storage.Load(context.Background(), "session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if false == exists {
        t.Fatalf("expected the purge to leave a session the injected clock says is still live")
    }
}

func TestFileStorage_TreatsTheExpiryInstantItselfAsLapsedAtTheDoor(t *testing.T) {
    frozenTime := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
    frozenClock := clock.NewFrozenClock(frozenTime)

    storagePath := filepath.Join(t.TempDir(), "sessions.json")

    storage, newErr := NewFileStorage(FileStorageConfig{Path: storagePath, Clock: frozenClock})
    if nil != newErr {
        t.Fatalf("unexpected storage error: %v", newErr)
    }
    defer func() { _ = storage.CloseWithContext(context.Background()) }()

    if saveErr := storage.Save(context.Background(), "session-1", map[string]any{"user": "editor"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    frozenClock.TravelTo(frozenTime.Add(time.Hour).Add(-time.Nanosecond))

    _, exists, loadErr := storage.Load(context.Background(), "session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if false == exists {
        t.Fatalf("expected the session to survive the last nanosecond before its expiry")
    }

    frozenClock.TravelTo(frozenTime.Add(time.Hour))

    _, exists, loadErr = storage.Load(context.Background(), "session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }

    if true == exists {
        t.Fatalf("expected the expiry instant itself to be lapsed")
    }
}

func TestFileStorage_RefusesANullSnapshotOnBothConstructors(t *testing.T) {
    path := filepath.Join(t.TempDir(), "session.json")
    if writeErr := os.WriteFile(path, []byte("null"), 0600); nil != writeErr {
        t.Fatalf("could not plant the snapshot: %v", writeErr)
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path})
    if nil == storageErr {
        _ = storage.CloseWithContext(t.Context())
        t.Fatal("expected a null snapshot to be refused by the path constructor")
    }
    if false == strings.Contains(storageErr.Error(), "must be a JSON object") {
        t.Fatalf("expected the snapshot refusal, got %v", storageErr)
    }

    fileInstance, openErr := os.OpenFile(path, os.O_RDWR, 0600)
    if nil != openErr {
        t.Fatalf("open error: %v", openErr)
    }
    defer func() {
        _ = fileInstance.Close()
    }()

    handleStorage, handleErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil == handleErr {
        _ = handleStorage.CloseWithContext(t.Context())
        t.Fatal("expected a null snapshot to be refused by the handle constructor")
    }
    if false == strings.Contains(handleErr.Error(), "must be a JSON object") {
        t.Fatalf("expected the snapshot refusal, got %v", handleErr)
    }

    content, readErr := os.ReadFile(path)
    if nil != readErr || "null" != string(content) {
        t.Fatalf("expected the refused snapshot to stay untouched, got %q, %v", string(content), readErr)
    }
}

func TestFileStorage_SavesUnderABasenameThatFillsTheFilesystemComponent(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, strings.Repeat("s", 250)+".json")

    orphanPath := filepath.Join(directory, sessionTemporaryPrefix(path)+"123456.tmp")
    if writeErr := os.WriteFile(orphanPath, []byte("{}"), 0600); nil != writeErr {
        t.Fatalf("could not plant the orphan: %v", writeErr)
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path})
    if nil != storageErr {
        t.Fatalf("unexpected construction error: %v", storageErr)
    }
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    if _, statErr := os.Stat(orphanPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the orphan of the long basename swept away, stat answered %v", statErr)
    }

    if saveErr := storage.Save(context.Background(), "session-id", map[string]any{"user": "u"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected a save under a basename of 255 bytes, got %v", saveErr)
    }

    entries, readErr := os.ReadDir(directory)
    if closeErr := storage.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    entries, readErr = os.ReadDir(directory)
    expectedNames := map[string]bool{filepath.Base(path): true, filepath.Base(sessionJournalPath(path)): true}
    if nil != readErr || 2 != len(entries) || false == expectedNames[entries[0].Name()] || false == expectedNames[entries[1].Name()] {
        t.Fatalf("expected only the snapshot and its journal in the directory, got %v, %v", entries, readErr)
    }
}

func TestFileStorage_MaxSessionsRefusesANewIdAtTheCeilingAndKeepsTheStoredOnes(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path, MaxSessions: bound.Of(2)})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }

    for _, sessionId := range []string{"first", "second"} {
        if saveErr := storage.Save(context.Background(), sessionId, map[string]any{"key": "value"}, bound.Of(time.Hour)); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    saveErr := storage.Save(context.Background(), "third", map[string]any{}, bound.Of(time.Hour))
    if nil == saveErr || "the session file storage is full" != saveErr.Error() {
        t.Fatalf("expected the new id refused, got %v", saveErr)
    }

    if "FileStorageConfig.MaxSessions" != exception.LogContext(saveErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the refusal to name the field, got %v", exception.LogContext(saveErr, exception.LogContextConfig{}))
    }

    if _, found, _ := storage.Load(context.Background(), "third"); true == found {
        t.Fatalf("expected the refused id not stored")
    }

    if resaveErr := storage.Save(context.Background(), "first", map[string]any{"key": "other"}, bound.Of(time.Hour)); nil != resaveErr {
        t.Fatalf("expected a stored id saved at the ceiling, got %v", resaveErr)
    }
}

func TestFileStorage_ASessionLapsedInALoadedFileFreesItsSlotAtTheCeiling(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2020, time.March, 1, 12, 0, 0, 0, time.UTC))
    path := filepath.Join(t.TempDir(), "sessions.json")

    writer, writerErr := NewFileStorage(FileStorageConfig{Path: path, Clock: frozenClock})
    if nil != writerErr {
        t.Fatalf("unexpected storage error: %v", writerErr)
    }

    for sessionId, ttl := range map[string]time.Duration{"short": time.Minute, "long": 2 * time.Hour} {
        if saveErr := writer.Save(context.Background(), sessionId, map[string]any{}, bound.Of(ttl)); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    frozenClock.Advance(time.Minute)

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path, Clock: frozenClock, MaxSessions: bound.Of(2)})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }

    if saveErr := storage.Save(context.Background(), "late", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected the lapsed session's slot freed, got %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), "fuller", map[string]any{}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected the id refused with the table full again")
    }

    if expected := frozenClock.Now().Add(time.Hour).UnixNano(); expected != storage.earliestExpiry {
        t.Fatalf("expected the sweep to move the earliest expiry to %d, got %d", expected, storage.earliestExpiry)
    }

    frozenClock.Advance(time.Hour)

    if saveErr := storage.Save(context.Background(), "after", map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected the sweep's next earliest expiry honoured, got %v", saveErr)
    }
}

func TestNewFileStorage_MaxSessionsResolves(t *testing.T) {
    defaulted, defaultedErr := NewFileStorage(FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json")})
    if nil != defaultedErr || 100_000 != defaulted.maxSessions.Value {
        t.Fatalf("expected 100,000 when left out, got %+v (%v)", defaulted, defaultedErr)
    }

    unlimited, unlimitedErr := NewFileStorage(FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json"), MaxSessions: bound.UnlimitedCount()})
    if nil != unlimitedErr || false == unlimited.maxSessions.Unlimited {
        t.Fatalf("expected unlimited admitted, got %+v (%v)", unlimited, unlimitedErr)
    }

    _, refusedErr := NewFileStorage(FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json"), MaxSessions: bound.Of(0)})
    if nil == refusedErr || false == strings.Contains(fmt.Sprint(exception.LogContext(refusedErr, exception.LogContextConfig{})), "FileStorageConfig.MaxSessions") {
        t.Fatalf("expected a zero ceiling refused naming the field, got %v", refusedErr)
    }
}

func TestFileStorage_AFileHoldingMoreThanTheCeilingKeepsThemAndRefusesANewId(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    writer, writerErr := NewFileStorage(FileStorageConfig{Path: path})
    if nil != writerErr {
        t.Fatalf("unexpected storage error: %v", writerErr)
    }

    for _, sessionId := range []string{"first", "second", "third"} {
        if saveErr := writer.Save(context.Background(), sessionId, map[string]any{}, bound.Of(time.Hour)); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    storage, storageErr := NewFileStorage(FileStorageConfig{Path: path, MaxSessions: bound.Of(2)})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }

    for _, sessionId := range []string{"first", "second", "third"} {
        if _, found, _ := storage.Load(context.Background(), sessionId); false == found {
            t.Fatalf("expected %q kept", sessionId)
        }
    }

    if saveErr := storage.Save(context.Background(), "fourth", map[string]any{}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected a new id refused")
    }

    if saveErr := storage.Save(context.Background(), "first", map[string]any{"key": "value"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected a stored id saved, got %v", saveErr)
    }
}

func TestFileStorage_TheHandleFormCarriesTheCeiling(t *testing.T) {
    fileInstance, createErr := os.CreateTemp(t.TempDir(), "sessions")
    if nil != createErr {
        t.Fatalf("unexpected create error: %v", createErr)
    }
    defer func() {
        _ = fileInstance.Close()
    }()

    storage, storageErr := NewFileStorage(FileStorageConfig{File: fileInstance, MaxSessions: bound.Of(1)})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }

    if saveErr := storage.Save(context.Background(), "first", map[string]any{}, bound.UnlimitedDuration()); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if saveErr := storage.Save(context.Background(), "second", map[string]any{}, bound.UnlimitedDuration()); nil == saveErr || "the session file storage is full" != saveErr.Error() {
        t.Fatalf("expected the new id refused, got %v", saveErr)
    }

    if deleteErr := storage.Delete(context.Background(), "first"); nil != deleteErr {
        t.Fatalf("unexpected delete error: %v", deleteErr)
    }

    if saveErr := storage.Save(context.Background(), "second", map[string]any{}, bound.UnlimitedDuration()); nil != saveErr {
        t.Fatalf("expected the deleted session's slot freed, got %v", saveErr)
    }
}

type cyclicChainError struct {
    next error
}

func (instance *cyclicChainError) Error() string {
    return "cyclic link"
}

func (instance *cyclicChainError) Unwrap() error {
    return instance.next
}

func newCyclicChainError() *cyclicChainError {
    first := &cyclicChainError{}
    first.next = &cyclicChainError{next: first}

    return first
}

func assertReturnsOnACyclicChain(t *testing.T, callback func()) {
    t.Helper()

    done := make(chan struct{})
    go func() {
        defer close(done)
        callback()
    }()

    select {
    case <-done:
    case <-time.After(5 * time.Second):
        t.Fatal("expected the call to return on an error chain that reaches itself")
    }
}

type failingMarshalValue struct {
    failure *error
}

func (instance failingMarshalValue) MarshalJSON() ([]byte, error) {
    return []byte("null"), *instance.failure
}

func TestFileStorage_SaveAndDeleteReturnOnACyclicChainFromAValue(t *testing.T) {
    fileInstance, createErr := os.CreateTemp(t.TempDir(), "melody_session_cyclic_*.json")
    if nil != createErr {
        t.Fatalf("unexpected create temp error: %s", createErr.Error())
    }

    defer func() {
        _ = fileInstance.Close()
    }()

    storage, storageErr := NewFileStorage(FileStorageConfig{File: fileInstance})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %s", storageErr.Error())
    }

    var failure error
    value := map[string]any{"custom": failingMarshalValue{failure: &failure}}

    for _, sessionId := range []string{"kept", "deleted"} {
        if saveErr := storage.Save(context.Background(), sessionId, value, bound.Of(time.Minute)); nil != saveErr {
            t.Fatalf("unexpected save error: %s", saveErr.Error())
        }
    }

    failure = newCyclicChainError()

    assertReturnsOnACyclicChain(t, func() {
        if saveErr := storage.Save(context.Background(), "refused", value, bound.Of(time.Minute)); nil == saveErr {
            t.Error("expected the save to carry the value's failure")
        }

        if deleteErr := storage.Delete(context.Background(), "deleted"); nil == deleteErr {
            t.Error("expected the delete to carry the value's failure")
        }
    })
}

func fileStorageFactories() map[string]storageFactory {
    return map[string]storageFactory{
        "path with the journal": func(t *testing.T, clockInstance clockcontract.Clock) sessioncontract.Storage {
            return newJournalStorage(t, FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json"), Clock: clockInstance})
        },
        "path without the journal": func(t *testing.T, clockInstance clockcontract.Clock) sessioncontract.Storage {
            return newJournalStorage(t, FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json"), JournalDisabled: true, Clock: clockInstance})
        },
        "file": func(t *testing.T, clockInstance clockcontract.Clock) sessioncontract.Storage {
            fileInstance, createErr := os.CreateTemp(t.TempDir(), "sessions_*.json")
            if nil != createErr {
                t.Fatalf("unexpected create temp error: %v", createErr)
            }
            t.Cleanup(func() { _ = fileInstance.Close() })

            return newJournalStorage(t, FileStorageConfig{File: fileInstance, Clock: clockInstance})
        },
    }
}

func runEveryFileStorageForm(t *testing.T, assertion func(t *testing.T, factory storageFactory)) {
    for name, factory := range fileStorageFactories() {
        t.Run(name, func(t *testing.T) {
            assertion(t, factory)
        })
    }
}

func TestFileStorage_EveryDoorRefusesANilAndATypedNilContext(t *testing.T) {
    runEveryFileStorageForm(t, assertEveryStorageDoorRefusesANilContextFirst)
}

func TestFileStorage_ADoneContextAnswersItsErrorBeforeTheStoreIsTouched(t *testing.T) {
    runEveryFileStorageForm(t, assertStorageAnswersADoneContextBeforeTheStoreIsTouched)
}

func TestFileStorage_TheArgumentsAreJudgedBeforeADoneContext(t *testing.T) {
    runEveryFileStorageForm(t, assertStorageJudgesItsArgumentsBeforeADoneContext)
}

func TestFileStorage_ADoneContextIsAnsweredBeforeTheFullStorage(t *testing.T) {
    storage, storageErr := NewFileStorage(FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json"), MaxSessions: bound.Of(1)})
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }
    defer func() {
        _ = storage.CloseWithContext(context.Background())
    }()

    if seedErr := storage.Save(context.Background(), "first", map[string]any{"key": "value"}, bound.Of(time.Hour)); nil != seedErr {
        t.Fatalf("unexpected seed error: %v", seedErr)
    }

    if saveErr := storage.Save(cancelledContext(), "second", map[string]any{}, bound.Of(time.Hour)); false == errors.Is(saveErr, context.Canceled) {
        t.Fatalf("expected the done context answered before the ceiling, got %v", saveErr)
    }
}

func TestFileStorage_SaveRefusesAnAbsentTtlAndStoresNothing(t *testing.T) {
    runEveryFileStorageForm(t, assertStorageRefusesAnAbsentTtlAndStoresNothing)
}

func TestFileStorage_AnUnlimitedTtlNeverLapses(t *testing.T) {
    runEveryFileStorageForm(t, assertStorageKeepsAnUnlimitedTtl)
}

func TestFileStorage_SaveRefusesATtlBelowASecond(t *testing.T) {
    runEveryFileStorageForm(t, assertStorageRefusesATtlBelowASecond)
}

func TestFileStorage_AnUnlimitedTtlIsPersistedAsNoExpiry(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    storage := newJournalStorage(t, FileStorageConfig{Path: path})

    if saveErr := storage.Save(context.Background(), contractTestSessionId, map[string]any{"a": 1}, bound.UnlimitedDuration()); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    if closeErr := storage.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    reopened := newJournalStorage(t, FileStorageConfig{Path: path})
    defer reopened.CloseWithContext(context.Background())

    entry, exists := reopened.sessionById[contractTestSessionId]
    if false == exists || 0 != entry.ExpiresAt {
        t.Fatalf("expected the unlimited session persisted with no expiry, got %v and %v", entry, exists)
    }
}

func TestFileStorage_ADoneContextPaysForNoPendingCompaction(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    mustSave(t, storage, "pending", map[string]any{"k": "v"})

    storage.journalNeedsCompaction = true
    snapshotBefore, _ := os.ReadFile(path)
    journalBefore := journalSize(t, path)

    if saveErr := storage.Save(cancelledContext(), "other", map[string]any{"k": "v"}, bound.Of(time.Hour)); false == errors.Is(saveErr, context.Canceled) {
        t.Fatalf("expected the context error, got %v", saveErr)
    }

    snapshotAfter, _ := os.ReadFile(path)
    if false == bytes.Equal(snapshotBefore, snapshotAfter) || journalBefore != journalSize(t, path) || false == storage.journalNeedsCompaction {
        t.Fatalf("expected a save under a done context to run no pending compaction")
    }
}

func TestFileStorage_ADoneContextDeleteOfAnUnknownIdAnswersTheContext(t *testing.T) {
    runEveryFileStorageForm(t, func(t *testing.T, factory storageFactory) {
        storage := factory(t, clock.NewSystemClock())
        defer storage.CloseWithContext(context.Background())

        if deleteErr := storage.Delete(cancelledContext(), "unknown"); false == errors.Is(deleteErr, context.Canceled) {
            t.Fatalf("expected the context error for an unknown id, got %v", deleteErr)
        }
    })
}

func TestFileStorage_AContextThatEndsDuringTheWriteDoesNotTearIt(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    storage := newJournalStorage(t, FileStorageConfig{Path: path})

    writeContext, cancel := context.WithCancel(context.Background())
    defer cancel()

    storage.journalSeam = func(point journalPoint) {
        if journalPointRecordWritten == point {
            cancel()
        }
    }

    if saveErr := storage.Save(writeContext, contractTestSessionId, map[string]any{"k": "whole"}, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("expected a write that began to finish, got %v", saveErr)
    }

    if nil == writeContext.Err() {
        t.Fatalf("expected the seam to have cancelled the context during the write")
    }

    storage.journalSeam = nil
    _ = storage.journal.Close()

    reopened := newJournalStorage(t, FileStorageConfig{Path: path})
    defer reopened.CloseWithContext(context.Background())

    data, exists, loadErr := reopened.Load(context.Background(), contractTestSessionId)
    if nil != loadErr || false == exists || "whole" != data["k"] {
        t.Fatalf("expected the replay to read the record whole, got %v, %v and %v", data, exists, loadErr)
    }
}
