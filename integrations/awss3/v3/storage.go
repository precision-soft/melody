package awss3

import (
    "io"
    "time"

    "github.com/minio/minio-go/v7"

    "github.com/precision-soft/melody/v3/exception"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

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
    _, putErr := instance.client.PutObject(
        runtimeInstance.Context(),
        instance.bucket,
        key,
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
    object, getErr := instance.client.GetObject(runtimeInstance.Context(), instance.bucket, key, minio.GetObjectOptions{})
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
    removeErr := instance.client.RemoveObject(runtimeInstance.Context(), instance.bucket, key, minio.RemoveObjectOptions{})
    if nil != removeErr {
        return exception.NewError("object storage delete failed", map[string]any{"key": key}, removeErr)
    }

    return nil
}

func (instance *Storage) Exists(
    runtimeInstance runtimecontract.Runtime,
    key string,
) (bool, error) {
    _, statErr := instance.client.StatObject(runtimeInstance.Context(), instance.bucket, key, minio.StatObjectOptions{})
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
    presigned, presignErr := instance.client.PresignedGetObject(runtimeInstance.Context(), instance.bucket, key, expiry, nil)
    if nil != presignErr {
        return "", exception.NewError("object storage presign failed", map[string]any{"key": key}, presignErr)
    }

    return presigned.String(), nil
}

var _ storagecontract.Storage = (*Storage)(nil)
