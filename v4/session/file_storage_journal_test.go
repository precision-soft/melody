package session

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "reflect"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    "github.com/precision-soft/melody/v4/exception"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

type journalLogRecord struct {
    level   string
    message string
    context loggingcontract.Context
}

type recordingJournalLogger struct {
    mutex   sync.Mutex
    records []journalLogRecord
}

func (instance *recordingJournalLogger) record(level string, message string, context loggingcontract.Context) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.records = append(instance.records, journalLogRecord{level: level, message: message, context: context})
}

func (instance *recordingJournalLogger) Log(level loggingcontract.Level, message string, context loggingcontract.Context) {
    instance.record(string(level), message, context)
}

func (instance *recordingJournalLogger) Debug(message string, context loggingcontract.Context) {
    instance.record("debug", message, context)
}

func (instance *recordingJournalLogger) Info(message string, context loggingcontract.Context) {
    instance.record("info", message, context)
}

func (instance *recordingJournalLogger) Warning(message string, context loggingcontract.Context) {
    instance.record("warning", message, context)
}

func (instance *recordingJournalLogger) Error(message string, context loggingcontract.Context) {
    instance.record("error", message, context)
}

func (instance *recordingJournalLogger) Emergency(message string, context loggingcontract.Context) {
    instance.record("emergency", message, context)
}

type simulatedJournalCrash struct {
    point journalPoint
}

func crashAt(t *testing.T, storage *FileStorage, point journalPoint, callback func()) {
    t.Helper()

    storage.journalSeam = func(reached journalPoint) {
        if point == reached {
            panic(simulatedJournalCrash{point: reached})
        }
    }

    defer func() {
        t.Helper()

        recovered := recover()
        if _, isCrash := recovered.(simulatedJournalCrash); false == isCrash {
            t.Fatalf("expected the storage to reach %q, recovered %v", point, recovered)
        }

        storage.journalSeam = nil
        if nil != storage.journal {
            _ = storage.journal.Close()
        }
    }()

    callback()
}

func newJournalStorage(t *testing.T, config FileStorageConfig) *FileStorage {
    t.Helper()

    storage, storageErr := NewFileStorage(config)
    if nil != storageErr {
        t.Fatalf("unexpected storage error: %v", storageErr)
    }

    return storage
}

func journalSize(t *testing.T, path string) int64 {
    t.Helper()

    stat, statErr := os.Stat(sessionJournalPath(path))
    if nil != statErr {
        t.Fatalf("unexpected journal stat error: %v", statErr)
    }

    return stat.Size()
}

func journalRecordCount(t *testing.T, path string) int {
    t.Helper()

    return strings.Count(mustReadJournal(t, path), "\n") - 1
}

func readSnapshot(t *testing.T, path string) map[string]fileSessionEntry {
    t.Helper()

    snapshot, readErr := readSessionFileAtPath(path)
    if nil != readErr {
        t.Fatalf("unexpected snapshot read error: %v", readErr)
    }

    return snapshot
}

func mustSave(t *testing.T, storage *FileStorage, sessionId string, data map[string]any) {
    t.Helper()

    if saveErr := storage.Save(sessionRuntime(), sessionId, data, bound.Of(time.Hour)); nil != saveErr {
        t.Fatalf("unexpected save error for %s: %v", sessionId, saveErr)
    }
}

func TestFileStorage_Journal_ASaveAppendsOneRecordAndLeavesTheSnapshotUntouched(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    if writeErr := os.WriteFile(path, []byte("{\"third\":{\"data\":{\"user\":\"v3\"},\"expiresAt\":0}}\n"), 0600); nil != writeErr {
        t.Fatalf("could not seed the third major's snapshot: %v", writeErr)
    }

    before, _ := os.ReadFile(path)

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    if _, exists, _ := storage.Load(sessionRuntime(), "third"); false == exists {
        t.Fatalf("expected the third major's snapshot to be read as is")
    }

    mustSave(t, storage, "fresh", map[string]any{"user": "v4"})

    after, _ := os.ReadFile(path)
    if string(before) != string(after) {
        t.Fatalf("expected the snapshot untouched by a save, got %s", after)
    }

    content, _ := os.ReadFile(sessionJournalPath(path))
    if 2 != strings.Count(string(content), "\n") || false == strings.HasPrefix(string(content), string(encodeSessionJournalHeader(digestOf(before)))) || false == strings.Contains(string(content), "\"operation\":\"save\"") {
        t.Fatalf("expected one save record in the journal, got %q", content)
    }
}

