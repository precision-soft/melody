package session

import (
    "encoding/json"
    "io"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    sessioncontract "github.com/precision-soft/melody/v2/session/contract"
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

func NewFileStorageFromFile(fileInstance *os.File) (*FileStorage, error) {
    if nil == fileInstance {
        return nil, exception.NewError("session storage file is nil", nil, nil)
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

/** @important recommended for dev only */
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
        delete(instance.sessionById, sessionId)

        flushErr := instance.flushLocked()
        if nil != flushErr {
            return nil, false, flushErr
        }

        return nil, false, nil
    }

    return copyAnyMap(entry.Data), true, nil
}

func (instance *FileStorage) Save(sessionId string, data map[string]any, ttl time.Duration) error {
    if "" == sessionId {
        return exception.NewError("session id is required in save session", nil, nil)
    }

    expiresAt := int64(0)
    if 0 < ttl {
        expiresAt = time.Now().Add(ttl).UnixNano()
    }

    entry := fileSessionEntry{
        Data:      copyAnyMap(data),
        ExpiresAt: expiresAt,
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return exception.NewError("session storage is closed", nil, nil)
    }

    instance.sessionById[sessionId] = entry

    return instance.flushLocked()
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

    delete(instance.sessionById, sessionId)

    return instance.flushLocked()
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

func (instance *FileStorage) flushLocked() error {
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

    return nil
}

func writeSessionFileInPlace(fileInstance *os.File, snapshot map[string]fileSessionEntry) error {
    _, err := fileInstance.Seek(0, io.SeekStart)
    if nil != err {
        return exception.NewError("failed to seek session storage file", nil, err)
    }

    err = fileInstance.Truncate(0)
    if nil != err {
        return exception.NewError("failed to truncate session storage file", nil, err)
    }

    encoder := json.NewEncoder(fileInstance)

    err = encoder.Encode(snapshot)
    if nil != err {
        return exception.NewError("failed to encode session storage file", nil, err)
    }

    err = fileInstance.Sync()
    if nil != err {
        return exception.NewError("failed to sync session storage file", nil, err)
    }

    return nil
}

func copyAnyMap(data map[string]any) map[string]any {
    if nil == data {
        return map[string]any{}
    }

    copied := make(map[string]any, len(data))
    for key, value := range data {
        switch typedValue := value.(type) {
        case map[string]any:
            copied[key] = copyAnyMap(typedValue)
        default:
            copied[key] = value
        }
    }

    return copied
}

var _ sessioncontract.Storage = (*FileStorage)(nil)
