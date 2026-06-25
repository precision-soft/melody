package awss3

import (
    "context"
    "io"
    "os"
    "strings"
    "testing"
    "time"

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

    client, clientErr := NewClient(Config{
        Endpoint:  endpoint,
        AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
        SecretKey: os.Getenv("MINIO_SECRET_KEY"),
        Secure:    false,
    })
    if nil != clientErr {
        t.Fatalf("client: %v", clientErr)
    }

    bucket := "melody-test"
    if ensureErr := EnsureBucket(context.Background(), client, bucket, ""); nil != ensureErr {
        t.Fatalf("ensure bucket: %v", ensureErr)
    }

    store := NewStorage(client, bucket)
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

func TestNormalizeObjectKey_MatchesLocalStorageContract(t *testing.T) {
    cases := []struct {
        input    string
        expected string
    }{
        {input: "report.txt", expected: "report.txt"},
        {input: "/report.txt", expected: "report.txt"},
        {input: "a\\b.txt", expected: "a/b.txt"},
        {input: "uploads/../f.txt", expected: "f.txt"},
        {input: "nested/dir/file.bin", expected: "nested/dir/file.bin"},
    }

    for _, testCase := range cases {
        normalized, err := normalizeObjectKey(testCase.input)
        if nil != err {
            t.Fatalf("key %q: unexpected error %s", testCase.input, err.Error())
        }
        if testCase.expected != normalized {
            t.Fatalf("key %q: expected %q, got %q", testCase.input, testCase.expected, normalized)
        }
    }
}

func TestNormalizeObjectKey_RejectsEmptyAndDotKeys(t *testing.T) {
    for _, input := range []string{"", "/", ".", "uploads/.."} {
        if _, err := normalizeObjectKey(input); nil == err {
            t.Fatalf("expected key %q to be rejected as empty or invalid", input)
        }
    }
}

func TestReaderHasTrailingBytes_DetectsBodyLongerThanDeclaredSize(t *testing.T) {
    /* @important mirrors the Put over-read guard: after minio consumes the declared size, a body exactly that size yields no more bytes while a longer body still yields one (which minio would silently truncate and store), so the guard must flag the latter to match LocalStorage's size-mismatch rejection */
    declaredSize := 3

    exhausted := strings.NewReader("abc")
    if _, err := io.ReadFull(exhausted, make([]byte, declaredSize)); nil != err {
        t.Fatalf("unexpected read error: %s", err.Error())
    }
    if true == readerHasTrailingBytes(exhausted) {
        t.Fatalf("a body exactly the declared size must report no trailing bytes")
    }

    oversize := strings.NewReader("abcd")
    if _, err := io.ReadFull(oversize, make([]byte, declaredSize)); nil != err {
        t.Fatalf("unexpected read error: %s", err.Error())
    }
    if false == readerHasTrailingBytes(oversize) {
        t.Fatalf("a body longer than the declared size must report a trailing byte so Put can reject it")
    }
}
