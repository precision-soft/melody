package storage

import (
    "context"
    "errors"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestLocalStorage_PutGetExistsDelete(t *testing.T) {
    local := NewLocalStorage(t.TempDir())
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
    local := NewLocalStorage(base)
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
    local := NewLocalStorage(t.TempDir())
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
    local := NewLocalStorage(base)
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

    local := NewLocalStorage(base)
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

    local := NewLocalStorage(base)
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
    local := NewLocalStorage(t.TempDir())
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
    local := NewLocalStorage(base)
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
    local := NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    key := "labels/partial.bin"
    reader := &readerFailingAfterData{data: []byte("partial-body-before-failure")}

    putErr := local.Put(runtimeInstance, key, reader, -1, storagecontract.PutOptions{})
    if nil == putErr {
        t.Fatalf("expected put to fail when the source reader errors mid-stream")
    }

    /* "copy", not "write": io.Copy answers one error for both sides, and on an upload streamed from a request body the likelier one is the READER dying with the client — a message blaming the storage write sends the operator at the disk when the source connection failed */
    if false == strings.Contains(putErr.Error(), "could not copy the payload") {
        t.Fatalf("expected the failure to name the copy, got %v", putErr)
    }

    exists, existsErr := local.Exists(runtimeInstance, key)
    if nil != existsErr {
        t.Fatalf("exists: %v", existsErr)
    }
    if true == exists {
        t.Fatalf("expected no object on disk after a failed put, but a partial object remains")
    }
}

/* a failed overwrite must not destroy the previously stored object (atomic put) */

func TestLocalStorage_FailedOverwritePreservesPriorObject(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base)
    runtimeInstance := testRuntime()

    key := "labels/awb-999.txt"
    original := "ORIGINAL-GOOD-CONTENT"

    if putErr := local.Put(runtimeInstance, key, strings.NewReader(original), int64(len(original)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("initial put: %v", putErr)
    }

    failing := &readerFailingAfterData{data: []byte("overwrite-that-fails-midway")}
    if overwriteErr := local.Put(runtimeInstance, key, failing, -1, storagecontract.PutOptions{}); nil == overwriteErr {
        t.Fatalf("expected the overwrite to fail when the source reader errors mid-stream")
    }

    reader, getErr := local.Get(runtimeInstance, key)
    if nil != getErr {
        t.Fatalf("the prior object must survive a failed overwrite, but Get failed: %v", getErr)
    }
    loaded, _ := io.ReadAll(reader)
    reader.Close()

    if original != string(loaded) {
        t.Fatalf("a failed overwrite destroyed or truncated the prior object: got %q, want %q", string(loaded), original)
    }

    entries, _ := os.ReadDir(filepath.Join(base, "labels"))
    for _, entry := range entries {
        if true == strings.Contains(entry.Name(), ".tmp-") {
            t.Fatalf("a failed overwrite left a temporary object behind: %s", entry.Name())
        }
    }
}

func TestLocalStorage_Get_ErrorOnDirectoryKey(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base)
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "subdir/file.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    rc, getErr := local.Get(runtimeInstance, "subdir")
    if nil == getErr {
        if nil != rc {
            rc.Close()
        }
        t.Fatalf("Get on a directory key must return an error, but returned nil error")
    }
}

