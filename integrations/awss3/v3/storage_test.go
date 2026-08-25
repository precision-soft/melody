package awss3

import (
    "context"
    "errors"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "strconv"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/minio/minio-go/v7"

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

func TestBoundedPutReader_StripsReaderAtAndCapsWhatMinioCanStore(t *testing.T) {
    /* minio's single-shot putObject wraps an io.ReaderAt+io.Seeker reader (bytes.Reader/strings.Reader/os.File) in an io.SectionReader and uploads it via ReadAt in one shot; the sequential path consumes the body one part buffer at a time instead, which is what lets a size-checked body cut an upload off before its last declared byte. boundedPutReader must therefore hand minio a reader that is neither io.ReaderAt nor io.Seeker. */
    exactBody := "exactly-sized-body"
    original := strings.NewReader(exactBody)
    putReader := boundedPutReader(original, int64(len(exactBody)))

    if _, isReaderAt := putReader.(io.ReaderAt); true == isReaderAt {
        t.Fatalf("the reader handed to minio must not be an io.ReaderAt, or minio uploads it through a SectionReader in one shot")
    }
    if _, isSeeker := putReader.(io.Seeker); true == isSeeker {
        t.Fatalf("the reader handed to minio must not be an io.Seeker, or minio takes the SectionReader optimization")
    }

    consumed, _ := io.Copy(io.Discard, putReader)
    if int64(len(exactBody)) != consumed {
        t.Fatalf("expected minio to read exactly %d bytes through the bounded reader, got %d", len(exactBody), consumed)
    }

    /* the cap bounds what minio can store at the declared size on any path, single-shot or multipart */
    longerBody := "declared-short-but-body-is-actually-longer"
    declared := int64(9)
    overConsumed, _ := io.Copy(io.Discard, boundedPutReader(strings.NewReader(longerBody), declared))
    if declared != overConsumed {
        t.Fatalf("expected the bounded reader to cap minio's read at the declared %d bytes, got %d", declared, overConsumed)
    }

    /* a negative size means unknown length: stream the reader whole with no cap, so the same reader instance is returned */
    streamed := strings.NewReader("whole")
    if streamed != boundedPutReader(streamed, -1) {
        t.Fatalf("expected a negative size to stream the original reader unwrapped")
    }
}

func TestSizeCheckedReader_StopsShortOfTheDeclaredSizeWhenTheBodyIsLonger(t *testing.T) {
    /* the upload must be left incomplete, not merely wrong: a request that carried every declared byte announces a content length it satisfied, and the bucket is entitled to commit it. Cutting the body off one byte early is what makes minio abort the multipart upload and the bucket refuse a single-shot put, so the object already at the key survives. */
    declared := int64(16)
    checked := newSizeCheckedReader(context.Background(), "invoices/2026-07.pdf", &sequentialReader{reader: strings.NewReader(strings.Repeat("b", 64))}, declared)

    yielded, readErr := io.Copy(io.Discard, checked)
    if nil == readErr {
        t.Fatalf("expected a body longer than the declared size to be rejected")
    }
    if "storage object size does not match the declared size" != readErr.Error() {
        t.Fatalf("expected a declared size mismatch, got %q", readErr.Error())
    }
    if false == checked.rejected.Load() {
        t.Fatalf("the reader must report that it rejected the body, so Put can name the size mismatch instead of the transport failure")
    }
    if declared <= yielded {
        t.Fatalf("expected fewer than the declared %d bytes to reach minio so the upload stays incomplete, got %d", declared, yielded)
    }
}

func TestSizeCheckedReader_YieldsAnExactlySizedBodyWhole(t *testing.T) {
    body := strings.Repeat("b", 64)

    for _, testCase := range []struct {
        name   string
        reader io.Reader
    }{
        {name: "non-seekable", reader: &sequentialReader{reader: strings.NewReader(body)}},
        {name: "non-seekable with zero length reads", reader: &stutteringReader{remaining: body}},
    } {
        checked := newSizeCheckedReader(context.Background(), "invoices/2026-07.pdf", testCase.reader, int64(len(body)))

        loaded, readErr := io.ReadAll(checked)
        if nil != readErr {
            t.Fatalf("%s: unexpected error: %s", testCase.name, readErr.Error())
        }
        if body != string(loaded) {
            t.Fatalf("%s: expected the whole body, got %d bytes", testCase.name, len(loaded))
        }
    }
}

func TestSizeCheckedReader_StopsWhenTheContextIsDone(t *testing.T) {
    /* a slow body would otherwise pin an upload with no deadline of its own */
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    checked := newSizeCheckedReader(ctx, "invoices/2026-07.pdf", &sequentialReader{reader: strings.NewReader("an invoice body")}, 15)

    if _, readErr := io.Copy(io.Discard, checked); nil == readErr {
        t.Fatalf("expected a cancelled context to stop the body")
    }
}

func TestSizeCheckedReader_RejectsADeclaredZeroSizeWithABodyBehindIt(t *testing.T) {
    checked := newSizeCheckedReader(context.Background(), "invoices/2026-07.pdf", &sequentialReader{reader: strings.NewReader("x")}, 0)

    if _, readErr := io.Copy(io.Discard, checked); nil == readErr {
        t.Fatalf("expected a body behind a zero declared size to be rejected")
    }
}

type recordedObjectRequest struct {
    method string
    path   string
    body   string
}

type recordingObjectServer struct {
    server      *httptest.Server
    mutex       sync.Mutex
    requestList []recordedObjectRequest
}

func newRecordingObjectServer(t *testing.T) *recordingObjectServer {
    recorder := &recordingObjectServer{}

    recorder.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        body, _ := io.ReadAll(request.Body)

        recordedBody := string(body)
        if true == strings.HasPrefix(request.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
            recordedBody = decodeChunkedPutBody(recordedBody)
        }

        recorder.mutex.Lock()
        recorder.requestList = append(recorder.requestList, recordedObjectRequest{
            method: request.Method,
            path:   request.URL.Path,
            body:   recordedBody,
        })
        recorder.mutex.Unlock()

        writer.Header().Set("ETag", "\"9a0364b9e99bb480dd25e1f0284c8555\"")
        writer.WriteHeader(http.StatusOK)
    }))

    t.Cleanup(recorder.server.Close)

    return recorder
}

