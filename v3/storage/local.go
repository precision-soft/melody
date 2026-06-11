package storage

import (
    "io"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

func NewLocalStorage(baseDirectory string) *LocalStorage {
    if "" == baseDirectory {
        exception.Panic(exception.NewError("local storage base directory is empty", nil, nil))
    }

    return &LocalStorage{
        baseDirectory: filepath.Clean(baseDirectory),
    }
}

type LocalStorage struct {
    baseDirectory string
}

func (instance *LocalStorage) Put(
    runtimeInstance runtimecontract.Runtime,
    key string,
    reader io.Reader,
    size int64,
    options storagecontract.PutOptions,
) error {
    relativeKey, keyErr := storageRelativeKey(key)
    if nil != keyErr {
        return keyErr
    }

    /** @important the base directory is created lazily on first write; os.OpenRoot then pins it so every key operation is confined to it, with each path component checked against symlink escape */
    if mkdirErr := os.MkdirAll(instance.baseDirectory, 0o750); nil != mkdirErr {
        return exception.NewError("could not create the storage directory", map[string]any{"key": key}, mkdirErr)
    }

    root, rootErr := os.OpenRoot(instance.baseDirectory)
    if nil != rootErr {
        return exception.NewError("could not open the storage base directory", map[string]any{"key": key}, rootErr)
    }
    defer root.Close()

    if directory := filepath.Dir(relativeKey); "." != directory {
        if mkdirErr := root.MkdirAll(directory, 0o750); nil != mkdirErr {
            return exception.NewError("could not create the storage directory", map[string]any{"key": key}, mkdirErr)
        }
    }

    file, createErr := root.OpenFile(relativeKey, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
    if nil != createErr {
        return exception.NewError("could not create the storage object", map[string]any{"key": key}, createErr)
    }

    written, copyErr := io.Copy(file, reader)
    if nil != copyErr {
        _ = file.Close()
        _ = root.Remove(relativeKey)
        return exception.NewError("could not write the storage object", map[string]any{"key": key}, copyErr)
    }

    if 0 <= size && written != size {
        _ = file.Close()
        _ = root.Remove(relativeKey)
        return exception.NewError("storage object size does not match the declared size", map[string]any{"key": key, "declared": size, "written": written}, nil)
    }

    if closeErr := file.Close(); nil != closeErr {
        _ = root.Remove(relativeKey)
        return exception.NewError("could not flush the storage object", map[string]any{"key": key}, closeErr)
    }

    return nil
}

func (instance *LocalStorage) Get(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (io.ReadCloser, error) {
    relativeKey, keyErr := storageRelativeKey(key)
    if nil != keyErr {
        return nil, keyErr
    }

    root, rootErr := os.OpenRoot(instance.baseDirectory)
    if nil != rootErr {
        return nil, exception.NewError("could not open the storage object", map[string]any{"key": key}, rootErr)
    }
    defer root.Close()

    file, openErr := root.Open(relativeKey)
    if nil != openErr {
        return nil, exception.NewError("could not open the storage object", map[string]any{"key": key}, openErr)
    }

    if info, statErr := file.Stat(); nil == statErr && info.IsDir() {
        _ = file.Close()
        return nil, exception.NewError("storage key resolves to a directory", map[string]any{"key": key}, nil)
    }

    return file, nil
}

func (instance *LocalStorage) Delete(
    runtimeInstance runtimecontract.Runtime,
    key string,
) error {
    relativeKey, keyErr := storageRelativeKey(key)
    if nil != keyErr {
        return keyErr
    }

    root, rootErr := os.OpenRoot(instance.baseDirectory)
    if nil != rootErr {
        if true == os.IsNotExist(rootErr) {
            return nil
        }

        return exception.NewError("could not delete the storage object", map[string]any{"key": key}, rootErr)
    }
    defer root.Close()

    removeErr := root.Remove(relativeKey)
    if nil != removeErr && false == os.IsNotExist(removeErr) {
        return exception.NewError("could not delete the storage object", map[string]any{"key": key}, removeErr)
    }

    return nil
}

func (instance *LocalStorage) Exists(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (bool, error) {
    relativeKey, keyErr := storageRelativeKey(key)
    if nil != keyErr {
        return false, keyErr
    }

    root, rootErr := os.OpenRoot(instance.baseDirectory)
    if nil != rootErr {
        if true == os.IsNotExist(rootErr) {
            return false, nil
        }

        return false, exception.NewError("could not stat the storage object", map[string]any{"key": key}, rootErr)
    }
    defer root.Close()

    /** @important Root.Stat cannot escape the base: a missing key reports absent, while a symlink pointing outside is rejected with an error that never leaks the external target (consistent with Get and Delete) */
    _, statErr := root.Stat(relativeKey)
    if nil == statErr {
        return true, nil
    }

    if true == os.IsNotExist(statErr) {
        return false, nil
    }

    return false, exception.NewError("could not stat the storage object", map[string]any{"key": key}, statErr)
}

func (instance *LocalStorage) PresignedUrl(
    runtimeInstance runtimecontract.Runtime,
    key string,
    expiry time.Duration,
) (string, error) {
    return "", exception.NewError("presigned urls are not supported by local storage", nil, nil)
}

func storageRelativeKey(key string) (string, error) {
    normalized := strings.ReplaceAll(key, "\\", "/")
    cleaned := strings.TrimPrefix(filepath.Clean("/"+normalized), "/")

    if "" == cleaned || "." == cleaned {
        return "", exception.NewError("storage key is empty or invalid", map[string]any{"key": key}, nil)
    }

    return cleaned, nil
}

var _ storagecontract.Storage = (*LocalStorage)(nil)
