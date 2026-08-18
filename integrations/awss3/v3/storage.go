package awss3

import (
    "bufio"
    "bytes"
    "context"
    "io"
    "path"
    "strings"
    "sync/atomic"
    "time"

    "github.com/minio/minio-go/v7"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

/* @important normalizes a key the same way LocalStorage does (backslash to slash, clean dot segments, strip the leading slash) so a given key addresses the same object on both backends and a '..' segment cannot produce a presigned URL the client collapses into a different signed path. */
func normalizeObjectKey(key string) (string, error) {
    normalized := strings.ReplaceAll(key, "\\", "/")
    cleaned := strings.TrimPrefix(path.Clean("/"+normalized), "/")

    if "" == cleaned || "." == cleaned {
        return "", exception.NewError("object storage key is empty or invalid", map[string]any{"key": key}, nil)
    }

    return cleaned, nil
}

func NewStorage(client *minio.Client, bucket string) *Storage {
    if nil == client {
        exception.Panic(exception.NewError("object storage client is nil", nil, nil))
    }

    if "" == bucket {
        exception.Panic(exception.NewError("object storage bucket is empty", nil, nil))
    }

    return &Storage{
        client: client,
        bucket: bucket,
    }
}

type Storage struct {
    client *minio.Client
    bucket string
}

func (instance *Storage) Put(
    runtimeInstance runtimecontract.Runtime,
    key string,
    reader io.Reader,
    size int64,
    options storagecontract.PutOptions,
) error {
    normalizedKey, keyErr := normalizeObjectKey(key)
    if nil != keyErr {
        return keyErr
    }

    /* an s3 put replaces the key atomically, so a body longer than its declared size must never reach the end of its upload: minio would store it truncated to the declared size and report success, and on a bucket without versioning nothing brings the replaced object back. Neither the check nor the body is ever held in memory beyond putSpoolLimit. */
    body, streaming, bodyErr := validatedPutBody(runtimeInstance.Context(), key, reader, size)
    if nil != bodyErr {
        return bodyErr
    }

    /* @important minio's single-shot putObject wraps an io.ReaderAt+io.Seeker reader (which *bytes.Reader, *strings.Reader and a non-stdio *os.File all satisfy) in an io.SectionReader and uploads the body via ReadAt. Hand minio an io.LimitReader instead: it is neither an io.ReaderAt nor an io.Seeker, so minio takes the sequential path and consumes exactly `size` bytes from `reader`; the cap also guarantees minio can never store more than the declared size on any path (single-shot or multipart). A negative size means "unknown length" and is streamed whole with no cap. */
    _, putErr := instance.client.PutObject(
        runtimeInstance.Context(),
        instance.bucket,
        normalizedKey,
        boundedPutReader(body, size),
        size,
        minio.PutObjectOptions{ContentType: options.ContentType},
    )
    if nil != putErr {
        instance.abortOrphanedMultipartUpload(runtimeInstance.Context(), normalizedKey, size)

        /* the body cut the upload off itself: report the size mismatch the caller can act on, not the transport failure it surfaced as */
        if nil != streaming && true == streaming.rejected.Load() {
            return declaredSizeMismatchError(key, size)
        }

        return exception.NewError("object storage put failed", map[string]any{"key": key}, putErr)
    }

    return nil
}

/* multipartAbortTimeout bounds the detached abort. The abort is a single DELETE preceded by one list, so a bucket that is answering at all finishes it in milliseconds; the window only has to be long enough to survive one slow round trip, and short enough that an unresponsive bucket cannot hold the caller's goroutine after the request it belonged to is already gone. */
const multipartAbortTimeout = 10 * time.Second

/* abortOrphanedMultipartUpload removes the multipart upload a failed Put leaves behind when the request context is what killed it.

minio aborts a failed multipart upload itself, but it builds that abort on the very context the upload ran under. Once the client disconnects that context is already cancelled, so the abort request is refused before it reaches the network and the initiated upload survives on the bucket, holding every part already sent and billing for them until a lifecycle rule expires it — which most buckets do not carry. The abort therefore runs on a context detached from the request, so a dead request cannot suppress it, and under its own short deadline, so a bucket that never answers cannot hang the caller instead.

Only a body that took the multipart path can leave anything behind: a declared size at or below minio's part size goes up as a single request, which leaves nothing at the key when it fails, while an unknown length always goes multipart. A live request context needs nothing either — minio's own abort went out on it.

The abort is addressed by key rather than by upload id, which minio's PutObject never hands back, so a second upload of the SAME key still in flight is aborted along with this one. Same-key puts already race to last-writer-wins here, and the loser then fails loudly instead of leaving an invisible orphan behind. A failed abort is not reported: the caller is already receiving the put failure, and the orphan it could not remove is the state the caller would have been left in anyway. */
func (instance *Storage) abortOrphanedMultipartUpload(ctx context.Context, normalizedKey string, size int64) {
    if nil == ctx.Err() {
        return
    }

    if 0 <= size && putSpoolLimit >= size {
        return
    }

    abortContext, cancelAbort := context.WithTimeout(context.WithoutCancel(ctx), multipartAbortTimeout)
    defer cancelAbort()

    _ = instance.client.RemoveIncompleteUpload(abortContext, instance.bucket, normalizedKey)
}

/* putSpoolLimit bounds what a Put may ever hold in memory, and is minio's default part size.

That number is the boundary minio itself uses: a body at or below it goes up as one PutObject request, which the bucket commits whole, so such a body is spooled and measured in full before any request is issued. A larger body goes up multipart, where nothing is visible at the key until CompleteMultipartUpload, so it streams straight through and an over-read cuts the upload off for minio to abort. Memory therefore never scales with the body, and the spool never exceeds the part buffer minio would itself allocate for a body one byte larger. */
const putSpoolLimit = 16 * 1024 * 1024

/* validatedPutBody prepares the body handed to minio so that a declared size shorter than the body can never leave a truncated object at the key, which is what makes this backend all-or-nothing like LocalStorage's. It returns the body to upload and, when that body still has to prove its own size as minio consumes it, the reader that will do so.

A seekable body (*bytes.Reader, *strings.Reader, multipart.File, *os.File) is measured in place and streamed as it is, with nothing buffered. A body that cannot seek — an http.Request.Body, the natural argument of this call — is wrapped in a reader that refuses to hand over the last declared byte once it finds anything behind it; up to putSpoolLimit that reader is drained first, so an over-sized body is rejected before the bucket is touched at all. A negative size means "unknown length" and is streamed unvalidated. A body SHORTER than the declared size is left to minio, which refuses it. */
func validatedPutBody(ctx context.Context, key string, reader io.Reader, size int64) (io.Reader, *sizeCheckedReader, error) {
    if 0 > size {
        return reader, nil, nil
    }

    if seeker, isSeeker := reader.(io.Seeker); true == isSeeker {
        remaining, measured, seekErr := seekableRemainingLength(seeker)
        if nil != seekErr {
            return nil, nil, exception.NewError("object storage put failed", map[string]any{"key": key}, seekErr)
        }

        if true == measured {
            if size < remaining {
                return nil, nil, declaredSizeMismatchError(key, size)
            }

            return reader, nil, nil
        }
    }

    checked := newSizeCheckedReader(ctx, key, reader, size)

    if putSpoolLimit < size {
        return checked, checked, nil
    }

    var spooled bytes.Buffer

    if _, copyErr := io.Copy(&spooled, checked); nil != copyErr {
        if true == checked.rejected.Load() {
            return nil, nil, copyErr
        }

        return nil, nil, exception.NewError("object storage put failed", map[string]any{"key": key}, copyErr)
    }

    return bytes.NewReader(spooled.Bytes()), nil, nil
}

/* sizeCheckLookahead is the smallest buffer bufio accepts and all the lookahead a size check needs: one byte past the declared size. A read larger than that buffer goes straight into the caller's buffer, so a streaming body is never copied through it. */
const sizeCheckLookahead = 16

/* maximumEmptyBodyRead bounds consecutive (0, nil) reads from a body. Such a read is legal and only means nothing happened, but a reader that returns it forever would otherwise spin a core with no bound and no deadline, so the body is declared stalled instead. */
const maximumEmptyBodyRead = 100

func newSizeCheckedReader(ctx context.Context, key string, source io.Reader, size int64) *sizeCheckedReader {
    return &sizeCheckedReader{
        key:       key,
        size:      size,
        source:    bufio.NewReaderSize(&contextBoundReader{ctx: ctx, source: source}, sizeCheckLookahead),
        remaining: size,
    }
}

/* sizeCheckedReader yields exactly the declared number of bytes and stops one byte short of it the moment the body turns out to hold more.

Stopping short is what keeps the put all-or-nothing without buffering: the upload is then a byte short of the content length it announced, so a single-shot request is refused by the bucket and a multipart upload is left incomplete, and neither leaves anything at the key. Nothing visible, that is — an incomplete multipart upload still holds its parts on the bucket until it is aborted, and minio only manages that while the request context is alive, which is why Put sweeps the key itself when the context is what failed. Every read is bounded and honours the context, so neither a stalled body nor a client that walked away can pin an upload. */
type sizeCheckedReader struct {
    key        string
    size       int64
    source     *bufio.Reader
    remaining  int64
    emptyReads int
    rejected   atomic.Bool
}

func (instance *sizeCheckedReader) Read(buffer []byte) (int, error) {
    if 0 == len(buffer) {
        return 0, nil
    }

    if 0 == instance.remaining {
        return 0, instance.endOfBody()
    }

    if 1 == instance.remaining {
        if guardErr := instance.guardLastByte(); nil != guardErr {
            return 0, guardErr
        }
    }

    limit := instance.remaining
    if 1 < limit {
        limit--
    }

    if int64(len(buffer)) < limit {
        limit = int64(len(buffer))
    }

    read, readErr := instance.source.Read(buffer[:limit])
    instance.remaining -= int64(read)

    if 0 < read || nil != readErr {
        instance.emptyReads = 0

        return read, readErr
    }

    instance.emptyReads++
    if maximumEmptyBodyRead <= instance.emptyReads {
        return 0, exception.NewError("object storage body stalled", map[string]any{"key": instance.key}, nil)
    }

    return 0, nil
}

/* @important guardLastByte withholds the final declared byte until the body is known to end there. Withholding it IS the atomicity mechanism: an over-read leaves the upload short of the content length it announced, so a single-shot put is refused by the bucket and a multipart upload is aborted, and nothing ever appears at the key. It reads like an off-by-one and is not one. A peek that comes up empty is the body ending early, which minio refuses on its own. */
func (instance *sizeCheckedReader) guardLastByte() error {
    peeked, peekErr := instance.source.Peek(2)
    if 1 < len(peeked) {
        return instance.reject()
    }

    if 0 == len(peeked) {
        return peekErr
    }

    return nil
}

/* endOfBody reports the body's end, or the size mismatch when it still yields data past the declared size — the state a zero-sized declaration lands in immediately. */
func (instance *sizeCheckedReader) endOfBody() error {
    if peeked, _ := instance.source.Peek(1); 0 < len(peeked) {
        return instance.reject()
    }

    return io.EOF
}

func (instance *sizeCheckedReader) reject() error {
    instance.rejected.Store(true)

    return declaredSizeMismatchError(instance.key, instance.size)
}

/* contextBoundReader stops feeding a body once the runtime context is done, so a client that stalls mid-upload cannot pin the spool or the upload past the request it belongs to. */
type contextBoundReader struct {
    ctx    context.Context
    source io.Reader
}

func (instance *contextBoundReader) Read(buffer []byte) (int, error) {
    if ctxErr := instance.ctx.Err(); nil != ctxErr {
        return 0, ctxErr
    }

    return instance.source.Read(buffer)
}

/* seekableRemainingLength measures the rest of a seekable body without reading it: capture the offset, probe the end, restore the offset. A seek that fails leaves the offset untouched, so a reader that only pretends to seek reports not measured and the caller falls back to checking the size as the body is read; a failed restore is fatal instead, because the body would otherwise be uploaded from the wrong offset. */
func seekableRemainingLength(seeker io.Seeker) (int64, bool, error) {
    current, currentErr := seeker.Seek(0, io.SeekCurrent)
    if nil != currentErr {
        return 0, false, nil
    }

    end, endErr := seeker.Seek(0, io.SeekEnd)
    if nil != endErr {
        return 0, false, nil
    }

    if _, restoreErr := seeker.Seek(current, io.SeekStart); nil != restoreErr {
        return 0, false, restoreErr
    }

    if end < current {
        return 0, false, nil
    }

    return end - current, true, nil
}

func declaredSizeMismatchError(key string, size int64) error {
    return exception.NewError(
        "storage object size does not match the declared size",
        map[string]any{"key": key, "declared": size},
        nil,
    )
}

/* @important boundedPutReader wraps the body handed to minio in an io.LimitReader capped at the declared size. The wrapper is neither an io.ReaderAt nor an io.Seeker, so it defeats minio's single-shot SectionReader/ReadAt optimization and forces the sequential path, which consumes the body one part buffer at a time and lets a size-checked body cut the upload off mid-flight. The cap also bounds what minio can store at exactly `size`. A negative size means "unknown length" and is streamed whole with no cap. */
func boundedPutReader(reader io.Reader, size int64) io.Reader {
    if 0 <= size {
        return io.LimitReader(reader, size)
    }

    return reader
}

func (instance *Storage) Get(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (io.ReadCloser, error) {
    normalizedKey, keyErr := normalizeObjectKey(key)
    if nil != keyErr {
        return nil, keyErr
    }

    object, getErr := instance.client.GetObject(runtimeInstance.Context(), instance.bucket, normalizedKey, minio.GetObjectOptions{})
    if nil != getErr {
        return nil, exception.NewError("object storage get failed", map[string]any{"key": key}, getErr)
    }

    if _, statErr := object.Stat(); nil != statErr {
        object.Close()

        if "NoSuchKey" == minio.ToErrorResponse(statErr).Code {
            return nil, exception.NewError("object storage object not found", map[string]any{"key": key}, statErr)
        }

        return nil, exception.NewError("object storage get failed", map[string]any{"key": key}, statErr)
    }

    return object, nil
}

func (instance *Storage) Delete(
    runtimeInstance runtimecontract.Runtime,
    key string,
) error {
    normalizedKey, keyErr := normalizeObjectKey(key)
    if nil != keyErr {
        return keyErr
    }

    removeErr := instance.client.RemoveObject(runtimeInstance.Context(), instance.bucket, normalizedKey, minio.RemoveObjectOptions{})
    if nil != removeErr {
        return exception.NewError("object storage delete failed", map[string]any{"key": key}, removeErr)
    }

    return nil
}

func (instance *Storage) Exists(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (bool, error) {
    normalizedKey, keyErr := normalizeObjectKey(key)
    if nil != keyErr {
        return false, keyErr
    }

    _, statErr := instance.client.StatObject(runtimeInstance.Context(), instance.bucket, normalizedKey, minio.StatObjectOptions{})
    if nil == statErr {
        return true, nil
    }

    if "NoSuchKey" == minio.ToErrorResponse(statErr).Code {
        return false, nil
    }

    return false, exception.NewError("object storage stat failed", map[string]any{"key": key}, statErr)
}

func (instance *Storage) PresignedUrl(
    runtimeInstance runtimecontract.Runtime,
    key string,
    expiry time.Duration,
) (string, error) {
    normalizedKey, keyErr := normalizeObjectKey(key)
    if nil != keyErr {
        return "", keyErr
    }

    presigned, presignErr := instance.client.PresignedGetObject(runtimeInstance.Context(), instance.bucket, normalizedKey, expiry, nil)
    if nil != presignErr {
        return "", exception.NewError("object storage presign failed", map[string]any{"key": key}, presignErr)
    }

    return presigned.String(), nil
}

var _ storagecontract.Storage = (*Storage)(nil)