func TestFileStorage_Journal_EveryRecordIsSyncedBeforeTheWriteAnswers(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    reached := make([]journalPoint, 0)
    storage.journalSeam = func(point journalPoint) {
        reached = append(reached, point)
    }

    mustSave(t, storage, "a", map[string]any{"k": "v"})

    if deleteErr := storage.Delete(sessionRuntime(), "a"); nil != deleteErr {
        t.Fatalf("unexpected delete error: %v", deleteErr)
    }

    expected := []journalPoint{journalPointRecordWritten, journalPointRecordSynced, journalPointRecordWritten, journalPointRecordSynced}
    if false == reflect.DeepEqual(expected, reached) {
        t.Fatalf("expected each record written then synced, got %v", reached)
    }
}

func TestFileStorage_Journal_ARecordWrittenBeforeACrashIsReplayed(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    logger := &recordingJournalLogger{}

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    mustSave(t, storage, "kept", map[string]any{"k": "kept"})

    crashAt(t, storage, journalPointRecordWritten, func() {
        _ = storage.Save(sessionRuntime(), "last", map[string]any{"k": "last"}, bound.Of(time.Hour))
    })

    reopened := newJournalStorage(t, FileStorageConfig{Path: path, Logger: logger})
    defer reopened.CloseWithContext(context.Background())

    for _, sessionId := range []string{"kept", "last"} {
        if _, exists, _ := reopened.Load(sessionRuntime(), sessionId); false == exists {
            t.Fatalf("expected %s replayed from the journal", sessionId)
        }
    }

    if 1 != len(logger.records) || "info" != logger.records[0].level || 2 != logger.records[0].context["replayed"] {
        t.Fatalf("expected one info naming two replayed records, got %+v", logger.records)
    }

    if 0 != journalRecordCount(t, path) || 2 != len(readSnapshot(t, path)) {
        t.Fatalf("expected the construction to compact the replayed journal into the snapshot")
    }
}

func TestFileStorage_Journal_ATornLastRecordIsCutAndReported(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    logger := &recordingJournalLogger{}

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    mustSave(t, storage, "whole", map[string]any{"k": "whole"})
    _ = storage.journal.Close()

    journalFile, openErr := os.OpenFile(sessionJournalPath(path), os.O_WRONLY|os.O_APPEND, 0)
    if nil != openErr {
        t.Fatalf("unexpected open error: %v", openErr)
    }

    _, _ = journalFile.WriteString("{\"operation\":\"save\",\"id\":\"torn\",\"da")
    _ = journalFile.Close()

    reopened := newJournalStorage(t, FileStorageConfig{Path: path, Logger: logger})
    defer reopened.CloseWithContext(context.Background())

    if _, exists, _ := reopened.Load(sessionRuntime(), "whole"); false == exists {
        t.Fatalf("expected the whole record before the torn one replayed")
    }

    if _, exists, _ := reopened.Load(sessionRuntime(), "torn"); true == exists {
        t.Fatalf("expected the torn record dropped")
    }

    if 1 != len(logger.records) || "warning" != logger.records[0].level || 1 != logger.records[0].context["replayed"] {
        t.Fatalf("expected one warning naming one replayed record, got %+v", logger.records)
    }
}

func TestReplaySessionJournal_StopsAtTheFirstRecordThatDoesNotDecodeAndCutsThere(t *testing.T) {
    path := filepath.Join(t.TempDir(), "journal")
    first := string(encodeSessionJournalHeader("")) + "{\"operation\":\"save\",\"id\":\"first\",\"data\":{\"k\":\"v\"}}\n"
    content := first + "not a record\n" + "{\"operation\":\"save\",\"id\":\"after\"}\n"

    if writeErr := os.WriteFile(path, []byte(content), 0600); nil != writeErr {
        t.Fatalf("could not seed the journal: %v", writeErr)
    }

    journal, _ := os.OpenFile(path, os.O_RDWR, 0)
    defer journal.Close()

    sessionById := make(map[string]fileSessionEntry)

    replay, replayErr := replaySessionJournal(journal, "", sessionById)
    if nil != replayErr {
        t.Fatalf("unexpected replay error: %v", replayErr)
    }

    if 1 != replay.replayed || false == replay.tailCut {
        t.Fatalf("expected one record and a cut tail, got %+v", replay)
    }

    if _, exists := sessionById["after"]; true == exists {
        t.Fatalf("expected the replay to stop at the record that does not decode")
    }

    cut, _ := os.ReadFile(path)
    if first != string(cut) {
        t.Fatalf("expected the journal cut after the last whole record, got %q", cut)
    }
}