func (instance *recordingObjectServer) newStorage(t *testing.T) *Storage {
    client, clientErr := NewClient(Config{
        Endpoint:  strings.TrimPrefix(instance.server.URL, "http://"),
        AccessKey: "test",
        SecretKey: "test",
        Secure:    false,
        Region:    "us-east-1",
    })
    if nil != clientErr {
        t.Fatalf("client: %s", clientErr.Error())
    }

    return NewStorage(client, "melody-test")
}

func (instance *recordingObjectServer) recorded() []recordedObjectRequest {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]recordedObjectRequest(nil), instance.requestList...)
}

/* minio signs an unencrypted put with the streaming signature, so the recorded body arrives wrapped in aws-chunked framing (a hex length plus signature line, the payload, then a zero length chunk) and has to be unwrapped before it can be compared with what the caller handed to Put */
func decodeChunkedPutBody(body string) string {
    var payload strings.Builder

    remaining := body

    for {
        headerEnd := strings.Index(remaining, "\r\n")
        if 0 > headerEnd {
            return payload.String()
        }

        header := remaining[:headerEnd]
        remaining = remaining[headerEnd+2:]

        sizeText := header
        if separatorIndex := strings.Index(header, ";"); 0 <= separatorIndex {
            sizeText = header[:separatorIndex]
        }

        chunkSize, parseErr := strconv.ParseInt(sizeText, 16, 64)
        if nil != parseErr || 0 >= chunkSize || int64(len(remaining)) < chunkSize {
            return payload.String()
        }

        payload.WriteString(remaining[:chunkSize])
        remaining = remaining[chunkSize:]

        if true == strings.HasPrefix(remaining, "\r\n") {
            remaining = remaining[2:]
        }
    }
}

type sequentialReader struct {
    reader io.Reader
}

