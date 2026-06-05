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
        baseDirectory: baseDirectory,
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
    path, pathErr := instance.resolvePath(key)
    if nil != pathErr {
        return pathErr
    }

    if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o750); nil != mkdirErr {
        return exception.NewError("could not create the storage directory", map[string]any{"key": key}, mkdirErr)
    }

    file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
    if nil != createErr {
        return exception.NewError("could not create the storage object", map[string]any{"key": key}, createErr)
    }
    defer file.Close()

    if _, copyErr := io.Copy(file, reader); nil != copyErr {
        return exception.NewError("could not write the storage object", map[string]any{"key": key}, copyErr)
    }

    return nil
}

func (instance *LocalStorage) Get(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (io.ReadCloser, error) {
    path, pathErr := instance.resolvePath(key)
    if nil != pathErr {
        return nil, pathErr
    }

    file, openErr := os.Open(path)
    if nil != openErr {
        return nil, exception.NewError("could not open the storage object", map[string]any{"key": key}, openErr)
    }

    return file, nil
}

func (instance *LocalStorage) Delete(
    runtimeInstance runtimecontract.Runtime,
    key string,
) error {
    path, pathErr := instance.resolvePath(key)
    if nil != pathErr {
        return pathErr
    }

    removeErr := os.Remove(path)
    if nil != removeErr && false == os.IsNotExist(removeErr) {
        return exception.NewError("could not delete the storage object", map[string]any{"key": key}, removeErr)
    }

    return nil
}

func (instance *LocalStorage) Exists(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (bool, error) {
    path, pathErr := instance.resolvePath(key)
    if nil != pathErr {
        return false, pathErr
    }

    _, statErr := os.Stat(path)
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

func (instance *LocalStorage) resolvePath(key string) (string, error) {
    cleaned := filepath.Clean("/" + strings.ReplaceAll(key, "\\", "/"))
    target := filepath.Join(instance.baseDirectory, cleaned)

    base := filepath.Clean(instance.baseDirectory)
    if target == base {
        return "", exception.NewError("storage key is empty or invalid", map[string]any{"key": key}, nil)
    }

    if false == strings.HasPrefix(target, base+string(os.PathSeparator)) {
        return "", exception.NewError("storage key escapes the base directory", map[string]any{"key": key}, nil)
    }

    if escapeErr := instance.ensureNoSymlinkEscape(base, target); nil != escapeErr {
        return "", exception.NewError("storage key escapes the base directory via a symlink", map[string]any{"key": key}, escapeErr)
    }

    return target, nil
}

/** ensureNoSymlinkEscape complements the textual containment check: the cleaned key cannot contain
"..", but a symlink planted inside the base directory could still resolve outside it. The deepest
existing ancestor of the target is resolved through symlinks and verified to stay within the
resolved base, which covers both reads (target exists) and writes (only the parent exists yet). */
func (instance *LocalStorage) ensureNoSymlinkEscape(base string, target string) error {
    realBase, baseErr := filepath.EvalSymlinks(base)
    if nil != baseErr {
        realBase = base
    }

    existing := target
    for {
        resolved, resolveErr := filepath.EvalSymlinks(existing)
        if nil == resolveErr {
            if resolved != realBase && false == strings.HasPrefix(resolved, realBase+string(os.PathSeparator)) {
                return exception.NewError("resolved path is outside the base directory", nil, nil)
            }

            return nil
        }

        parent := filepath.Dir(existing)
        if parent == existing {
            return nil
        }

        existing = parent
    }
}

var _ storagecontract.Storage = (*LocalStorage)(nil)