func TestReplaySessionJournal_AppliesDeletesAndIsIdempotent(t *testing.T) {
    path := filepath.Join(t.TempDir(), "journal")
    content := string(encodeSessionJournalHeader("")) + "{\"operation\":\"save\",\"id\":\"gone\",\"data\":{\"k\":\"v\"}}\n" +
        "{\"operation\":\"save\",\"id\":\"absent-from-snapshot\",\"data\":{\"k\":\"w\"},\"expiresAt\":42}\n" +
        "{\"operation\":\"delete\",\"id\":\"gone\"}\n" +
        "{\"operation\":\"delete\",\"id\":\"snapshot-only\"}\n"

    if writeErr := os.WriteFile(path, []byte(content), 0600); nil != writeErr {
        t.Fatalf("could not seed the journal: %v", writeErr)
    }

    journal, _ := os.OpenFile(path, os.O_RDWR, 0)
    defer journal.Close()

    snapshot := map[string]fileSessionEntry{"snapshot-only": {Data: map[string]any{}}, "untouched": {Data: map[string]any{"k": "u"}}}

    if _, replayErr := replaySessionJournal(journal, "", snapshot); nil != replayErr {
        t.Fatalf("unexpected replay error: %v", replayErr)
    }

    once := make(map[string]fileSessionEntry, len(snapshot))
    for key, value := range snapshot {
        once[key] = value
    }

    if _, replayErr := replaySessionJournal(journal, "", snapshot); nil != replayErr {
        t.Fatalf("unexpected second replay error: %v", replayErr)
    }

    expected := map[string]fileSessionEntry{
        "absent-from-snapshot": {Data: map[string]any{"k": "w"}, ExpiresAt: 42},
        "untouched":            {Data: map[string]any{"k": "u"}},
    }

    if false == reflect.DeepEqual(expected, once) || false == reflect.DeepEqual(once, snapshot) {
        t.Fatalf("expected deletes applied and a second replay to change nothing, got %v then %v", once, snapshot)
    }
}

func TestFileStorage_Journal_ACrashBetweenTheSnapshotRenameAndTheJournalCutReplaysToTheSameMap(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    mustSave(t, storage, "a", map[string]any{"k": "a"})
    mustSave(t, storage, "b", map[string]any{"k": "b"})

    if deleteErr := storage.Delete(sessionRuntime(), "a"); nil != deleteErr {
        t.Fatalf("unexpected delete error: %v", deleteErr)
    }

    crashAt(t, storage, journalPointSnapshotRenamed, func() {
        _ = storage.CloseWithContext(context.Background())
    })

    if 3 != journalRecordCount(t, path) || 1 != len(readSnapshot(t, path)) {
        t.Fatalf("expected the crash to leave the new snapshot beside the whole journal")
    }

    reopened := newJournalStorage(t, FileStorageConfig{Path: path})
    defer reopened.CloseWithContext(context.Background())

    if _, exists, _ := reopened.Load(sessionRuntime(), "a"); true == exists {
        t.Fatalf("expected the deleted session to stay deleted after the replay")
    }

    if data, exists, _ := reopened.Load(sessionRuntime(), "b"); false == exists || "b" != data["k"] {
        t.Fatalf("expected the saved session replayed, got %v %v", exists, data)
    }
}

func TestFileStorage_Journal_TheCloseCompactsSoTheSnapshotAloneHoldsEveryWrite(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    mustSave(t, storage, "a", map[string]any{"k": "a"})
    mustSave(t, storage, "b", map[string]any{"k": "b"})

    if closeErr := storage.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if 0 != journalRecordCount(t, path) {
        t.Fatalf("expected the close to leave the journal empty")
    }

    snapshot := readSnapshot(t, path)
    if 2 != len(snapshot) || "a" != snapshot["a"].Data["k"] || "b" != snapshot["b"].Data["k"] {
        t.Fatalf("expected the snapshot to hold every write for a build that does not read the journal, got %v", snapshot)
    }
}

