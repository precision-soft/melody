package session

import (
    "math"
    "os"
    "os/exec"
    "os/signal"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "syscall"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/clock"
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

/* expireStoredEntry rewinds a stored entry's expiry so it lapses without waiting for the wall clock. FileStorage
reads time.Now directly, and Save purges anything already lapsed before it returns, so a ttl short enough to expire
on its own never leaves an entry for Load to find. */
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

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    /* a ttl that outlives Save, so both entries reach the file and stay in the map */
    if saveErr := storage.Save("expired", map[string]any{"k": "v"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if saveErr := storage.Save("live", map[string]any{"k": "live"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    expireStoredEntry(t, storage, "expired")

    data, exists, loadErr := storage.Load("expired")
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

    liveData, liveExists, liveErr := storage.Load("live")
    if nil != liveErr {
        t.Fatalf("unexpected live load error: %s", liveErr.Error())
    }
    if false == liveExists || "live" != liveData["k"] {
        t.Fatalf("expected the unexpired sibling to survive, got exists=%v data=%v", liveExists, liveData)
    }

    /* the file still carried the entry with its original future expiry, so a reader that sees it gone proves Load
       rewrote the snapshot rather than merely reporting the entry as absent */
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

    _, liveExistsAfterReload, liveReloadErr := storage2.Load("live")
    if nil != liveReloadErr {
        t.Fatalf("unexpected live reload error: %s", liveReloadErr.Error())
    }

    if false == liveExistsAfterReload {
        t.Fatalf("expected the unexpired sibling to survive the rewrite")
    }
}

/* an entry nobody loads is only ever dropped by the purge every flush runs; without it the map and the snapshot
grow with everything that ever expired */
func TestFileStorage_Save_PurgesEntriesThatLapsedWithoutBeingLoaded(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    if saveErr := storage.Save("lapsed", map[string]any{"k": "v"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if saveErr := storage.Save("keeper", map[string]any{"k": "keeper"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    expireStoredEntry(t, storage, "lapsed")

    /* an unrelated Save is the only thing that happens: nothing ever names the lapsed entry */
    if saveErr := storage.Save("keeper", map[string]any{"k": "keeper"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if _, stillStored := storage.sessionById["lapsed"]; true == stillStored {
        t.Fatalf("expected the lapsed entry to be purged from the in-memory state")
    }

    reader, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer reader.Close()

    if _, stillPersisted := reader.sessionById["lapsed"]; true == stillPersisted {
        t.Fatalf("expected the lapsed entry to be purged from the snapshot")
    }

    if _, keeperPersisted := reader.sessionById["keeper"]; false == keeperPersisted {
        t.Fatalf("expected the unexpired entry to survive the purge")
    }
}

/* Delete refuses an empty id the way Load and Save do — the id is what names the entry, and an empty one would otherwise reach the map as a real key */
func TestFileStorage_Delete_RefusesAnEmptySessionId(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    deleteErr := storage.Delete("")
    if nil == deleteErr {
        t.Fatalf("expected an empty session id to be refused")
    }

    if "session id is required in delete session" != deleteErr.Error() {
        t.Fatalf("expected the empty id refusal, got %q", deleteErr.Error())
    }
}

/* a closed storage refuses to delete, exactly as it refuses to load and to save: the file is gone and the map is no longer authoritative, so a delete that reported success would tell a caller a session was dropped when nothing was written */
func TestFileStorage_Delete_AfterCloseReturnsError(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save("abc", map[string]any{"k": "v"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    _ = storage.Close()

    deleteErr := storage.Delete("abc")
    if nil == deleteErr {
        t.Fatalf("expected delete after close to error")
    }

    if "session storage is closed" != deleteErr.Error() {
        t.Fatalf("expected the closed refusal, got %q", deleteErr.Error())
    }
}

/* Delete takes the entry out of the map and out of the file, and a delete of an id the storage does not hold answers success without writing anything.

The absent id is proved on a storage whose flush cannot write: the read-only handle turns any flush into an error, so a delete that skipped the early return would surface it. That distinguishes "returned nil because nothing was there" from "returned nil after rewriting the snapshot". */
func TestFileStorage_Delete_RemovesTheEntryAndSkipsTheFlushForAnAbsentId(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save("deleted", map[string]any{"k": "v"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if saveErr := storage.Save("kept", map[string]any{"k": "kept"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    if deleteErr := storage.Delete("deleted"); nil != deleteErr {
        t.Fatalf("unexpected delete error: %s", deleteErr.Error())
    }

    if _, stillStored := storage.sessionById["deleted"]; true == stillStored {
        t.Fatalf("expected the deleted entry to be gone from the in-memory state")
    }

    _, exists, loadErr := storage.Load("deleted")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }
    if true == exists {
        t.Fatalf("expected the deleted session to be absent")
    }

    _ = storage.Close()

    reader, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer reader.Close()

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

    readOnlyHandle, openErr := os.OpenFile(readOnlyPath, os.O_RDONLY, 0644)
    if nil != openErr {
        t.Fatalf("unexpected error opening the read-only storage file: %s", openErr.Error())
    }
    defer readOnlyHandle.Close()

    unwritableStorage, err := NewFileStorageFromFile(readOnlyHandle)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if deleteErr := unwritableStorage.Delete("never-stored"); nil != deleteErr {
        t.Fatalf("expected deleting an absent id to answer success without flushing, got %s", deleteErr.Error())
    }
}

/* a Delete whose flush fails must put the entry back: the session is still in the file, and dropping it from the map as well would make the storage answer "no such session" for something a reader reopening the file still finds */
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

    storage, err := NewFileStorageFromFile(fileInstance)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save("doomed", map[string]any{"k": "v"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    /* the handle the storage writes through is closed under it, so the flush the delete triggers cannot write */
    if closeErr := fileInstance.Close(); nil != closeErr {
        t.Fatalf("unexpected error closing the handle: %s", closeErr.Error())
    }

    deleteErr := storage.Delete("doomed")
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

/* the storage directory is created at construction, and a path whose directory cannot exist is refused there rather than on the first flush — the boot is where a bad path is fixable */
func TestNewFileStorageFromPath_RefusesAPathWhoseDirectoryCannotBeCreated(t *testing.T) {
    directory := t.TempDir()

    blockingFilePath := filepath.Join(directory, "blocking")
    if writeErr := os.WriteFile(blockingFilePath, []byte("x"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the blocking file: %s", writeErr.Error())
    }

    storage, storageErr := NewFileStorageFromPath(filepath.Join(blockingFilePath, "nested", "session.json"))
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

/* a closed storage refuses to load as well as to save and delete: the map it still holds is no longer authoritative, and answering from it would serve a session the file may no longer carry */
func TestFileStorage_Load_AfterCloseReturnsError(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    if saveErr := storage.Save("abc", map[string]any{"k": "v"}, time.Hour); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    _ = storage.Close()

    _, exists, loadErr := storage.Load("abc")
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

/* a file that cannot be opened for a reason other than "it is not there yet" is reported: the absent file is the ordinary first-boot case and answers an empty store, while anything else — a path that is not reachable at all — must not be read as "no sessions yet" and then overwritten by the first flush.

The state is built by calling the reader directly, because the constructor above it creates the directory first and would fail before this line. */
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

    /* the missing file itself is the ordinary case and answers an empty store */
    empty, missingErr := readSessionFileAtPath(filepath.Join(directory, "never-written.json"))
    if nil != missingErr {
        t.Fatalf("expected a missing file to answer an empty store, got %s", missingErr.Error())
    }
    if 0 != len(empty) {
        t.Fatalf("expected an empty store for a missing file, got %d entries", len(empty))
    }
}

/* a storage that does not own its file has nothing to write through once the handle is gone, and the flush says so instead of reporting success over a write that never happened. The constructors never produce this state — the injecting one refuses a nil handle — so it is built here; the guard is what keeps the invariant from being the only thing between a lost write and a caller that was told it succeeded. */
func TestFileStorage_FlushLocked_RefusesWhenTheInjectedHandleIsGone(t *testing.T) {
    fileInstance, err := os.CreateTemp("", "melody_session_handle_gone_*.json")
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

    storage.file = nil

    saveErr := storage.Save("abc", map[string]any{"k": "v"}, time.Hour)
    if nil == saveErr {
        t.Fatalf("expected the flush to refuse when there is no handle to write through")
    }

    if "session storage file is nil" != saveErr.Error() {
        t.Fatalf("expected the missing handle refusal, got %q", saveErr.Error())
    }
}

/* a storage that owns its file reports a failing close instead of swallowing it, because that failure is where a buffered write is lost. The two constructors never produce this state — the one that owns the file opens it per write and never holds a handle — so the state is built here directly; the guard is what keeps the invariant from being the only thing holding the report up. */
func TestFileStorage_Close_ReportsAFailingCloseOfTheOwnedFile(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }

    ownedFile, createErr := os.Create(filepath.Join(directory, "owned.json"))
    if nil != createErr {
        t.Fatalf("unexpected create error: %s", createErr.Error())
    }

    /* closing it here is what makes the storage's own close fail */
    if closeErr := ownedFile.Close(); nil != closeErr {
        t.Fatalf("unexpected error closing the handle: %s", closeErr.Error())
    }

    storage.file = ownedFile

    closeErr := storage.Close()
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

/* the snapshot is written to a temp file and moved into place, so a failed move must take the temp file with it: the directory the sessions live in would otherwise collect one orphan per failed write, and each orphan holds a full copy of every live session on disk.

The move is made to fail structurally — the destination is a non-empty directory, which rename can never replace — because the test runs as root, where permissions refuse nothing. */
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

/* a storage directory that cannot be created is named as such rather than surfacing as a temp-file failure one line below, because the path an operator has to fix is the directory */
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

/* the injected handle is read from the start, and a handle that cannot be positioned is refused at construction rather than decoded from wherever it happened to stand: a storage built over it would answer with whatever the tail of the file parsed into. */
func TestNewFileStorageFromFile_RefusesAHandleThatCannotBeSeeked(t *testing.T) {
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

    storage, storageErr := NewFileStorageFromFile(fileInstance)
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

/* an appending handle ignores every seek, so each snapshot landed after the document it was replacing and the truncation then cut the pair to the new length. Refusing it at construction is the only place the operator can still be told: the saves that follow report success. */
func TestNewFileStorageFromFile_RefusesAHandleOpenedForAppending(t *testing.T) {
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

    storage, storageErr := NewFileStorageFromFile(fileInstance)
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

/* the refusal above is a door, not the guarantee: the write itself names its offset, so even a handle that reached the storage another way cannot land a snapshot at the end of the file. */
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

/* a snapshot file whose content is not the map the storage wrote is refused at construction: decoding it into a zero-value map would present an empty session store as a healthy one and silently log every user out, and the next write would overwrite whatever was really there. */
func TestNewFileStorageFromPath_RefusesAFileThatIsNotASnapshot(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    if writeErr := os.WriteFile(path, []byte("not json at all"), 0644); nil != writeErr {
        t.Fatalf("unexpected error seeding the storage file: %s", writeErr.Error())
    }

    storage, storageErr := NewFileStorageFromPath(path)
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

/* the path is what names the file the sessions live in, so a path that names nothing is refused at construction instead of writing a snapshot into the working directory */
func TestNewFileStorageFromPath_RefusesAnEmptyPath(t *testing.T) {
    for _, path := range []string{"", ".", "./"} {
        storage, storageErr := NewFileStorageFromPath(path)
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

/* a nil handle is refused where a nil path is refused: the storage would otherwise construct successfully and fail on the first flush, long after the wiring mistake */
func TestNewFileStorageFromFile_RefusesANilHandle(t *testing.T) {
    storage, storageErr := NewFileStorageFromFile(nil)
    if nil == storageErr {
        t.Fatalf("expected a nil handle to be refused")
    }

    if nil != storage {
        t.Fatalf("expected no storage over a nil handle")
    }

    if "session storage file is nil" != storageErr.Error() {
        t.Fatalf("expected the nil handle refusal, got %q", storageErr.Error())
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

    /* a Save whose value cannot be JSON-encoded (here a channel) must fail without truncating the live file and destroying the already-persisted "keep" session — the in-place writer must encode before it truncates, mirroring the atomic writer */
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

    /* a failed Save of a NEW id must roll the in-memory entry back so Load does not surface a session that was never persisted */
    if newErr := storage.Save("fresh", map[string]any{"ch": make(chan int)}, 0); nil == newErr {
        t.Fatalf("expected the non-marshalable save to fail")
    }
    if _, exists, loadErr := storage.Load("fresh"); nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    } else if true == exists {
        t.Fatalf("a failed Save of a new id must not be observable via Load")
    }

    /* a failed Save that updates an EXISTING id must restore the previous in-memory value */
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

func TestFileStorage_Save_TtlBeyondYear2262IsKeptNotPurged(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    /* a ttl that pushes the expiry past 2262-04-11 overflows time.Time.UnixNano to a negative int64; the session must be kept with a saturated far-future expiry exactly as InMemoryStorage keeps it, not treated as already expired and purged on the same Save */
    if saveErr := storage.Save("forever", map[string]any{"k": "v"}, time.Duration(math.MaxInt64)); nil != saveErr {
        t.Fatalf("unexpected save error: %s", saveErr.Error())
    }

    data, exists, loadErr := storage.Load("forever")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }
    if false == exists {
        t.Fatalf("expected the session saved with a very large ttl to be kept, not purged as already expired")
    }
    if "v" != data["k"].(string) {
        t.Fatalf("expected the persisted session data to survive, got %v", data)
    }

    /* it must also survive a reload from disk — an overflowed negative expiry would have been dropped from the snapshot on the same Save and never written to the file */
    reader, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected reader storage error: %s", err.Error())
    }
    defer reader.Close()

    reloadData, reloadExists, reloadErr := reader.Load("forever")
    if nil != reloadErr {
        t.Fatalf("unexpected reload error: %s", reloadErr.Error())
    }
    if false == reloadExists || "v" != reloadData["k"].(string) {
        t.Fatalf("expected the session to persist across instances, got exists=%v data=%v", reloadExists, reloadData)
    }
}

/* Loading an expired session must remove it from the map and from the file, and the flush inside Load is the only thing that does it: purgeExpiredLocked runs inside flushLocked against the same clock and the same predicate, so Load names no session of its own. This pins that mechanism — if the purge ever stops covering a lapsed entry, the explicit delete has to come back.

Both sessions are stored with a lifetime that cannot lapse while they are being written, and the one under test is aged afterwards by rewriting its stored instant rather than by sleeping. A short ttl plus a sleep does not pin this: the second Save flushes too, and the purge inside that flush drops an entry the first Save aged past its lifetime while the file was being written, so Load is handed a session that is already gone and the branch this test names is never entered. It stayed green with the flush removed from Load entirely. */
func TestFileStorage_LoadingAnExpiredSessionRemovesItFromTheFile(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "sessions.json")

    storage, newErr := NewFileStorageFromPath(path)
    if nil != newErr {
        t.Fatalf("unexpected storage error: %v", newErr)
    }

    saveErr := storage.Save("expired", map[string]any{"user": "alice"}, time.Hour)
    if nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    saveErr = storage.Save("live", map[string]any{"user": "bob"}, time.Hour)
    if nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    agedEntry := storage.sessionById["expired"]
    agedEntry.ExpiresAt = time.Now().Add(-time.Hour).UnixNano()
    storage.sessionById["expired"] = agedEntry

    _, exists, loadErr := storage.Load("expired")
    if nil != loadErr {
        t.Fatalf("unexpected load error: %v", loadErr)
    }

    if true == exists {
        t.Fatalf("expected the expired session not to be handed back")
    }

    if _, stillThere := storage.sessionById["expired"]; true == stillThere {
        t.Fatalf("expected the expired session to be gone from the map")
    }

    reopened, reopenErr := NewFileStorageFromPath(path)
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

/* The answer to a load of a lapsed entry — no such session — is settled before the housekeeping flush is attempted, so that flush failing must not change it. Returning its error instead would make Manager.Session panic, so an expired cookie on a store that cannot be written would answer 500 where a client holding no cookie at all is served a fresh session. */
func TestFileStorage_LoadOfALapsedEntryAnswersAbsentWhenTheFlushCannotWrite(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "sessions.json")

    if err := os.WriteFile(path, []byte("{}\n"), 0644); nil != err {
        t.Fatalf("unexpected error seeding the storage file: %v", err)
    }

    readOnlyHandle, err := os.OpenFile(path, os.O_RDONLY, 0644)
    if nil != err {
        t.Fatalf("unexpected error opening the storage file: %v", err)
    }
    defer readOnlyHandle.Close()

    storage, err := NewFileStorageFromFile(readOnlyHandle)
    if nil != err {
        t.Fatalf("unexpected error constructing the storage: %v", err)
    }

    sessionId := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

    storage.sessionById[sessionId] = fileSessionEntry{
        Data:      map[string]any{"userId": "u-1"},
        ExpiresAt: time.Now().Add(-time.Hour).UnixNano(),
    }

    data, exists, loadErr := storage.Load(sessionId)
    if nil != loadErr {
        t.Fatalf("expected a lapsed entry to answer absent rather than surface the housekeeping failure, got %v", loadErr)
    }

    if true == exists || nil != data {
        t.Fatalf("expected the lapsed entry to be reported absent")
    }

    manager := NewManager(storage, time.Minute)

    if nil != manager.Session(sessionId) {
        t.Fatalf("expected the manager to answer no session instead of panicking")
    }
}

const fileStorageWriteWindowProbeMarker = "MELODY_SESSION_WRITE_WINDOW_PROBE"

/* the in-place writer must never leave the file empty: the order was a truncation to zero followed by the write, so a process killed between the two — an OOM kill, a docker kill, a deploy with no grace period — left a zero-length file that the next boot reads as "no sessions at all" and answers by logging every user out with no error anywhere.

The kill is stood in for by a file size limit of zero, which is the only injection that reproduces it deterministically: a truncation to zero stays inside the limit and succeeds, while the write that follows fails at its first byte. The limit is process-wide, so this runs in a child of its own. */
func TestFileStorage_InPlaceWrite_ARefusedWriteLeavesThePersistedSessionsIntact(t *testing.T) {
    if "1" == os.Getenv(fileStorageWriteWindowProbeMarker) {
        runFileStorageWriteWindowChild()

        return
    }

    command := exec.Command(
        os.Args[0],
        "-test.run=^TestFileStorage_InPlaceWrite_ARefusedWriteLeavesThePersistedSessionsIntact$",
    )
    command.Env = append(os.Environ(), fileStorageWriteWindowProbeMarker+"=1")

    output, runErr := command.CombinedOutput()
    if nil != runErr {
        t.Fatalf("expected the child to finish normally, got %v with output %q", runErr, string(output))
    }

    if true == strings.Contains(string(output), "emptied") {
        t.Fatalf("the refused write left the file empty, destroying every persisted session: %q", string(output))
    }

    if false == strings.Contains(string(output), "intact") {
        t.Fatalf("expected the child to report the file intact, got %q", string(output))
    }
}

func runFileStorageWriteWindowChild() {
    signal.Ignore(syscall.SIGXFSZ)

    path := filepath.Join(os.TempDir(), "melody_session_write_window.json")
    defer os.Remove(path)

    seed, seedErr := NewFileStorageFromPath(path)
    if nil != seedErr {
        _, _ = os.Stdout.WriteString("seed-failed\n")

        return
    }

    if nil != seed.Save("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", map[string]any{"userId": "u-1"}, 0) {
        _, _ = os.Stdout.WriteString("seed-save-failed\n")

        return
    }

    _ = seed.Close()

    fileInstance, openErr := os.OpenFile(path, os.O_RDWR, 0o644)
    if nil != openErr {
        _, _ = os.Stdout.WriteString("open-failed\n")

        return
    }
    defer fileInstance.Close()

    storage, storageErr := NewFileStorageFromFile(fileInstance)
    if nil != storageErr {
        _, _ = os.Stdout.WriteString("storage-failed\n")

        return
    }

    var previous syscall.Rlimit
    if nil != syscall.Getrlimit(syscall.RLIMIT_FSIZE, &previous) {
        _, _ = os.Stdout.WriteString("intact\n")

        return
    }

    if nil != syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 0, Max: previous.Max}) {
        _, _ = os.Stdout.WriteString("intact\n")

        return
    }

    saveErr := storage.Save("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", map[string]any{"userId": "u-2"}, 0)

    _ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous)

    if nil == saveErr {
        _, _ = os.Stdout.WriteString("intact\n")

        return
    }

    after, readErr := os.ReadFile(path)
    if nil != readErr {
        _, _ = os.Stdout.WriteString("read-failed\n")

        return
    }

    if 0 == len(after) {
        _, _ = os.Stdout.WriteString("emptied\n")

        return
    }

    _, _ = os.Stdout.WriteString("intact\n")
}

/* the truncation cuts to the length just written rather than to zero, so a snapshot that shrinks — a session deleted, a value removed — leaves no tail of the previous document behind for the next decode to choke on */
func TestFileStorage_InPlaceWrite_AShrinkingSnapshotLeavesNoTailBehind(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    fileInstance, createErr := os.Create(path)
    if nil != createErr {
        t.Fatalf("unexpected create error: %v", createErr)
    }
    defer fileInstance.Close()

    storage, storageErr := NewFileStorageFromFile(fileInstance)
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }

    for index := 0; index < 12; index++ {
        sessionId := strings.Repeat(strconv.Itoa(index%10), 32)

        if saveErr := storage.Save(sessionId, map[string]any{"padding": strings.Repeat("x", 64)}, 0); nil != saveErr {
            t.Fatalf("unexpected save error: %v", saveErr)
        }
    }

    large, largeErr := os.ReadFile(path)
    if nil != largeErr {
        t.Fatalf("unexpected read error: %v", largeErr)
    }

    for index := 0; index < 12; index++ {
        sessionId := strings.Repeat(strconv.Itoa(index%10), 32)

        if deleteErr := storage.Delete(sessionId); nil != deleteErr {
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

    reopened, reopenErr := NewFileStorageFromFile(fileInstance)
    if nil != reopenErr {
        t.Fatalf("the shrunk file did not decode, so a tail of the previous document survived: %v", reopenErr)
    }

    if 0 != len(reopened.sessionById) {
        t.Fatalf("expected an empty snapshot after every session was deleted, got %d", len(reopened.sessionById))
    }
}

/* a hard kill between CreateTemp and the rename skips every cleanup path, and each orphan is a complete snapshot of every live session; the sweep runs at construction, where the per-process ownership the session documentation states means nothing else can be mid-rename over this path */
func TestNewFileStorageFromPath_SweepsTheOrphanTemporaryFiles(t *testing.T) {
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

    storage, storageErr := NewFileStorageFromPath(path)
    if nil != storageErr {
        t.Fatalf("unexpected construction error: %v", storageErr)
    }
    defer func() {
        _ = storage.Close()
    }()

    if _, statErr := os.Stat(orphanPath); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the orphan temp file swept away, stat answered %v", statErr)
    }

    if _, statErr := os.Stat(unrelatedPath); nil != statErr {
        t.Fatalf("a file outside this storage's temp spelling was touched: %v", statErr)
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
}

func TestSyncSessionDirectory_AcceptsARealDirectory(t *testing.T) {
    if syncErr := syncSessionDirectory(t.TempDir()); nil != syncErr {
        t.Fatalf("unexpected error over a real directory: %v", syncErr)
    }
}

/* the file storage persists the WALL reading of the injected clock, and compares against the same clock: frozen, nothing lapses; travelled past the ttl, the entry is gone — with no sleep and no dependence on the process clock */
func TestFileStorage_ExpiryFollowsTheInjectedClock(t *testing.T) {
    frozenClock := clock.NewFrozenClock(time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC))

    storage, storageErr := NewFileStorageFromPathWithClock(filepath.Join(t.TempDir(), "sessions.json"), frozenClock)
    if nil != storageErr {
        t.Fatalf("storage error: %v", storageErr)
    }
    defer func() { _ = storage.Close() }()

    saveErr := storage.Save("session-1", map[string]any{"user": "editor"}, time.Hour)
    if nil != saveErr {
        t.Fatalf("save error: %v", saveErr)
    }

    _, exists, loadErr := storage.Load("session-1")
    if nil != loadErr || false == exists {
        t.Fatalf("expected the fresh session, got exists=%v err=%v", exists, loadErr)
    }

    frozenClock.Advance(2 * time.Hour)

    _, exists, loadErr = storage.Load("session-1")
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if true == exists {
        t.Fatalf("expected the session to lapse once the injected clock passed its ttl")
    }
}
