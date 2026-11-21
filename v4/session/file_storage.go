package session

import (
    "bytes"
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "io"
    "math"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* FileStorageConfig names the file the storage persists to: exactly one of Path and File. */
type FileStorageConfig struct {
    /* Path is a file the storage owns, snapshotted through a temporary file and a rename; each write appends a synced record to <path>.journal, compacted into the snapshot at construction, past JournalMaxBytes and at close. */
    Path string

    /* File is an open handle the caller keeps owning; every write rewrites the whole snapshot through it, never journaled, and a crash mid-write can leave a torn document. */
    File *os.File

    /* JournalDisabled writes the whole snapshot on every Save and Delete in Path mode; a journal left at the path is replayed and compacted at construction, then removed. */
    JournalDisabled bool

    /* JournalMaxBytes is the journal size past which a write compacts it into the snapshot: 8 MiB when left out, at least 64 KiB, unlimited refused. */
    JournalMaxBytes bound.Count

    /* Logger journals what the construction replayed or discarded and each failed threshold compaction; nil journals nothing, a typed nil is refused. */
    Logger loggingcontract.Logger

    /* MaxSessions bounds the sessions held at once: 100,000 when left out, at least 1, unlimited admitted; at the ceiling a new id is refused. */
    MaxSessions bound.Count

    /* Clock is where every expiry instant is read; nil is the system clock, and the file persists the wall reading. */
    Clock clockcontract.Clock
}

var fileStorageMaxSessionsRule = bound.Rule[int]{
    Name:              "FileStorageConfig.MaxSessions",
    Default:           DefaultMaxSessions,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

/* NewFileStorage builds the storage over the configured path or handle and loads the sessions the file holds. */
func NewFileStorage(config FileStorageConfig) (*FileStorage, error) {
    if "" != config.Path && nil != config.File {
        return nil, exception.NewError(
            "session file storage declares both a path and a file",
            exceptioncontract.Context{
                "path": config.Path,
                "file": config.File.Name(),
            },
            nil,
        )
    }

    maxSessions, maxSessionsErr := bound.Resolve(config.MaxSessions, fileStorageMaxSessionsRule)
    if nil != maxSessionsErr {
        return nil, maxSessionsErr
    }

    journalMaxBytes, journalMaxBytesErr := bound.Resolve(config.JournalMaxBytes, fileStorageJournalMaxBytesRule)
    if nil != journalMaxBytesErr {
        return nil, journalMaxBytesErr
    }

    if nil != config.Logger && true == internal.IsNilInterface(config.Logger) {
        return nil, exception.NewError(
            "session file storage logger is a typed nil",
            exceptioncontract.Context{
                "field": "FileStorageConfig.Logger",
            },
            nil,
        )
    }

    clockInstance := config.Clock
    if true == internal.IsNilInterface(clockInstance) {
        clockInstance = clock.NewSystemClock()
    }

    var storage *FileStorage
    var storageErr error

    if nil != config.File {
        storage, storageErr = newFileStorageFromFile(config.File, clockInstance)
    } else if "" == config.Path {
        return nil, exception.NewError("session file storage declares neither a path nor a file", nil, nil)
    } else {
        storage, storageErr = newFileStorageFromPath(config.Path, clockInstance, false == config.JournalDisabled, journalMaxBytes, config.Logger)
    }

    if nil != storageErr {
        return nil, storageErr
    }

    storage.maxSessions = maxSessions
    storage.logger = config.Logger
    storage.earliestExpiry = storage.earliestExpiryLocked()

    return storage, nil
}

func newFileStorageFromPath(path string, clockInstance clockcontract.Clock, journalEnabled bool, journalMaxBytes bound.Resolved[int], logger loggingcontract.Logger) (*FileStorage, error) {
    trimmedPath := filepath.Clean(path)
    if "." == trimmedPath {
        return nil, exception.NewError(
            "invalid session storage path",
            exceptioncontract.Context{
                "path": path,
            },
            nil,
        )
    }

    directoryPath := filepath.Dir(trimmedPath)
    err := os.MkdirAll(directoryPath, 0755)
    if nil != err {
        return nil, exception.NewError(
            "failed to create session storage directory",
            exceptioncontract.Context{
                "path": directoryPath,
            },
            err,
        )
    }

    removeOrphanSessionTemporaryFiles(trimmedPath)

    decoded, err := readSessionFileAtPath(trimmedPath)
    if nil != err {
        return nil, err
    }

    storage := &FileStorage{
        path:            trimmedPath,
        ownsFile:        true,
        sessionById:     decoded,
        clock:           clockInstance,
        journalMaxBytes: journalMaxBytes,
    }

    journalErr := storage.openJournal(journalEnabled, logger)
    if nil != journalErr {
        return nil, journalErr
    }

    return storage, nil
}

/* replayed even with the journal off, so turning it off loses no write */
func (instance *FileStorage) openJournal(journalEnabled bool, logger loggingcontract.Logger) error {
    journalPath := sessionJournalPath(instance.path)

    if false == journalEnabled {
        if _, statErr := os.Stat(journalPath); true == os.IsNotExist(statErr) {
            return nil
        }
    }

    snapshotDigest, digestErr := sessionSnapshotDigest(instance.path)
    if nil != digestErr {
        return digestErr
    }

    journal, replay, openErr := openSessionJournal(instance.path, snapshotDigest, instance.sessionById)
    if nil != openErr {
        return openErr
    }

    instance.journal = journal

    reportSessionJournalReplay(logger, instance.path, replay)

    var settleErr error
    if 0 < replay.replayed {
        settleErr = instance.compactLocked()
    } else {
        settleErr = instance.resetJournalLocked(snapshotDigest)
    }

    if nil != settleErr {
        _ = journal.Close()

        return exception.NewError("failed to settle the session storage journal at construction", exceptioncontract.Context{"path": journalPath}, settleErr)
    }

    if true == journalEnabled {
        return nil
    }

    instance.journal = nil

    if closeErr := journal.Close(); nil != closeErr {
        return exception.NewError("failed to close the session storage journal", exceptioncontract.Context{"path": journalPath}, closeErr)
    }

    if removeErr := os.Remove(journalPath); nil != removeErr {
        return exception.NewError("failed to remove the compacted session storage journal", exceptioncontract.Context{"path": journalPath}, removeErr)
    }

    return nil
}

func newFileStorageFromFile(fileInstance *os.File, clockInstance clockcontract.Clock) (*FileStorage, error) {
    if handleErr := refuseUnwritableHandle(fileInstance); nil != handleErr {
        return nil, handleErr
    }

    decoded, err := readSessionFileFromHandle(fileInstance)
    if nil != err {
        return nil, err
    }

    storage := &FileStorage{
        file:        fileInstance,
        ownsFile:    false,
        sessionById: decoded,
        clock:       clockInstance,
    }

    return storage, nil
}

/* FileStorage is recommended for development only: values round-trip through JSON, so after a restart an int reads back float64. Every door honours its context on entry alone, and a write that began runs to its end. */
type FileStorage struct {
    maxSessions    bound.Resolved[int]
    earliestExpiry int64
    mutex    sync.Mutex
    path     string
    file     *os.File
    ownsFile bool
    closed   bool
    clock    clockcontract.Clock
    logger   loggingcontract.Logger

    journal                *os.File
    journalSize            int64
    journalHeaderSize      int64
    journalMaxBytes        bound.Resolved[int]
    journalNeedsCompaction bool
    journalSeam            func(point journalPoint)

    sessionById map[string]fileSessionEntry
}

type fileSessionEntry struct {
    Data      map[string]any `json:"data"`
    ExpiresAt int64          `json:"expiresAt"`
}

func (instance *FileStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nil, false, nilErr
    }

    ctx := runtimeInstance.Context()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return nil, false, exception.NewError("session storage is closed", nil, nil)
    }

    if "" == sessionId {
        return nil, false, exception.NewError("session id is required in load session", nil, nil)
    }

    if doneErr := ctx.Err(); nil != doneErr {
        return nil, false, doneErr
    }

    entry, exists := instance.sessionById[sessionId]
    if false == exists {
        return nil, false, nil
    }

    if 0 != entry.ExpiresAt && instance.clock.Now().UnixNano() >= entry.ExpiresAt {
        /* the journal needs no record: the replay drops a lapsed session by its expiry */
        if nil != instance.journal {
            delete(instance.sessionById, sessionId)

            return nil, false, nil
        }

        /* the flush failure is not returned: this load's answer is settled, and an error would answer 503 where a fresh session is served */
        _ = instance.flushLocked()

        return nil, false, nil
    }

    return internal.CopyAnyMap(entry.Data), true, nil
}

