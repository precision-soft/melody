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
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* FileStorageConfig names the file the storage persists to, one way or the other; exactly one of Path and File is set: both is refused naming the two, and neither is refused. */
type FileStorageConfig struct {
    /* Path is a file the storage owns: it creates the file with its directory and writes every snapshot through a temporary file and a rename. Each Save and Delete appends one synced record to the journal beside it, <path>.journal, and the snapshot is rewritten only when the journal is compacted: at construction, past JournalMaxBytes and at close. */
    Path string

    /* File is an open handle the caller owns and keeps owning: it is not closed here, and every write goes through it. A write cannot be atomic through a handle, but the snapshot is encoded whole first, the write precedes the truncation and the truncation cuts to the length written, so no crash leaves a zero-length file; a kill mid-write can leave a torn document, which the next construction reports as a decode failure. A handle that cannot seek or that appends is refused, and so is a read-only one where the descriptor's access mode can be read; elsewhere its first Save reports it. A handle never journals: every write rewrites the whole snapshot, as JournalDisabled does over a Path. */
    File *os.File

    /* JournalDisabled writes the whole snapshot on every Save and Delete in Path mode, the third major's form, which a build without the journal reads in full; a journal left at the path is still replayed and compacted at construction, then removed. File mode never journals. */
    JournalDisabled bool

    /* JournalMaxBytes is the journal size past which a write compacts it into the snapshot: 8 MiB when left out, at least 64 KiB, unlimited refused. */
    JournalMaxBytes bound.Count

    /* Logger journals what the construction replayed or discarded and each threshold compaction that failed after its write landed, which the next write past the threshold retries; nil journals nothing, a typed nil is refused. */
    Logger loggingcontract.Logger

    /* MaxSessions bounds the sessions held at once: 100,000 when left out, at least 1, unlimited admitted. At the ceiling, once the lapsed ones are swept, a new id is refused and a stored one is still saved; a file already holding more keeps them. */
    MaxSessions bound.Count

    /* Clock is where every expiry instant is read; nil is the system clock. The file persists the wall reading, so expiry follows every step of the wall clock; SESSION.md names the trade. */
    Clock clockcontract.Clock
}

var fileStorageMaxSessionsRule = bound.Rule[int]{
    Name:              "FileStorageConfig.MaxSessions",
    Default:           DefaultMaxSessions,
    Minimum:           1,
    UnlimitedAdmitted: true,
}

/* NewFileStorage builds the storage over the path or the handle the configuration names and loads the sessions the file holds, answering an error for a MaxSessions or a JournalMaxBytes it refuses, a typed nil Logger, a configuration naming both or neither, a path that cannot be created, a handle that cannot be written, a file that does not decode, a journal of a format it does not read and a journal it cannot open, replay or settle. */
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

/* a journal left at the path is replayed even with the door off, so turning the journal off loses no write */
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

/* FileStorage is recommended for development only. Values are flushed as JSON and reloaded at construction, so after a restart an int reads back float64, a struct map[string]any and a time.Time a string, while in-process they keep the stored types. In Path mode a write appends and fsyncs one journal record, and the whole snapshot is re-encoded only at compaction; with the journal off, and in File mode, every write re-encodes and fsyncs the whole snapshot, so its cost grows with the number of sessions. */
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

func (instance *FileStorage) Load(sessionId string) (map[string]any, bool, error) {
    if "" == sessionId {
        return nil, false, exception.NewError("session id is required in load session", nil, nil)
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return nil, false, exception.NewError("session storage is closed", nil, nil)
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

        /* the flush drops the lapsed entry, and its failure is not returned: this load's answer is settled, and an error would refuse the request 503 where a client without a cookie is served a fresh session */
        _ = instance.flushLocked()

        return nil, false, nil
    }

    return internal.CopyAnyMap(entry.Data), true, nil
}

