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

/** @important normalizes a key the same way LocalStorage does (backslash to slash, clean dot segments, strip the leading slash) so a given key addresses the same object on both backends and a '..' segment cannot produce a presigned URL the client collapses into a different signed path. */
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

    _, putErr := instance.client.PutObject(
        runtimeInstance.Context(),
        instance.bucket,
        normalizedKey,
        reader,
        size,
        minio.PutObjectOptions{ContentType: options.ContentType},
    )
    if nil != putErr {
        return exception.NewError("object storage put failed", map[string]any{"key": key}, putErr)
    }

    return nil
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