func (instance *FileStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    ctx := runtimeInstance.Context()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
    }

    if "" == sessionId {
        return exception.NewError("session id is required in save session", nil, nil)
    }

    expiry, ttlErr := sessioncontract.ResolveTtl(ttl)
    if nil != ttlErr {
        return ttlErr
    }

    /* asked before the pending compaction, so a caller whose context ended pays for none */
    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    expiresAt := int64(0)
    if 0 < expiry {
        /* UnixNano wraps past 2262, so a very large ttl saturates at the maximum instant */
        expiration := instance.clock.Now().Add(expiry)
        if true == expiration.After(time.Unix(0, math.MaxInt64)) {
            expiresAt = math.MaxInt64
        } else {
            expiresAt = expiration.UnixNano()
        }
    }

    entry := fileSessionEntry{
        Data:      internal.CopyAnyMap(data),
        ExpiresAt: expiresAt,
    }

    if nil != instance.journal {
        if pendingErr := instance.compactPendingLocked(); nil != pendingErr {
            return pendingErr
        }
    }

    previousEntry, hadPrevious := instance.sessionById[sessionId]

    if false == hadPrevious {
        if refusedErr := instance.refuseNewSessionAtCeilingLocked(); nil != refusedErr {
            return refusedErr
        }
    }

    instance.sessionById[sessionId] = entry

    if 0 != expiresAt && expiresAt < instance.earliestExpiry {
        instance.earliestExpiry = expiresAt
    }

    if nil != instance.journal {
        appendErr := instance.appendJournalRecordLocked(sessionJournalRecord{
            Operation: sessionJournalOperationSave,
            Id:        sessionId,
            ExpiresAt: expiresAt,
            Data:      entry.Data,
        })
        if nil != appendErr {
            if true == hadPrevious {
                instance.sessionById[sessionId] = previousEntry
            } else {
                delete(instance.sessionById, sessionId)
            }

            return appendErr
        }

        instance.purgeLapsedInMemoryLocked()
        instance.compactPastThresholdLocked()

        return nil
    }

    flushErr := instance.flushLocked()
    if nil != flushErr {
        /* rolled back on a flush failure, unless it struck after the new document was written */
        if false == exception.ChainIs(flushErr, errSessionStoragePersistedDespiteFlushFailure) {
            if true == hadPrevious {
                instance.sessionById[sessionId] = previousEntry
            } else {
                delete(instance.sessionById, sessionId)
            }
        }

        return flushErr
    }

    return nil
}

