package storage_test

import (
    "context"
    "errors"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/precision-soft/melody/v3/storage"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestLocalStorage_PutGetExistsDelete(t *testing.T) {
    local := storage.NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    key := "labels/awb-123.txt"
    content := "shipping label body"

    putErr := local.Put(runtimeInstance, key, strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{ContentType: "text/plain"})
    if nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    exists, existsErr := local.Exists(runtimeInstance, key)
    if nil != existsErr || false == exists {
        t.Fatalf("expected object to exist: %v %v", exists, existsErr)
    }

    reader, getErr := local.Get(runtimeInstance, key)
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    loaded, _ := io.ReadAll(reader)
    reader.Close()

    if content != string(loaded) {
        t.Fatalf("content mismatch: %q", string(loaded))
    }

    if deleteErr := local.Delete(runtimeInstance, key); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    existsAfter, _ := local.Exists(runtimeInstance, key)
    if true == existsAfter {
        t.Fatalf("expected object to be gone after delete")
    }
}

func TestLocalStorage_PutCreatesBaseDirectoryOnFirstWrite(t *testing.T) {
    base := filepath.Join(t.TempDir(), "missing", "nested-base")
    local := storage.NewLocalStorage(base)
    runtimeInstance := testRuntime()

    key := "labels/awb-1.txt"
    content := "created lazily"

    if putErr := local.Put(runtimeInstance, key, strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put into a not-yet-existing base directory should succeed: %v", putErr)
    }

    reader, getErr := local.Get(runtimeInstance, key)
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    loaded, _ := io.ReadAll(reader)
    reader.Close()

    if content != string(loaded) {
        t.Fatalf("content mismatch: %q", string(loaded))
    }
}

func TestLocalStorage_RejectsPathTraversal(t *testing.T) {
    local := storage.NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    _, getErr := local.Get(runtimeInstance, "../../etc/passwd")
    if nil == getErr {
        t.Fatalf("expected path traversal to be rejected")
    }
}

func TestLocalStorage_AllowsBaseUnderSymlinkedAncestorCreatedLazily(t *testing.T) {
    realRoot := t.TempDir()
    linkRoot := filepath.Join(t.TempDir(), "link")
    if linkErr := os.Symlink(realRoot, linkRoot); nil != linkErr {
        t.Fatalf("create symlink: %v", linkErr)
    }

    base := filepath.Join(linkRoot, "store")
    local := storage.NewLocalStorage(base)
    runtimeInstance := testRuntime()

    key := "labels/awb-1.txt"
    content := "inside the base via a symlink"

    if putErr := local.Put(runtimeInstance, key, strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put under a symlinked base should succeed: %v", putErr)
    }

    reader, getErr := local.Get(runtimeInstance, key)
    if nil != getErr {
        t.Fatalf("get under a symlinked base should not be rejected as a symlink escape: %v", getErr)
    }
    loaded, _ := io.ReadAll(reader)
    reader.Close()

    if content != string(loaded) {
        t.Fatalf("content mismatch: %q", string(loaded))
    }
}

func TestLocalStorage_RejectsSymlinkEscape(t *testing.T) {
    base := t.TempDir()
    outside := t.TempDir()

    secret := filepath.Join(outside, "secret.txt")
    if writeErr := os.WriteFile(secret, []byte("top secret"), 0o600); nil != writeErr {
        t.Fatalf("seed secret: %v", writeErr)
    }

    if linkErr := os.Symlink(secret, filepath.Join(base, "escape")); nil != linkErr {
        t.Fatalf("create symlink: %v", linkErr)
    }

    local := storage.NewLocalStorage(base)
    runtimeInstance := testRuntime()

    if _, getErr := local.Get(runtimeInstance, "escape"); nil == getErr {
        t.Fatalf("expected symlink escape to be rejected")
    }
}

func TestLocalStorage_RejectsDanglingSymlinkLeafOnPut(t *testing.T) {
    base := t.TempDir()
    outside := t.TempDir()

    target := filepath.Join(outside, "planted.txt")
    if linkErr := os.Symlink(target, filepath.Join(base, "dangling")); nil != linkErr {
        t.Fatalf("create dangling symlink: %v", linkErr)
    }

    local := storage.NewLocalStorage(base)
    runtimeInstance := testRuntime()

    content := "should not escape"
    if putErr := local.Put(runtimeInstance, "dangling", strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil == putErr {
        t.Fatalf("expected dangling symlink leaf to be rejected on put")
    }

    if _, statErr := os.Stat(target); false == os.IsNotExist(statErr) {
        t.Fatalf("expected no file planted outside the base directory, stat err: %v", statErr)
    }
}

func TestLocalStorage_RejectsSizeMismatch(t *testing.T) {
    local := storage.NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    content := "four"
    if putErr := local.Put(runtimeInstance, "obj.bin", strings.NewReader(content), 10, storagecontract.PutOptions{}); nil == putErr {
        t.Fatalf("expected a size-mismatch error when the reader length does not match the declared size")
    }

    if exists, _ := local.Exists(runtimeInstance, "obj.bin"); true == exists {
        t.Fatalf("expected the mismatched object to be removed")
    }
}

func TestLocalStorage_WritesObjectsWithRestrictivePermissions(t *testing.T) {
    base := t.TempDir()
    local := storage.NewLocalStorage(base)
    runtimeInstance := testRuntime()

    key := "private/data.bin"
    if putErr := local.Put(runtimeInstance, key, strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    info, statErr := os.Stat(filepath.Join(base, key))
    if nil != statErr {
        t.Fatalf("stat: %v", statErr)
    }

    if 0o640 != info.Mode().Perm() {
        t.Fatalf("expected file mode 0640, got %o", info.Mode().Perm())
    }
}

type readerFailingAfterData struct {
    data      []byte
    readSoFar int
}

func (instance *readerFailingAfterData) Read(buffer []byte) (int, error) {
    if instance.readSoFar >= len(instance.data) {
        return 0, errors.New("source read failure")
    }

    written := copy(buffer, instance.data[instance.readSoFar:])
    instance.readSoFar += written

    return written, nil
}

func TestLocalStorage_PutRemovesPartialObjectOnReaderError(t *testing.T) {
    local := storage.NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    key := "labels/partial.bin"
    reader := &readerFailingAfterData{data: []byte("partial-body-before-failure")}

    putErr := local.Put(runtimeInstance, key, reader, -1, storagecontract.PutOptions{})
    if nil == putErr {
        t.Fatalf("expected put to fail when the source reader errors mid-stream")
    }

    exists, existsErr := local.Exists(runtimeInstance, key)
    if nil != existsErr {
        t.Fatalf("exists: %v", existsErr)
    }
    if true == exists {
        t.Fatalf("expected no object on disk after a failed put, but a partial object remains")
    }
}
