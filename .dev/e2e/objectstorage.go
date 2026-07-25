package main

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "time"

    awss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

/* objectStorageBucket is created on demand, so a fresh localstack volume needs no preparation. */
const objectStorageBucket = "melody-e2e"

/* objectStorageStreamingDeclaredSize sits just above minio's 16 MiB part size, the boundary the put's size
check switches on: at or below it the body is spooled and measured in full before any request is issued, above
it the body goes up multipart and has to prove its size as minio consumes it. The second half is the one that
matters here, because it is the only one that reaches the bucket at all. */
const objectStorageStreamingDeclaredSize = 16*1024*1024 + 64*1024

/* opaqueReader hides a body's io.Seeker from the put, which is what an http.Request.Body — the natural
argument of the call — looks like: a seekable body is measured in place before it is uploaded, an opaque one
has to be checked as it is read. */
type opaqueReader struct {
    source io.Reader
}

func (instance *opaqueReader) Read(buffer []byte) (int, error) {
    return instance.source.Read(buffer)
}

/* runObjectStorageCheck exercises the awss3 Put size contract against a live s3: the declared size is
enforced before anything reaches the bucket, so a body that turns out to be longer than its declaration is
refused and whatever the key already held is still there afterwards. That is the data-loss story no fake
backend can tell — an s3 put replaces a key atomically, and on a bucket without versioning nothing brings the
replaced object back — and it is asserted on both halves of the check: the spooled one for a body inside the
part size and the streaming one for a body above it. A negative size still means "unknown length" and is
streamed unchecked. */
func runObjectStorageCheck(endpoint string, accessKey string, secretKey string) {
    client, clientErr := awss3.NewClient(awss3.Config{
        Endpoint:  endpoint,
        AccessKey: accessKey,
        SecretKey: secretKey,
        Secure:    false,
    })
    if nil != clientErr {
        fail("object storage: client: %v", clientErr)
    }

    if ensureErr := awss3.EnsureBucket(context.Background(), client, objectStorageBucket, ""); nil != ensureErr {
        fail("object storage: ensure bucket: %v", ensureErr)
    }

    storage := awss3.NewStorage(client, objectStorageBucket)
    runtimeInstance := newRuntime()

    /* a per-run key namespace so an object left behind by an interrupted run cannot stand in for this run's */
    prefix := fmt.Sprintf("size-contract/%d/", time.Now().UnixNano())

    /* (1) a body that matches its declared size round-trips byte for byte */
    roundTripKey := prefix + "declared-exactly.txt"
    roundTripBody := []byte("the object storage put uploads exactly the size it declared")

    roundTripErr := storage.Put(
        runtimeInstance,
        roundTripKey,
        bytes.NewReader(roundTripBody),
        int64(len(roundTripBody)),
        storagecontract.PutOptions{ContentType: "text/plain"},
    )
    if nil != roundTripErr {
        fail("object storage: put with the exact size: %v", roundTripErr)
    }

    if false == bytes.Equal(roundTripBody, readObject(storage, runtimeInstance, roundTripKey)) {
        fail("object storage: the round-tripped body differs from the one that was put")
    }

    if deleteErr := storage.Delete(runtimeInstance, roundTripKey); nil != deleteErr {
        fail("object storage: delete: %v", deleteErr)
    }

    stillThere, existsErr := storage.Exists(runtimeInstance, roundTripKey)
    if nil != existsErr {
        fail("object storage: exists after delete: %v", existsErr)
    }
    if true == stillThere {
        fail("object storage: the object survived its delete")
    }
    pass("(1) put/get round-trip on live s3 under an exact declared size (%d bytes, gone after the delete)", len(roundTripBody))

    preserved := []byte("the object that was already at the key when the rejected put arrived")

    /* (2) the data-loss regression, spooled path: a declaration shorter than the body must be refused before
       the bucket is touched, so the object already at the key is still the one that is there afterwards */
    spooledKey := prefix + "survivor-spooled.txt"

    seedObject(storage, runtimeInstance, spooledKey, preserved)

    overlongBody := []byte("a body that holds more bytes than the size its caller declared for it")

    spooledErr := storage.Put(
        runtimeInstance,
        spooledKey,
        &opaqueReader{source: bytes.NewReader(overlongBody)},
        int64(len(overlongBody))-1,
        storagecontract.PutOptions{ContentType: "text/plain"},
    )
    if nil == spooledErr {
        fail("object storage: a declared size shorter than the body was accepted on the spooled path")
    }

    if false == bytes.Equal(preserved, readObject(storage, runtimeInstance, spooledKey)) {
        fail("object storage: the refused put destroyed the object already at %q — the pre-existing body is gone or truncated", spooledKey)
    }
    pass("(2) short declared size refused on the spooled path and the pre-existing object survived unchanged (%d bytes declared for %d)", len(overlongBody)-1, len(overlongBody))

    /* (3) the same, above the part size, where the body streams straight into a multipart upload instead of
       being spooled — the only path on which bytes of the rejected body ever reach the bucket */
    streamingKey := prefix + "survivor-streaming.bin"

    seedObject(storage, runtimeInstance, streamingKey, preserved)

    streamingBody := make([]byte, objectStorageStreamingDeclaredSize+32*1024)

    streamingErr := storage.Put(
        runtimeInstance,
        streamingKey,
        &opaqueReader{source: bytes.NewReader(streamingBody)},
        objectStorageStreamingDeclaredSize,
        storagecontract.PutOptions{ContentType: "application/octet-stream"},
    )
    if nil == streamingErr {
        fail("object storage: a declared size shorter than the body was accepted on the streaming path")
    }

    if false == bytes.Equal(preserved, readObject(storage, runtimeInstance, streamingKey)) {
        fail("object storage: the refused multipart put destroyed the object already at %q", streamingKey)
    }
    pass("(3) short declared size refused above the 16 MiB part size and the pre-existing object survived unchanged (%d bytes declared for %d)", objectStorageStreamingDeclaredSize, len(streamingBody))

    /* (4) size = -1 still means "unknown length" and streams the body unchecked */
    unknownLengthKey := prefix + "unknown-length.txt"
    unknownLengthBody := []byte("streamed with no declared length at all, so nothing is enforced")

    unknownLengthErr := storage.Put(
        runtimeInstance,
        unknownLengthKey,
        &opaqueReader{source: bytes.NewReader(unknownLengthBody)},
        -1,
        storagecontract.PutOptions{ContentType: "text/plain"},
    )
    if nil != unknownLengthErr {
        fail("object storage: put with an unknown length: %v", unknownLengthErr)
    }

    if false == bytes.Equal(unknownLengthBody, readObject(storage, runtimeInstance, unknownLengthKey)) {
        fail("object storage: the unchecked stream did not round-trip")
    }
    pass("(4) size = -1 streamed an unchecked body of %d bytes", len(unknownLengthBody))

    for _, key := range []string{spooledKey, streamingKey, unknownLengthKey} {
        if deleteErr := storage.Delete(runtimeInstance, key); nil != deleteErr {
            fail("object storage: clean up %q: %v", key, deleteErr)
        }
    }
}

func seedObject(storage *awss3.Storage, runtimeInstance runtimecontract.Runtime, key string, body []byte) {
    putErr := storage.Put(
        runtimeInstance,
        key,
        bytes.NewReader(body),
        int64(len(body)),
        storagecontract.PutOptions{ContentType: "text/plain"},
    )
    if nil != putErr {
        fail("object storage: seed %q: %v", key, putErr)
    }
}

func readObject(storage *awss3.Storage, runtimeInstance runtimecontract.Runtime, key string) []byte {
    reader, getErr := storage.Get(runtimeInstance, key)
    if nil != getErr {
        fail("object storage: get %q: %v", key, getErr)
    }
    defer reader.Close()

    body, readErr := io.ReadAll(reader)
    if nil != readErr {
        fail("object storage: read %q: %v", key, readErr)
    }

    return body
}
