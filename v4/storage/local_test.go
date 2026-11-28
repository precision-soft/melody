package storage

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    goruntime "runtime"
    "strings"
    "testing"

    "time"

    "github.com/precision-soft/melody/v4/exception"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/http/static"
    "github.com/precision-soft/melody/v4/internal/testhelper"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

func testRuntime() containercontract.Runtime {
    serviceContainer := container.NewContainer(container.ContainerConfig{})
    return runtime.NewRun(context.Background(), serviceContainer.NewScope())
}

func TestLocalStorage_PutGetExistsDelete(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    key := "labels/awb-123.txt"
    content := "shipping label body"

    putErr := local.Put(runtimeInstance, key, strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{ContentType: "text/plain"})
    if nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    exists, existsErr := Exists(For(runtimeInstance, local), key)
    if nil != existsErr || false == exists {
        t.Fatalf("expected object to exist: %v %v", exists, existsErr)
    }

    object, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    loaded, _ := io.ReadAll(object.Body)
    object.Body.Close()

    if content != string(loaded) {
        t.Fatalf("content mismatch: %q", string(loaded))
    }

    if deleteErr := local.Delete(runtimeInstance, key, storagecontract.DeleteOptions{}); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    existsAfter, _ := Exists(For(runtimeInstance, local), key)
    if true == existsAfter {
        t.Fatalf("expected object to be gone after delete")
    }
}

func TestNewLocalStorage_CreatesTheBaseDirectoryAtConstruction(t *testing.T) {
    base := filepath.Join(t.TempDir(), "missing", "nested-base")
    NewLocalStorage(base, LocalStorageConfig{})

    info, statErr := os.Stat(base)
    if nil != statErr || false == info.IsDir() {
        t.Fatalf("expected the base directory created at construction, stat answered %v", statErr)
    }
}

func TestNewLocalStorage_RefusesABaseDirectoryThatIsAFile(t *testing.T) {
    base := filepath.Join(t.TempDir(), "occupied")
    if writeErr := os.WriteFile(base, []byte("not a directory"), 0o600); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    testhelper.AssertPanicsWithError(t, func() {
        NewLocalStorage(base, LocalStorageConfig{})
    }, "could not create the storage base directory")

    context := recoveredPanicContext(func() {
        NewLocalStorage(base, LocalStorageConfig{})
    })
    if base != context["baseDirectory"] {
        t.Fatalf("expected the refusal to name the base directory %s, got %v", base, context)
    }
}

func TestNewLocalStorage_RefusesABaseDirectoryItCannotOpen(t *testing.T) {
    if 0 == os.Geteuid() {
        t.Skip("root opens a directory whatever its mode")
    }

    base := filepath.Join(t.TempDir(), "closed")
    if mkdirErr := os.Mkdir(base, 0o000); nil != mkdirErr {
        t.Fatalf("mkdir: %v", mkdirErr)
    }
    t.Cleanup(func() { _ = os.Chmod(base, 0o700) })

    testhelper.AssertPanicsWithError(t, func() {
        NewLocalStorage(base, LocalStorageConfig{})
    }, "could not open the storage base directory")

    context := recoveredPanicContext(func() {
        NewLocalStorage(base, LocalStorageConfig{})
    })
    if base != context["baseDirectory"] {
        t.Fatalf("expected the refusal to name the base directory %s, got %v", base, context)
    }
}

func TestLocalStorage_PutWritesIntoABaseDirectoryReplacedAfterTheConstruction(t *testing.T) {
    base := filepath.Join(t.TempDir(), "base")
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if removeErr := os.RemoveAll(base); nil != removeErr {
        t.Fatalf("remove: %v", removeErr)
    }

    key := "labels/awb-1.txt"
    content := "written after the replacement"

    if putErr := local.Put(runtimeInstance, key, strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put into a removed base directory should create it again: %v", putErr)
    }

    loaded, readErr := os.ReadFile(filepath.Join(base, key))
    if nil != readErr || content != string(loaded) {
        t.Fatalf("expected the object under the new base directory, got %q, %v", string(loaded), readErr)
    }
}

func TestLocalStorage_RejectsPathTraversal(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "etc/passwd", strings.NewReader("clamped"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }

    object, getErr := local.Get(runtimeInstance, "../../etc/passwd", storagecontract.GetOptions{})
    if nil == getErr {
        loaded, _ := io.ReadAll(object.Body)
        _ = object.Body.Close()

        t.Fatalf("expected path traversal to be refused, read %q", string(loaded))
    }

    if false == strings.Contains(getErr.Error(), `".." segment`) {
        t.Fatalf("expected the traversal refused by name, got %q", getErr.Error())
    }
}

func TestLocalStorage_RefusesAParentSegmentAtEveryDoor(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "tenant-a/secret.txt", strings.NewReader("A"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }

    for _, key := range []string{"tenant-b/../tenant-a/secret.txt", "tenant-b\\..\\tenant-a\\secret.txt", "../tenant-a/secret.txt", "tenant-a/x/.."} {
        if _, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{}); nil == getErr || false == strings.Contains(getErr.Error(), `".." segment`) {
            t.Fatalf("expected Get of %q to be refused by name, got %v", key, getErr)
        }

        if putErr := local.Put(runtimeInstance, key, strings.NewReader("B"), -1, storagecontract.PutOptions{}); nil == putErr || false == strings.Contains(putErr.Error(), `".." segment`) {
            t.Fatalf("expected Put of %q to be refused by name, got %v", key, putErr)
        }

        if deleteErr := local.Delete(runtimeInstance, key, storagecontract.DeleteOptions{}); nil == deleteErr || false == strings.Contains(deleteErr.Error(), `".." segment`) {
            t.Fatalf("expected Delete of %q to be refused by name, got %v", key, deleteErr)
        }

        exists, existsErr := Exists(For(runtimeInstance, local), key)
        if nil == existsErr || false == strings.Contains(existsErr.Error(), `".." segment`) || true == exists {
            t.Fatalf("expected Exists of %q to be refused by name, got %v, %v", key, exists, existsErr)
        }

        if objectInfo, statErr := local.Stat(runtimeInstance, key, storagecontract.StatOptions{}); nil == statErr || false == strings.Contains(statErr.Error(), `".." segment`) || (storagecontract.ObjectInfo{}) != objectInfo {
            t.Fatalf("expected Stat of %q to be refused by name, got %v, %v", key, objectInfo, statErr)
        }
    }

    object, getErr := local.Get(runtimeInstance, "tenant-a/secret.txt", storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("expected the other prefix's object to survive, got %v", getErr)
    }
    loaded, _ := io.ReadAll(object.Body)
    _ = object.Body.Close()

    if "A" != string(loaded) {
        t.Fatalf("expected the other prefix's object unchanged, got %q", string(loaded))
    }

    for _, key := range []string{"a..b/f.txt", "reports/..hidden", "f.txt..", "tenant-a/./x.txt"} {
        if putErr := local.Put(runtimeInstance, key, strings.NewReader("ok"), -1, storagecontract.PutOptions{}); nil != putErr {
            t.Fatalf("expected the key %q, which carries no \"..\" segment, to be stored, got %v", key, putErr)
        }
    }
}

