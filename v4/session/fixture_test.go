package session

import (
    "context"
    "errors"
    "os"
    "sync"
    "syscall"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/clock"
    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    container "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    runtime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

func assertSessionFieldPanic(t *testing.T, expectedMessage string, expectedField string, callback func()) {
    t.Helper()

    defer func() {
        t.Helper()

        recoveredErr, isError := recover().(error)
        if false == isError || expectedMessage != recoveredErr.Error() {
            t.Fatalf("expected %q to be refused, got %v", expectedMessage, recoveredErr)
        }

        if expectedField != exception.LogContext(recoveredErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the refusal to name %s, got %v", expectedField, exception.LogContext(recoveredErr, exception.LogContextConfig{}))
        }
    }()

    callback()
}

type foreignSession struct {
    id       string
    values   map[string]any
    modified bool
    cleared  bool
}

func (instance *foreignSession) Id() string { return instance.id }

func (instance *foreignSession) Get(key string) any { return instance.values[key] }

func (instance *foreignSession) String(key string) string {
    value, _ := instance.values[key].(string)

    return value
}

func (instance *foreignSession) Set(key string, value any) {
    instance.values[key] = value
    instance.modified = true
}

func (instance *foreignSession) Has(key string) bool {
    _, exists := instance.values[key]

    return exists
}

func (instance *foreignSession) Delete(key string) { delete(instance.values, key) }

func (instance *foreignSession) Clear() { instance.cleared = true }

func (instance *foreignSession) All() map[string]any {
    values := make(map[string]any, len(instance.values))
    for key, value := range instance.values {
        values[key] = value
    }

    return values
}

func (instance *foreignSession) IsModified() bool { return instance.modified }

func (instance *foreignSession) IsCleared() bool { return instance.cleared }

func (instance *foreignSession) Snapshot() (map[string]any, bool, bool) {
    return instance.All(), instance.modified, instance.cleared
}

const refusingCharacterDevice = "/dev/zero"

func mustSession(sessionInstance sessioncontract.Session, sessionErr error) sessioncontract.Session {
    if nil != sessionErr {
        panic(sessionErr)
    }

    return sessionInstance
}

func (instance *Manager) isTombstoned(sessionId string) bool {
    instance.tombstoneMutex.Lock()
    defer instance.tombstoneMutex.Unlock()

    return instance.isTombstonedLocked(sessionId)
}

/* contextlessSessionRuntime carries no context, which no constructor of the runtime package builds */
type contextlessSessionRuntime struct {
    containercontract.Runtime
}

func (instance *contextlessSessionRuntime) Context() context.Context {
    return nil
}

const refusedRuntimeMessage = "the runtime is nil or carries no context"

func refusedSessionRuntimes() []containercontract.Runtime {
    var typedNil *contextlessSessionRuntime

    return []containercontract.Runtime{nil, typedNil, &contextlessSessionRuntime{}}
}

func cancelledContext() context.Context {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    return ctx
}

type contextBlockingStorage struct {
    inner       *InMemoryStorage
    blockLoad   bool
    blockSave   bool
    blockDelete bool
    entered     chan string
    mutex       sync.Mutex
    doors       []string
}

func newContextBlockingStorage() *contextBlockingStorage {
    return &contextBlockingStorage{
        inner:   NewInMemoryStorage(InMemoryStorageConfig{}),
        entered: make(chan string, 16),
    }
}

func (instance *contextBlockingStorage) record(door string) {
    instance.mutex.Lock()
    instance.doors = append(instance.doors, door)
    instance.mutex.Unlock()
}

func (instance *contextBlockingStorage) recordedDoors() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.doors...)
}

func (instance *contextBlockingStorage) block(ctx context.Context, door string) error {
    instance.entered <- door
    <-ctx.Done()

    return ctx.Err()
}

func (instance *contextBlockingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    ctx := runtimeInstance.Context()
    instance.record("Load")

    if true == instance.blockLoad {
        return nil, false, instance.block(ctx, "Load")
    }

    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *contextBlockingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    ctx := runtimeInstance.Context()
    instance.record("Save")

    if true == instance.blockSave {
        return instance.block(ctx, "Save")
    }

    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *contextBlockingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    ctx := runtimeInstance.Context()
    instance.record("Delete")

    if true == instance.blockDelete {
        return instance.block(ctx, "Delete")
    }

    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *contextBlockingStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(closeContext)
}

type contextRecordingStorage struct {
    inner    *InMemoryStorage
    mutex    sync.Mutex
    contexts map[string][]context.Context
}