func TestFileStorage_Journal_PastTheThresholdAWriteCompacts(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path, JournalMaxBytes: bound.Of(64 << 10)})
    defer storage.CloseWithContext(context.Background())

    payload := strings.Repeat("x", 1024)

    for index := 0; index < 64; index++ {
        mustSave(t, storage, "s"+strconv.Itoa(index), map[string]any{"payload": payload})

        if 64<<10 < storage.journalSize {
            t.Fatalf("expected the journal compacted once past the threshold, at %d bytes", storage.journalSize)
        }
    }

    snapshot := readSnapshot(t, path)
    if 0 == len(snapshot) {
        t.Fatalf("expected a compaction to have written the snapshot")
    }

    if journalSize(t, path) != storage.journalSize {
        t.Fatalf("expected the tracked size to match the journal, %d against %d", storage.journalSize, journalSize(t, path))
    }
}

func TestNewFileStorage_JournalMaxBytesResolves(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    if DefaultJournalMaxBytes != storage.journalMaxBytes.Value {
        t.Fatalf("expected the default of %d, got %d", DefaultJournalMaxBytes, storage.journalMaxBytes.Value)
    }
    _ = storage.CloseWithContext(context.Background())

    for _, refused := range []bound.Count{bound.Of(64<<10 - 1), bound.UnlimitedCount()} {
        if _, storageErr := NewFileStorage(FileStorageConfig{Path: path, JournalMaxBytes: refused}); nil == storageErr || false == strings.Contains(fmt.Sprint(exception.LogContext(storageErr, exception.LogContextConfig{})), "FileStorageConfig.JournalMaxBytes") {
            t.Fatalf("expected %v refused naming the field, got %v", refused, storageErr)
        }
    }
}

func TestNewFileStorage_RefusesATypedNilLogger(t *testing.T) {
    var typedNil *recordingJournalLogger

    _, storageErr := NewFileStorage(FileStorageConfig{Path: filepath.Join(t.TempDir(), "sessions.json"), Logger: typedNil})
    if nil == storageErr || "session file storage logger is a typed nil" != storageErr.Error() {
        t.Fatalf("expected the typed nil refused, got %v", storageErr)
    }
}

func TestFileStorage_Journal_TheHandleFormWritesNoJournal(t *testing.T) {
    directory := t.TempDir()

    fileInstance, openErr := os.OpenFile(filepath.Join(directory, "sessions.json"), os.O_RDWR|os.O_CREATE, 0600)
    if nil != openErr {
        t.Fatalf("unexpected open error: %v", openErr)
    }
    defer fileInstance.Close()

    storage := newJournalStorage(t, FileStorageConfig{File: fileInstance})
    mustSave(t, storage, "a", map[string]any{"k": "a"})
    _ = storage.CloseWithContext(context.Background())

    entries, _ := os.ReadDir(directory)
    if 1 != len(entries) {
        t.Fatalf("expected the handle alone in the directory, got %v", entries)
    }
}

func TestFileStorage_Journal_TheDoorOffWritesTheSnapshotAndRetiresALeftJournal(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    journaled := newJournalStorage(t, FileStorageConfig{Path: path})
    mustSave(t, journaled, "left", map[string]any{"k": "left"})
    _ = journaled.journal.Close()

    storage := newJournalStorage(t, FileStorageConfig{Path: path, JournalDisabled: true})
    defer storage.CloseWithContext(context.Background())

    if _, statErr := os.Stat(sessionJournalPath(path)); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the journal left at the path removed, stat answered %v", statErr)
    }

    if _, exists, _ := storage.Load(sessionRuntime(), "left"); false == exists {
        t.Fatalf("expected the journal left at the path replayed before its removal")
    }

    mustSave(t, storage, "fresh", map[string]any{"k": "fresh"})

    if snapshot := readSnapshot(t, path); 2 != len(snapshot) {
        t.Fatalf("expected the door off to write the whole snapshot on a save, got %v", snapshot)
    }
}

