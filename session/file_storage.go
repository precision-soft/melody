package session

import (
    "encoding/json"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
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

    fileInstance, err := os.OpenFile(trimmedPath, os.O_RDWR|os.O_CREATE, 0644)
    if nil != err {
        return nil, exception.NewError(
            "failed to open session storage file",
            exceptioncontract.Context{
                "path": trimmedPath,
            },
            err,
        )
    }

    storage := &FileStorage{
        path:        trimmedPath,
        file:        fileInstance,
        ownsFile:    true,
        sessionById: make(map[string]fileSessionEntry),
    }

    err = storage.loadFromFile()
    if nil != err {
        _ = fileInstance.Close()
        return nil, err
    }

    return storage, nil
}

func NewFileStorageFromFile(fileInstance *os.File) (*FileStorage, error) {
    if nil == fileInstance {
        return nil, exception.NewError("session storage file is nil", nil, nil)
    }

    storage := &FileStorage{
        file:        fileInstance,
        ownsFile:    false,
        sessionById: make(map[string]fileSessionEntry),
    }

    err := storage.loadFromFile()
    if nil != err {
        return nil, err
    }

    return storage, nil
}

/** @important recommended for dev only */
type FileStorage struct {
    mutex    sync.RWMutex
    path     string
    file     *os.File
    ownsFile bool

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

    now := time.Now().UnixNano()

    instance.mutex.RLock()
    entry, exists := instance.sessionById[sessionId]
    instance.mutex.RUnlock()

    if false == exists {
        return nil, false, nil
    }

    if 0 != entry.ExpiresAt && now >= entry.ExpiresAt {
        instance.mutex.Lock()
        _, stillExists := instance.sessionById[sessionId]
        if true == stillExists {
            delete(instance.sessionById, sessionId)
        }
        instance.mutex.Unlock()

        flushErr := instance.flushToFile()
        if nil != flushErr {
            return nil, false, flushErr
        }

        return nil, false, nil
    }

    dataCopy := copyAnyMap(entry.Data)

    return dataCopy, true, nil
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
    instance.sessionById[sessionId] = entry
    instance.mutex.Unlock()

    return instance.flushToFile()
}

func (instance *FileStorage) Delete(sessionId string) error {
    if "" == sessionId {
        return exception.NewError("session id is required in delete session", nil, nil)
    }

    instance.mutex.Lock()
    _, exists := instance.sessionById[sessionId]
    if true == exists {
        delete(instance.sessionById, sessionId)
    }
    instance.mutex.Unlock()

    return instance.flushToFile()
}

func (instance *FileStorage) Close() error {
    instance.mutex.Lock()

    if false == instance.ownsFile {
        instance.mutex.Unlock()
        return nil
    }

    if nil == instance.file {
        instance.mutex.Unlock()
        return nil
    }

    fileInstance := instance.file
    instance.file = nil

    instance.mutex.Unlock()

    err := fileInstance.Close()
    if nil != err {
        return exception.NewError("failed to close session storage file", nil, err)
    }

    return nil
}

func (instance *FileStorage) loadFromFile() error {
    instance.mutex.RLock()
    fileInstance := instance.file
    instance.mutex.RUnlock()

    if nil == fileInstance {
        return exception.NewError("session storage file is nil", nil, nil)
    }

    _, err := fileInstance.Seek(0, 0)
    if nil != err {
        return exception.NewError("failed to seek session storage file", nil, err)
    }

    stat, err := fileInstance.Stat()
    if nil != err {
        return exception.NewError("failed to stat session storage file", nil, err)
    }

    decoded := make(map[string]fileSessionEntry)

    if 0 != stat.Size() {
        decoder := json.NewDecoder(fileInstance)

        err = decoder.Decode(&decoded)
        if nil != err {
            return exception.NewError("failed to decode session storage file", nil, err)
        }
    }

    instance.mutex.Lock()
    instance.sessionById = decoded
    instance.mutex.Unlock()

    return nil
}

func (instance *FileStorage) flushToFile() error {
    instance.mutex.RLock()
    snapshot := make(map[string]fileSessionEntry, len(instance.sessionById))
    for sessionId, entry := range instance.sessionById {
        snapshot[sessionId] = entry
    }
    fileInstance := instance.file
    path := instance.path
    ownsFile := instance.ownsFile
    instance.mutex.RUnlock()

    if nil == fileInstance {
        return exception.NewError("session storage file is nil", nil, nil)
    }

    if true == ownsFile && "" != path {
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

        tempPath := path + ".tmp"

        tempFile, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
        if nil != err {
            return exception.NewError(
                "failed to open session storage temp file",
                exceptioncontract.Context{
                    "path": tempPath,
                },
                err,
            )
        }

        encoder := json.NewEncoder(tempFile)
        encoder.SetIndent("", "")

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

        newFileInstance, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
        if nil != err {
            return exception.NewError(
                "failed to open session storage file",
                map[string]any{
                    "path": path,
                },
                err,
            )
        }

        instance.mutex.Lock()
        oldFileInstance := instance.file
        instance.file = newFileInstance
        instance.mutex.Unlock()

        if nil != oldFileInstance {
            closeErr := oldFileInstance.Close()
            if nil != closeErr {
                return exception.NewError("failed to close session storage file", nil, closeErr)
            }
        }

        return instance.loadFromFile()
    }

    _, err := fileInstance.Seek(0, 0)
    if nil != err {
        return exception.NewError("failed to seek session storage file", nil, err)
    }

    err = fileInstance.Truncate(0)
    if nil != err {
        return exception.NewError("failed to truncate session storage file", nil, err)
    }

    encoder := json.NewEncoder(fileInstance)
    encoder.SetIndent("", "")

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
        copied[key] = value
    }

    return copied
}

var _ sessioncontract.Storage = (*FileStorage)(nil)