/* sweeps only once an entry can have lapsed, so a flood of refused saves does not walk the whole table each time */
func (instance *FileStorage) refuseNewSessionAtCeilingLocked() error {
    if true == instance.maxSessions.Unlimited || len(instance.sessionById) < instance.maxSessions.Value {
        return nil
    }

    now := instance.clock.Now().UnixNano()
    if now >= instance.earliestExpiry {
        instance.purgeExpiredLocked()
        instance.earliestExpiry = instance.earliestExpiryLocked()
    }

    if len(instance.sessionById) < instance.maxSessions.Value {
        return nil
    }

    return exception.NewError(
        "the session file storage is full",
        exceptioncontract.Context{
            "field":       fileStorageMaxSessionsRule.Name,
            "maxSessions": instance.maxSessions.Value,
        },
        nil,
    )
}

func (instance *FileStorage) earliestExpiryLocked() int64 {
    earliestExpiry := int64(math.MaxInt64)

    for _, entry := range instance.sessionById {
        if 0 != entry.ExpiresAt && entry.ExpiresAt < earliestExpiry {
            earliestExpiry = entry.ExpiresAt
        }
    }

    return earliestExpiry
}

func (instance *FileStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    if nilErr := containercontract.RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    ctx := runtimeInstance.Context()

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
    }

    if "" == sessionId {
        return exception.NewError("session id is required in delete session", nil, nil)
    }

    /* asked before the unknown-id shortcut, so a done context is answered one way */
    if doneErr := ctx.Err(); nil != doneErr {
        return doneErr
    }

    if _, exists := instance.sessionById[sessionId]; false == exists {
        return nil
    }

    if nil != instance.journal {
        if pendingErr := instance.compactPendingLocked(); nil != pendingErr {
            return pendingErr
        }
    }

    previousEntry := instance.sessionById[sessionId]
    delete(instance.sessionById, sessionId)

    if nil != instance.journal {
        appendErr := instance.appendJournalRecordLocked(sessionJournalRecord{
            Operation: sessionJournalOperationDelete,
            Id:        sessionId,
        })
        if nil != appendErr {
            instance.sessionById[sessionId] = previousEntry

            return appendErr
        }

        instance.purgeLapsedInMemoryLocked()
        instance.compactPastThresholdLocked()

        return nil
    }

    flushErr := instance.flushLocked()
    if nil != flushErr {
        /* restored on a flush failure, unless it struck after the new document was written */
        if false == exception.ChainIs(flushErr, errSessionStoragePersistedDespiteFlushFailure) {
            instance.sessionById[sessionId] = previousEntry
        }

        return flushErr
    }

    return nil
}

