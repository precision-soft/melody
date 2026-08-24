package session

import (
    "bytes"
    "encoding/json"
    "io"
    "math"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
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

/* NewFileStorageFromFile builds the storage over a handle the caller owns and keeps owning: it is not closed here, and every write goes through that same handle rather than through a path. The atomicity its NewFileStorageFromPath sibling guarantees is therefore NOT available to this door — a temp file and a rename would unlink the inode the caller still holds, leaving it writing into a file nothing can reach. What this door guarantees instead: the snapshot is encoded whole before a byte is written, the write precedes the truncation, and the truncation cuts to the length just written, so no crash can leave a zero-length file and lose every persisted session. A kill landing inside the write itself can still leave a torn document, which the next construction reports as a decode failure rather than reading as an empty one.

The handle must be seekable and must not be opened for appending. Both are refused here rather than at the first save, since both are properties of what the caller opened and neither can improve later: appending in particular used to be accepted and then to corrupt silently, because every write landed after the document it was replacing. */
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

/* FileStorage is recommended for development only. Two reasons are written here because neither shows until the store has been up for a while. Values are flushed as JSON and reloaded at construction, so a session survives a restart with its SHAPES changed — an int comes back float64, a struct comes back map[string]any, a time.Time comes back a string — while the same session read in-process keeps the types the handler stored: a type assertion on a session value therefore holds for the life of a process and starts failing after the first restart. And every write re-encodes and fsyncs the whole snapshot, so what one save costs is set by how many sessions everyone else has: measured at about 6.7ms over 100 sessions, 9.1ms over 1 000 and 30ms over 10 000, which is a few dozen writes a second rather than a few thousand. */
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
        /* the flush is what drops this entry: purgeExpiredLocked runs inside it against the same clock and the same predicate, so naming the session again here would only duplicate the removal.

           its failure is deliberately not returned. The answer to this load — no such session — was settled before the flush was attempted, and the flush is housekeeping no caller asked for; handing back its error instead would make Manager.Session panic, so an expired cookie on a store that cannot be written would answer 500 where a client holding no cookie at all is served a fresh session. The purge has already taken the entry out of the map, so nothing serves it again; only the file still carries it, until the next write that succeeds. */
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
        /* roll the in-memory entry back on a flush failure so a Save that returns an error is not observable through a later Load — the in-memory state must not diverge from what was persisted */
        if true == hadPrevious {
            instance.sessionById[sessionId] = previousEntry
        } else {
            delete(instance.sessionById, sessionId)
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
        /* restore the entry on a flush failure so a Delete that returns an error does not drop the session from the in-memory state while it is still persisted */
        instance.sessionById[sessionId] = previousEntry

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

/* purgeExpiredLocked drops every lapsed session before a snapshot is written. Without it an expired session is only ever removed when a Load happens to name it, so entries accumulate forever in the map and in the file — and because every Save rewrites the whole snapshot, the write cost grows with everything that ever expired.

This is the one place a lapsed entry is removed: no caller deletes the session it just found expired, they all rely on the flush below reaching this. Anything that narrows the predicate here — leaving a class of lapsed entries in place — has to give those callers their explicit delete back. */
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

    /* the rename is durable only once the directory itself is flushed: without this, a power loss after a save could resurface the previous snapshot — the session that was just written is gone and its user silently logged out. The cron generator's atomic writer holds the same rule for the same reason. */
    if directorySyncErr := syncSessionDirectory(filepath.Dir(path)); nil != directorySyncErr {
        return directorySyncErr
    }

    return nil
}

/* syncSessionDirectory fsyncs the directory that just received a rename, which is where the file's NAME lives — the temp file's own Sync covered only its bytes. */
func syncSessionDirectory(path string) error {
    directory, openErr := os.Open(path)
    if nil != openErr {
        return exception.NewError(
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
        return exception.NewError(
            "failed to fsync session storage directory",
            exceptioncontract.Context{
                "path": path,
            },
            syncErr,
        )
    }

    if nil != closeErr {
        return exception.NewError(
            "failed to close session storage directory after fsync",
            exceptioncontract.Context{
                "path": path,
            },
            closeErr,
        )
    }

    return nil
}

/* removeOrphanSessionTemporaryFiles sweeps the temp files a killed process left behind: a hard kill between CreateTemp and the rename skips every cleanup path, each orphan is a complete snapshot of every live session and its tokens, and nothing ever opened them again — the surface only grew. The sweep runs at construction, where the per-process ownership the session documentation states means nothing else can be mid-rename over this path. A file that cannot be removed is left for the next construction rather than failing this one. */
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

/* refuseAppendModeHandle asks the handle the only question that settles it, and asks it with a write of nothing: WriteAt refuses an appending handle before it looks at the bytes, so an empty slice answers the question and touches no file — a zero-length write on a handle that is not appending returns without reaching the descriptor at all. There is no portable way to read the open flags back otherwise, and os.File itself keeps the answer: it is the same field WriteAt consults on every save, so the door and the write agree by construction rather than by two guesses. */
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
    /* encode into an in-memory buffer first so a failed encode (for example a session value that is not JSON-marshalable) never truncates the live file and destroys the previously-persisted sessions; the file is only seeked, truncated and rewritten once the encode has succeeded, mirroring the validate-before-commit guarantee of writeSessionFileAtomically */
    var buffer bytes.Buffer

    encoder := json.NewEncoder(&buffer)

    err := encoder.Encode(snapshot)
    if nil != err {
        return exception.NewError("failed to encode session storage file", nil, err)
    }

    /* the write goes first and the truncation cuts to the length it produced, because the reverse order held a window in which the file was empty on disk: a process killed between a truncation to zero and the write that was to follow — an OOM kill, a docker kill, a deploy with no grace period — left a zero-length file, which the next boot reads as "no sessions at all" and answers by logging every user out without a single error. This door writes through a handle it does not own, so the temp-and-rename its FromPath sibling uses is not available to it: a rename would unlink the very inode the caller still holds. What remains is a window in which a torn document can survive a kill mid-write, and that one is written on the contract instead of being made to disappear.

       The offset is named on the write rather than sought beforehand, because a seek is advice a handle opened for appending is free to ignore: write(2) on an O_APPEND descriptor lands at the end of the file whatever was sought, so the snapshot went AFTER the previous document and the truncation below then cut the pair to the new length — keeping the old document's first bytes and calling them the session file. WriteAt refuses such a handle by contract instead, so the write either lands at zero or fails, and the failure reaches the caller as one. */
    _, err = fileInstance.WriteAt(buffer.Bytes(), 0)
    if nil != err {
        return exception.NewError("failed to write session storage file", nil, err)
    }

    err = fileInstance.Truncate(int64(buffer.Len()))
    if nil != err {
        return exception.NewError("failed to truncate session storage file", nil, err)
    }

    err = fileInstance.Sync()
    if nil != err {
        return exception.NewError("failed to sync session storage file", nil, err)
    }

    return nil
}

var _ sessioncontract.Storage = (*FileStorage)(nil)