func (instance *sequentialReader) Read(buffer []byte) (int, error) {
    return instance.reader.Read(buffer)
}

/* yields the body one byte at a time, returning a legal (0, nil) read before every byte and before the final io.EOF, which an over-read probe must not mistake for the end of the body */
type stutteringReader struct {
    remaining string
    stutter   bool
}

func (instance *stutteringReader) Read(buffer []byte) (int, error) {
    if 0 == len(buffer) {
        return 0, nil
    }

    if false == instance.stutter {
        instance.stutter = true

        return 0, nil
    }

    instance.stutter = false

    if "" == instance.remaining {
        return 0, io.EOF
    }

    buffer[0] = instance.remaining[0]
    instance.remaining = instance.remaining[1:]

    return 1, nil
}

func TestPut_RejectsADeclaredSizeShorterThanTheBodyBeforeTouchingTheBucket(t *testing.T) {
    body := "an invoice body that is a lot longer than the declared size"

    cases := []struct {
        name   string
        reader io.Reader
    }{
        {name: "seekable", reader: strings.NewReader(body)},
        {name: "non-seekable", reader: &sequentialReader{reader: strings.NewReader(body)}},
        {name: "non-seekable with zero length reads", reader: &stutteringReader{remaining: body}},
    }

    for _, testCase := range cases {
        recorder := newRecordingObjectServer(t)
        store := recorder.newStorage(t)

        putErr := store.Put(
            newRuntime(),
            "invoices/2026-07.pdf",
            testCase.reader,
            12,
            storagecontract.PutOptions{ContentType: "application/pdf"},
        )
        if nil == putErr {
            t.Fatalf("%s: expected a body longer than the declared size to be rejected", testCase.name)
        }
        if "storage object size does not match the declared size" != putErr.Error() {
            t.Fatalf("%s: expected a declared size mismatch, got %q", testCase.name, putErr.Error())
        }

        if recordedList := recorder.recorded(); 0 != len(recordedList) {
            t.Fatalf("%s: a rejected put must not reach the bucket, got %v", testCase.name, recordedList)
        }
    }
}

func TestPut_UploadsTheWholeBodyWhenTheDeclaredSizeMatches(t *testing.T) {
    body := "exactly-sized-invoice-body"

    cases := []struct {
        name   string
        reader io.Reader
    }{
        {name: "seekable", reader: strings.NewReader(body)},
        {name: "non-seekable", reader: &sequentialReader{reader: strings.NewReader(body)}},
        {name: "non-seekable with zero length reads", reader: &stutteringReader{remaining: body}},
    }

    for _, testCase := range cases {
        recorder := newRecordingObjectServer(t)
        store := recorder.newStorage(t)

        putErr := store.Put(
            newRuntime(),
            "/invoices/../invoices/2026-07.pdf",
            testCase.reader,
            int64(len(body)),
            storagecontract.PutOptions{ContentType: "application/pdf"},
        )
        if nil != putErr {
            t.Fatalf("%s: put: %s", testCase.name, putErr.Error())
        }

        recordedList := recorder.recorded()
        if 1 != len(recordedList) {
            t.Fatalf("%s: expected exactly one request, got %v", testCase.name, recordedList)
        }
        if "PUT" != recordedList[0].method || "/melody-test/invoices/2026-07.pdf" != recordedList[0].path {
            t.Fatalf("%s: unexpected request %v", testCase.name, recordedList[0])
        }
        if body != recordedList[0].body {
            t.Fatalf("%s: expected the whole body to be stored, got %q", testCase.name, recordedList[0].body)
        }
    }
}

