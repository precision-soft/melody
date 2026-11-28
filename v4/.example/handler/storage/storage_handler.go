package storage

import (
    "bytes"
    "encoding/base64"
    "errors"
    "io"
    nethttp "net/http"
    "strconv"
    "strings"
    "time"
    "unicode/utf8"

    "github.com/precision-soft/melody/v4/.example/presenter"
    melodybag "github.com/precision-soft/melody/v4/bag"
    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    storagecontract "github.com/precision-soft/melody/v4/storage/contract"
)

/* PutHandler stores the request body under the given key in the object store. The store is a container.Lazy handle on the application storage, resolved at each request, so a storage whose bucket check failed at boot is checked again by the next upload, and one that still cannot be built answers 503. */
func PutHandler(storage *melodycontainer.LazyService[storagecontract.Storage]) melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
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
                /* the kernel bounds the body by the route's own cap; a body past it is the client's too large a payload, answered as the kernel answers it on a bind, not as an unreadable body */
                var maxBytesErr *nethttp.MaxBytesError
                if true == errors.As(readErr, &maxBytesErr) {
                    return presenter.ApiError(runtimeInstance, request, nethttp.StatusRequestEntityTooLarge, "payload too large"), nil
                }

                return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "could not read the request body"), nil
            }

            body = readBytes
        }

        storageInstance, resolveErr := storage.Resolve(runtimeInstance)
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the object storage is unavailable", resolveErr), nil
        }

        putErr := melodystorage.For(runtimeInstance, storageInstance).Put(key, bytes.NewReader(body), int64(len(body)), storagecontract.PutOptions{ContentType: "application/octet-stream"})
        if nil != putErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not store the object", putErr), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"stored": key, "bytes": len(body)}), nil
    }
}

/* GetHandler answers the object stored under the given key: 404 when the store has no such object, 500 with the cause journaled when the store cannot be read. */
func GetHandler(storage *melodycontainer.LazyService[storagecontract.Storage]) melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        key := melodybag.StringOrDefault(request.Query(), "key", "")
        if "" == key {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, "key query parameter is required"), nil
        }

        if true == objectKeyCarriesParentSegment(key) {
            return presenter.ApiError(runtimeInstance, request, nethttp.StatusBadRequest, objectKeyParentSegmentRefusal), nil
        }

        storageInstance, resolveErr := storage.Resolve(runtimeInstance)
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the object storage is unavailable", resolveErr), nil
        }

        object, getErr := melodystorage.For(runtimeInstance, storageInstance).Get(key, storagecontract.GetOptions{})
        if nil != getErr {
            if true == melodyexception.ChainIs(getErr, storagecontract.ErrObjectNotFound) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "object not found"), nil
            }

            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object", getErr), nil
        }
        defer object.Body.Close()

        if objectInlineMaximumBytes < object.Info.Size {
            return objectTooLargeInline(runtimeInstance, request), nil
        }

        /* the read stops one byte past the bound, for a backend that declared less than it serves */
        content, readErr := io.ReadAll(io.LimitReader(object.Body, objectInlineMaximumBytes+1))
        if nil != readErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object", readErr), nil
        }

        if objectInlineMaximumBytes < len(content) {
            return objectTooLargeInline(runtimeInstance, request), nil
        }

        /* a json string carries text: bytes that are not utf-8 would be replaced in the encoding, so such an object is answered in base64 and the encoding names which */
        if true == utf8.Valid(content) {
            return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"key": key, "contentType": object.Info.ContentType, "size": object.Info.Size, "encoding": "utf-8", "content": string(content)}), nil
        }

        return presenter.ApiSuccess(runtimeInstance, request, nethttp.StatusOK, map[string]any{"key": key, "contentType": object.Info.ContentType, "size": object.Info.Size, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(content)}), nil
    }
}

func objectTooLargeInline(runtimeInstance melodycontainercontract.Runtime, request melodyhttpcontract.Request) melodyhttpcontract.Response {
    return presenter.ApiError(runtimeInstance, request, nethttp.StatusUnprocessableEntity, "the object is larger than this door answers inline ("+strconv.Itoa(objectInlineMaximumBytes)+" bytes)")
}

/* objectInlineMaximumBytes bounds the object the get door reads into memory to answer inline */
const objectInlineMaximumBytes = 8 << 20

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
func LinkHandler(storage *melodycontainer.LazyService[storagecontract.Storage]) melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
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

        storageInstance, resolveErr := storage.Resolve(runtimeInstance)
        if nil != resolveErr {
            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusServiceUnavailable, "the object storage is unavailable", resolveErr), nil
        }

        boundStorage := melodystorage.For(runtimeInstance, storageInstance)

        if _, statErr := boundStorage.Stat(key, storagecontract.StatOptions{}); nil != statErr {
            if true == melodyexception.ChainIs(statErr, storagecontract.ErrObjectNotFound) {
                return presenter.ApiError(runtimeInstance, request, nethttp.StatusNotFound, "object not found"), nil
            }

            return presenter.ApiErrorWithErr(runtimeInstance, request, nethttp.StatusInternalServerError, "could not read the object", statErr), nil
        }

        link, presignErr := boundStorage.PresignedUrl(key, storagecontract.PresignConfig{Expiry: melodybound.Of(ttl)})
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