func TestLocalStorage_AllowsBaseUnderSymlinkedAncestor(t *testing.T) {
    realRoot := t.TempDir()
    linkRoot := filepath.Join(t.TempDir(), "link")
    if linkErr := os.Symlink(realRoot, linkRoot); nil != linkErr {
        t.Fatalf("create symlink: %v", linkErr)
    }

    base := filepath.Join(linkRoot, "store")
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    key := "labels/awb-1.txt"
    content := "inside the base via a symlink"

    if putErr := local.Put(runtimeInstance, key, strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put under a symlinked base should succeed: %v", putErr)
    }

    object, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("get under a symlinked base should not be rejected as a symlink escape: %v", getErr)
    }
    loaded, _ := io.ReadAll(object.Body)
    object.Body.Close()

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

    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if _, getErr := local.Get(runtimeInstance, "escape", storagecontract.GetOptions{}); nil == getErr {
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

    local := NewLocalStorage(base, LocalStorageConfig{})
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
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    content := "four"
    if putErr := local.Put(runtimeInstance, "obj.bin", strings.NewReader(content), 10, storagecontract.PutOptions{}); nil == putErr {
        t.Fatalf("expected a size-mismatch error when the reader length does not match the declared size")
    }

    if exists, _ := Exists(For(runtimeInstance, local), "obj.bin"); true == exists {
        t.Fatalf("expected the mismatched object to be removed")
    }
}

func TestLocalStorage_WritesObjectsWithRestrictivePermissions(t *testing.T) {
    withZeroUmask(t)

    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
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
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    key := "labels/partial.bin"
    reader := &readerFailingAfterData{data: []byte("partial-body-before-failure")}

    putErr := local.Put(runtimeInstance, key, reader, -1, storagecontract.PutOptions{})
    if nil == putErr {
        t.Fatalf("expected put to fail when the source reader errors mid-stream")
    }

    if false == strings.Contains(putErr.Error(), "could not copy the payload") {
        t.Fatalf("expected the failure to name the copy, got %v", putErr)
    }

    exists, existsErr := Exists(For(runtimeInstance, local), key)
    if nil != existsErr {
        t.Fatalf("exists: %v", existsErr)
    }
    if true == exists {
        t.Fatalf("expected no object on disk after a failed put, but a partial object remains")
    }
}

func TestLocalStorage_FailedOverwritePreservesPriorObject(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
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

    object, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("the prior object must survive a failed overwrite, but Get failed: %v", getErr)
    }
    loaded, _ := io.ReadAll(object.Body)
    object.Body.Close()

    if original != string(loaded) {
        t.Fatalf("a failed overwrite destroyed or truncated the prior object: got %q, want %q", string(loaded), original)
    }

    entries, _ := os.ReadDir(filepath.Join(base, "labels"))
    for _, entry := range entries {
        if "awb-999.txt" != entry.Name() {
            t.Fatalf("a failed overwrite left a temporary object behind: %s", entry.Name())
        }
    }
}

func TestLocalStorage_Get_ErrorOnDirectoryKey(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "subdir/file.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    object, getErr := local.Get(runtimeInstance, "subdir", storagecontract.GetOptions{})
    if nil == getErr {
        if nil != object.Body {
            object.Body.Close()
        }
        t.Fatalf("Get on a directory key must return an error, but returned nil error")
    }

    if false == exception.ChainIs(getErr, storagecontract.ErrKeyIsDirectory) {
        t.Fatalf("expected the refusal to carry ErrKeyIsDirectory, the cause Delete refuses with, got %v", getErr)
    }
}

func TestLocalStorage_ExistsReturnsFalseForDirectoryKey(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    content := "nested object"
    if putErr := local.Put(runtimeInstance, "nested/object.txt", strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    exists, existsErr := Exists(For(runtimeInstance, local), "nested")
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

    local := NewLocalStorage(base, LocalStorageConfig{})

    exists, _ := Exists(For(testRuntime(), local), "leak")
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

    local := NewLocalStorage(base, LocalStorageConfig{})
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
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})

    if _, getErr := local.Get(testRuntime(), "/etc/passwd", storagecontract.GetOptions{}); nil == getErr {
        t.Fatalf("expected an absolute-path key to be confined and rejected")
    }
}