func TestPut_HandlesABodyShorterThanItsDeclaredSizeAndAnEmptyBody(t *testing.T) {
    /* a body shorter than its declared size is left to minio, which refuses the put rather than storing a short object; the wait is minio's own retry ladder */
    shortRecorder := newRecordingObjectServer(t)
    shortStore := shortRecorder.newStorage(t)

    shortErr := shortStore.Put(newRuntime(), "invoices/2026-07.pdf", &sequentialReader{reader: strings.NewReader("short")}, 4096, storagecontract.PutOptions{ContentType: "application/pdf"})
    if nil == shortErr {
        t.Fatalf("expected a body shorter than the declared size to be refused")
    }
    if "object storage put failed" != shortErr.Error() {
        t.Fatalf("expected minio to refuse the short body, got %q", shortErr.Error())
    }

    for _, testCase := range []struct {
        name   string
        reader io.Reader
    }{
        {name: "seekable", reader: strings.NewReader("")},
        {name: "non-seekable", reader: &sequentialReader{reader: strings.NewReader("")}},
    } {
        recorder := newRecordingObjectServer(t)
        store := recorder.newStorage(t)

        emptyErr := store.Put(newRuntime(), "invoices/empty.pdf", testCase.reader, 0, storagecontract.PutOptions{ContentType: "application/pdf"})
        if nil != emptyErr {
            t.Fatalf("%s: an empty body declared as zero bytes must be stored: %s", testCase.name, emptyErr.Error())
        }

        recordedList := recorder.recorded()
        if 1 != len(recordedList) || "" != recordedList[0].body {
            t.Fatalf("%s: expected one empty put, got %v", testCase.name, recordedList)
        }
    }
}

func TestValidatedPutBody_StreamsASeekableBodyAndSpoolsOnlyWhatASmallNonSeekableOneYields(t *testing.T) {
    body := "exactly-sized-invoice-body"

    seekable := strings.NewReader(body)
    streamed, _, streamedErr := validatedPutBody(context.Background(), "invoices/2026-07.pdf", seekable, int64(len(body)))
    if nil != streamedErr {
        t.Fatalf("unexpected error: %s", streamedErr.Error())
    }
    if seekable != streamed {
        t.Fatalf("a seekable body must be streamed straight to minio, not buffered")
    }

    /* a declared size can come from a client supplied content length, so the spool must hold what the body actually yields, never the declared size itself */
    spooled, _, spooledErr := validatedPutBody(context.Background(), "invoices/2026-07.pdf", &sequentialReader{reader: strings.NewReader("tiny")}, putSpoolLimit)
    if nil != spooledErr {
        t.Fatalf("unexpected error: %s", spooledErr.Error())
    }
    loaded, _ := io.ReadAll(spooled)
    if "tiny" != string(loaded) {
        t.Fatalf("expected the spooled body to be preserved, got %q", string(loaded))
    }

    unknown := &sequentialReader{reader: strings.NewReader(body)}
    unvalidated, _, unvalidatedErr := validatedPutBody(context.Background(), "invoices/2026-07.pdf", unknown, -1)
    if nil != unvalidatedErr {
        t.Fatalf("unexpected error: %s", unvalidatedErr.Error())
    }
    if unknown != unvalidated {
        t.Fatalf("an unknown length body must be streamed unvalidated")
    }
}

func TestValidatedPutBody_LooksPastAZeroLengthRead(t *testing.T) {
    /* a (0, nil) read is legal and means nothing happened, so treating it as the end of the body would let an over-read through and store a silently truncated object */
    _, _, trailingErr := validatedPutBody(context.Background(), "invoices/2026-07.pdf", &stutteringReader{remaining: "abcd"}, 3)
    if nil == trailingErr {
        t.Fatalf("a zero length read must not be taken for the end of the body")
    }
    if "storage object size does not match the declared size" != trailingErr.Error() {
        t.Fatalf("expected a declared size mismatch, got %q", trailingErr.Error())
    }

    exhausted, _, exhaustedErr := validatedPutBody(context.Background(), "invoices/2026-07.pdf", &stutteringReader{remaining: "abc"}, 3)
    if nil != exhaustedErr {
        t.Fatalf("unexpected error: %s", exhaustedErr.Error())
    }
    loaded, _ := io.ReadAll(exhausted)
    if "abc" != string(loaded) {
        t.Fatalf("expected an exhausted stuttering body to be preserved, got %q", string(loaded))
    }
}

