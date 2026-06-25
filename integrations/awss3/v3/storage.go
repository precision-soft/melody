package awss3

import (
    "io"
    "path"
    "strings"
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

    /* @important minio's single-shot putObject wraps an io.ReaderAt+io.Seeker reader (which *bytes.Reader, *strings.Reader and a non-stdio *os.File all satisfy) in an io.SectionReader and uploads the body via ReadAt, which does NOT advance the caller's sequential Read cursor — so probing the original reader afterward would report trailing bytes on every valid Put and wrongly delete the object. Hand minio an io.LimitReader instead: it is neither an io.ReaderAt nor an io.Seeker, so minio takes the sequential path and consumes exactly `size` bytes from `reader`, leaving its cursor advanced by exactly `size`; the cap also guarantees minio can never store more than the declared size on any path (single-shot or multipart). A negative size means "unknown length" and is streamed whole with no cap. */
    _, putErr := instance.client.PutObject(
        runtimeInstance.Context(),
        instance.bucket,
        normalizedKey,
        boundedPutReader(reader, size),
        size,
        minio.PutObjectOptions{ContentType: options.ContentType},
    )
    if nil != putErr {
        return exception.NewError("object storage put failed", map[string]any{"key": key}, putErr)
    }

    /* @important after minio has consumed exactly `size` bytes through the io.LimitReader above, any byte still readable from the original `reader` means the caller declared a size shorter than the body; minio silently ignores the trailing bytes and stores a truncated object reporting success, whereas LocalStorage rejects a reader longer than its declared size, so detect the over-read here and fail — removing the truncated object — to keep the two backends' Put contract identical. */
    if 0 <= size && true == readerHasTrailingBytes(reader) {
        _ = instance.client.RemoveObject(runtimeInstance.Context(), instance.bucket, normalizedKey, minio.RemoveObjectOptions{})

        return exception.NewError(
            "storage object size does not match the declared size",
            map[string]any{"key": key, "declared": size},
            nil,
        )
    }

    return nil
}

/* @important boundedPutReader wraps the body handed to minio in an io.LimitReader capped at the declared size. The wrapper is neither an io.ReaderAt nor an io.Seeker, so it defeats minio's single-shot SectionReader/ReadAt optimization (which would upload via ReadAt without advancing the caller's sequential cursor) and forces the sequential path that consumes exactly `size` bytes straight from the caller's reader — leaving any over-read byte readable from the original for readerHasTrailingBytes. The cap also bounds what minio can store at exactly `size`. A negative size means "unknown length" and is streamed whole with no cap. */
func boundedPutReader(reader io.Reader, size int64) io.Reader {
    if 0 <= size {
        return io.LimitReader(reader, size)
    }

    return reader
}

/* @important readerHasTrailingBytes reports whether the reader still yields data; called after minio has consumed the declared size to detect a body longer than its declared size (which minio silently truncates to size). */
func readerHasTrailingBytes(reader io.Reader) bool {
    var probe [1]byte
    read, _ := reader.Read(probe[:])

    return 0 < read
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
