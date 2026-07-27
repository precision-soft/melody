package session

import (
    "math"
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

func TestFileStorage_Save_TtlBeyondYear2262IsKeptNotPurged(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "session.json")

    storage, err := NewFileStorageFromPath(path)
    if nil != err {
        t.Fatalf("unexpected storage error: %s", err.Error())
    }
    defer storage.Close()

    /* @important a ttl that pushes the expiry past 2262-04-11 overflows time.Time.UnixNano to a negative int64; the session must be kept with a saturated far-future expiry exactly as InMemoryStorage keeps it, not treated as already expired and purged on the same Save */
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

    /* @important it must also survive a reload from disk — an overflowed negative expiry would have been dropped from the snapshot on the same Save and never written to the file */
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

/* @info Loading an expired session must remove it from the map and from the file, and the flush inside Load is the only thing that does it: purgeExpiredLocked runs inside flushLocked against the same clock and the same predicate, so Load names no session of its own. This pins that mechanism — if the purge ever stops covering a lapsed entry, the explicit delete has to come back.

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
