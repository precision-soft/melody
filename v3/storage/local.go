package storage

import (
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
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

    /* the base directory is created on first write; os.OpenRoot then confines every key operation to it, each path component checked against symlink escape */
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

    /* a key whose leaf is an existing symlink is refused rather than replaced by the rename below */
    if info, lstatErr := root.Lstat(relativeKey); nil == lstatErr && 0 != info.Mode()&os.ModeSymlink {
        return exception.NewError("storage key resolves to a symlink", map[string]any{"key": key}, nil)
    }

    /* written to a temporary object and renamed over the key once flushed, so a failed write never destroys the stored object; the rename is atomic within the pinned root */
    tempKey, file, createErr := createStorageTempFile(root, relativeKey)
    if nil != createErr {
        return exception.NewError("could not create the storage object", map[string]any{"key": key}, createErr)
    }

    written, copyErr := io.Copy(file, reader)
    if nil != copyErr {
        _ = file.Close()
        _ = root.Remove(tempKey)
        /* "copy", not "write": io.Copy answers one error for both sides, and the cause names which */
        return exception.NewError("could not copy the payload into the storage object", map[string]any{"key": key}, copyErr)
    }

    if 0 <= size && written != size {
        _ = file.Close()
        _ = root.Remove(tempKey)
        return exception.NewError("storage object size does not match the declared size", map[string]any{"key": key, "declared": size, "written": written}, nil)
    }

    if syncErr := file.Sync(); nil != syncErr {
        _ = file.Close()
        _ = root.Remove(tempKey)
        return exception.NewError("could not flush the storage object", map[string]any{"key": key}, syncErr)
    }

    if closeErr := file.Close(); nil != closeErr {
        _ = root.Remove(tempKey)
        return exception.NewError("could not flush the storage object", map[string]any{"key": key}, closeErr)
    }

    if renameErr := root.Rename(tempKey, relativeKey); nil != renameErr {
        _ = root.Remove(tempKey)
        return exception.NewError("could not store the storage object", map[string]any{"key": key}, renameErr)
    }

    sweepStaleTempObjects(root, relativeKey)

    return nil
}

/* storageTempStaleAge is the age past which a later Put sweeps a leftover temp object a crash mid-write left; an in-flight Put refreshes its temp's mtime with every write. */
const storageTempStaleAge = 1 * time.Hour

/* storageTempObjectSuffix closes the name of every temp object; the random part sits between the reserved prefix and it. */
const storageTempObjectSuffix = ".tmp"

/* storageTempPrefix names the reserved namespace of a key's temp objects: a hidden name carrying the digest of the key's leaf, so no ordinary key shares it by accident, and a leaf of any length leaves room for the random part within one path component. */
func storageTempPrefix(relativeKey string) string {
    digest := sha256.Sum256([]byte(filepath.Base(relativeKey)))

    return ".melody-storage-" + hex.EncodeToString(digest[:]) + "."
}

/* sweepStaleTempObjects removes abandoned temp objects for this key after a successful Put, best-effort: a failure is retried by the next Put. */
func sweepStaleTempObjects(root *os.Root, relativeKey string) {
    directory := filepath.Dir(relativeKey)
    prefix := storageTempPrefix(relativeKey)

    directoryFile, openErr := root.Open(directory)
    if nil != openErr {
        return
    }
    defer directoryFile.Close()

    names, readErr := directoryFile.Readdirnames(-1)
    if nil != readErr {
        return
    }

    for _, name := range names {
        if false == strings.HasPrefix(name, prefix) || false == strings.HasSuffix(name, storageTempObjectSuffix) {
            continue
        }

        if false == isStorageTempRandomPart(strings.TrimSuffix(name[len(prefix):], storageTempObjectSuffix)) {
            continue
        }

        candidate := filepath.Join(directory, name)

        info, statErr := root.Lstat(candidate)
        if nil != statErr || false == info.Mode().IsRegular() {
            continue
        }

        if storageTempStaleAge > time.Since(info.ModTime()) {
            continue
        }

        _ = root.Remove(candidate)
    }
}

/* isStorageTempRandomPart matches exactly the sixteen lowercase hex characters createStorageTempFile generates. */
func isStorageTempRandomPart(randomPart string) bool {
    if 16 != len(randomPart) {
        return false
    }

    for _, character := range randomPart {
        if ('0' > character || '9' < character) && ('a' > character || 'f' < character) {
            return false
        }
    }

    return true
}

/* createStorageTempFile creates a uniquely named temp object beside the target, so the rename stays within the pinned root; O_EXCL never clobbers another writer's temp or the live key. */
func createStorageTempFile(root *os.Root, relativeKey string) (string, *os.File, error) {
    directory := filepath.Dir(relativeKey)
    prefix := storageTempPrefix(relativeKey)

    for attempt := 0; attempt < 10; attempt++ {
        suffix := make([]byte, 8)
        if _, randErr := rand.Read(suffix); nil != randErr {
            return "", nil, randErr
        }

        tempKey := filepath.Join(directory, prefix+hex.EncodeToString(suffix)+storageTempObjectSuffix)

        file, openErr := root.OpenFile(tempKey, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
        if nil == openErr {
            return tempKey, file, nil
        }

        if false == os.IsExist(openErr) {
            return "", nil, openErr
        }
    }

    return "", nil, exception.NewError("could not allocate a unique storage temp object", nil, nil)
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

    /* a symlink pointing outside is refused with an error that does not leak its target, as in Get and Delete */
    info, statErr := root.Stat(relativeKey)
    if nil == statErr {
        if true == info.IsDir() {
            return false, nil
        }

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

    /* a key spelled exactly like a temp object would be swept by a later Put of the key whose digest it carries, so the reserved namespace is refused at every door */
    if true == isStorageTempObjectName(filepath.Base(cleaned)) {
        return "", exception.NewError("storage key names a temp object of the reserved .melody-storage- namespace", map[string]any{"key": key}, nil)
    }

    return cleaned, nil
}

/* isStorageTempObjectName matches exactly the names createStorageTempFile gives: the reserved prefix, a sha256 in lowercase hex, a dot, the random part and the suffix. */
func isStorageTempObjectName(name string) bool {
    const reservedPrefix = ".melody-storage-"

    if false == strings.HasPrefix(name, reservedPrefix) || false == strings.HasSuffix(name, storageTempObjectSuffix) {
        return false
    }

    body := strings.TrimSuffix(strings.TrimPrefix(name, reservedPrefix), storageTempObjectSuffix)
    if 64+1+16 != len(body) || '.' != body[64] {
        return false
    }

    for _, character := range body[:64] {
        if ('0' > character || '9' < character) && ('a' > character || 'f' < character) {
            return false
        }
    }

    return isStorageTempRandomPart(body[65:])
}

var _ storagecontract.Storage = (*LocalStorage)(nil)
