package awss3

import (
    "bufio"
    "bytes"
    "context"
    "errors"
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

    body, streaming, bodyErr := validatedPutBody(runtimeInstance.Context(), key, reader, size)
    if nil != bodyErr {
        return bodyErr
    }

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

        if nil != streaming && true == streaming.rejected.Load() {
            return declaredSizeMismatchError(key, size)
        }

        return exception.NewError("object storage put failed", map[string]any{"key": key, "bucket": instance.bucket}, putErr)
    }

    return nil
}

const multipartAbortTimeout = 10 * time.Second

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

const putSpoolLimit = 16 * 1024 * 1024

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

const sizeCheckLookahead = 16

const maximumEmptyBodyRead = 100

func newSizeCheckedReader(ctx context.Context, key string, source io.Reader, size int64) *sizeCheckedReader {
    return &sizeCheckedReader{
        key:       key,
        size:      size,
        source:    bufio.NewReaderSize(&contextBoundReader{ctx: ctx, source: source}, sizeCheckLookahead),
        remaining: size,
    }
}

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

func (instance *sizeCheckedReader) endOfBody() error {
    peeked, peekErr := instance.source.Peek(1)
    if 0 < len(peeked) {
        return instance.reject()
    }

    if nil != peekErr && false == errors.Is(peekErr, io.EOF) {
        return peekErr
    }

    return io.EOF
}

func (instance *sizeCheckedReader) reject() error {
    instance.rejected.Store(true)

    return declaredSizeMismatchError(instance.key, instance.size)
}

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
        return nil, exception.NewError("object storage get failed", map[string]any{"key": key, "bucket": instance.bucket}, getErr)
    }

    if _, statErr := object.Stat(); nil != statErr {
        object.Close()

        if "NoSuchKey" == minio.ToErrorResponse(statErr).Code {
            return nil, exception.NewError("object storage object not found", map[string]any{"key": key, "bucket": instance.bucket}, statErr)
        }

        return nil, exception.NewError("object storage get failed", map[string]any{"key": key, "bucket": instance.bucket}, statErr)
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
        return exception.NewError("object storage delete failed", map[string]any{"key": key, "bucket": instance.bucket}, removeErr)
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

    return false, exception.NewError("object storage stat failed", map[string]any{"key": key, "bucket": instance.bucket}, statErr)
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
        return "", exception.NewError("object storage presign failed", map[string]any{"key": key, "bucket": instance.bucket}, presignErr)
    }

    return presigned.String(), nil
}

var _ storagecontract.Storage = (*Storage)(nil)
