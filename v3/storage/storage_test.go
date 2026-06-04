package storage_test

import (
    "context"
    "io"
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

func TestLocalStorage_RejectsPathTraversal(t *testing.T) {
    local := storage.NewLocalStorage(t.TempDir())
    runtimeInstance := testRuntime()

    _, getErr := local.Get(runtimeInstance, "../../etc/passwd")
    if nil == getErr {
        t.Fatalf("expected path traversal to be rejected")
    }
}
