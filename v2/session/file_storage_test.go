package session

import (
    "os"
    "path/filepath"
    "strconv"
    "sync"
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

func TestFileStorage_Load_ExpiredEntryIsDeleted(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    if saveErr := storage.Save("expired", map[string]any{"k": "v"}, time.Nanosecond); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    time.Sleep(10 * time.Millisecond)

    data, exists, loadErr := storage.Load("expired")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if true == exists {
        t.Fatalf("expected expired entry to be removed")
    }

    if nil != data {
        t.Fatalf("expected nil data for expired entry")
    }

    storage2, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage2.Close()

    _, existsAfterReload, loadAfterReloadErr := storage2.Load("expired")
    if nil != loadAfterReloadErr {
        t.Fatalf("unexpected reload error: %s", loadAfterReloadErr.Error())
    }

    if true == existsAfterReload {
        t.Fatalf("expected expired entry to be persisted as removed")
    }
}

func TestFileStorage_Close_IsIdempotent(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if err := storage.Close(); nil != err {
        t.Fatalf("unexpected first close error: %s", err.Error())
    }

    if err := storage.Close(); nil != err {
        t.Fatalf("unexpected second close error: %s", err.Error())
    }
}

func TestFileStorage_Save_AfterCloseReturnsError(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    _ = storage.Close()

    saveErr := storage.Save("k", map[string]any{"v": 1}, time.Minute)
    if nil == saveErr {
        t.Fatalf("expected save after close to error")
    }
}

func TestFileStorage_AtomicWrite_DoesNotLeaveTempFiles(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    for iteration := 0; iteration < 5; iteration++ {
        saveErr := storage.Save(
            "s"+strconv.Itoa(iteration),
            map[string]any{"iteration": iteration},
            time.Minute,
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
        if "session.json" == name {
            continue
        }

        t.Fatalf("unexpected leftover file in session directory: %s", name)
    }
}

func TestFileStorage_ConcurrentLoadSaveIsRaceFree(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    sessionId := "concurrent-session"

    if saveErr := storage.Save(sessionId, map[string]any{"counter": 0}, time.Minute); nil != saveErr {
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
                    sessionId,
                    map[string]any{
                        "counter": index,
                        "worker":  strconv.Itoa(writerId),
                        "nested": map[string]any{
                            "value": index,
                        },
                    },
                    time.Minute,
                )
            }
        }(writerIndex)
    }

    for readerIndex := 0; readerIndex < 4; readerIndex++ {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()
            for index := 0; index < iterations; index++ {
                loaded, _, loadErr := storage.Load(sessionId)
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

    storage, err := NewFileStorageFromFile(fileInstance)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    saveErr := storage.Save("keep", map[string]any{"k": "v"}, 0)
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

    /* @important a Save whose value cannot be JSON-encoded (here a channel) must fail without truncating the live file and destroying the already-persisted "keep" session — the in-place writer must encode before it truncates, mirroring the atomic writer */
    badSaveErr := storage.Save("bad", map[string]any{"ch": make(chan int)}, 0)
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

    reader, err := NewFileStorageFromFile(fileInstance)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    data, exists, loadErr := reader.Load("keep")
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

    storage, err := NewFileStorageFromFile(fileInstance)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save("existing", map[string]any{"v": "old"}, 0); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    /* @important a failed Save of a NEW id must roll the in-memory entry back so Load does not surface a session that was never persisted */
    if newErr := storage.Save("fresh", map[string]any{"ch": make(chan int)}, 0); nil == newErr {
        t.Fatalf("expected the non-marshalable save to fail")
    }
    if _, exists, loadErr := storage.Load("fresh"); nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    } else if true == exists {
        t.Fatalf("a failed Save of a new id must not be observable via Load")
    }

    /* @important a failed Save that updates an EXISTING id must restore the previous in-memory value */
    if updErr := storage.Save("existing", map[string]any{"ch": make(chan int)}, 0); nil == updErr {
        t.Fatalf("expected the non-marshalable update to fail")
    }
    data, exists, loadErr := storage.Load("existing")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }
    if false == exists || "old" != data["v"].(string) {
        t.Fatalf("a failed update must restore the previous in-memory value, got exists=%v data=%v", exists, data)
    }
}