func newContextRecordingStorage() *contextRecordingStorage {
    return &contextRecordingStorage{
        inner:    NewInMemoryStorage(InMemoryStorageConfig{}),
        contexts: make(map[string][]context.Context),
    }
}

func (instance *contextRecordingStorage) record(door string, ctx context.Context) {
    instance.mutex.Lock()
    instance.contexts[door] = append(instance.contexts[door], ctx)
    instance.mutex.Unlock()
}

func (instance *contextRecordingStorage) recorded(door string) []context.Context {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]context.Context{}, instance.contexts[door]...)
}

func (instance *contextRecordingStorage) Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    ctx := runtimeInstance.Context()
    instance.record("Load", ctx)

    return instance.inner.Load(runtimeInstance, sessionId)
}

func (instance *contextRecordingStorage) Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error {
    ctx := runtimeInstance.Context()
    instance.record("Save", ctx)

    return instance.inner.Save(runtimeInstance, sessionId, data, ttl)
}

func (instance *contextRecordingStorage) Delete(runtimeInstance containercontract.Runtime, sessionId string) error {
    ctx := runtimeInstance.Context()
    instance.record("Delete", ctx)

    return instance.inner.Delete(runtimeInstance, sessionId)
}

func (instance *contextRecordingStorage) CloseWithContext(closeContext context.Context) error {
    return instance.inner.CloseWithContext(closeContext)
}

var _ sessioncontract.Storage = (*contextBlockingStorage)(nil)
var _ sessioncontract.Storage = (*contextRecordingStorage)(nil)

type storageFactory func(t *testing.T, clockInstance clockcontract.Clock) sessioncontract.Storage

const contractTestSessionId = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func closedStorage(t *testing.T, factory storageFactory) sessioncontract.Storage {
    t.Helper()

    storage := factory(t, clock.NewSystemClock())
    if closeErr := storage.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("unexpected close error: %v", closeErr)
    }

    return storage
}

func requireErrorMessage(t *testing.T, err error, expected string) {
    t.Helper()

    if nil == err || expected != err.Error() {
        t.Fatalf("expected %q, got %v", expected, err)
    }
}

func assertEveryStorageDoorRefusesAnAbsentRuntimeFirst(t *testing.T, factory storageFactory) {
    storage := closedStorage(t, factory)

    for _, refused := range refusedSessionRuntimes() {
        _, _, loadErr := storage.Load(refused, "")
        requireErrorMessage(t, loadErr, refusedRuntimeMessage)

        requireErrorMessage(t, storage.Save(refused, "", map[string]any{}, bound.Duration{}), refusedRuntimeMessage)

        requireErrorMessage(t, storage.Delete(refused, ""), refusedRuntimeMessage)
    }
}

func assertStorageAnswersADoneContextBeforeTheStoreIsTouched(t *testing.T, factory storageFactory) {
    storage := factory(t, clock.NewSystemClock())
    defer storage.CloseWithContext(context.Background())

    saveErr := storage.Save(sessionRuntimeUnder(cancelledContext()), contractTestSessionId, map[string]any{"a": 1}, bound.Of(time.Hour))
    if false == errors.Is(saveErr, context.Canceled) || true == exception.IsInfrastructureFailure(saveErr) {
        t.Fatalf("expected the bare context error, got %v", saveErr)
    }

    if _, exists, _ := storage.Load(sessionRuntime(), contractTestSessionId); true == exists {
        t.Fatalf("expected a save under a done context to store nothing")
    }

    if seedErr := storage.Save(sessionRuntime(), contractTestSessionId, map[string]any{"a": 1}, bound.Of(time.Hour)); nil != seedErr {
        t.Fatalf("unexpected seed error: %v", seedErr)
    }

    if deleteErr := storage.Delete(sessionRuntimeUnder(cancelledContext()), contractTestSessionId); false == errors.Is(deleteErr, context.Canceled) {
        t.Fatalf("expected the delete to answer the context error, got %v", deleteErr)
    }

    if _, exists, _ := storage.Load(sessionRuntime(), contractTestSessionId); false == exists {
        t.Fatalf("expected a delete under a done context to leave the entry")
    }

    data, exists, loadErr := storage.Load(sessionRuntimeUnder(cancelledContext()), contractTestSessionId)
    if nil != data || true == exists || false == errors.Is(loadErr, context.Canceled) {
        t.Fatalf("expected nil, false and the context error, got %v, %v and %v", data, exists, loadErr)
    }

    deadlineContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    if _, _, deadlineErr := storage.Load(sessionRuntimeUnder(deadlineContext), contractTestSessionId); false == errors.Is(deadlineErr, context.DeadlineExceeded) {
        t.Fatalf("expected a context past its deadline to answer it, got %v", deadlineErr)
    }
}

