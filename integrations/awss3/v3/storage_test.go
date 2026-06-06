package awss3_test

import (
    "context"
    "io"
    "os"
    "strings"
    "testing"
    "time"

    awss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

func newRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestObjectStorage_PutGetExistsPresignDelete(t *testing.T) {
    endpoint := os.Getenv("MINIO_ENDPOINT")
    if "" == endpoint {
        t.Skip("MINIO_ENDPOINT not set; skipping object storage integration test")
    }

    client, clientErr := awss3.NewClient(awss3.Config{
        Endpoint:  endpoint,
        AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
        SecretKey: os.Getenv("MINIO_SECRET_KEY"),
        Secure:    false,
    })
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }

    bucket := "melody-test"
    if ensureErr := awss3.EnsureBucket(context.Background(), client, bucket, ""); nil != ensureErr {
        t.Fatalf("ensure bucket: %v", ensureErr)
    }

    store := awss3.NewStorage(client, bucket)
    runtimeInstance := newRuntime()

    key := "labels/awb-123.txt"
    content := "shipping label body"

    putErr := store.Put(runtimeInstance, key, strings.NewReader(content), int64(len(content)), storagecontract.PutOptions{ContentType: "text/plain"})
    if nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    exists, existsErr := store.Exists(runtimeInstance, key)
    if nil != existsErr || false == exists {
        t.Fatalf("expected object to exist: %v %v", exists, existsErr)
    }

    reader, getErr := store.Get(runtimeInstance, key)
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    loaded, _ := io.ReadAll(reader)
    reader.Close()
    if content != string(loaded) {
        t.Fatalf("content mismatch: %q", string(loaded))
    }

    presigned, presignErr := store.PresignedUrl(runtimeInstance, key, 5*time.Minute)
    if nil != presignErr || false == strings.Contains(presigned, key) {
        t.Fatalf("unexpected presigned url: %q %v", presigned, presignErr)
    }

    if deleteErr := store.Delete(runtimeInstance, key); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }

    existsAfter, _ := store.Exists(runtimeInstance, key)
    if true == existsAfter {
        t.Fatalf("expected object to be gone after delete")
    }
}