func TestLocalStorage_NegativeSizeMeansUnknownAndSkipsTheLengthCheck(t *testing.T) {
    storage := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := storage.Put(runtimeInstance, "unknown-size.txt", strings.NewReader("payload"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("expected the unknown-size put to succeed, got %v", putErr)
    }

    if putErr := storage.Put(runtimeInstance, "declared-size.txt", strings.NewReader("payload"), 3, storagecontract.PutOptions{}); nil == putErr {
        t.Fatalf("expected the mismatched declared size to be refused")
    }
}

/* spelled from the digest, not read from the helper, so a namespace change fails here */
func reservedTemporaryName(leaf string, randomPart string) string {
    digest := sha256.Sum256([]byte(leaf))

    return ".melody-storage-" + hex.EncodeToString(digest[:]) + "." + randomPart + ".tmp"
}

func TestLocalStorage_PutSweepsStaleTemporaryObjectsAndKeepsLiveOnes(t *testing.T) {
    baseDirectory := t.TempDir()
    storage := NewLocalStorage(baseDirectory, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := storage.Put(runtimeInstance, "report.txt", strings.NewReader("first"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }

    staleInstant := time.Now().Add(-2 * time.Hour)
    plant := func(name string, content string, aged bool) string {
        planted := filepath.Join(baseDirectory, name)
        if writeErr := os.WriteFile(planted, []byte(content), 0o640); nil != writeErr {
            t.Fatalf("could not plant %s: %v", name, writeErr)
        }
        if true == aged {
            if touchErr := os.Chtimes(planted, staleInstant, staleInstant); nil != touchErr {
                t.Fatalf("could not age %s: %v", name, touchErr)
            }
        }

        return planted
    }

    staleTemporary := plant(reservedTemporaryName("report.txt", "00112233445566aa"), "orphan", true)
    freshTemporary := plant(reservedTemporaryName("report.txt", "ffeeddccbbaa9988"), "in flight", false)
    otherKeyTemporary := plant(reservedTemporaryName("invoice.txt", "00112233445566aa"), "orphan of another key", true)
    lookalikeKey := plant("report.txt.tmp-00112233445566aa", "application data", true)
    reservedButNotGenerated := plant(reservedTemporaryName("report.txt", "notahexsuffix!!!"), "operator data", true)

    if putErr := storage.Put(runtimeInstance, "invoice.txt", strings.NewReader("invoice"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }

    if _, statErr := os.Stat(otherKeyTemporary); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the stale temp of the other key to be swept by its own Put, stat answered %v", statErr)
    }
    if _, statErr := os.Stat(staleTemporary); nil != statErr {
        t.Fatalf("expected the stale temp of report.txt to survive a Put of another key: %v", statErr)
    }

    if putErr := storage.Put(runtimeInstance, "report.txt", strings.NewReader("second"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }

    if _, statErr := os.Stat(staleTemporary); false == os.IsNotExist(statErr) {
        t.Fatalf("expected the stale temp to be swept, stat answered %v", statErr)
    }

    for _, survivor := range []string{freshTemporary, lookalikeKey, reservedButNotGenerated} {
        if _, statErr := os.Stat(survivor); nil != statErr {
            t.Fatalf("expected %s to survive the sweep: %v", filepath.Base(survivor), statErr)
        }
    }

    exists, existsErr := Exists(For(runtimeInstance, storage), "report.txt.tmp-00112233445566aa")
    if nil != existsErr || false == exists {
        t.Fatalf("expected the lookalike key to stay stored, exists %v error %v", exists, existsErr)
    }
}

func TestNewLocalStorage_RefusesAFieldOutsideItsRuleByName(t *testing.T) {
    cases := map[string]struct {
        config LocalStorageConfig
        field  string
    }{
        "stale age below 1s":    {LocalStorageConfig{TemporaryStaleAge: bound.Of(time.Millisecond)}, "LocalStorageConfig.TemporaryStaleAge"},
        "sweep batch zero":      {LocalStorageConfig{SweepBatchSize: bound.Of(0)}, "LocalStorageConfig.SweepBatchSize"},
        "sweep batch unlimited": {LocalStorageConfig{SweepBatchSize: bound.UnlimitedCount()}, "LocalStorageConfig.SweepBatchSize"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            context := recoveredPanicContext(func() {
                NewLocalStorage(t.TempDir(), testCase.config)
            })
            if testCase.field != context["field"] {
                t.Fatalf("expected the refusal to name %s, got %v", testCase.field, context)
            }
        })
    }
}

func TestLocalStorage_TheConfiguredStaleAgeDecidesWhatIsSwept(t *testing.T) {
    cases := map[string]struct {
        config LocalStorageConfig
        age    time.Duration
        swept  bool
    }{
        "default, two seconds": {LocalStorageConfig{}, 2 * time.Second, false},
        "default, two hours":   {LocalStorageConfig{}, 2 * time.Hour, true},
        "one second":           {LocalStorageConfig{TemporaryStaleAge: bound.Of(time.Second)}, 2 * time.Second, true},
        "unlimited, two hours": {LocalStorageConfig{TemporaryStaleAge: bound.UnlimitedDuration()}, 2 * time.Hour, false},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            baseDirectory := t.TempDir()
            storage := NewLocalStorage(baseDirectory, testCase.config)

            planted := filepath.Join(baseDirectory, reservedTemporaryName("report.txt", "00112233445566aa"))
            if writeErr := os.WriteFile(planted, []byte("orphan"), 0o640); nil != writeErr {
                t.Fatalf("could not plant the temp: %v", writeErr)
            }
            agedInstant := time.Now().Add(-testCase.age)
            if touchErr := os.Chtimes(planted, agedInstant, agedInstant); nil != touchErr {
                t.Fatalf("could not age the temp: %v", touchErr)
            }

            if putErr := storage.Put(testRuntime(), "report.txt", strings.NewReader("report"), -1, storagecontract.PutOptions{}); nil != putErr {
                t.Fatalf("unexpected put error: %v", putErr)
            }

            _, statErr := os.Stat(planted)
            if testCase.swept != os.IsNotExist(statErr) {
                t.Fatalf("expected swept %v, stat answered %v", testCase.swept, statErr)
            }
        })
    }
}

func TestLocalStorage_PutSweepsEveryStaleTemporaryObjectOfADirectoryReadInBatches(t *testing.T) {
    for _, config := range []LocalStorageConfig{{}, {SweepBatchSize: bound.Of(2)}} {
        assertPutSweepsEveryStaleTemporaryObjectReadInBatches(t, config)
    }
}

func assertPutSweepsEveryStaleTemporaryObjectReadInBatches(t *testing.T, config LocalStorageConfig) {
    baseDirectory := t.TempDir()
    storage := NewLocalStorage(baseDirectory, config)
    runtimeInstance := testRuntime()

    for index := 0; index < 5*storageSweepBatchSize; index++ {
        if writeErr := os.WriteFile(filepath.Join(baseDirectory, fmt.Sprintf("object-%05d", index)), []byte("x"), 0o640); nil != writeErr {
            t.Fatalf("could not plant object %d: %v", index, writeErr)
        }
    }

    staleInstant := time.Now().Add(-2 * time.Hour)
    staleTemps := make([]string, 0, 8)
    for index := 0; index < 8; index++ {
        planted := filepath.Join(baseDirectory, reservedTemporaryName("report.txt", fmt.Sprintf("%016x", index)))
        if writeErr := os.WriteFile(planted, []byte("orphan"), 0o640); nil != writeErr {
            t.Fatalf("could not plant temp %d: %v", index, writeErr)
        }
        if touchErr := os.Chtimes(planted, staleInstant, staleInstant); nil != touchErr {
            t.Fatalf("could not age temp %d: %v", index, touchErr)
        }
        staleTemps = append(staleTemps, planted)
    }

    startedAt := time.Now()
    if putErr := storage.Put(runtimeInstance, "report.txt", strings.NewReader("report"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("unexpected put error: %v", putErr)
    }
    t.Logf("put over %d objects took %v", 5*storageSweepBatchSize+8, time.Since(startedAt))

    for _, staleTemporary := range staleTemps {
        if _, statErr := os.Stat(staleTemporary); false == os.IsNotExist(statErr) {
            t.Fatalf("expected %s swept wherever its batch fell, stat answered %v", filepath.Base(staleTemporary), statErr)
        }
    }

    if _, statErr := os.Stat(filepath.Join(baseDirectory, "object-00000")); nil != statErr {
        t.Fatalf("expected the ordinary objects kept: %v", statErr)
    }
}

func TestLocalStorage_RefusesAKeySpelledLikeAReservedTemporaryObject(t *testing.T) {
    storage := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    reservedKey := "sub/" + reservedTemporaryName("report.txt", "00112233445566aa")
    if putErr := storage.Put(runtimeInstance, reservedKey, strings.NewReader("user data"), -1, storagecontract.PutOptions{}); nil == putErr || false == strings.Contains(putErr.Error(), "reserved .melody-storage- namespace") {
        t.Fatalf("expected a key spelled like a reserved temp object to be refused, got %v", putErr)
    }
    if _, existsErr := Exists(For(runtimeInstance, storage), reservedKey); nil == existsErr {
        t.Fatalf("expected Exists to refuse the reserved spelling too")
    }
    if _, statErr := storage.Stat(runtimeInstance, reservedKey, storagecontract.StatOptions{}); nil == statErr || false == strings.Contains(statErr.Error(), "reserved .melody-storage- namespace") {
        t.Fatalf("expected Stat to refuse the reserved spelling too, got %v", statErr)
    }
    if _, getErr := storage.Get(runtimeInstance, reservedKey, storagecontract.GetOptions{}); nil == getErr || false == strings.Contains(getErr.Error(), "reserved .melody-storage- namespace") {
        t.Fatalf("expected Get to refuse the reserved spelling too, got %v", getErr)
    }

    for _, ordinaryKey := range []string{".melody-storage-notes.txt", reservedTemporaryName("report.txt", "notahexsuffix!!!"), "report.txt.tmp-00112233445566aa"} {
        if putErr := storage.Put(runtimeInstance, ordinaryKey, strings.NewReader("user data"), -1, storagecontract.PutOptions{}); nil != putErr {
            t.Fatalf("expected the ordinary key %q to be stored, got %v", ordinaryKey, putErr)
        }
    }
}

func TestLocalStorage_PutStoresAKeyWhoseLeafFillsTheFilesystemComponent(t *testing.T) {
    baseDirectory := t.TempDir()
    storage := NewLocalStorage(baseDirectory, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    key := "exports/" + strings.Repeat("a", 251) + ".csv"

    if putErr := storage.Put(runtimeInstance, key, strings.NewReader("first"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("expected a leaf of 255 bytes to be stored, got %v", putErr)
    }
    if putErr := storage.Put(runtimeInstance, key, strings.NewReader("second"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("expected a leaf of 255 bytes to be replaced, got %v", putErr)
    }

    object, getErr := storage.Get(runtimeInstance, key, storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("get error: %v", getErr)
    }
    loaded, _ := io.ReadAll(object.Body)
    object.Body.Close()
    if "second" != string(loaded) {
        t.Fatalf("expected the replaced content, got %q", string(loaded))
    }

    entries, _ := os.ReadDir(filepath.Join(baseDirectory, "exports"))
    if 1 != len(entries) {
        t.Fatalf("expected only the stored object in the directory, got %d entries", len(entries))
    }
}

func TestLocalStorage_PresignedUrlIsRefusedAsUnsupported(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})

    for _, config := range []storagecontract.PresignOptions{{}, {Method: http.MethodPut}} {
        url, presignErr := local.PresignedUrl(testRuntime(), "reports/report.csv", config)
        if false == exception.ChainIs(presignErr, storagecontract.ErrPresignUnsupported) || false == strings.Contains(presignErr.Error(), "presigned urls are not supported by local storage") {
            t.Fatalf("%v: expected the local storage to refuse a presigned url as unsupported, got %v", config, presignErr)
        }

        if "" != url {
            t.Fatalf("%v: expected no url beside the refusal, got %q", config, url)
        }
    }
}

func TestLocalStorage_ADirectoryInTheWayIsRefusedNamingTheObjectDirectory(t *testing.T) {
    baseDirectory := t.TempDir()
    if writeErr := os.WriteFile(filepath.Join(baseDirectory, "labels"), []byte("in the way"), 0o644); nil != writeErr {
        t.Fatalf("could not put a file in the way: %v", writeErr)
    }

    local := NewLocalStorage(baseDirectory, LocalStorageConfig{})
    putErr := local.Put(testRuntime(), "labels/awb-1.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{})

    melodyErr, isMelodyErr := putErr.(*exception.Error)
    if false == isMelodyErr || "could not create the storage object's directory" != melodyErr.Message() {
        t.Fatalf("expected the object directory refusal, got %v", putErr)
    }

    if "labels" != melodyErr.Context()["directory"] {
        t.Fatalf("expected the refusal to name the directory, got %v", melodyErr.Context())
    }
}

type countingReader struct {
    yielded int64
}

func (instance *countingReader) Read(buffer []byte) (int, error) {
    const streamLength = 1 << 20

    if streamLength <= instance.yielded {
        return 0, io.EOF
    }

    count := min(int64(len(buffer)), streamLength-instance.yielded)
    for index := range buffer[:count] {
        buffer[index] = 'x'
    }

    instance.yielded += count

    return int(count), nil
}

func TestLocalStorage_PutStopsReadingOneBytePastTheDeclaredSize(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    reader := &countingReader{}

    putErr := local.Put(testRuntime(), "bounded.bin", reader, 10, storagecontract.PutOptions{})
    if nil == putErr || false == strings.Contains(putErr.Error(), "does not match the declared size") {
        t.Fatalf("expected the size mismatch refused, got %v", putErr)
    }

    if 64*1024 < reader.yielded {
        t.Fatalf("expected the copy to stop near the declared size, the reader yielded %d bytes", reader.yielded)
    }

    if exists, _ := Exists(For(testRuntime(), local), "bounded.bin"); true == exists {
        t.Fatal("expected no object behind a refused put")
    }
}

func TestLocalStorage_PutRefusesANilReaderBeforeCreatingAnything(t *testing.T) {
    base := filepath.Join(t.TempDir(), "base")
    local := NewLocalStorage(base, LocalStorageConfig{})

    putErr := local.Put(testRuntime(), "nested/object.bin", nil, -1, storagecontract.PutOptions{})
    if nil == putErr || false == strings.Contains(putErr.Error(), "reader is nil") {
        t.Fatalf("expected a nil reader refused by name, got %v", putErr)
    }

    if _, statErr := os.Stat(filepath.Join(base, "nested")); false == os.IsNotExist(statErr) {
        t.Fatalf("expected nothing created for a refused put, stat answered %v", statErr)
    }
}

func TestLocalStorage_DeleteOfAKeyNamingADirectoryIsRefused(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "prefix/object.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    deleteErr := local.Delete(runtimeInstance, "prefix", storagecontract.DeleteOptions{})
    if false == exception.ChainIs(deleteErr, storagecontract.ErrKeyIsDirectory) {
        t.Fatalf("expected the refusal to carry ErrKeyIsDirectory, got %v", deleteErr)
    }

    if "prefix" != exception.FromError(deleteErr).Context()["key"] {
        t.Fatalf("expected the refusal to name the key, got %v", exception.FromError(deleteErr).Context())
    }

    if info, statErr := os.Stat(filepath.Join(base, "prefix")); nil != statErr || false == info.IsDir() {
        t.Fatalf("expected the directory left in place, stat answered %v", statErr)
    }

    if _, statErr := os.Stat(filepath.Join(base, "prefix", "object.txt")); nil != statErr {
        t.Fatalf("expected the object under the directory left in place, stat answered %v", statErr)
    }
    if mkdirErr := os.MkdirAll(filepath.Join(base, "empty"), 0o750); nil != mkdirErr {
        t.Fatalf("mkdir: %v", mkdirErr)
    }

    if emptyErr := local.Delete(runtimeInstance, "empty", storagecontract.DeleteOptions{}); false == exception.ChainIs(emptyErr, storagecontract.ErrKeyIsDirectory) {
        t.Fatalf("expected the empty directory refused with ErrKeyIsDirectory, got %v", emptyErr)
    }

    if info, statErr := os.Stat(filepath.Join(base, "empty")); nil != statErr || false == info.IsDir() {
        t.Fatalf("expected the empty directory left in place, stat answered %v", statErr)
    }
}

func TestLocalStorage_DeleteRemovesASymlinkToADirectoryAsALink(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "target/object.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if linkErr := os.Symlink("target", filepath.Join(base, "link")); nil != linkErr {
        t.Skipf("symlinks not supported: %v", linkErr)
    }

    if deleteErr := local.Delete(runtimeInstance, "link", storagecontract.DeleteOptions{}); nil != deleteErr {
        t.Fatalf("expected the link deleted, got %v", deleteErr)
    }

    if _, lstatErr := os.Lstat(filepath.Join(base, "link")); false == os.IsNotExist(lstatErr) {
        t.Fatalf("expected the link gone, lstat answered %v", lstatErr)
    }

    if _, statErr := os.Stat(filepath.Join(base, "target", "object.txt")); nil != statErr {
        t.Fatalf("expected the link's target kept, stat answered %v", statErr)
    }
}

func TestLocalStorage_AKeyUnderARegularFileIsAbsent(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "object.bin", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    exists, existsErr := Exists(For(runtimeInstance, local), "object.bin/child")
    if nil != existsErr || true == exists {
        t.Fatalf("expected the key absent, got %v %v", exists, existsErr)
    }

    if deleteErr := local.Delete(runtimeInstance, "object.bin/child", storagecontract.DeleteOptions{}); nil != deleteErr {
        t.Fatalf("expected nil, got %v", deleteErr)
    }
}

type removingDirectoryReader struct {
    directory string
    done      bool
}

func (instance *removingDirectoryReader) Read(buffer []byte) (int, error) {
    if true == instance.done {
        return 0, io.EOF
    }

    instance.done = true
    _ = os.RemoveAll(instance.directory)

    buffer[0] = 'x'

    return 1, nil
}

func TestLocalStorage_ATempLostOnAShortWriteIsNotBlamedOnTheSweep(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})

    putErr := local.Put(testRuntime(), "nested/object.bin", &removingDirectoryReader{directory: filepath.Join(base, "nested")}, -1, storagecontract.PutOptions{})
    if nil == putErr {
        t.Fatal("expected the put to fail once its directory was removed")
    }

    if true == strings.Contains(putErr.Error(), "swept as stale") {
        t.Fatalf("expected a short write not to blame the stale sweep, got %v", putErr)
    }
}

func TestLocalStorage_Put_RefusesADirectoryKeyWithItsCause(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "subdir/file.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    putErr := local.Put(runtimeInstance, "subdir", strings.NewReader("y"), 1, storagecontract.PutOptions{})
    if false == exception.ChainIs(putErr, storagecontract.ErrKeyIsDirectory) {
        t.Fatalf("expected the refusal to carry ErrKeyIsDirectory, got %v", putErr)
    }

    object, getErr := local.Get(runtimeInstance, "subdir/file.txt", storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("expected the object under the prefix kept, got %v", getErr)
    }
    _ = object.Body.Close()
}

func TestLocalStorage_GetAnswersTheObjectWithItsInfo(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    content := "shipping label body"
    before := time.Now().Add(-time.Second)
    if putErr := local.Put(runtimeInstance, "labels\\awb-1.txt", strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }
    after := time.Now().Add(time.Second)

    object, getErr := local.Get(runtimeInstance, "labels\\awb-1.txt", storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    loaded, _ := io.ReadAll(object.Body)
    _ = object.Body.Close()

    if content != string(loaded) || int64(len(content)) != object.Info.Size {
        t.Fatalf("expected the %d bytes and their size, got %q and %d", len(content), string(loaded), object.Info.Size)
    }

    if object.Info.ModifiedAt.Before(before) || object.Info.ModifiedAt.After(after) {
        t.Fatalf("expected the modification time of the put, got %v outside %v..%v", object.Info.ModifiedAt, before, after)
    }

    if "labels\\awb-1.txt" != object.Info.Key {
        t.Fatalf("expected the key asked, got %q", object.Info.Key)
    }

    fileInfo, statErr := os.Stat(filepath.Join(base, "labels", "awb-1.txt"))
    if nil != statErr {
        t.Fatalf("stat: %v", statErr)
    }

    if strings.Trim(static.GenerateEtag(fileInfo, false), `"`) != object.Info.EntityTag || "" == object.Info.EntityTag {
        t.Fatalf("expected the static file server's tag of the object without quotes, got %q", object.Info.EntityTag)
    }

    if "" != object.Info.ContentType {
        t.Fatalf("expected no content type for a put that declared none, got %q", object.Info.ContentType)
    }
}

func TestLocalStorage_TheEntityTagMovesWithEveryStore(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "report.txt", strings.NewReader("first"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    firstInfo, firstErr := local.Stat(runtimeInstance, "report.txt", storagecontract.StatOptions{})
    if nil != firstErr {
        t.Fatalf("stat: %v", firstErr)
    }

    earlier := time.Now().Add(-time.Hour)
    if touchErr := os.Chtimes(filepath.Join(base, "report.txt"), earlier, earlier); nil != touchErr {
        t.Fatalf("chtimes: %v", touchErr)
    }

    secondInfo, secondErr := local.Stat(runtimeInstance, "report.txt", storagecontract.StatOptions{})
    if nil != secondErr || firstInfo.EntityTag == secondInfo.EntityTag {
        t.Fatalf("expected another modification time to move the tag, got %q and %q (%v)", firstInfo.EntityTag, secondInfo.EntityTag, secondErr)
    }

    if putErr := local.Put(runtimeInstance, "report.txt", strings.NewReader("second, longer"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    thirdInfo, thirdErr := local.Stat(runtimeInstance, "report.txt", storagecontract.StatOptions{})
    if nil != thirdErr || secondInfo.EntityTag == thirdInfo.EntityTag || 14 != thirdInfo.Size {
        t.Fatalf("expected a second store to move the tag and the size, got %v (%v)", thirdInfo, thirdErr)
    }
}

func TestLocalStorage_GetAnswersNoBodyBesideAnError(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "prefix/object.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    for _, key := range []string{"prefix", "../prefix/object.txt", "prefix/" + reservedTemporaryName("object.txt", "00112233445566aa"), "absent.txt"} {
        object, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{})
        if nil == getErr || nil != object.Body || (storagecontract.ObjectInfo{}) != object.Info {
            if nil != object.Body {
                _ = object.Body.Close()
            }

            t.Fatalf("%q: expected an error beside the zero object, got %v (%v)", key, object, getErr)
        }
    }
}

func TestLocalStorage_StatAnswersTheInfoGetAnswers(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "reports/2026/report.csv", strings.NewReader("a,b,c"), 5, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    object, getErr := local.Get(runtimeInstance, "reports/2026/report.csv", storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    _ = object.Body.Close()

    objectInfo, statErr := local.Stat(runtimeInstance, "reports/2026/report.csv", storagecontract.StatOptions{})
    if nil != statErr || object.Info != objectInfo {
        t.Fatalf("expected Stat to answer the info Get answered, %v, got %v (%v)", object.Info, objectInfo, statErr)
    }
}

func TestLocalStorage_StatRefusesWhatGetRefuses(t *testing.T) {
    base := t.TempDir()
    outside := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "prefix/object.txt", strings.NewReader("inside"), -1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if writeErr := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("top secret"), 0o600); nil != writeErr {
        t.Fatalf("seed: %v", writeErr)
    }

    if linkErr := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(base, "escape")); nil != linkErr {
        t.Skipf("symlinks not supported: %v", linkErr)
    }

    if linkErr := os.Symlink(filepath.Join("prefix", "object.txt"), filepath.Join(base, "inside-link")); nil != linkErr {
        t.Fatalf("symlink: %v", linkErr)
    }

    for name, testCase := range map[string]struct {
        key          string
        directory    bool
        message      string
        refused      bool
        notFoundless bool
    }{
        "a directory":             {key: "prefix", directory: true, refused: true, notFoundless: true},
        "a parent segment":        {key: "../prefix/object.txt", message: `".." segment`, refused: true, notFoundless: true},
        "a reserved temporary":    {key: reservedTemporaryName("x.txt", "00112233445566aa"), message: "reserved .melody-storage- namespace", refused: true, notFoundless: true},
        "a link leaving the base": {key: "escape", refused: true, notFoundless: true},
        "a link inside the base":  {key: "inside-link"},
    } {
        object, getErr := local.Get(runtimeInstance, testCase.key, storagecontract.GetOptions{})
        if nil != object.Body {
            _ = object.Body.Close()
        }
        objectInfo, statErr := local.Stat(runtimeInstance, testCase.key, storagecontract.StatOptions{})

        if testCase.refused != (nil != getErr) || testCase.refused != (nil != statErr) {
            t.Fatalf("%s: expected both doors refused %v, got %v and %v", name, testCase.refused, getErr, statErr)
        }

        if false == testCase.refused {
            if 6 != objectInfo.Size || object.Info != objectInfo {
                t.Fatalf("%s: expected both doors to answer the linked object, got %v and %v", name, object.Info, objectInfo)
            }

            continue
        }

        if testCase.directory != exception.ChainIs(getErr, storagecontract.ErrKeyIsDirectory) || testCase.directory != exception.ChainIs(statErr, storagecontract.ErrKeyIsDirectory) {
            t.Fatalf("%s: expected ErrKeyIsDirectory %v at both doors, got %v and %v", name, testCase.directory, getErr, statErr)
        }

        if "" != testCase.message && (false == strings.Contains(getErr.Error(), testCase.message) || false == strings.Contains(statErr.Error(), testCase.message)) {
            t.Fatalf("%s: expected both refusals to say %q, got %v and %v", name, testCase.message, getErr, statErr)
        }

        if true == testCase.notFoundless && (true == exception.ChainIs(getErr, storagecontract.ErrObjectNotFound) || true == exception.ChainIs(statErr, storagecontract.ErrObjectNotFound)) {
            t.Fatalf("%s: expected a refusal that is no absence, got %v and %v", name, getErr, statErr)
        }

        if (storagecontract.ObjectInfo{}) != objectInfo {
            t.Fatalf("%s: expected the zero info beside the refusal, got %v", name, objectInfo)
        }
    }
}

func TestLocalStorage_AnAbsentKeyAnswersObjectNotFoundAtGetAndStat(t *testing.T) {
    base := filepath.Join(t.TempDir(), "base")
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "object.bin", strings.NewReader("x"), 1, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    assertAbsent := func(name string, key string) {
        object, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{})
        if false == exception.ChainIs(getErr, storagecontract.ErrObjectNotFound) || nil != object.Body {
            t.Fatalf("%s: expected Get to answer ErrObjectNotFound and no body, got %v", name, getErr)
        }

        if _, statErr := local.Stat(runtimeInstance, key, storagecontract.StatOptions{}); false == exception.ChainIs(statErr, storagecontract.ErrObjectNotFound) {
            t.Fatalf("%s: expected Stat to answer ErrObjectNotFound, got %v", name, statErr)
        }
    }

    assertAbsent("a key never stored", "never/stored.bin")
    assertAbsent("a key under a regular file", "object.bin/child")

    if removeErr := os.RemoveAll(base); nil != removeErr {
        t.Fatalf("remove: %v", removeErr)
    }

    assertAbsent("a key after the base directory was removed", "object.bin")
}

func TestLocalStorage_DeleteOfAnAbsentKeyAnswersNil(t *testing.T) {
    base := filepath.Join(t.TempDir(), "base")
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if deleteErr := local.Delete(runtimeInstance, "never/stored.bin", storagecontract.DeleteOptions{}); nil != deleteErr {
        t.Fatalf("expected nil for a key never stored, got %v", deleteErr)
    }

    if removeErr := os.RemoveAll(base); nil != removeErr {
        t.Fatalf("remove: %v", removeErr)
    }

    if deleteErr := local.Delete(runtimeInstance, "object.bin", storagecontract.DeleteOptions{}); nil != deleteErr {
        t.Fatalf("expected nil once the base directory is gone, got %v", deleteErr)
    }
}

func TestLocalStorage_GetReadsTheRangeAsked(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "digits.txt", strings.NewReader("0123456789"), 10, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    for name, testCase := range map[string]struct {
        byteRange storagecontract.ByteRange
        expected  string
    }{
        "the zero value":           {storagecontract.ByteRange{}, "0123456789"},
        "a range inside":           {storagecontract.ByteRange{Offset: 3, Length: 4}, "3456"},
        "a zero length to the end": {storagecontract.ByteRange{Offset: 3}, "3456789"},
        "a length past the end":    {storagecontract.ByteRange{Offset: 3, Length: 100}, "3456789"},
        "the first bytes":          {storagecontract.ByteRange{Length: 2}, "01"},
    } {
        object, getErr := local.Get(runtimeInstance, "digits.txt", storagecontract.GetOptions{Range: testCase.byteRange})
        if nil != getErr {
            t.Fatalf("%s: get: %v", name, getErr)
        }

        content, _ := io.ReadAll(object.Body)
        if closeErr := object.Body.Close(); nil != closeErr {
            t.Fatalf("%s: expected the body closed, got %v", name, closeErr)
        }

        if testCase.expected != string(content) || 10 != object.Info.Size {
            t.Fatalf("%s: expected %q of an object of 10 bytes, got %q of %d", name, testCase.expected, string(content), object.Info.Size)
        }
    }

    object, getErr := local.Get(runtimeInstance, "digits.txt", storagecontract.GetOptions{Range: storagecontract.ByteRange{Offset: 10}})
    if false == exception.ChainIs(getErr, storagecontract.ErrRangeNotSatisfiable) || nil != object.Body {
        t.Fatalf("expected a range at the end refused with no body, got %v", getErr)
    }

    if deleteErr := local.Delete(runtimeInstance, "digits.txt", storagecontract.DeleteOptions{}); nil != deleteErr {
        t.Fatalf("expected the object deleted after its ranged reads, got %v", deleteErr)
    }

    if putErr := local.Put(runtimeInstance, "digits.txt", strings.NewReader("again"), 5, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("expected the object stored again after its ranged reads, got %v", putErr)
    }
}

/* spelled from the digest, not read from the helper, so a namespace change fails here */
func reservedMetadataName(leaf string) string {
    digest := sha256.Sum256([]byte(leaf))

    return ".melody-storage-" + hex.EncodeToString(digest[:]) + ".metadata"
}

func TestLocalStorage_TheContentTypeDeclaredAtPutIsReadBack(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{ContentType: "application/pdf"}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    object, getErr := local.Get(runtimeInstance, "labels/awb-1.pdf", storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    _ = object.Body.Close()

    objectInfo, statErr := local.Stat(runtimeInstance, "labels/awb-1.pdf", storagecontract.StatOptions{})
    if nil != statErr || "application/pdf" != object.Info.ContentType || "application/pdf" != objectInfo.ContentType {
        t.Fatalf("expected the declared content type at Get and Stat, got %q and %q (%v)", object.Info.ContentType, objectInfo.ContentType, statErr)
    }

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF-2"), 6, storagecontract.PutOptions{ContentType: "application/x-pdf"}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if replacedInfo, replacedErr := local.Stat(runtimeInstance, "labels/awb-1.pdf", storagecontract.StatOptions{}); nil != replacedErr || "application/x-pdf" != replacedInfo.ContentType {
        t.Fatalf("expected the content type of the second put, got %q (%v)", replacedInfo.ContentType, replacedErr)
    }
}

func TestLocalStorage_APutWithoutAContentTypeClearsThePreviousOne(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{ContentType: "application/pdf"}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if entries, _ := os.ReadDir(filepath.Join(base, "labels")); 2 != len(entries) {
        t.Fatalf("expected the object and its metadata in the directory, got %d entries", len(entries))
    }

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if objectInfo, statErr := local.Stat(runtimeInstance, "labels/awb-1.pdf", storagecontract.StatOptions{}); nil != statErr || "" != objectInfo.ContentType {
        t.Fatalf("expected no content type after a put that declared none, got %q (%v)", objectInfo.ContentType, statErr)
    }

    if entries, _ := os.ReadDir(filepath.Join(base, "labels")); 1 != len(entries) {
        t.Fatalf("expected only the object in the directory, got %d entries", len(entries))
    }
}

func TestLocalStorage_MetadataOfAnotherWriteIsNotHonoured(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    put := func() {
        if putErr := local.Put(runtimeInstance, "report.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{ContentType: "application/pdf"}); nil != putErr {
            t.Fatalf("put: %v", putErr)
        }
    }

    assertNoContentType := func(name string, expectedBody string) {
        object, getErr := local.Get(runtimeInstance, "report.pdf", storagecontract.GetOptions{})
        if nil != getErr {
            t.Fatalf("%s: get: %v", name, getErr)
        }
        content, _ := io.ReadAll(object.Body)
        _ = object.Body.Close()

        if "" != object.Info.ContentType || expectedBody != string(content) {
            t.Fatalf("%s: expected the body served with no content type, got %q and %q", name, object.Info.ContentType, string(content))
        }
    }

    put()
    earlier := time.Now().Add(-time.Hour)
    if touchErr := os.Chtimes(filepath.Join(base, "report.pdf"), earlier, earlier); nil != touchErr {
        t.Fatalf("chtimes: %v", touchErr)
    }
    assertNoContentType("another modification time", "%PDF")

    put()
    objectPath := filepath.Join(base, "report.pdf")
    info, _ := os.Stat(objectPath)
    if writeErr := os.WriteFile(objectPath, []byte("replaced by hand"), 0o640); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }
    if touchErr := os.Chtimes(objectPath, info.ModTime(), info.ModTime()); nil != touchErr {
        t.Fatalf("chtimes: %v", touchErr)
    }
    assertNoContentType("another size", "replaced by hand")
}