func TestFileStorage_Journal_AFailedAppendRollsBackAndAFailedCutCompactsBeforeTheNextWrite(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    mustSave(t, storage, "kept", map[string]any{"k": "old"})

    writable := storage.journal

    readOnly, openErr := os.Open(sessionJournalPath(path))
    if nil != openErr {
        t.Fatalf("unexpected open error: %v", openErr)
    }

    storage.journal = readOnly

    if saveErr := storage.Save(sessionRuntime(), "kept", map[string]any{"k": "new"}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected the refused append reported")
    }

    if deleteErr := storage.Delete(sessionRuntime(), "kept"); nil == deleteErr {
        t.Fatalf("expected the refused append of the delete reported")
    }

    if data, exists, _ := storage.Load(sessionRuntime(), "kept"); false == exists || "old" != data["k"] {
        t.Fatalf("expected both failed writes rolled back, got %v %v", exists, data)
    }

    if false == storage.journalNeedsCompaction {
        t.Fatalf("expected a journal that could not be cut back to ask for a compaction")
    }

    _ = readOnly.Close()
    storage.journal = writable

    mustSave(t, storage, "next", map[string]any{"k": "next"})

    if true == storage.journalNeedsCompaction || 1 != journalRecordCount(t, path) {
        t.Fatalf("expected the next write to compact first and then append its own record alone")
    }
}

func TestFileStorage_Journal_ARecordWhoseSyncFailsIsReportedAndRolledBack(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    writable := storage.journal

    characterDevice, openErr := os.OpenFile(refusingCharacterDevice, os.O_RDWR, 0)
    if nil != openErr {
        t.Fatalf("unexpected open error: %v", openErr)
    }

    storage.journal = characterDevice

    saveErr := storage.Save(sessionRuntime(), "unsynced", map[string]any{"k": "v"}, bound.Of(time.Hour))
    if nil == saveErr || "failed to sync session storage journal record" != saveErr.Error() {
        t.Fatalf("expected the refused fsync reported, got %v", saveErr)
    }

    if _, exists, _ := storage.Load(sessionRuntime(), "unsynced"); true == exists {
        t.Fatalf("expected the save whose record never synced rolled back")
    }

    _ = characterDevice.Close()
    storage.journal = writable
}

func mustReadJournal(t *testing.T, path string) string {
    t.Helper()

    content, readErr := os.ReadFile(sessionJournalPath(path))
    if nil != readErr {
        t.Fatalf("unexpected journal read error: %v", readErr)
    }

    return string(content)
}

func TestFileStorage_Journal_ALapsedSessionIsDroppedWithoutARecordAndAtTheReplay(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    frozenClock := clock.NewFrozenClock(time.Unix(1_000_000, 0))

    storage := newJournalStorage(t, FileStorageConfig{Path: path, Clock: frozenClock})
    if saveErr := storage.Save(sessionRuntime(), "lapsing", map[string]any{"k": "v"}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }
    mustSave(t, storage, "keeper", map[string]any{"k": "keeper"})

    frozenClock.Advance(2 * time.Minute)

    sizeBefore := storage.journalSize
    mustSave(t, storage, "keeper", map[string]any{"k": "keeper"})

    if _, stillStored := storage.sessionById["lapsing"]; true == stillStored {
        t.Fatalf("expected the lapsed session purged from memory by the next write")
    }

    storage.sessionById["lapsing"] = fileSessionEntry{Data: map[string]any{}, ExpiresAt: frozenClock.Now().Add(-time.Second).UnixNano()}

    if _, exists, _ := storage.Load(sessionRuntime(), "lapsing"); true == exists {
        t.Fatalf("expected the lapsed session absent")
    }

    if _, stillStored := storage.sessionById["lapsing"]; true == stillStored {
        t.Fatalf("expected the load to drop the lapsed session from memory")
    }

    if _, statErr := os.Stat(path); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the load of a lapsed session to write no snapshot, stat answered %v", statErr)
    }

    if 1 != strings.Count(mustReadJournal(t, path)[sizeBefore:], "\n") {
        t.Fatalf("expected no record written for the lapsed session")
    }

    _ = storage.journal.Close()

    reopened := newJournalStorage(t, FileStorageConfig{Path: path, Clock: frozenClock})
    defer reopened.CloseWithContext(context.Background())

    if _, exists := reopened.sessionById["lapsing"]; true == exists {
        t.Fatalf("expected the replay to drop the lapsed session by its expiry")
    }
}