func (instance *FileStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    if "" == sessionId {
        return exception.NewError("session id is required in save session", nil, nil)
    }

    expiresAt := int64(0)
    if 0 < ttl {
        /* UnixNano wraps past 2262, so a very large ttl saturates at the maximum instant, as InMemoryStorage keeps it */
        expiration := instance.clock.Now().Add(ttl)
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

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
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
        /* the entry is rolled back on a flush failure, so a failed Save is not visible to a later Load, unless the failure struck after the new document was written */
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

/* refuseNewSessionAtCeilingLocked sweeps only once an entry can have lapsed, so a flood of refused saves does not walk the whole table on each one. */
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

/* earliestExpiryLocked answers the first instant a stored session lapses, the far end of time where none of them expires. */
func (instance *FileStorage) earliestExpiryLocked() int64 {
    earliestExpiry := int64(math.MaxInt64)

    for _, entry := range instance.sessionById {
        if 0 != entry.ExpiresAt && entry.ExpiresAt < earliestExpiry {
            earliestExpiry = entry.ExpiresAt
        }
    }

    return earliestExpiry
}

func (instance *FileStorage) Delete(sessionId string) error {
    if "" == sessionId {
        return exception.NewError("session id is required in delete session", nil, nil)
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
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
        /* the entry is restored on a flush failure, unless the failure struck after the new document was written */
        if false == exception.ChainIs(flushErr, errSessionStoragePersistedDespiteFlushFailure) {
            instance.sessionById[sessionId] = previousEntry
        }

        return flushErr
    }

    return nil
}

/* CloseWithContext compacts the journal, so a clean stop leaves the whole state in the snapshot for a build that does not read the journal; a failed compaction keeps the journal for the next construction to replay. */
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

/* purgeExpiredLocked drops every lapsed session before a snapshot is written. It is the one place a lapsed entry is removed: no caller deletes the expired session it found, so narrowing the predicate gives them back that delete. */
func (instance *FileStorage) purgeExpiredLocked() {
    now := instance.clock.Now().UnixNano()

    for sessionId, entry := range instance.sessionById {
        if 0 != entry.ExpiresAt && now >= entry.ExpiresAt {
            delete(instance.sessionById, sessionId)
        }
    }
}

/* flushLocked purges and then writes the snapshot, on every path. */
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

    /* a JSON null decodes without an error into a nil map, which the first Save would write into */
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

    /* the rename is durable only once the directory is flushed; the failure carries the persisted-despite-flush mark, since the rename already committed the document */
    if directorySyncErr := syncSessionDirectory(filepath.Dir(path)); nil != directorySyncErr {
        return directorySyncErr
    }

    return nil
}

/* syncSessionDirectory fsyncs the directory that received a rename. Every failure it answers is over a document already at its path, so each carries the persisted-despite-flush mark and Save and Delete keep the in-memory state. */
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

/* sessionTemporaryPrefix is the prefix of the temp files of a snapshot path, shared by the write and the sweep. A basename longer than 200 bytes is replaced by its digest, since the random part CreateTemp appends would push the name past the 255 bytes of one path component. */
func sessionTemporaryPrefix(path string) string {
    base := filepath.Base(path)
    if 200 < len(base) {
        digest := sha256.Sum256([]byte(base))
        base = ".melody-session-" + hex.EncodeToString(digest[:])
    }

    return base + "."
}

/* removeOrphanSessionTemporaryFiles sweeps the temp files a killed process left, each a full snapshot of every live session. It runs at construction, where per-process ownership means nothing is mid-rename; a file that cannot be removed is left for the next construction. */
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

/* refuseUnwritableHandle refuses a handle no Save could write through. An appending handle is refused by a zero-length WriteAt, which refuses such a handle before touching the file; a read-only one passes that write, which never reaches the descriptor, and is refused on the descriptor's access mode where the platform lets it be read. A mode that cannot be read is not a refusal: the handle is left to the read that follows. A truncate probe is not used, since it would also refuse a handle whose writes work while its truncate does not. */
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
    /* encode into a buffer first, so a failed encode never touches the live file */
    var buffer bytes.Buffer

    encoder := json.NewEncoder(&buffer)

    err := encoder.Encode(snapshot)
    if nil != err {
        return exception.NewError("failed to encode session storage file", nil, err)
    }

    /* the write goes first and the truncation cuts to its length, so the file is never empty on disk. The offset is named on the write rather than sought, since write(2) on an appending descriptor ignores a seek and WriteAt refuses one. */
    writtenCount, err := fileInstance.WriteAt(buffer.Bytes(), 0)
    if nil != err {
        /* a write that failed part way has already torn the document it replaces, so it is marked persisted-despite-flush and the caller keeps its in-memory state */
        if 0 < writtenCount {
            return persistedDespiteFlushFailure("failed to write session storage file after it was partly written", nil, err)
        }

        return exception.NewError("failed to write session storage file", nil, err)
    }

    /* past this point the new document sits whole at offset 0 and a single Decode ignores a stale tail, so a Truncate or Sync failure is marked persisted-despite-flush */
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

/* errSessionStoragePersistedDespiteFlushFailure marks a flush failure that happened after the new document was on disk, so the caller keeps its in-memory state. */
var errSessionStoragePersistedDespiteFlushFailure = errors.New("session storage document was persisted before the flush step failed")

func persistedDespiteFlushFailure(message string, context exceptioncontract.Context, cause error) error {
    return exception.NewError(
        message,
        context,
        errors.Join(cause, errSessionStoragePersistedDespiteFlushFailure),
    )
}

var _ sessioncontract.Storage = (*FileStorage)(nil)