func TestLocalStorage_DeleteRemovesTheMetadata(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{ContentType: "application/pdf"}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if deleteErr := local.Delete(runtimeInstance, "labels/awb-1.pdf", storagecontract.DeleteOptions{}); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    if entries, _ := os.ReadDir(filepath.Join(base, "labels")); 0 != len(entries) {
        t.Fatalf("expected the directory empty after the delete, got %d entries", len(entries))
    }
}

func TestLocalStorage_RefusesAKeySpelledLikeAMetadataObject(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    for _, key := range []string{reservedMetadataName("report.pdf"), "sub/" + reservedMetadataName("any leaf at all")} {
        putErr := local.Put(runtimeInstance, key, strings.NewReader("x"), 1, storagecontract.PutOptions{})
        _, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{})
        _, statErr := local.Stat(runtimeInstance, key, storagecontract.StatOptions{})
        deleteErr := local.Delete(runtimeInstance, key, storagecontract.DeleteOptions{})

        for door, doorErr := range map[string]error{"Put": putErr, "Get": getErr, "Stat": statErr, "Delete": deleteErr} {
            if nil == doorErr || false == strings.Contains(doorErr.Error(), "metadata object of the reserved .melody-storage- namespace") {
                t.Fatalf("%s of %q: expected the metadata spelling refused, got %v", door, key, doorErr)
            }
        }
    }

    for _, ordinaryKey := range []string{".melody-storage-notes.metadata", ".melody-storage-" + strings.Repeat("A", 64) + ".metadata", reservedMetadataName("x") + ".txt"} {
        if putErr := local.Put(runtimeInstance, ordinaryKey, strings.NewReader("user data"), -1, storagecontract.PutOptions{}); nil != putErr {
            t.Fatalf("expected the ordinary key %q to be stored, got %v", ordinaryKey, putErr)
        }
    }
}