/* generates a body of the requested length without allocating it, so a test can assert how much of a large body Put is willing to hold in memory; the filler tells one generated body from another once it is stored */
type generatedBodyReader struct {
    remaining int64
    filler    byte
    consumed  int64
}

func (instance *generatedBodyReader) Read(buffer []byte) (int, error) {
    if 0 == instance.remaining {
        return 0, io.EOF
    }

    limit := int64(len(buffer))
    if instance.remaining < limit {
        limit = instance.remaining
    }

    for index := int64(0); index < limit; index++ {
        buffer[index] = instance.filler
    }

    instance.remaining -= limit
    instance.consumed += limit

    return int(limit), nil
}

/* a reader that only ever returns a legal (0, nil): nothing to deliver, no error, forever */
type stalledBodyReader struct{}

func (instance *stalledBodyReader) Read(buffer []byte) (int, error) {
    return 0, nil
}

func TestValidatedPutBody_StreamsABodyAboveTheSpoolLimitInsteadOfHoldingItInMemory(t *testing.T) {
    /* the natural call is store.Put(runtimeInstance, key, request.Body, request.ContentLength, options) and an http request body cannot seek, so buffering the declared size means a client that uploads gigabytes makes the process attempt an allocation of the same order — an unrecoverable out of memory that takes every other in-flight request with it */
    declared := int64(4 * putSpoolLimit)
    body := &generatedBodyReader{remaining: declared}

    _, streaming, bodyErr := validatedPutBody(context.Background(), "uploads/big.bin", body, declared)
    if nil != bodyErr {
        t.Fatalf("unexpected error: %s", bodyErr.Error())
    }
    if nil == streaming {
        t.Fatalf("a body above the spool limit must be handed to minio as a size-checked stream, not as a spooled buffer")
    }

    if putSpoolLimit < body.consumed {
        t.Fatalf("a body of %d bytes must stream, %d bytes of it were drained into memory before the upload started", declared, body.consumed)
    }
}

func TestValidatedPutBody_GivesUpOnABodyThatNeverYieldsAndNeverFails(t *testing.T) {
    settled := make(chan error, 1)

    go func() {
        _, _, bodyErr := validatedPutBody(context.Background(), "uploads/stalled.bin", &stalledBodyReader{}, 32)
        settled <- bodyErr
    }()

    select {
    case bodyErr := <-settled:
        if nil == bodyErr {
            t.Fatalf("expected a body that never yields and never fails to be rejected")
        }
    case <-time.After(2 * time.Second):
        t.Fatalf("a body that legally returns (0, nil) forever was never given up on: it spins a core with no bound and no deadline")
    }
}

func TestObjectStorage_RejectedPutPreservesTheStoredObject(t *testing.T) {
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

    key := "invoices/2026-07.pdf"
    stored := strings.Repeat("original-invoice-body ", 256)

    putErr := store.Put(runtimeInstance, key, strings.NewReader(stored), int64(len(stored)), storagecontract.PutOptions{ContentType: "application/pdf"})
    if nil != putErr {
        t.Fatalf("put: %v", putErr)
    }

    replacement := strings.Repeat("replacement-body ", 64)

    cases := []struct {
        name   string
        reader io.Reader
    }{
        {name: "seekable", reader: strings.NewReader(replacement)},
        {name: "non-seekable", reader: &sequentialReader{reader: strings.NewReader(replacement)}},
        {name: "non-seekable with zero length reads", reader: &stutteringReader{remaining: replacement}},
    }

    for _, testCase := range cases {
        rejectedErr := store.Put(runtimeInstance, key, testCase.reader, 17, storagecontract.PutOptions{ContentType: "application/pdf"})
        if nil == rejectedErr {
            t.Fatalf("%s: expected a body longer than the declared size to be rejected", testCase.name)
        }

        exists, existsErr := store.Exists(runtimeInstance, key)
        if nil != existsErr || false == exists {
            t.Fatalf("%s: the stored object must survive a rejected put: %v %v", testCase.name, exists, existsErr)
        }

        reader, getErr := store.Get(runtimeInstance, key)
        if nil != getErr {
            t.Fatalf("%s: the stored object must survive a rejected put: %v", testCase.name, getErr)
        }
        loaded, _ := io.ReadAll(reader)
        reader.Close()

        if stored != string(loaded) {
            t.Fatalf("%s: the rejected put replaced the stored object: %d bytes instead of %d", testCase.name, len(loaded), len(stored))
        }
    }

    if deleteErr := store.Delete(runtimeInstance, key); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }
}

