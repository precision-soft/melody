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

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    sessioncontract "github.com/precision-soft/melody/session/contract"
)

func NewFileStorageFromPath(path string) (*FileStorage, error) {
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
    }

    return storage, nil
}

/* NewFileStorageFromFile builds the storage over a handle the caller owns and keeps owning: it is not closed here, and every write goes through it, so the temp-file-and-rename atomicity of NewFileStorageFromPath is not available. The snapshot is encoded whole before a byte is written, the write precedes the truncation and the truncation cuts to the length written, so no crash leaves a zero-length file; a kill inside the write can leave a torn document, which the next construction reports as a decode failure. The handle must be seekable and not opened for appending, and both are refused here. */
func NewFileStorageFromFile(fileInstance *os.File) (*FileStorage, error) {
    if nil == fileInstance {
        return nil, exception.NewError("session storage file is nil", nil, nil)
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
    }

    return storage, nil
}

/* FileStorage is recommended for development only. Values are flushed as JSON and reloaded at construction, so a session survives a restart with its shapes changed (an int comes back float64, a struct map[string]any, a time.Time a string) while an in-process read keeps the stored types. Every write re-encodes and fsyncs the whole snapshot, so a save costs in proportion to the number of live sessions. */
type FileStorage struct {
    mutex    sync.Mutex
    path     string
    file     *os.File
    ownsFile bool
    closed   bool

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

    if 0 != entry.ExpiresAt && time.Now().UnixNano() >= entry.ExpiresAt {
        /* the flush drops this entry: purgeExpiredLocked runs inside it with the same clock and predicate. Its failure is deliberately not returned: the answer to this load, no such session, is already settled, and returning the flush error would make Manager.Session panic, answering an expired cookie with a 500 on a store that cannot be written. */
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
        /* time.Time.UnixNano is only defined up to 2262-04-11 and wraps to a negative int64 past it; a caller using a very large ttl as a "never expire" value would otherwise land a negative ExpiresAt that Load and purgeExpiredLocked read as already lapsed and drop the session on the same Save, so saturate at the maximum representable instant the way InMemoryStorage keeps such sessions */
        expiration := time.Now().Add(ttl)
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
        /* roll the in-memory entry back on a flush failure so a Save that returns an error is not observable through a later Load — the in-memory state must not diverge from what was persisted. The one exception is a failure that struck AFTER the new document was already written: there the disk holds the new snapshot, so keeping the in-memory entry is what matches it, and rolling back is what would diverge. */
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
        /* restore the entry on a flush failure so a Delete that returns an error does not drop the session from the in-memory state while it is still persisted — unless the failure struck after the new document (the one without this session) was already written, where the disk already reflects the deletion and keeping it deleted in memory is what matches. */
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

/* purgeExpiredLocked drops every lapsed session before a snapshot is written, so expired entries do not accumulate in the map and the file. It is the one place a lapsed entry is removed: callers that find a session expired rely on the flush reaching this, so narrowing its predicate has to give them an explicit delete back. */
func (instance *FileStorage) purgeExpiredLocked() {
    now := time.Now().UnixNano()

    for sessionId, entry := range instance.sessionById {
        if 0 != entry.ExpiresAt && now >= entry.ExpiresAt {
            delete(instance.sessionById, sessionId)
        }
    }
}

/* flushLocked writes the snapshot, and purges first — unconditionally, on every path that reaches it. That is what lets Load answer an expired session without deleting it itself; a flush that stopped purging would leave the lapsed entry in the file. */
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

    /* the rename is durable only once the directory itself is flushed: without this, a power loss after a save could resurface the previous snapshot — the session that was just written is gone and its user silently logged out. The cron generator's atomic writer holds the same rule for the same reason. Its refusal travels back as it comes: the failure is already marked persisted-despite-flush, because the rename above committed the document before this step could fail. */
    if directorySyncErr := syncSessionDirectory(filepath.Dir(path)); nil != directorySyncErr {
        return directorySyncErr
    }

    return nil
}

/* syncSessionDirectory fsyncs the directory that just received a rename, where the file's name lives. Every failure it answers is over a document already at its path, so each carries the persisted-despite-flush mark: Save and Delete then keep the in-memory state that matches the disk rather than rolling it back. A caller that needs the directory flushed before its commit point must not use it. */
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

/* removeOrphanSessionTemporaryFiles sweeps the temp files a killed process left between CreateTemp and the rename, each a complete snapshot of every live session and its tokens. It runs at construction, where per-process ownership means nothing else is mid-rename over this path; a file that cannot be removed is left for the next construction. */
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

/* refuseAppendModeHandle asks the handle with a write of nothing: WriteAt refuses an appending handle before it looks at the bytes, so an empty slice settles the question without touching the file, through the same field WriteAt consults on every save. It cannot tell whether the handle can write at all: a read-only handle passes, loads sessions and fails every Save, because Go exposes the descriptor's access mode nowhere portable, and a truncate probe would also refuse a handle whose writes work while its truncate does not. */
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
    /* encode into an in-memory buffer first, so a failed encode (a session value that is not JSON-marshalable, say) never touches the live file; the file is written only once the encode has succeeded, the validate-before-commit guarantee writeSessionFileAtomically gives too */
    var buffer bytes.Buffer

    encoder := json.NewEncoder(&buffer)

    err := encoder.Encode(snapshot)
    if nil != err {
        return exception.NewError("failed to encode session storage file", nil, err)
    }

    /* the write goes first and the truncation cuts to the length it produced, so no kill can leave the file empty on disk, which the next boot would read as no sessions at all; a kill mid-write can still leave a torn document, as the contract states. The offset is named on the write rather than sought beforehand: an O_APPEND descriptor ignores a seek, while WriteAt refuses it, so the write lands at zero or fails. */
    writtenCount, err := fileInstance.WriteAt(buffer.Bytes(), 0)
    if nil != err {
        /* a WriteAt that fails part way has already put the head of the new document over the tail of the document it replaces, so the file decodes as neither; rolling the in-memory entry back would restore a state the disk does not hold, so a torn write carries the mark the truncate and sync failures below carry. A write that placed nothing is the ordinary failure it looks like. */
        if 0 < writtenCount {
            return persistedDespiteFlushFailure("failed to write session storage file after it was partly written", nil, err)
        }

        return exception.NewError("failed to write session storage file", nil, err)
    }

    /* past this point the new document sits at offset 0 in full, and readSessionFileFromHandle decodes exactly it — a single Decode consumes one JSON value and ignores any stale tail a shorter document leaves behind. So a Truncate or Sync failure now leaves the file already holding the NEW snapshot: rolling the in-memory entry back would make it disagree with what is on disk, the very divergence the rollback exists to prevent, only inverted. The failure is still reported, wrapped so Save/Delete keep the in-memory state that now matches the disk instead of undoing it. */
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

/* errSessionStoragePersistedDespiteFlushFailure marks a flush failure that happened AFTER the new document was already laid down on disk, so the caller keeps its in-memory state rather than rolling it back to disagree with what is persisted. */
var errSessionStoragePersistedDespiteFlushFailure = errors.New("session storage document was persisted before the flush step failed")

func persistedDespiteFlushFailure(message string, context exceptioncontract.Context, cause error) error {
    return exception.NewError(
        message,
        context,
        errors.Join(cause, errSessionStoragePersistedDespiteFlushFailure),
    )
}

var _ sessioncontract.Storage = (*FileStorage)(nil)
