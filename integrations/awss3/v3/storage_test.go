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

func TestBoundedPutReader_StripsReaderAtAndDetectsOverReadAtCorrectSize(t *testing.T) {
    /* @important regression for the v3.0.1 over-read guard mis-firing on io.ReaderAt+io.Seeker readers (bytes.Reader/strings.Reader/os.File): minio's single-shot putObject wraps such a reader in an io.SectionReader and uploads via ReadAt without advancing the caller's sequential cursor, so probing the original afterward reported trailing bytes on every valid Put and deleted the stored object. boundedPutReader must hand minio a reader that is neither io.ReaderAt nor io.Seeker so the sequential path is forced and the original cursor advances by exactly the consumed size. */
    exactBody := "exactly-sized-body"
    original := strings.NewReader(exactBody)
    putReader := boundedPutReader(original, int64(len(exactBody)))

    if _, isReaderAt := putReader.(io.ReaderAt); true == isReaderAt {
        t.Fatalf("the reader handed to minio must not be an io.ReaderAt, or minio's SectionReader/ReadAt path leaves the caller's cursor at 0")
    }
    if _, isSeeker := putReader.(io.Seeker); true == isSeeker {
        t.Fatalf("the reader handed to minio must not be an io.Seeker, or minio takes the SectionReader optimization")
    }

    /* @important consuming the put reader (as minio's sequential path does, reading exactly the declared size) must advance the ORIGINAL reader's cursor, so an exact-size body reports no trailing byte — no manual pre-read of the original, unlike the older helper test */
    consumed, _ := io.Copy(io.Discard, putReader)
    if int64(len(exactBody)) != consumed {
        t.Fatalf("expected minio to read exactly %d bytes through the bounded reader, got %d", len(exactBody), consumed)
    }
    if true == readerHasTrailingBytes(original) {
        t.Fatalf("an exact-size body must report no trailing bytes after the bounded reader is consumed, so a valid Put is not wrongly rejected")
    }

    /* @important a body longer than the declared size: minio reads only `declared` bytes through the cap, leaving the rest on the original for the over-read probe to catch */
    longerBody := "declared-short-but-body-is-actually-longer"
    overOriginal := strings.NewReader(longerBody)
    declared := int64(9)
    overPutReader := boundedPutReader(overOriginal, declared)
    overConsumed, _ := io.Copy(io.Discard, overPutReader)
    if declared != overConsumed {
        t.Fatalf("expected the bounded reader to cap minio's read at the declared %d bytes, got %d", declared, overConsumed)
    }
    if false == readerHasTrailingBytes(overOriginal) {
        t.Fatalf("a body longer than the declared size must report a trailing byte so Put rejects it")
    }

    /* @important a negative size means unknown length: stream the reader whole with no cap, so the same reader instance is returned */
    streamed := strings.NewReader("whole")
    if streamed != boundedPutReader(streamed, -1) {
        t.Fatalf("expected a negative size to stream the original reader unwrapped")
    }
}