func TestFileStorage_Journal_ConcurrentWritesReplayWhole(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})

    var waitGroup sync.WaitGroup
    for worker := 0; worker < 8; worker++ {
        waitGroup.Add(1)

        go func(worker int) {
            defer waitGroup.Done()

            for index := 0; index < 25; index++ {
                sessionId := "w" + strconv.Itoa(worker) + "-" + strconv.Itoa(index)
                if saveErr := storage.Save(sessionRuntime(), sessionId, map[string]any{"index": index}, bound.Of(time.Hour)); nil != saveErr {
                    t.Errorf("unexpected save error: %v", saveErr)
                }
            }
        }(worker)
    }

    waitGroup.Wait()
    _ = storage.journal.Close()

    reopened := newJournalStorage(t, FileStorageConfig{Path: path})
    defer reopened.CloseWithContext(context.Background())

    if 200 != len(reopened.sessionById) {
        t.Fatalf("expected every concurrent write replayed, got %d", len(reopened.sessionById))
    }
}

func TestFileStorage_Journal_AWriteRefusedByAPendingCompactionLeavesTheDiskAsTheMap(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    mustSave(t, storage, "kept", map[string]any{"k": "old"})

    writable := storage.journal

    readOnly, openErr := os.Open(sessionJournalPath(path))
    if nil != openErr {
        t.Fatalf("unexpected open error: %v", openErr)
    }

    storage.journal = readOnly
    storage.journalNeedsCompaction = true

    if saveErr := storage.Save(sessionRuntime(), "kept", map[string]any{"k": "new"}, bound.Of(time.Hour)); nil == saveErr {
        t.Fatalf("expected the pending compaction's refusal reported")
    }

    if deleteErr := storage.Delete(sessionRuntime(), "kept"); nil == deleteErr {
        t.Fatalf("expected the pending compaction's refusal reported on the delete")
    }

    if data, exists, _ := storage.Load(sessionRuntime(), "kept"); false == exists || "old" != data["k"] {
        t.Fatalf("expected the map untouched, got %v %v", exists, data)
    }

    if snapshot := readSnapshot(t, path); "old" != snapshot["kept"].Data["k"] {
        t.Fatalf("expected the snapshot to hold the map, not the refused write, got %v", snapshot)
    }

    _ = readOnly.Close()
    storage.journal = writable
}

func TestFileStorage_Journal_AJournalBehindASnapshotAnotherBuildRewroteIsDiscarded(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    logger := &recordingJournalLogger{}

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    mustSave(t, storage, "resurrected", map[string]any{"user": "admin"})
    _ = storage.journal.Close()

    if writeErr := os.WriteFile(path, []byte("{}\n"), 0600); nil != writeErr {
        t.Fatalf("could not stand in for the other build: %v", writeErr)
    }

    reopened := newJournalStorage(t, FileStorageConfig{Path: path, Logger: logger})
    defer reopened.CloseWithContext(context.Background())

    if _, exists, _ := reopened.Load(sessionRuntime(), "resurrected"); true == exists {
        t.Fatalf("expected the stale journal not replayed over the newer snapshot")
    }

    if 1 != len(logger.records) || "warning" != logger.records[0].level || 1 != logger.records[0].context["discarded"] {
        t.Fatalf("expected one warning naming one discarded record, got %+v", logger.records)
    }

    if 0 != journalRecordCount(t, path) || false == strings.HasPrefix(mustReadJournal(t, path), string(encodeSessionJournalHeader(digestOf([]byte("{}\n"))))) {
        t.Fatalf("expected the journal reset to a header naming the snapshot on disk, got %q", mustReadJournal(t, path))
    }
}

func TestNewFileStorage_RefusesAJournalOfAFormatItDoesNotRead(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    if writeErr := os.WriteFile(sessionJournalPath(path), []byte("{\"operation\":\"header\",\"version\":2,\"snapshot\":\"\"}\n"), 0600); nil != writeErr {
        t.Fatalf("could not seed the journal: %v", writeErr)
    }

    if _, storageErr := NewFileStorage(FileStorageConfig{Path: path}); nil == storageErr || "session storage journal has a format this build does not read" != storageErr.Error() {
        t.Fatalf("expected the newer format refused, got %v", storageErr)
    }
}

