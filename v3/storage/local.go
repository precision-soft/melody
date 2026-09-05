package storage

import (
    "crypto/rand"
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

    /* the base directory is created lazily on first write; os.OpenRoot then pins it so every key operation is confined to it, with each path component checked against symlink escape */
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

    /* reject a key whose leaf is an existing symlink rather than replacing it through the rename below; os.Root never traverses the link so nothing escapes, but refusing keeps the backend's no-symlink contract explicit, matching the prior O_CREATE-on-Root behavior */
    if info, lstatErr := root.Lstat(relativeKey); nil == lstatErr && 0 != info.Mode()&os.ModeSymlink {
        return exception.NewError("storage key resolves to a symlink", map[string]any{"key": key}, nil)
    }

    /* write to a temporary object first and rename it over the key only once it is fully flushed, so a failed or partial write never destroys or truncates a previously stored object; the rename is atomic within the pinned root, matching the awss3 backend's all-or-nothing Put */
    tempKey, file, createErr := createStorageTempFile(root, relativeKey)
    if nil != createErr {
        return exception.NewError("could not create the storage object", map[string]any{"key": key}, createErr)
    }

    written, copyErr := io.Copy(file, reader)
    if nil != copyErr {
        _ = file.Close()
        _ = root.Remove(tempKey)
        /* "copy", not "write": io.Copy answers one error for both sides, and the likelier one on an upload streamed from a request body is the READER dying with the client — a message that blames the storage write sends the operator at the disk when the source connection failed; the cause names the side */
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

/* storageTempStaleAge is how old a leftover temp object must be before a later Put sweeps it. The in-process error paths already clean their own temp; what they cannot clean is a crash mid-write (kill -9, oom), whose temp would otherwise sit in the data directory forever — no sweeper, no List to even notice it through, unbounded growth under a crash-looping uploader. An hour is safely beyond any live writer: an in-flight Put refreshes its temp's mtime with every write, so only a file nothing has touched for the whole hour is provably abandoned. */
const storageTempStaleAge = 1 * time.Hour

/* sweepStaleTempObjects removes abandoned temp objects for this key, opportunistically, after a successful Put. Best-effort by design: the Put already succeeded, the sweep changes nothing the caller observes, and a failure here will be retried by the next Put of the same key — while a hard error would fail a store that worked. */
func sweepStaleTempObjects(root *os.Root, relativeKey string) {
    directory := filepath.Dir(relativeKey)
    prefix := filepath.Base(relativeKey) + ".tmp-"

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
        if false == strings.HasPrefix(name, prefix) || false == isStorageTempSuffix(name[len(prefix):]) {
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

/* isStorageTempSuffix matches exactly the sixteen lowercase hex characters createStorageTempFile appends, so a user object that merely resembles a temp name is never swept. */
func isStorageTempSuffix(suffix string) bool {
    if 16 != len(suffix) {
        return false
    }

    for _, character := range suffix {
        if ('0' > character || '9' < character) && ('a' > character || 'f' < character) {
            return false
        }
    }

    return true
}

/* allocate a uniquely named temporary object in the same directory as the target so the final rename stays within the pinned root and on the same filesystem; O_EXCL guarantees we never clobber a concurrent writer's temp or the live key */
func createStorageTempFile(root *os.Root, relativeKey string) (string, *os.File, error) {
    directory := filepath.Dir(relativeKey)
    base := filepath.Base(relativeKey)

    for attempt := 0; attempt < 10; attempt++ {
        suffix := make([]byte, 8)
        if _, randErr := rand.Read(suffix); nil != randErr {
            return "", nil, randErr
        }

        tempKey := filepath.Join(directory, base+".tmp-"+hex.EncodeToString(suffix))

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

    /* Root.Stat cannot escape the base: a missing key reports absent, while a symlink pointing outside is rejected with an error that never leaks the external target (consistent with Get and Delete) */
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

    return cleaned, nil
}

var _ storagecontract.Storage = (*LocalStorage)(nil)