/* CloseWithContext compacts the journal into the snapshot; a failed compaction keeps the journal for the next construction to replay. */
func (instance *FileStorage) CloseWithContext(closeContext context.Context) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return nil
    }

    instance.closed = true

    if nil != instance.journal {
        journal := instance.journal

        var compactErr error
        if instance.journalHeaderSize < instance.journalSize || true == instance.journalNeedsCompaction {
            compactErr = instance.compactLocked()
        }
        instance.journal = nil

        closeErr := journal.Close()

        if nil != compactErr {
            return compactErr
        }

        if nil != closeErr {
            return exception.NewError("failed to close session storage journal", nil, closeErr)
        }
    }

    if false == instance.ownsFile {
        instance.file = nil
        return nil
    }

    fileInstance := instance.file
    instance.file = nil

    if nil == fileInstance {
        return nil
    }

    err := fileInstance.Close()
    if nil != err {
        return exception.NewError("failed to close session storage file", nil, err)
    }

    return nil
}

/* the one place a lapsed entry is removed: no caller deletes the expired session it found */
func (instance *FileStorage) purgeExpiredLocked() {
    now := instance.clock.Now().UnixNano()

    for sessionId, entry := range instance.sessionById {
        if 0 != entry.ExpiresAt && now >= entry.ExpiresAt {
            delete(instance.sessionById, sessionId)
        }
    }
}

func (instance *FileStorage) flushLocked() error {
    instance.purgeExpiredLocked()

    snapshot := instance.sessionById

    if true == instance.ownsFile && "" != instance.path {
        return writeSessionFileAtomically(instance.path, snapshot)
    }

    if nil == instance.file {
        return exception.NewError("session storage file is nil", nil, nil)
    }

    return writeSessionFileInPlace(instance.file, snapshot)
}