func TestFileStorage_Journal_AJournalWithoutItsHeaderIsCutWhole(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    logger := &recordingJournalLogger{}

    if writeErr := os.WriteFile(sessionJournalPath(path), []byte("{\"operation\":\"save\",\"id\":\"headless\"}\n"), 0600); nil != writeErr {
        t.Fatalf("could not seed the journal: %v", writeErr)
    }

    storage := newJournalStorage(t, FileStorageConfig{Path: path, Logger: logger})
    defer storage.CloseWithContext(context.Background())

    if _, exists, _ := storage.Load(sessionRuntime(), "headless"); true == exists {
        t.Fatalf("expected a record under no header dropped")
    }

    if 1 != len(logger.records) || "warning" != logger.records[0].level || 0 != journalRecordCount(t, path) {
        t.Fatalf("expected the headless journal cut and reported, got %+v", logger.records)
    }
}

func TestReplaySessionJournal_RefusesARecordThatDecodesButIsNoWrite(t *testing.T) {
    for name, tail := range map[string]string{
        "without its newline": "{\"operation\":\"save\",\"id\":\"last\"}",
        "an empty id":         "{\"operation\":\"save\",\"id\":\"\"}\n",
        "an unknown operation": "{\"operation\":\"rename\",\"id\":\"x\"}\n",
    } {
        path := filepath.Join(t.TempDir(), "journal")
        whole := string(encodeSessionJournalHeader("")) + "{\"operation\":\"save\",\"id\":\"first\"}\n"

        if writeErr := os.WriteFile(path, []byte(whole+tail), 0600); nil != writeErr {
            t.Fatalf("could not seed the journal: %v", writeErr)
        }

        journal, _ := os.OpenFile(path, os.O_RDWR, 0)
        sessionById := make(map[string]fileSessionEntry)

        replay, replayErr := replaySessionJournal(journal, "", sessionById)
        _ = journal.Close()

        cut, _ := os.ReadFile(path)
        if nil != replayErr || 1 != replay.replayed || false == replay.tailCut || 1 != len(sessionById) || whole != string(cut) {
            t.Fatalf("%s: expected the record cut as a torn tail, got %+v %v %v %q", name, replay, replayErr, sessionById, cut)
        }
    }
}

func TestFileStorage_Journal_ADeleteAlsoPurgesTheLapsedAndCompactsPastTheThreshold(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    frozenClock := clock.NewFrozenClock(time.Unix(1_000_000, 0))

    storage := newJournalStorage(t, FileStorageConfig{Path: path, Clock: frozenClock})
    defer storage.CloseWithContext(context.Background())

    if saveErr := storage.Save(sessionRuntime(), "lapsing", map[string]any{"k": "v"}, bound.Of(time.Minute)); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }
    mustSave(t, storage, "deleted", map[string]any{"k": "v"})

    frozenClock.Advance(2 * time.Minute)
    storage.journalMaxBytes.Value = 1

    if deleteErr := storage.Delete(sessionRuntime(), "deleted"); nil != deleteErr {
        t.Fatalf("unexpected delete error: %v", deleteErr)
    }

    if _, stillStored := storage.sessionById["lapsing"]; true == stillStored {
        t.Fatalf("expected the delete to purge the lapsed session from memory")
    }

    if 0 != journalRecordCount(t, path) || 0 != len(readSnapshot(t, path)) {
        t.Fatalf("expected the delete past the threshold to compact")
    }
}

func TestFileStorage_Journal_TheThresholdIsExclusive(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    mustSave(t, storage, "a", map[string]any{"k": "a"})

    storage.journalMaxBytes.Value = int(storage.journalSize)
    storage.compactPastThresholdLocked()

    if 1 != journalRecordCount(t, path) {
        t.Fatalf("expected a journal exactly at the threshold left alone")
    }

    storage.journalMaxBytes.Value = int(storage.journalSize) - 1
    storage.compactPastThresholdLocked()

    if 0 != journalRecordCount(t, path) {
        t.Fatalf("expected a journal past the threshold compacted")
    }
}

func TestFileStorage_Journal_TheCloseRunsAPendingCompactionAndClosesTheJournal(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    storage.sessionById["unjournaled"] = fileSessionEntry{Data: map[string]any{"k": "v"}}
    storage.journalNeedsCompaction = true

    journal := storage.journal

    if closeErr := storage.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    if _, held := readSnapshot(t, path)["unjournaled"]; false == held {
        t.Fatalf("expected the close to run the pending compaction")
    }

    if _, writeErr := journal.Write([]byte("x")); nil == writeErr {
        t.Fatalf("expected the close to close the journal handle")
    }
}