func TestLocalStorage_UnreadableMetadataIsNoContentType(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "report.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    info, _ := os.Stat(filepath.Join(base, "report.pdf"))
    boundMetadata := fmt.Sprintf(`{"contentType":"application/pdf","size":%d,"modifiedAtUnixNano":%d}`, info.Size(), info.ModTime().UnixNano())

    for name, content := range map[string]string{
        "not json":              "{not json",
        "a mebibyte":            boundMetadata + strings.Repeat(" ", 1<<20),
        "past the bound by one": boundMetadata + strings.Repeat(" ", 4096-len(boundMetadata)+1),
    } {
        if writeErr := os.WriteFile(filepath.Join(base, reservedMetadataName("report.pdf")), []byte(content), 0o640); nil != writeErr {
            t.Fatalf("write: %v", writeErr)
        }

        objectInfo, statErr := local.Stat(runtimeInstance, "report.pdf", storagecontract.StatOptions{})
        if nil != statErr || "" != objectInfo.ContentType {
            t.Fatalf("%s: expected no content type and no error, got %q (%v)", name, objectInfo.ContentType, statErr)
        }
    }

    if writeErr := os.WriteFile(filepath.Join(base, reservedMetadataName("report.pdf")), []byte(boundMetadata+strings.Repeat(" ", 4096-len(boundMetadata))), 0o640); nil != writeErr {
        t.Fatalf("write: %v", writeErr)
    }

    if objectInfo, statErr := local.Stat(runtimeInstance, "report.pdf", storagecontract.StatOptions{}); nil != statErr || "application/pdf" != objectInfo.ContentType {
        t.Fatalf("expected metadata of exactly the bound honoured, got %q (%v)", objectInfo.ContentType, statErr)
    }
}

