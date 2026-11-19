package security

import (
    "context"
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
    melodysessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

type admissionPlantedKey struct{}

type loadContextRecordingStorage struct {
    melodysessioncontract.Storage
    loadContexts []context.Context
}

func (instance *loadContextRecordingStorage) Load(ctx context.Context, sessionId string) (map[string]any, bool, error) {
    instance.loadContexts = append(instance.loadContexts, ctx)

    return instance.Storage.Load(ctx, sessionId)
}

type deleteContextRecordingManager struct {
    melodysessioncontract.Manager
    deleteContexts []context.Context
}

func (instance *deleteContextRecordingManager) DeleteSession(ctx context.Context, sessionId string) error {
    instance.deleteContexts = append(instance.deleteContexts, ctx)

    return instance.Manager.DeleteSession(ctx, sessionId)
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

    victim := mustSession(manager.NewSession(context.Background()))
    victim.Set("userId", "user-1")
    if saveErr := manager.SaveSession(context.Background(), victim); nil != saveErr {
        t.Fatalf("unexpected seed error: %v", saveErr)
    }

    requestContext := context.WithValue(context.Background(), admissionPlantedKey{}, "planted")
    runtimeInstance := melodyruntime.NewRequest(requestContext, containerInstance.NewScope())
    request := melodyhttp.NewRequest(httptest.NewRequest(nethttp.MethodPost, "/login", nil), nil, runtimeInstance, nil)

    index := &callbackInvokingSessionIndex{victimId: victim.Id()}
    rotated := mustSession(manager.NewSession(context.Background()))
    storage.loadContexts = nil

    if admitErr := AdmitSession(request, func(request melodyhttpcontract.Request) (SessionIndex, error) { return index, nil }, "user-1", "", rotated); nil != admitErr {
        t.Fatalf("unexpected admission error: %v", admitErr)
    }

    if 1 != len(storage.loadContexts) || "planted" != storage.loadContexts[0].Value(admissionPlantedKey{}) {
        t.Fatalf("expected the liveness check to run on the request's context, got %d loads", len(storage.loadContexts))
    }

    if 1 != len(manager.deleteContexts) || "planted" != manager.deleteContexts[0].Value(admissionPlantedKey{}) {
        t.Fatalf("expected the release to run on the request's context, got %d deletes", len(manager.deleteContexts))
    }
}
