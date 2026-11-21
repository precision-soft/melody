package security

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

type admissionPlantedKey struct{}

type loadContextRecordingStorage struct {
    melodysessioncontract.Storage
    loadContexts []context.Context
}

func (instance *loadContextRecordingStorage) Load(runtimeInstance melodycontainercontract.Runtime, sessionId string) (map[string]any, bool, error) {
    ctx := runtimeInstance.Context()
    instance.loadContexts = append(instance.loadContexts, ctx)

    return instance.Storage.Load(runtimeInstance, sessionId)
}

type deleteContextRecordingManager struct {
    melodysessioncontract.Manager
    deleteContexts []context.Context
}

func (instance *deleteContextRecordingManager) DeleteSession(runtimeInstance melodycontainercontract.Runtime, sessionId string) error {
    ctx := runtimeInstance.Context()
    instance.deleteContexts = append(instance.deleteContexts, ctx)

    return instance.Manager.DeleteSession(runtimeInstance, sessionId)
}

type callbackInvokingSessionIndex struct {
    recordingSessionIndex
    victimId string
}

func (instance *callbackInvokingSessionIndex) Admit(ctx context.Context, userId string, previousSessionId string, sessionId string, createdAt time.Time, sessionLive func(sessionId string) (bool, error), release func(sessionId string) error) error {
    if _, liveErr := sessionLive(instance.victimId); nil != liveErr {
        return liveErr
    }

    return release(instance.victimId)
}

func TestAdmitSession_ChecksAndReleasesUnderTheRequestsContext(t *testing.T) {
    storage := &loadContextRecordingStorage{Storage: melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})}
    manager := &deleteContextRecordingManager{Manager: melodysession.NewManager(storage, melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})}

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    melodycontainer.MustRegister[melodysessioncontract.Storage](containerInstance, melodysession.ServiceSessionStorage, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Storage, error) {
        return storage, nil
    }, melodycontainer.RegisterOptions{})
    melodycontainer.MustRegister[melodysessioncontract.Manager](containerInstance, melodysession.ServiceSessionManager, func(resolver melodycontainercontract.Resolver) (melodysessioncontract.Manager, error) {
        return manager, nil
    }, melodycontainer.RegisterOptions{})
    registerAdmissionClock(t, containerInstance)

    victim := mustSession(manager.NewSession(exampleTestRuntime()))
    victim.Set("userId", "user-1")
    if saveErr := manager.SaveSession(exampleTestRuntime(), victim); nil != saveErr {
        t.Fatalf("unexpected seed error: %v", saveErr)
    }

    requestContext := context.WithValue(context.Background(), admissionPlantedKey{}, "planted")
    runtimeInstance := melodyruntime.NewRequest(requestContext, containerInstance.NewScope())

    index := &callbackInvokingSessionIndex{victimId: victim.Id()}
    rotated := mustSession(manager.NewSession(exampleTestRuntime()))
    storage.loadContexts = nil

    if admitErr := AdmitSession(runtimeInstance, func(runtimeInstance melodycontainercontract.Runtime) (SessionIndex, error) { return index, nil }, "user-1", "", rotated); nil != admitErr {
        t.Fatalf("unexpected admission error: %v", admitErr)
    }

    if 1 != len(storage.loadContexts) || "planted" != storage.loadContexts[0].Value(admissionPlantedKey{}) {
        t.Fatalf("expected the liveness check to run on the request's context, got %d loads", len(storage.loadContexts))
    }

    if 1 != len(manager.deleteContexts) || "planted" != manager.deleteContexts[0].Value(admissionPlantedKey{}) {
        t.Fatalf("expected the release to run on the request's context, got %d deletes", len(manager.deleteContexts))
    }
}

func TestSessionAdmission_ANilRuntimeIsRefusedBeforeTheIndex(t *testing.T) {
    index := &runtimeRecordingSessionIndex{}
    rotated := mustSession(melodysession.NewManager(melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{}), melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)}).NewSession(exampleTestRuntime()))

    if admitErr := AdmitSession(nil, index.lookupRecording, "user-1", "", rotated); nil == admitErr || "the runtime is nil or carries no context" != admitErr.Error() {
        t.Fatalf("expected the admission to refuse a nil runtime, got %v", admitErr)
    }

    if releaseErr := ReleaseSession(nil, index.lookupRecording, "session-1"); nil == releaseErr || "the runtime is nil or carries no context" != releaseErr.Error() {
        t.Fatalf("expected the release to refuse a nil runtime, got %v", releaseErr)
    }

    if 0 != len(index.runtimes) {
        t.Fatalf("expected the index not to be looked up, got %d lookups", len(index.runtimes))
    }
}