func readSessionFileAtPath(path string) (map[string]fileSessionEntry, error) {
    fileInstance, err := os.Open(path)
    if nil != err {
        if true == os.IsNotExist(err) {
            return make(map[string]fileSessionEntry), nil
        }

        return nil, exception.NewError(
            "failed to open session storage file",
            exceptioncontract.Context{
                "path": path,
            },
            err,
        )
    }

    defer fileInstance.Close()

    return readSessionFileFromHandle(fileInstance)
}

func readSessionFileFromHandle(fileInstance *os.File) (map[string]fileSessionEntry, error) {
    _, err := fileInstance.Seek(0, io.SeekStart)
    if nil != err {
        return nil, exception.NewError("failed to seek session storage file", nil, err)
    }

    stat, err := fileInstance.Stat()
    if nil != err {
        return nil, exception.NewError("failed to stat session storage file", nil, err)
    }

    decoded := make(map[string]fileSessionEntry)

    if 0 == stat.Size() {
        return decoded, nil
    }

    decoder := json.NewDecoder(fileInstance)

    err = decoder.Decode(&decoded)
    if nil != err {
        return nil, exception.NewError("failed to decode session storage file", nil, err)
    }

    /* a JSON null decodes without an error into a nil map */
    if nil == decoded {
        return nil, exception.NewError("session storage snapshot must be a JSON object", nil, nil)
    }

    return decoded, nil
}

func encodeSessionSnapshot(snapshot map[string]fileSessionEntry) ([]byte, error) {
    var buffer bytes.Buffer

    if encodeErr := json.NewEncoder(&buffer).Encode(snapshot); nil != encodeErr {
        return nil, exception.NewError("failed to encode session storage file", nil, encodeErr)
    }

    return buffer.Bytes(), nil
}

func writeSessionFileAtomically(path string, snapshot map[string]fileSessionEntry) error {
    encoded, encodeErr := encodeSessionSnapshot(snapshot)
    if nil != encodeErr {
        return encodeErr
    }

    return writeSessionBytesAtomically(path, encoded)
}

func writeSessionBytesAtomically(path string, encoded []byte) error {
    directoryPath := filepath.Dir(path)
    err := os.MkdirAll(directoryPath, 0755)
    if nil != err {
        return exception.NewError(
            "failed to create session storage directory",
            exceptioncontract.Context{
                "path": directoryPath,
            },
            err,
        )
    }

    tempFile, err := os.CreateTemp(directoryPath, sessionTemporaryPrefix(path)+"*.tmp")
    if nil != err {
        return exception.NewError(
            "failed to create session storage temp file",
            exceptioncontract.Context{
                "path": path,
            },
            err,
        )
    }

    tempPath := tempFile.Name()

    _, err = tempFile.Write(encoded)
    if nil != err {
        _ = tempFile.Close()
        _ = os.Remove(tempPath)

        return exception.NewError("failed to write session storage temp file", nil, err)
    }

    err = tempFile.Sync()
    if nil != err {
        _ = tempFile.Close()
        _ = os.Remove(tempPath)

        return exception.NewError("failed to sync session storage file", nil, err)
    }

    err = tempFile.Close()
    if nil != err {
        _ = os.Remove(tempPath)

        return exception.NewError("failed to close session storage temp file", nil, err)
    }

    err = os.Rename(tempPath, path)
    if nil != err {
        _ = os.Remove(tempPath)

        return exception.NewError("failed to replace session storage file", nil, err)
    }

    /* the rename is durable only once the directory is flushed; the rename already committed the document */
    if directorySyncErr := syncSessionDirectory(filepath.Dir(path)); nil != directorySyncErr {
        return directorySyncErr
    }

    return nil
}

