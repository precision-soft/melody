package session

import (
    "bytes"
    "encoding/json"
    "errors"
    "io"
    "math"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/clock"
    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

func NewFileStorageFromPath(path string) (*FileStorage, error) {
    return NewFileStorageFromPathWithClock(path, clock.NewSystemClock())
}

/* NewFileStorageFromPathWithClock reads every expiry instant from the given clock. The file persists the wall reading, so expiry follows every step of the wall clock; SESSION.md names the trade. */
func NewFileStorageFromPathWithClock(path string, clockInstance clockcontract.Clock) (*FileStorage, error) {
    if true == internal.IsNilInterface(clockInstance) {
        return nil, exception.NewError("session storage clock is not provided", nil, nil)
    }

    trimmedPath := filepath.Clean(path)
    if "" == trimmedPath || "." == trimmedPath {
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
        path:        trimmedPath,
        ownsFile:    true,
        sessionById: decoded,
        clock:       clockInstance,
    }

    return storage, nil
}

/* NewFileStorageFromFile builds the storage over a handle the caller owns and keeps owning: it is not closed here, and every write goes through it. A write cannot be atomic through a handle, but the snapshot is encoded whole first, the write precedes the truncation and the truncation cuts to the length written, so no crash leaves a zero-length file; a kill mid-write can leave a torn document, which the next construction reports as a decode failure. A handle that cannot seek or that appends is refused. */
func NewFileStorageFromFile(fileInstance *os.File) (*FileStorage, error) {
    return NewFileStorageFromFileWithClock(fileInstance, clock.NewSystemClock())
}

/* NewFileStorageFromFileWithClock is NewFileStorageFromFile with the clock injected. */
func NewFileStorageFromFileWithClock(fileInstance *os.File, clockInstance clockcontract.Clock) (*FileStorage, error) {
    if nil == fileInstance {
        return nil, exception.NewError("session storage file is nil", nil, nil)
    }

    if true == internal.IsNilInterface(clockInstance) {
        return nil, exception.NewError("session storage clock is not provided", nil, nil)
    }

    if appendErr := refuseAppendModeHandle(fileInstance); nil != appendErr {
        return nil, appendErr
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

/* FileStorage is recommended for development only. Values are flushed as JSON and reloaded at construction, so after a restart an int reads back float64, a struct map[string]any and a time.Time a string, while in-process they keep the stored types. Every write re-encodes and fsyncs the whole snapshot, so its cost grows with the number of sessions. */
type FileStorage struct {
    mutex    sync.Mutex
    path     string
    file     *os.File
    ownsFile bool
    closed   bool
    clock    clockcontract.Clock

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
        /* the flush drops the lapsed entry, and its failure is not returned: this load's answer is settled, and an error would make Manager.Session panic where a client without a cookie is served a fresh session */
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

    previousEntry, hadPrevious := instance.sessionById[sessionId]
    instance.sessionById[sessionId] = entry

    flushErr := instance.flushLocked()
    if nil != flushErr {
        /* the entry is rolled back on a flush failure, so a failed Save is not visible to a later Load, unless the failure struck after the new document was written */
        if false == errors.Is(flushErr, errSessionStoragePersistedDespiteFlushFailure) {
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

    previousEntry := instance.sessionById[sessionId]
    delete(instance.sessionById, sessionId)

    flushErr := instance.flushLocked()
    if nil != flushErr {
        /* the entry is restored on a flush failure, unless the failure struck after the new document was written */
        if false == errors.Is(flushErr, errSessionStoragePersistedDespiteFlushFailure) {
            instance.sessionById[sessionId] = previousEntry
        }

        return flushErr
    }

    return nil
}

func (instance *FileStorage) Close() error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return nil
    }

    instance.closed = true

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

    return decoded, nil
}

func writeSessionFileAtomically(path string, snapshot map[string]fileSessionEntry) error {
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

    tempFile, err := os.CreateTemp(directoryPath, filepath.Base(path)+".*.tmp")
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

    encoder := json.NewEncoder(tempFile)

    err = encoder.Encode(snapshot)
    if nil != err {
        _ = tempFile.Close()
        _ = os.Remove(tempPath)

        return exception.NewError("failed to encode session storage file", nil, err)
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

/* removeOrphanSessionTemporaryFiles sweeps the temp files a killed process left, each a full snapshot of every live session. It runs at construction, where per-process ownership means nothing is mid-rename; a file that cannot be removed is left for the next construction. */
func removeOrphanSessionTemporaryFiles(path string) {
    directoryPath := filepath.Dir(path)

    entries, readErr := os.ReadDir(directoryPath)
    if nil != readErr {
        return
    }

    prefix := filepath.Base(path) + "."

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

/* refuseAppendModeHandle refuses an appending handle with a zero-length WriteAt, which refuses such a handle before touching the file. A read-only handle passes it, since the zero-length write never reaches the descriptor, and then fails every Save. */
func refuseAppendModeHandle(fileInstance *os.File) error {
    _, err := fileInstance.WriteAt([]byte{}, 0)
    if nil == err {
        return nil
    }

    return exception.NewError(
        "session storage file is opened for appending",
        exceptioncontract.Context{
            "name": fileInstance.Name(),
        },
        err,
    )
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