func TestLocalStorage_AMetadataObjectIsReadNoFurtherThanItsBound(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "report.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    metadataFile, createErr := os.Create(filepath.Join(base, reservedMetadataName("report.pdf")))
    if nil != createErr {
        t.Fatalf("create: %v", createErr)
    }
    if truncateErr := metadataFile.Truncate(64 << 20); nil != truncateErr {
        t.Fatalf("truncate: %v", truncateErr)
    }
    _ = metadataFile.Close()

    var before, after goruntime.MemStats
    goruntime.ReadMemStats(&before)
    objectInfo, statErr := local.Stat(runtimeInstance, "report.pdf", storagecontract.StatOptions{})
    goruntime.ReadMemStats(&after)

    if nil != statErr || "" != objectInfo.ContentType {
        t.Fatalf("expected no content type and no error, got %q (%v)", objectInfo.ContentType, statErr)
    }

    if allocated := after.TotalAlloc - before.TotalAlloc; 1<<20 < allocated {
        t.Fatalf("expected the read bounded, it allocated %d bytes", allocated)
    }
}

func TestLocalStorage_GetRefusesANegativeRangeBeforeLookingForTheObject(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})

    for name, byteRange := range map[string]storagecontract.ByteRange{"a negative offset": {Offset: -1}, "a negative length": {Length: -1}} {
        object, getErr := local.Get(testRuntime(), "absent.txt", storagecontract.GetOptions{Range: byteRange})
        if nil == getErr || true == exception.ChainIs(getErr, storagecontract.ErrObjectNotFound) || nil != object.Body {
            t.Fatalf("%s: expected the range refused, not the key found absent, got %v", name, getErr)
        }

        if "GetOptions.Range" != exception.FromError(getErr).Context()["field"] {
            t.Fatalf("%s: expected the refusal to name GetOptions.Range, got %v", name, exception.FromError(getErr).Context())
        }
    }
}

