package handler

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
    "time"

    melodybound "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodylock "github.com/precision-soft/melody/v4/lock"
    melodylockcontract "github.com/precision-soft/melody/v4/lock/contract"
    melodystorage "github.com/precision-soft/melody/v4/storage"
    melodystoragecontract "github.com/precision-soft/melody/v4/storage/contract"
)

func TestPlatformCheck_AnswersTheStoredObjectUnderTheLock(t *testing.T) {
    locker := melodylock.NewInMemoryLocker(melodylock.InMemoryLockerConfig{})
    runtimeInstance, _ := newPlatformCheckRuntime(t, locker)

    response := servePlatformCheck(t, runtimeInstance)
    if nethttp.StatusOK != response.StatusCode() {
        t.Fatalf("expected 200, got %d", response.StatusCode())
    }

    body := bodyOf(t, response.BodyReader())
    if false == strings.Contains(body, `"locked":true`) || false == strings.Contains(body, "the storage backend accepted a write and returned it") {
        t.Fatalf("expected the lock and the stored object in the answer, got %s", body)
    }

    assertPlatformCheckLockIsFree(t, locker)
}

func TestPlatformCheck_ReportsTheLockHeldElsewhere(t *testing.T) {
    locker := melodylock.NewInMemoryLocker(melodylock.InMemoryLockerConfig{})
    runtimeInstance, _ := newPlatformCheckRuntime(t, locker)

    holder, createErr := locker.CreateLock("example.platform.check", melodylockcontract.CreateLockOptions{Ttl: melodybound.Of(time.Minute)})
    if nil != createErr {
        t.Fatalf("create the holder's lock: %v", createErr)
    }

    if acquired, acquireErr := holder.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the holder to take the lock, got %v (%v)", acquired, acquireErr)
    }

    response := servePlatformCheck(t, runtimeInstance)
    if nethttp.StatusConflict != response.StatusCode() {
        t.Fatalf("expected 409 while the lock is held elsewhere, got %d", response.StatusCode())
    }

    if body := bodyOf(t, response.BodyReader()); false == strings.Contains(body, `"busy"`) {
        t.Fatalf("expected the busy status, got %s", body)
    }
}

func TestPlatformCheck_ReleasesAfterACancelledRequest(t *testing.T) {
    inMemoryLocker := melodylock.NewInMemoryLocker(melodylock.InMemoryLockerConfig{})
    locker := &cancellingLocker{locker: inMemoryLocker}
    runtimeInstance, cancelRequest := newPlatformCheckRuntime(t, locker)
    locker.cancelRequest = cancelRequest

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/platform/check", nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("platform-check-test", time.Now())})
    _, _ = PlatformCheckHandler()(runtimeInstance, httptest.NewRecorder(), request)

    if nil == runtimeInstance.Context().Err() {
        t.Fatalf("expected the request cancelled while the lock was held")
    }

    if false == locker.releasedUnderALiveContext() {
        t.Fatalf("expected the release handed a context the request's cancellation does not reach")
    }

    assertPlatformCheckLockIsFree(t, inMemoryLocker)
}

func newPlatformCheckRuntime(t *testing.T, locker melodylockcontract.Locker) (melodycontainercontract.Runtime, context.CancelFunc) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    melodycontainer.MustRegister[melodylockcontract.Locker](
        containerInstance,
        melodylock.ServiceLocker,
        func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            return locker, nil
        },
        melodycontainer.RegisterOptions{},
    )

    storageInstance := melodystorage.NewLocalStorage(t.TempDir(), melodystorage.LocalStorageConfig{})
    melodycontainer.MustRegister[melodystoragecontract.Storage](
        containerInstance,
        melodystorage.ServiceStorage,
        func(resolver melodycontainercontract.Resolver) (melodystoragecontract.Storage, error) {
            return storageInstance, nil
        },
        melodycontainer.RegisterOptions{},
    )

    requestContext, cancel := context.WithCancel(context.Background())
    t.Cleanup(cancel)

    return melodyruntime.NewRequest(requestContext, containerInstance.NewScope()), cancel
}

func servePlatformCheck(t *testing.T, runtimeInstance melodycontainercontract.Runtime) *melodyhttp.Response {
    t.Helper()

    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodGet, "/platform/check", nil), runtimeInstance, melodyhttp.RequestConfig{RequestContext: melodyhttp.NewRequestContext("platform-check-test", time.Now())})

    response, handlerErr := PlatformCheckHandler()(runtimeInstance, httptest.NewRecorder(), request)
    if nil != handlerErr {
        t.Fatalf("expected the platform check to answer, got %v", handlerErr)
    }

    typed, isTyped := response.(*melodyhttp.Response)
    if false == isTyped {
        t.Fatalf("expected an http response, got %T", response)
    }

    return typed
}

func assertPlatformCheckLockIsFree(t *testing.T, locker melodylockcontract.Locker) {
    t.Helper()

    probe, createErr := locker.CreateLock("example.platform.check", melodylockcontract.CreateLockOptions{Ttl: melodybound.Of(time.Minute)})
    if nil != createErr {
        t.Fatalf("create the probe lock: %v", createErr)
    }

    runtimeInstance := melodyruntime.NewRequest(context.Background(), melodycontainer.NewContainer(melodycontainer.ContainerConfig{}).NewScope())
    if acquired, acquireErr := probe.Acquire(runtimeInstance); nil != acquireErr || false == acquired {
        t.Fatalf("expected the platform check to have released its lock, got %v (%v)", acquired, acquireErr)
    }
}

/* cancellingLocker hangs the client up once the lock is taken, and, as a networked backend does, fails a release handed a context already done. */
type cancellingLocker struct {
    locker        melodylockcontract.Locker
    cancelRequest context.CancelFunc
    mutex         sync.Mutex
    liveRelease   bool
}

func (instance *cancellingLocker) CreateLock(name string, options melodylockcontract.CreateLockOptions) (melodylockcontract.Lock, error) {
    lock, createErr := instance.locker.CreateLock(name, options)
    if nil != createErr {
        return nil, createErr
    }

    return &cancellingLock{lock: lock, owner: instance}, nil
}

func (instance *cancellingLocker) releasedUnderALiveContext() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.liveRelease
}

type cancellingLock struct {
    lock  melodylockcontract.Lock
    owner *cancellingLocker
}

func (instance *cancellingLock) Acquire(runtimeInstance melodycontainercontract.Runtime) (bool, error) {
    acquired, acquireErr := instance.lock.Acquire(runtimeInstance)
    if true == acquired {
        instance.owner.cancelRequest()
    }

    return acquired, acquireErr
}

func (instance *cancellingLock) Release(runtimeInstance melodycontainercontract.Runtime) error {
    if contextErr := runtimeInstance.Context().Err(); nil != contextErr {
        return contextErr
    }

    instance.owner.mutex.Lock()
    instance.owner.liveRelease = true
    instance.owner.mutex.Unlock()

    return instance.lock.Release(runtimeInstance)
}

func (instance *cancellingLock) Refresh(runtimeInstance melodycontainercontract.Runtime, options melodylockcontract.RefreshOptions) error {
    return instance.lock.Refresh(runtimeInstance, options)
}
