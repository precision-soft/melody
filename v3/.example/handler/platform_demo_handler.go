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

/**
 * PlatformDemoHandler exercises the lock and storage modules end to end: it takes a named lock, writes,
 * reads back and deletes an object, then releases the lock. It exists so the example build matrix wires
 * and runs both modules. In a load-balanced deployment swap the in-memory locker for a Redis-backed one.
 */
func PlatformDemoHandler() melodyhttpcontract.Handler {
    return func(runtimeInstance melodyruntimecontract.Runtime, writer nethttp.ResponseWriter, request melodyhttpcontract.Request) (melodyhttpcontract.Response, error) {
        storageInstance := melodystorage.StorageMustFromContainer(runtimeInstance.Container())
        locker := melodylock.LockerMustFromContainer(runtimeInstance.Container())

        lockInstance := locker.CreateLock("example.platform.demo", 5*time.Second)

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

        key := "example/platform-demo.txt"
        payload := []byte("hello from the platform demo")

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
