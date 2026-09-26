package session

import (
    "bytes"
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

/* NewFileStorageFromPathWithClock reads every expiry instant from the given clock. What this storage persists is the WALL reading — an entry carries expiresAt as unix nanoseconds, and no monotonic reading can survive a restart — so its expiry follows every step of the wall clock where the in-memory storage's monotonic comparison does not; SESSION.md names the trade. */
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

/* NewFileStorageFromFile borrows a seekable, non-append handle and never closes it. Snapshots are fully encoded before writing, and truncation follows the write. Unlike path-backed storage, updates are not atomic; interruption can leave a torn document that subsequent construction reports as a decode error. */
func NewFileStorageFromFile(fileInstance *os.File) (*FileStorage, error) {
    return NewFileStorageFromFileWithClock(fileInstance, clock.NewSystemClock())
}

/* NewFileStorageFromFileWithClock is the handle-owning door with the clock injected, the way the path door takes one. */
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

/* FileStorage is intended for development. Restarting reloads JSON values with generic JSON types, so concrete Go types do not round-trip. Every write serializes and syncs the full session snapshot; cost grows with all stored sessions. */
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

    /* BH-01: decoding JSON null succeeds but leaves a nil map, so the next Save
       would panic. Both path and handle constructors reject this snapshot before
       exposing storage; empty files and JSON objects remain valid.
       Regression: TestFileStorage_RejectsNullSnapshot (v1/v2/v3). */
    if nil == decoded {
        return nil, exception.NewError("session storage snapshot must be a JSON object", nil, nil)
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

    if directorySyncErr := syncSessionDirectory(filepath.Dir(path)); nil != directorySyncErr {
        return directorySyncErr
    }

    return nil
}

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

/* sessionTemporaryPrefix bounds names used by both snapshot writes and cleanup.
   BH-02: a valid 255-byte basename cannot carry an appended random suffix.
   Above 200 bytes, a digest leaves room for that suffix while retaining a
   per-destination prefix. Keep creation and orphan cleanup on this same helper
   when porting; otherwise long-name snapshots leave undiscoverable temp files.
   Regression: TestFileStorage_LongValidFilename (v1/v2/v3), including reopen. */
func sessionTemporaryPrefix(path string) string {
    base := filepath.Base(path)
    if 200 < len(base) {
        digest := sha256.Sum256([]byte(base))
        base = ".melody-session-" + hex.EncodeToString(digest[:])
    }

    return base + "."
}

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

    var buffer bytes.Buffer

    encoder := json.NewEncoder(&buffer)

    err := encoder.Encode(snapshot)
    if nil != err {
        return exception.NewError("failed to encode session storage file", nil, err)
    }

    writtenCount, err := fileInstance.WriteAt(buffer.Bytes(), 0)
    if nil != err {

        if 0 < writtenCount {
            return persistedDespiteFlushFailure("failed to write session storage file after it was partly written", nil, err)
        }

        return exception.NewError("failed to write session storage file", nil, err)
    }

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