/* every failure here is over a document already at its path, so each carries the persisted-despite-flush mark */
func syncSessionDirectory(path string) error {
    directory, openErr := os.Open(path)
    if nil != openErr {
        return persistedDespiteFlushFailure(
            "failed to open session storage directory for fsync",
            exceptioncontract.Context{
                "path": path,
            },
            openErr,
        )
    }

    syncErr := directory.Sync()
    closeErr := directory.Close()

    if nil != syncErr {
        return persistedDespiteFlushFailure(
            "failed to fsync session storage directory",
            exceptioncontract.Context{
                "path": path,
            },
            syncErr,
        )
    }

    if nil != closeErr {
        return persistedDespiteFlushFailure(
            "failed to close session storage directory after fsync",
            exceptioncontract.Context{
                "path": path,
            },
            closeErr,
        )
    }

    return nil
}

/* a basename past 200 bytes is replaced by its digest, or CreateTemp's random part would pass the 255-byte component limit */
func sessionTemporaryPrefix(path string) string {
    base := filepath.Base(path)
    if 200 < len(base) {
        digest := sha256.Sum256([]byte(base))
        base = ".melody-session-" + hex.EncodeToString(digest[:])
    }

    return base + "."
}

/* runs at construction, where per-process ownership means nothing is mid-rename */
func removeOrphanSessionTemporaryFiles(path string) {
    directoryPath := filepath.Dir(path)

    entries, readErr := os.ReadDir(directoryPath)
    if nil != readErr {
        return
    }

    prefix := sessionTemporaryPrefix(path)

    for _, entry := range entries {
        if true == entry.IsDir() {
            continue
        }

        name := entry.Name()
        if true == strings.HasPrefix(name, prefix) && true == strings.HasSuffix(name, ".tmp") {
            _ = os.Remove(filepath.Join(directoryPath, name))
        }
    }
}

/* an appending handle is refused by a zero-length WriteAt and a read-only one by its access mode; a truncate probe would refuse a handle whose writes work */
func refuseUnwritableHandle(fileInstance *os.File) error {
    _, err := fileInstance.WriteAt([]byte{}, 0)
    if nil != err {
        return exception.NewError(
            "session storage file is opened for appending",
            exceptioncontract.Context{
                "name": fileInstance.Name(),
            },
            err,
        )
    }

    writable, modeErr := fileHandleWritable(fileInstance)
    if nil == modeErr && false == writable {
        return exception.NewError(
            "session storage file is opened read-only",
            exceptioncontract.Context{
                "name": fileInstance.Name(),
            },
            nil,
        )
    }

    return nil
}

func writeSessionFileInPlace(fileInstance *os.File, snapshot map[string]fileSessionEntry) error {
    /* encoded first, so a failed encode never touches the live file */
    var buffer bytes.Buffer

    encoder := json.NewEncoder(&buffer)

    err := encoder.Encode(snapshot)
    if nil != err {
        return exception.NewError("failed to encode session storage file", nil, err)
    }

    /* write first, then truncate to its length, so the file is never empty on disk; WriteAt, since write(2) on an appending descriptor ignores a seek */
    writtenCount, err := fileInstance.WriteAt(buffer.Bytes(), 0)
    if nil != err {
        /* a part-way write already tore the document it replaces */
        if 0 < writtenCount {
            return persistedDespiteFlushFailure("failed to write session storage file after it was partly written", nil, err)
        }

        return exception.NewError("failed to write session storage file", nil, err)
    }

    /* the new document sits whole at offset 0 and a single Decode ignores a stale tail */
    err = fileInstance.Truncate(int64(buffer.Len()))
    if nil != err {
        return persistedDespiteFlushFailure("failed to truncate session storage file", nil, err)
    }

    err = fileInstance.Sync()
    if nil != err {
        return persistedDespiteFlushFailure("failed to sync session storage file", nil, err)
    }

    return nil
}

var errSessionStoragePersistedDespiteFlushFailure = errors.New("session storage document was persisted before the flush step failed")

func persistedDespiteFlushFailure(message string, context exceptioncontract.Context, cause error) error {
    return exception.NewError(
        message,
        context,
        errors.Join(cause, errSessionStoragePersistedDespiteFlushFailure),
    )
}

var _ sessioncontract.Storage = (*FileStorage)(nil)
