package handler

import (
    "bytes"
    "io"
    nethttp "net/http"
    "time"

    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodylock "github.com/precision-soft/melody/v4/lock"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    melodystoragecontract "github.com/precision-soft/melody/v4/storage/contract"
)

/* PlatformCheckHandler is a readiness probe: under the distributed lock it writes, reads back and removes one object in storage, answering whether the two backends a request needs are reachable and behaving. */
func PlatformCheckHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodycontainercontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        locker := melodylock.LockerMustFromRuntime(runtimeInstance)

        var stored []byte
        acquired, runErr := melodylock.RunExclusive(
            runtimeInstance,
            locker,
            "example.platform.check",
            melodylock.ExclusiveConfig{Ttl: melodybound.Of(5 * time.Second)},
            func(exclusiveRuntime melodycontainercontract.Runtime) error {
                var checkErr error
                stored, checkErr = checkStorage(melodystorage.FromRuntime(exclusiveRuntime))

                return checkErr
            },
        )
        if nil != runErr {
            return nil, runErr
        }

        if false == acquired {
            return melodyhttp.JsonResponse(nethttp.StatusConflict, map[string]string{"status": "busy"})
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "locked": acquired,
            "stored": string(stored),
        })
    }
}

func checkStorage(boundStorage melodystorage.Bound) ([]byte, error) {
    key := "example/platform-check.txt"
    payload := []byte("the storage backend accepted a write and returned it")

    if putErr := boundStorage.Put(key, bytes.NewReader(payload), int64(len(payload)), melodystoragecontract.PutOptions{ContentType: "text/plain"}); nil != putErr {
        return nil, putErr
    }

    object, getErr := boundStorage.Get(key, melodystoragecontract.GetOptions{})
    if nil != getErr {
        return nil, getErr
    }

    stored, readErr := io.ReadAll(object.Body)
    object.Body.Close()
    if nil != readErr {
        return nil, readErr
    }

    if deleteErr := boundStorage.Delete(key); nil != deleteErr {
        return nil, deleteErr
    }

    return stored, nil
}