func TestObjectStorage_StreamedPutAboveTheSpoolLimitIsAllOrNothing(t *testing.T) {
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

    key := "uploads/large.bin"

    cases := []struct {
        name     string
        declared int64
    }{
        {name: "last part shorter than the part size", declared: putSpoolLimit + 1024},
        /* a declared size that lands exactly on a part boundary is the case an over-read probe run after the upload cannot catch: minio fills its part buffer completely, and a reader error alongside a full buffer is dropped, so the truncated upload would be completed and the key replaced */
        {name: "declared size on a part boundary", declared: 2 * putSpoolLimit},
    }

    for _, testCase := range cases {
        /* above the spool limit nothing is held in memory: the body proves its own size to minio as it is streamed multipart, so an exactly sized one must still arrive whole */
        putErr := store.Put(runtimeInstance, key, &generatedBodyReader{remaining: testCase.declared, filler: 'p'}, testCase.declared, storagecontract.PutOptions{ContentType: "application/octet-stream"})
        if nil != putErr {
            t.Fatalf("%s: put: %v", testCase.name, putErr)
        }

        if storedSize, storedFiller := storedObjectShape(t, store, runtimeInstance, key); testCase.declared != storedSize || 'p' != storedFiller {
            t.Fatalf("%s: expected the whole %d byte streamed body to be stored, got %d bytes of %q", testCase.name, testCase.declared, storedSize, string(storedFiller))
        }

        /* a longer body carrying a different filler is cut off before its last declared byte, which leaves the multipart upload incomplete for minio to abort: the stored object must be untouched, filler included */
        rejectedErr := store.Put(runtimeInstance, key, &generatedBodyReader{remaining: testCase.declared + 4096, filler: 'r'}, testCase.declared, storagecontract.PutOptions{ContentType: "application/octet-stream"})
        if nil == rejectedErr {
            t.Fatalf("%s: expected a streamed body longer than the declared size to be rejected", testCase.name)
        }
        if "storage object size does not match the declared size" != rejectedErr.Error() {
            t.Fatalf("%s: expected a declared size mismatch, got %q", testCase.name, rejectedErr.Error())
        }

        survivedSize, survivedFiller := storedObjectShape(t, store, runtimeInstance, key)
        if testCase.declared != survivedSize || 'p' != survivedFiller {
            t.Fatalf("%s: the rejected streamed put replaced the stored object: %d bytes of %q instead of %d bytes of \"p\"", testCase.name, survivedSize, string(survivedFiller), testCase.declared)
        }
    }

    if deleteErr := store.Delete(runtimeInstance, key); nil != deleteErr {
        t.Fatalf("delete: %v", deleteErr)
    }
}

func storedObjectShape(t *testing.T, store *Storage, runtimeInstance runtimecontract.Runtime, key string) (int64, byte) {
    reader, getErr := store.Get(runtimeInstance, key)
    if nil != getErr {
        t.Fatalf("get: %v", getErr)
    }
    defer reader.Close()

    head := make([]byte, 1)
    if _, headErr := io.ReadFull(reader, head); nil != headErr {
        t.Fatalf("read stored object: %v", headErr)
    }

    tail, copyErr := io.Copy(io.Discard, reader)
    if nil != copyErr {
        t.Fatalf("read stored object: %v", copyErr)
    }

    return tail + 1, head[0]
}

/* a body that cancels the request context once it has delivered part of itself, which is what a client that walks away mid-upload looks like from inside Put */
type disconnectingBodyReader struct {
    remaining   int64
    filler      byte
    cancelAfter int64
    delivered   int64
    cancel      context.CancelFunc
}