/* the longest size and modification time spend 87 metadata bytes, leaving 4009 for the content type */
func TestLocalStorage_PutRefusesAContentTypeTooLongToBeReadBack(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    longest := "text/plain; name=" + strings.Repeat("a", 4009-len("text/plain; name="))

    putErr := local.Put(runtimeInstance, "labels/too-long.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{ContentType: longest + "a"})
    if nil == putErr || "PutOptions.ContentType" != exception.FromError(putErr).Context()["field"] {
        t.Fatalf("expected the content type refused naming PutOptions.ContentType, got %v", putErr)
    }

    if _, statErr := os.Lstat(filepath.Join(base, "labels")); false == os.IsNotExist(statErr) {
        t.Fatalf("expected a refused put to create nothing, got %v", statErr)
    }

    if putErr := local.Put(runtimeInstance, "labels/longest.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{ContentType: longest}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if objectInfo, statErr := local.Stat(runtimeInstance, "labels/longest.txt", storagecontract.StatOptions{}); nil != statErr || longest != objectInfo.ContentType {
        t.Fatalf("expected the longest content type read back whole, got %d bytes (%v)", len(objectInfo.ContentType), statErr)
    }

    if entries, _ := os.ReadDir(filepath.Join(base, "labels")); 2 != len(entries) {
        t.Fatalf("expected the object and its metadata only, got %d entries", len(entries))
    }
}

func TestLocalStorage_RefusesAReservedNameAsAnySegmentOfTheKey(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})
    runtimeInstance := testRuntime()

    for expected, key := range map[string]string{
        "metadata object of the reserved .melody-storage- namespace": "labels/" + reservedMetadataName("awb-1.pdf") + "/x",
        "temp object of the reserved .melody-storage- namespace":     "labels/" + reservedTemporaryName("awb-1.pdf", "0123456789abcdef") + "/x",
    } {
        putErr := local.Put(runtimeInstance, key, strings.NewReader("x"), 1, storagecontract.PutOptions{})
        _, getErr := local.Get(runtimeInstance, key, storagecontract.GetOptions{})
        _, statErr := local.Stat(runtimeInstance, key, storagecontract.StatOptions{})
        deleteErr := local.Delete(runtimeInstance, key, storagecontract.DeleteOptions{})

        for door, doorErr := range map[string]error{"Put": putErr, "Get": getErr, "Stat": statErr, "Delete": deleteErr} {
            if nil == doorErr || false == strings.Contains(doorErr.Error(), expected) {
                t.Fatalf("%s of %q: expected the reserved segment refused, got %v", door, key, doorErr)
            }
        }
    }

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{ContentType: "application/pdf"}); nil != putErr {
        t.Fatalf("expected the sibling key stored with its content type, got %v", putErr)
    }
}