func TestLocalStorage_ExistsReturnsFalseForDirectoryKey(t *testing.T) {
    local := NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    content := "nested object"
    if putErr := local.Put(runtimeInstance, "nested/object.txt", strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    exists, existsErr := local.Exists(runtimeInstance, "nested")
    if nil != existsErr {
        t.Fatalf("exists: %v", existsErr)
    }

    if true == exists {
        t.Fatalf("Exists reported a directory key as an existing object, contradicting Get/Put and the S3 backend")
    }
}

func TestLocalStorage_ExistsDoesNotFollowSymlinkToExternalTarget(t *testing.T) {
    base := t.TempDir()
    outside := t.TempDir()

    secret := filepath.Join(outside, "secret.txt")
    if writeErr := os.WriteFile(secret, []byte("top secret"), 0o600); nil != writeErr {
        t.Fatalf("seed external file: %v", writeErr)
    }

    if linkErr := os.Symlink(secret, filepath.Join(base, "leak")); nil != linkErr {
        t.Fatalf("symlink: %v", linkErr)
    }

    local := NewLocalStorage(base)

    exists, _ := local.Exists(testRuntime(), "leak")
    if true == exists {
        t.Fatalf("Exists followed a symlink and reported an external target as existing")
    }
}

func TestLocalStorage_RejectsIntermediateDirectorySymlinkEscapeOnPut(t *testing.T) {
    base := t.TempDir()
    outside := t.TempDir()

    if linkErr := os.Symlink(outside, filepath.Join(base, "evil")); nil != linkErr {
        t.Fatalf("create intermediate symlink: %v", linkErr)
    }

    local := NewLocalStorage(base)
    content := "should not escape"

    putErr := local.Put(testRuntime(), "evil/object.txt", strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{})
    if nil == putErr {
        t.Fatalf("expected an escape through an intermediate-directory symlink to be rejected")
    }

    if _, statErr := os.Stat(filepath.Join(outside, "object.txt")); false == os.IsNotExist(statErr) {
        t.Fatalf("an object escaped the base directory through an intermediate symlink, stat err: %v", statErr)
    }
}

func TestLocalStorage_RejectsAbsoluteKeyEscape(t *testing.T) {
    local := NewLocalStorage(t.TempDir())

    if _, getErr := local.Get(testRuntime(), "/etc/passwd"); nil == getErr {
        t.Fatalf("expected an absolute-path key to be confined and rejected")
    }
}

func TestLocalStorage_NegativeSizeMeansUnknownAndSkipsTheLengthCheck(t *testing.T) {
    storage := NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    /* the contract's documented convention: a negative size is "length unknown" (the io convention http.Request.ContentLength uses), so the backend stores whatever the reader yields — while a non-negative declaration is verified strictly */
    if putErr := storage.Put(runtimeInstance, "unknown-size.txt", strings.NewReader("payload"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("expected the unknown-size put to succeed, got %v", putErr)
    }

    if putErr := storage.Put(runtimeInstance, "declared-size.txt", strings.NewReader("payload"), 3, storagecontract.PutOptions{}); nil == putErr {
        t.Fatalf("expected the mismatched declared size to be refused")
    }
}

func TestLocalStorage_PutSweepsStaleTempObjectsAndKeepsLiveOnes(t *testing.T) {
    baseDirectory := t.TempDir()
    storage := NewLocalStorage(baseDirectory)
    runtimeInstance := testRuntime()

    if putErr := storage.Put(runtimeInstance, "report.txt", strings.NewReader("first"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }

    /* a crash mid-write leaves exactly this shape behind: a temp object no error path could clean; nothing sweeps it but a later Put of the same key */
    staleTemp := filepath.Join(baseDirectory, "report.txt.tmp-00112233445566aa")
    if writeErr := os.WriteFile(staleTemp, []byte("orphan"), 0o640); nil != writeErr {
        t.Fatalf("could not plant the stale temp: %v", writeErr)
    }
    staleInstant := time.Now().Add(-2 * time.Hour)
    if touchErr := os.Chtimes(staleTemp, staleInstant, staleInstant); nil != touchErr {
        t.Fatalf("could not age the stale temp: %v", touchErr)
    }

    /* a FRESH temp is a concurrent writer still at work — its mtime is recent, so the sweep must leave it alone */
    freshTemp := filepath.Join(baseDirectory, "report.txt.tmp-ffeeddccbbaa9988")
    if writeErr := os.WriteFile(freshTemp, []byte("in flight"), 0o640); nil != writeErr {
        t.Fatalf("could not plant the fresh temp: %v", writeErr)
    }

    /* an operator's own file that merely resembles a temp name — wrong suffix shape — is never the sweep's to touch */
    unowned := filepath.Join(baseDirectory, "report.txt.tmp-notahexsuffix!!")
    if writeErr := os.WriteFile(unowned, []byte("operator data"), 0o640); nil != writeErr {
        t.Fatalf("could not plant the unowned file: %v", writeErr)
    }
    if touchErr := os.Chtimes(unowned, staleInstant, staleInstant); nil != touchErr {
        t.Fatalf("could not age the unowned file: %v", touchErr)
    }

    if putErr := storage.Put(runtimeInstance, "report.txt", strings.NewReader("second"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }

    if _, statErr := os.Stat(staleTemp); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the stale temp to be swept, stat answered %v", statErr)
    }

    if _, statErr := os.Stat(freshTemp); nil != statErr {
        t.Fatalf("expected the fresh temp to survive the sweep: %v", statErr)
    }

    if _, statErr := os.Stat(unowned); nil != statErr {
        t.Fatalf("expected the operator's own file to survive the sweep: %v", statErr)
    }
}