func (instance *disconnectingBodyReader) Read(buffer []byte) (int, error) {
    if 0 == instance.remaining {
        return 0, io.EOF
    }

    if instance.cancelAfter <= instance.delivered {
        instance.cancel()

        return 0, context.Canceled
    }

    limit := int64(len(buffer))
    if instance.remaining < limit {
        limit = instance.remaining
    }

    if remainingBeforeCancel := instance.cancelAfter - instance.delivered; remainingBeforeCancel < limit {
        limit = remainingBeforeCancel
    }

    for index := int64(0); index < limit; index++ {
        buffer[index] = instance.filler
    }

    instance.remaining -= limit
    instance.delivered += limit

    return int(limit), nil
}

/* newMultipartAbortServer answers the two requests an abort is made of — the list of incomplete uploads for the bucket and the DELETE that removes one — and records every DELETE it serves, so a test can tell whether the abort actually left the process. */
func newMultipartAbortServer(t *testing.T, uploadKey string, uploadId string) *multipartAbortServer {
    recorder := &multipartAbortServer{}

    recorder.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        query := request.URL.Query()

        if http.MethodDelete == request.Method && "" != query.Get("uploadId") {
            recorder.mutex.Lock()
            recorder.deletedUploadIdList = append(recorder.deletedUploadIdList, query.Get("uploadId"))
            recorder.mutex.Unlock()

            writer.WriteHeader(http.StatusNoContent)

            return
        }

        if _, listsUploads := query["uploads"]; true == listsUploads {
            writer.Header().Set("Content-Type", "application/xml")
            writer.WriteHeader(http.StatusOK)
            _, _ = io.WriteString(writer, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>"+
                "<ListMultipartUploadsResult><Bucket>melody-test</Bucket><IsTruncated>false</IsTruncated>"+
                "<Upload><Key>"+uploadKey+"</Key><UploadId>"+uploadId+"</UploadId></Upload>"+
                "</ListMultipartUploadsResult>")

            return
        }

        writer.WriteHeader(http.StatusOK)
    }))

    t.Cleanup(recorder.server.Close)

    return recorder
}

type multipartAbortServer struct {
    server              *httptest.Server
    mutex               sync.Mutex
    deletedUploadIdList []string
}

func (instance *multipartAbortServer) newStorage(t *testing.T) *Storage {
    client, clientErr := NewClient(Config{
        Endpoint:  strings.TrimPrefix(instance.server.URL, "http://"),
        AccessKey: "test",
        SecretKey: "test",
        Secure:    false,
        Region:    "us-east-1",
    })
    if nil != clientErr {
        t.Fatalf("client: %s", clientErr.Error())
    }

    return NewStorage(client, "melody-test")
}

func (instance *multipartAbortServer) deletedUploadIds() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string(nil), instance.deletedUploadIdList...)
}

/* minio builds its own abort on the context the upload ran under, so a client that disconnects cancels the cleanup along with the upload and the initiated multipart upload survives on the bucket, billing for the parts already sent. The sweep therefore has to run on a context the dead request cannot reach. */
func TestAbortOrphanedMultipartUpload_LeavesEvenWhenTheRequestContextIsAlreadyCancelled(t *testing.T) {
    server := newMultipartAbortServer(t, "uploads/large.bin", "upload-id-42")
    store := server.newStorage(t)

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    store.abortOrphanedMultipartUpload(cancelledContext, "uploads/large.bin", 40*1024*1024)

    deleted := server.deletedUploadIds()
    if 1 != len(deleted) || "upload-id-42" != deleted[0] {
        t.Fatalf("expected the abort of upload-id-42 to reach the bucket on a cancelled request context, got %v", deleted)
    }
}