func assertStorageJudgesItsArgumentsBeforeADoneContext(t *testing.T, factory storageFactory) {
    closed := closedStorage(t, factory)

    _, _, closedLoadErr := closed.Load(sessionRuntimeUnder(cancelledContext()), "")
    requireErrorMessage(t, closedLoadErr, "session storage is closed")
    requireErrorMessage(t, closed.Save(sessionRuntimeUnder(cancelledContext()), "", map[string]any{}, bound.Duration{}), "session storage is closed")
    requireErrorMessage(t, closed.Delete(sessionRuntimeUnder(cancelledContext()), ""), "session storage is closed")

    storage := factory(t, clock.NewSystemClock())
    defer storage.CloseWithContext(context.Background())

    _, _, idLoadErr := storage.Load(sessionRuntimeUnder(cancelledContext()), "")
    requireErrorMessage(t, idLoadErr, "session id is required in load session")
    requireErrorMessage(t, storage.Save(sessionRuntimeUnder(cancelledContext()), "", map[string]any{}, bound.Duration{}), "session id is required in save session")
    requireErrorMessage(t, storage.Delete(sessionRuntimeUnder(cancelledContext()), ""), "session id is required in delete session")

    requireErrorMessage(t, storage.Save(sessionRuntimeUnder(cancelledContext()), contractTestSessionId, map[string]any{}, bound.Duration{}), "the session ttl is required")
}

func assertStorageRefusesAnAbsentTtlAndStoresNothing(t *testing.T, factory storageFactory) {
    storage := factory(t, clock.NewSystemClock())
    defer storage.CloseWithContext(context.Background())

    requireErrorMessage(t, storage.Save(sessionRuntime(), contractTestSessionId, map[string]any{"a": 1}, bound.Duration{}), "the session ttl is required")

    if _, exists, _ := storage.Load(sessionRuntime(), contractTestSessionId); true == exists {
        t.Fatalf("expected a refused ttl to store nothing")
    }
}

func assertStorageRefusesATtlBelowASecond(t *testing.T, factory storageFactory) {
    storage := factory(t, clock.NewSystemClock())
    defer storage.CloseWithContext(context.Background())

    saveErr := storage.Save(sessionRuntime(), contractTestSessionId, map[string]any{"a": 1}, bound.Of(time.Millisecond))
    if nil == saveErr || "ttl" != exception.LogContext(saveErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected a ttl under a second to be refused naming the ttl, got %v", saveErr)
    }
}

func assertStorageKeepsAnUnlimitedTtl(t *testing.T, factory storageFactory) {
    frozenClock := clock.NewFrozenClock(time.Unix(1_000_000, 0))
    storage := factory(t, frozenClock)
    defer storage.CloseWithContext(context.Background())

    if saveErr := storage.Save(sessionRuntime(), contractTestSessionId, map[string]any{"a": 1}, bound.UnlimitedDuration()); nil != saveErr {
        t.Fatalf("unexpected save error: %v", saveErr)
    }

    frozenClock.Advance(100 * 365 * 24 * time.Hour)

    if _, exists, loadErr := storage.Load(sessionRuntime(), contractTestSessionId); false == exists || nil != loadErr {
        t.Fatalf("expected an unlimited session never to lapse, got %v and %v", exists, loadErr)
    }
}

func sessionRuntime() containercontract.Runtime {
    return sessionRuntimeUnder(context.Background())
}

func sessionRuntimeUnder(ctx context.Context) containercontract.Runtime {
    return runtime.NewRun(ctx, container.NewContainer(container.ContainerConfig{}).NewScope())
}

func defaultSessionFileModes() sessionFileModes {
    return sessionFileModes{file: DefaultFileStorageFileMode, directory: DefaultFileStorageDirectoryMode}
}

/* the umask is process-wide: no session test runs in parallel, so a test asserting a created mode sets it to zero and restores it */
func withZeroUmask(t *testing.T) {
    t.Helper()

    previousUmask := syscall.Umask(0)
    t.Cleanup(func() { syscall.Umask(previousUmask) })
}

func requirePermission(t *testing.T, path string, expected os.FileMode) {
    t.Helper()

    info, statErr := os.Stat(path)
    if nil != statErr {
        t.Fatalf("unexpected stat error on %s: %s", path, statErr.Error())
    }

    if expected != info.Mode().Perm() {
        t.Fatalf("expected %s at %#o, got %#o", path, uint32(expected), uint32(info.Mode().Perm()))
    }
}
