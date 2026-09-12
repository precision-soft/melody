package handler

import (
    "bytes"
    "io"
    nethttp "net/http"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodystorage "github.com/precision-soft/melody/v3/storage"
    melodystoragecontract "github.com/precision-soft/melody/v3/storage/contract"
)

/* PlatformCheckHandler answers whether the platform primitives this application depends on are actually working: it takes the distributed lock, refreshes it, then writes, reads back and removes one object in storage.

   It is a readiness probe rather than a liveness one. /health says the process is up; this says the two backends it cannot serve a request without are reachable and behaving, which is the question an operator asks before sending traffic to a new replica. */
func PlatformCheckHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        storageInstance := melodystorage.StorageMustFromContainer(runtimeInstance.Container())
        locker := melodylock.LockerMustFromContainer(runtimeInstance.Container())

        lockInstance := locker.CreateLock("example.platform.check", 5*time.Second)

        acquired, acquireErr := lockInstance.Acquire(runtimeInstance)
        if nil != acquireErr {
            return nil, acquireErr
        }

        if false == acquired {
            return melodyhttp.JsonResponse(nethttp.StatusConflict, map[string]string{"status": "busy"})
        }

        defer lockInstance.Release(runtimeInstance)

        if refreshErr := lockInstance.Refresh(runtimeInstance, 5*time.Second); nil != refreshErr {
            return nil, refreshErr
        }

        key := "example/platform-check.txt"
        payload := []byte("the storage backend accepted a write and returned it")

        if putErr := storageInstance.Put(runtimeInstance, key, bytes.NewReader(payload), int64(len(payload)), melodystoragecontract.PutOptions{ContentType: "text/plain"}); nil != putErr {
            return nil, putErr
        }

        reader, getErr := storageInstance.Get(runtimeInstance, key)
        if nil != getErr {
            return nil, getErr
        }

        stored, readErr := io.ReadAll(reader)
        reader.Close()
        if nil != readErr {
            return nil, readErr
        }

        if deleteErr := storageInstance.Delete(runtimeInstance, key); nil != deleteErr {
            return nil, deleteErr
        }

        return melodyhttp.JsonResponse(nethttp.StatusOK, map[string]any{
            "locked": acquired,
            "stored": string(stored),
        })
    }
}