/* the sweep is only ever needed when the request context is what killed the upload and only ever possible for a body that took the multipart path; anything else must cost the caller no request at all */
func TestAbortOrphanedMultipartUpload_StaysQuietWhenThereIsNothingToSweep(t *testing.T) {
    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    cases := []struct {
        name string
        ctx  context.Context
        size int64
    }{
        {name: "live request context", ctx: context.Background(), size: 40 * 1024 * 1024},
        {name: "declared size on the single-shot path", ctx: cancelledContext, size: putSpoolLimit},
        {name: "empty body", ctx: cancelledContext, size: 0},
    }

    for _, testCase := range cases {
        server := newMultipartAbortServer(t, "uploads/large.bin", "upload-id-42")
        store := server.newStorage(t)

        store.abortOrphanedMultipartUpload(testCase.ctx, "uploads/large.bin", testCase.size)

        if deleted := server.deletedUploadIds(); 0 != len(deleted) {
            t.Fatalf("%s: expected no abort request, got %v", testCase.name, deleted)
        }
    }
}

/* a multipart put whose client disconnects mid-body leaves an initiated upload on the bucket that holds every part already sent and bills for it until a lifecycle rule expires it, which most buckets do not carry; the key must come out of the incomplete upload list even though the request that started it is gone. */
func TestObjectStorage_CancelledMultipartPutLeavesNoIncompleteUpload(t *testing.T) {
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

    key := "uploads/disconnected.bin"

    /* an orphan an earlier run left behind would make the assertion pass or fail for the wrong reason */
    if removeErr := client.RemoveIncompleteUpload(context.Background(), bucket, key); nil != removeErr {
        t.Fatalf("clear incomplete uploads: %v", removeErr)
    }

    if before := countIncompleteUploads(t, client, bucket, key); 0 != before {
        t.Fatalf("precondition: expected no incomplete upload for %q, found %d", key, before)
    }

    declared := int64(40 * 1024 * 1024)

    requestContext, cancelRequest := context.WithCancel(context.Background())
    defer cancelRequest()

    serviceContainer := container.NewContainer()
    runtimeInstance := runtime.New(requestContext, serviceContainer.NewScope(), serviceContainer)

    body := &disconnectingBodyReader{
        remaining:   declared,
        filler:      'd',
        cancelAfter: declared / 2,
        cancel:      cancelRequest,
    }

    putErr := store.Put(runtimeInstance, key, body, declared, storagecontract.PutOptions{ContentType: "application/octet-stream"})
    if nil == putErr {
        t.Fatalf("expected a put whose client disconnected mid-body to fail")
    }

    if after := countIncompleteUploads(t, client, bucket, key); 0 != after {
        t.Fatalf("expected the cancelled multipart put to leave no incomplete upload, found %d", after)
    }
}

func countIncompleteUploads(t *testing.T, client *minio.Client, bucket string, key string) int {
    t.Helper()

    listContext, cancelList := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancelList()

    count := 0
    for upload := range client.ListIncompleteUploads(listContext, bucket, key, true) {
        if nil != upload.Err {
            t.Fatalf("list incomplete uploads: %v", upload.Err)
        }

        if key == upload.Key {
            count++
        }
    }

    return count
}

/* faultAfterPayloadReader yields its payload and then fails every read with the same transport error,
the shape of a connection that breaks exactly at the declared boundary. */
type faultAfterPayloadReader struct {
    payload  []byte
    position int
    fault    error
}

func (instance *faultAfterPayloadReader) Read(buffer []byte) (int, error) {
    if instance.position >= len(instance.payload) {
        return 0, instance.fault
    }

    copied := copy(buffer, instance.payload[instance.position:])
    instance.position += copied

    return copied, nil
}

func TestSizeCheckedReader_SurfacesAProbeFailureAtTheBoundaryInsteadOfFabricatingAnExactMatch(t *testing.T) {
    fault := errors.New("transport broke at the boundary")
    reader := newSizeCheckedReader(context.Background(), "key", &faultAfterPayloadReader{payload: []byte("abcd"), fault: fault}, 4)

    payload, readErr := io.ReadAll(reader)

    if "abcd" != string(payload) {
        t.Fatalf("expected the declared bytes through, got %q", payload)
    }

    if nil == readErr || false == errors.Is(readErr, fault) {
        t.Fatalf("expected the boundary probe failure to surface rather than read as a clean end, got %v", readErr)
    }
}