type temporaryRemovingReader struct {
    directory string
    done      bool
}

func (instance *temporaryRemovingReader) Read(buffer []byte) (int, error) {
    if true == instance.done {
        return 0, io.EOF
    }

    instance.done = true
    temporaryList, _ := filepath.Glob(filepath.Join(instance.directory, ".melody-storage-*.tmp"))
    for _, temporary := range temporaryList {
        _ = os.Remove(temporary)
    }

    buffer[0] = 'x'

    return 1, nil
}

func TestLocalStorage_AFailedObjectRenameKeepsThePreviousContentType(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{ContentType: "application/pdf"}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    if putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", &temporaryRemovingReader{directory: filepath.Join(base, "labels")}, -1, storagecontract.PutOptions{ContentType: "text/plain"}); nil == putErr {
        t.Fatal("expected the put to fail once its temporary object was removed")
    }

    object, getErr := local.Get(runtimeInstance, "labels/awb-1.pdf", storagecontract.GetOptions{})
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    content, _ := io.ReadAll(object.Body)
    _ = object.Body.Close()

    if "%PDF" != string(content) || "application/pdf" != object.Info.ContentType {
        t.Fatalf("expected the previous object and its content type, got %q as %q", string(content), object.Info.ContentType)
    }
}

func TestLocalStorage_AMetadataWriteThatFailsAnswersAnErrorOverTheStoredObject(t *testing.T) {
    base := t.TempDir()
    local := NewLocalStorage(base, LocalStorageConfig{})
    runtimeInstance := testRuntime()

    if mkdirErr := os.MkdirAll(filepath.Join(base, "labels", reservedMetadataName("awb-1.pdf"), "squatter"), 0o750); nil != mkdirErr {
        t.Fatalf("mkdir: %v", mkdirErr)
    }

    putErr := local.Put(runtimeInstance, "labels/awb-1.pdf", strings.NewReader("%PDF"), 4, storagecontract.PutOptions{ContentType: "application/pdf"})
    if nil == putErr || false == strings.Contains(putErr.Error(), "could not store the storage object's content type") {
        t.Fatalf("expected the content type failure answered, got %v", putErr)
    }

    objectInfo, statErr := local.Stat(runtimeInstance, "labels/awb-1.pdf", storagecontract.StatOptions{})
    if nil != statErr || 4 != objectInfo.Size || "" != objectInfo.ContentType {
        t.Fatalf("expected the object stored without a content type, got %+v (%v)", objectInfo, statErr)
    }

    if temporaryList, _ := filepath.Glob(filepath.Join(base, "labels", ".melody-storage-*.tmp")); 0 != len(temporaryList) {
        t.Fatalf("expected no temporary left behind, got %v", temporaryList)
    }
}

func TestLocalStorage_EveryDoorRefusesANilRuntimeFirst(t *testing.T) {
    local := NewLocalStorage(t.TempDir(), LocalStorageConfig{})

    _, getErr := local.Get(nil, "a.txt", storagecontract.GetOptions{})
    _, statErr := local.Stat(nil, "a.txt", storagecontract.StatOptions{})
    _, presignErr := local.PresignedUrl(nil, "a.txt", storagecontract.PresignOptions{})
    for door, doorErr := range map[string]error{
        "Put":          local.Put(nil, "a.txt", strings.NewReader("x"), 1, storagecontract.PutOptions{}),
        "Get":          getErr,
        "Stat":         statErr,
        "Delete":       local.Delete(nil, "a.txt", storagecontract.DeleteOptions{}),
        "PresignedUrl": presignErr,
    } {
        if nil == doorErr || false == strings.Contains(doorErr.Error(), "the runtime is nil or carries no context") {
            t.Fatalf("%s: expected the nil runtime refused, got %v", door, doorErr)
        }
    }

    if entries, readErr := os.ReadDir(local.baseDirectory); nil != readErr || 0 != len(entries) {
        t.Fatalf("expected nothing written under the base, got %v %v", entries, readErr)
    }
}

func requireStoragePermission(t *testing.T, path string, expected os.FileMode) {
    t.Helper()

    info, statErr := os.Stat(path)
    if nil != statErr {
        t.Fatalf("stat %s: %v", path, statErr)
    }

    if expected != info.Mode().Perm() {
        t.Fatalf("expected %s at %#o, got %#o", path, uint32(expected), uint32(info.Mode().Perm()))
    }
}

func storedUnderModes(t *testing.T, config LocalStorageConfig, fileMode os.FileMode, directoryMode os.FileMode) {
    t.Helper()
    withZeroUmask(t)

    base := filepath.Join(t.TempDir(), "base")
    local := NewLocalStorage(base, config)
    requireStoragePermission(t, base, directoryMode)

    if removeErr := os.Remove(base); nil != removeErr {
        t.Fatalf("remove: %v", removeErr)
    }

    if putErr := local.Put(testRuntime(), "labels/awb-123.txt", strings.NewReader("body"), 4, storagecontract.PutOptions{ContentType: "text/plain"}); nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    requireStoragePermission(t, base, directoryMode)
    requireStoragePermission(t, filepath.Join(base, "labels"), directoryMode)
    requireStoragePermission(t, filepath.Join(base, "labels", "awb-123.txt"), fileMode)
    requireStoragePermission(t, filepath.Join(base, storageMetadataKey("labels/awb-123.txt")), fileMode)
}

func TestNewLocalStorage_CreatesTheDirectoriesAt0750AndTheObjectsAt0640ByDefault(t *testing.T) {
    if 0o640 != DefaultLocalStorageFileMode || 0o750 != DefaultLocalStorageDirectoryMode {
        t.Fatalf("expected the defaults 0640 and 0750, got %#o and %#o", uint32(DefaultLocalStorageFileMode), uint32(DefaultLocalStorageDirectoryMode))
    }

    storedUnderModes(t, LocalStorageConfig{}, 0o640, 0o750)
}

func TestNewLocalStorage_CreatesTheDirectoriesAndTheObjectsAtTheConfiguredModes(t *testing.T) {
    storedUnderModes(t, LocalStorageConfig{FileMode: 0o644, DirectoryMode: 0o755}, 0o644, 0o755)
    storedUnderModes(t, LocalStorageConfig{FileMode: 0o600, DirectoryMode: 0o700}, 0o600, 0o700)
}

func TestNewLocalStorage_RefusesAModeOutsideItsRuleNamingTheField(t *testing.T) {
    cases := []struct {
        config LocalStorageConfig
        field  string
        value  string
    }{
        {config: LocalStorageConfig{FileMode: 0o440}, field: "LocalStorageConfig.FileMode", value: "0440"},
        {config: LocalStorageConfig{FileMode: 0o2640}, field: "LocalStorageConfig.FileMode", value: "02640"},
        {config: LocalStorageConfig{DirectoryMode: 0o650}, field: "LocalStorageConfig.DirectoryMode", value: "0650"},
        {config: LocalStorageConfig{DirectoryMode: 0o1750}, field: "LocalStorageConfig.DirectoryMode", value: "01750"},
    }

    for _, testCase := range cases {
        base := filepath.Join(t.TempDir(), "base")
        context := recoveredPanicContext(func() {
            NewLocalStorage(base, testCase.config)
        })
        if testCase.field != context["field"] || testCase.value != context["value"] {
            t.Fatalf("expected the refusal to name %s and %s, got %v", testCase.field, testCase.value, context)
        }

        if _, statErr := os.Stat(base); false == os.IsNotExist(statErr) {
            t.Fatalf("expected nothing created under a refused mode, got %v", statErr)
        }
    }
}
