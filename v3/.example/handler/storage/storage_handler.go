package storage

import (
    "bytes"
    "errors"
    "io"
    nethttp "net/http"
    "strconv"
    "strings"
    "time"

    melodyawss3 "github.com/precision-soft/melody/integrations/awss3/v3"
    "github.com/precision-soft/melody/v3/.example/presenter"
    melodybag "github.com/precision-soft/melody/v3/bag"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    storagecontract "github.com/precision-soft/melody/v3/storage/contract"
)

/* PutHandler stores the request body under the given key in the object store. */
func PutHandler(storage *melodyawss3.Storage) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        key := melodybag.StringOrDefault(request.Query(), "key", "")
        if "" == key {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "key query parameter is required"), nil
        }

        if true == objectKeyCarriesParentSegment(key) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, objectKeyParentSegmentRefusal), nil
        }

        body := []byte{}
        if httpRequest := request.HttpRequest(); nil != httpRequest && nil != httpRequest.Body {
            readBytes, readErr := io.ReadAll(httpRequest.Body)
            if nil != readErr {
                /* the kernel bounds every body by MELODY_HTTP_MAX_REQUEST_BODY_BYTES; a body past it is the client's too large a payload, answered as the kernel answers it on a bind, not as an unreadable body */
                var maxBytesErr *nethttp.MaxBytesError
                if true == errors.As(readErr, &maxBytesErr) {
                    return presenter.ApiError(runtimeInstance, request, nethttp.StatusRequestEntityTooLarge, "payload too large"), nil
                }

                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "could not read the request body"), nil
            }

            body = readBytes
        }

        putErr := storage.Put(runtimeInstance, key, bytes.NewReader(body), int64(len(body)), storagecontract.PutOptions{ContentType: "application/octet-stream"})
        if nil != putErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not store the object", putErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"stored": key, "bytes": len(body)}), nil
    }
}

/* GetHandler answers the object stored under the given key: 404 when the store has no such object, 500 with the cause journaled when the store cannot be read. */
func GetHandler(storage *melodyawss3.Storage) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        key := melodybag.StringOrDefault(request.Query(), "key", "")
        if "" == key {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "key query parameter is required"), nil
        }

        if true == objectKeyCarriesParentSegment(key) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, objectKeyParentSegmentRefusal), nil
        }

        exists, existsErr := storage.Exists(runtimeInstance, key)
        if nil != existsErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object", existsErr), nil
        }

        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "object not found"), nil
        }

        /* the object was there a statement ago, so a get that fails now is a failure of the store, not an absence */
        reader, getErr := storage.Get(runtimeInstance, key)
        if nil != getErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object", getErr), nil
        }
        defer reader.Close()

        content, readErr := io.ReadAll(reader)
        if nil != readErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object", readErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"key": key, "content": string(content)}), nil
    }
}

const (
    /* linkDefaultTtl is how long a link lives when the caller asked for nothing */
    linkDefaultTtl = 5 * time.Minute

    /* linkMaximumTtl bounds what a caller may ask for: a link is a bearer credential to the object for as long as it lives */
    linkMaximumTtl = time.Hour
)

/* objectKeyParentSegmentRefusal is the answer to a key the object store refuses: a ".." segment would climb out of the prefix the key names, so the store refuses it by name and this door answers it as the client's error rather than as a failure of the store */
const objectKeyParentSegmentRefusal = `key may not carry a ".." segment`

func objectKeyCarriesParentSegment(key string) bool {
    for _, segment := range strings.Split(strings.ReplaceAll(key, "\\", "/"), "/") {
        if ".." == segment {
            return true
        }
    }

    return false
}

/* errInvalidLinkTtl is the one refusal of every ttl this door cannot serve */
var errInvalidLinkTtl = errors.New("ttl must be a whole number of seconds between 1 and 3600")

/* LinkHandler answers a presigned download link to the object stored under the given key, so a client reads the bytes straight from the object store instead of through this application. The object must exist, and the link lives for ?ttl= seconds, five minutes when none is asked. */
func LinkHandler(storage *melodyawss3.Storage) melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        key := melodybag.StringOrDefault(request.Query(), "key", "")
        if "" == key {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "key query parameter is required"), nil
        }

        if true == objectKeyCarriesParentSegment(key) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, objectKeyParentSegmentRefusal), nil
        }

        ttl, ttlErr := linkTtlOf(request)
        if nil != ttlErr {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, ttlErr.Error()), nil
        }

        exists, existsErr := storage.Exists(runtimeInstance, key)
        if nil != existsErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object", existsErr), nil
        }

        if false == exists {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "object not found"), nil
        }

        link, presignErr := storage.PresignedUrl(runtimeInstance, key, ttl)
        if nil != presignErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not sign the link", presignErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{
            "key":       key,
            "url":       link,
            "expiresIn": int(ttl / time.Second),
        }), nil
    }
}

/* linkTtlOf reads the caller's ttl in seconds, the first value of a repeated key, or answers the default */
func linkTtlOf(request melodyhttpcontract.Request) (time.Duration, error) {
    raw := melodybag.StringOrDefault(request.Query(), "ttl", "")
    if "" == raw {
        return linkDefaultTtl, nil
    }

    seconds, parseErr := strconv.Atoi(raw)
    if nil != parseErr || 0 >= seconds {
        return 0, errInvalidLinkTtl
    }

    /* the bound is compared on the seconds, before the multiplication, which would wrap past it */
    if int(linkMaximumTtl/time.Second) < seconds {
        return 0, errInvalidLinkTtl
    }

    return time.Duration(seconds) * time.Second, nil
}

