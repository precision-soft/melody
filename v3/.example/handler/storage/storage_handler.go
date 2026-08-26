package storage

import (
    "bytes"
    "io"
    nethttp "net/http"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    "github.com/precision-soft/melody/v3/.example/presenter"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

/* PutHandler stores the request body under the given key in the object store (localstack S3 in dev), demonstrating the awss3 integration's Put over real HTTP. */
func PutHandler(storage *melodyawss3.Storage) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        key := queryString(request, "key")
        if "" == key {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "key query parameter is required"), nil
        }

        body := []byte{}
        if httpRequest := request.HttpRequest(); nil != httpRequest && nil != httpRequest.Body {
            readBytes, readErr := io.ReadAll(httpRequest.Body)
            if nil != readErr {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "could not read the request body"), nil
            }

            body = readBytes
        }

        putErr := storage.Put(runtimeInstance, key, bytes.NewReader(body), int64(len(body)), storagecontract.PutOptions{ContentType: "application/octet-stream"})
        if nil != putErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not store the object"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"stored": key, "bytes": len(body)}), nil
    }
}

/* GetHandler retrieves the object stored under the given key, demonstrating the awss3 integration's Get. */
func GetHandler(storage *melodyawss3.Storage) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        key := queryString(request, "key")
        if "" == key {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "key query parameter is required"), nil
        }

        reader, getErr := storage.Get(runtimeInstance, key)
        if nil != getErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "object not found"), nil
        }
        defer reader.Close()

        content, readErr := io.ReadAll(reader)
        if nil != readErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object"), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"key": key, "content": string(content)}), nil
    }
}

/* queryString reads a query parameter as a string, handling the bag's []string storage. */
func queryString(request melodyhttpcontract.Request, name string) string {
    value, exists := request.Query().Get(name)
    if false == exists {
        return ""
    }

    switch typed := value.(type) {
    case string:
        return typed
    case []string:
        if 0 == len(typed) {
            return ""
        }

        return typed[0]
    default:
        return ""
    }
}