func TestFileStorage_Journal_AFailedResetAfterTheSnapshotAsksForACompaction(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")

    storage := newJournalStorage(t, FileStorageConfig{Path: path})
    defer storage.CloseWithContext(context.Background())

    writable := storage.journal

    readOnly, _ := os.Open(sessionJournalPath(path))
    storage.journal = readOnly

    if compactErr := storage.compactLocked(); nil == compactErr || false == storage.journalNeedsCompaction {
        t.Fatalf("expected the refused reset reported and a compaction asked for, got %v", compactErr)
    }

    _ = readOnly.Close()
    storage.journal = writable
}

func TestFileStorage_Journal_AThresholdCompactionThatFailsIsLoggedAndTheWriteStands(t *testing.T) {
    directory := t.TempDir()
    path := filepath.Join(directory, "sessions.json")
    logger := &recordingJournalLogger{}

    storage := newJournalStorage(t, FileStorageConfig{Path: path, Logger: logger})
    defer storage.CloseWithContext(context.Background())

    if mkdirErr := os.MkdirAll(filepath.Join(path, "blocks-the-rename"), 0755); nil != mkdirErr {
        t.Fatalf("could not block the rename: %v", mkdirErr)
    }

    storage.journalMaxBytes.Value = 1

    mustSave(t, storage, "stands", map[string]any{"k": "v"})

    if 1 != len(logger.records) || "warning" != logger.records[0].level || "session storage journal compaction failed" != logger.records[0].message {
        t.Fatalf("expected the failed compaction logged as a warning, got %+v", logger.records)
    }

    if 1 != journalRecordCount(t, path) {
        t.Fatalf("expected the write to stand in the journal")
    }

    _ = os.RemoveAll(path)
}

func TestFileStorage_Journal_ANewJournalIsPrivateAndAQuietOpenLogsNothing(t *testing.T) {
    path := filepath.Join(t.TempDir(), "sessions.json")
    logger := &recordingJournalLogger{}

    storage := newJournalStorage(t, FileStorageConfig{Path: path, Logger: logger})
    _ = storage.CloseWithContext(context.Background())

    stat, _ := os.Stat(sessionJournalPath(path))
    if 0600 != stat.Mode().Perm() {
        t.Fatalf("expected the journal created 0600, got %v", stat.Mode().Perm())
    }

    reopened := newJournalStorage(t, FileStorageConfig{Path: path, Logger: logger})
    _ = reopened.CloseWithContext(context.Background())

    if 0 != len(logger.records) {
        t.Fatalf("expected nothing logged when nothing was replayed, got %+v", logger.records)
    }
}

func TestFileStorage_Journal_AWriteAfterAFailedResetLandsUnderTheNewSnapshotsHeader(t *testing.T) {
    for _, operation := range []string{"save", "delete"} {
        path := filepath.Join(t.TempDir(), "sessions.json")

        storage := newJournalStorage(t, FileStorageConfig{Path: path})
        mustSave(t, storage, "target", map[string]any{"k": "old"})

        writable := storage.journal

        readOnly, _ := os.Open(sessionJournalPath(path))
        storage.journal = readOnly

        if compactErr := storage.compactLocked(); nil == compactErr {
            t.Fatalf("%s: expected the refused reset reported", operation)
        }

        _ = readOnly.Close()
        storage.journal = writable

        if "save" == operation {
            mustSave(t, storage, "target", map[string]any{"k": "new"})
        } else if deleteErr := storage.Delete(sessionRuntime(), "target"); nil != deleteErr {
            t.Fatalf("unexpected delete error: %v", deleteErr)
        }

        _ = storage.journal.Close()

        reopened := newJournalStorage(t, FileStorageConfig{Path: path})
        data, exists, _ := reopened.Load(sessionRuntime(), "target")
        _ = reopened.CloseWithContext(context.Background())

        if ("save" == operation && (false == exists || "new" != data["k"])) || ("delete" == operation && true == exists) {
            t.Fatalf("%s: expected the acknowledged write to survive the reopen, got %v %v", operation, exists, data)
        }
    }
}
