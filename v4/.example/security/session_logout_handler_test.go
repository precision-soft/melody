package security

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
)

func TestSessionLogoutHandlerLeavesEndingTheSessionToTheDoor(t *testing.T) {
    storage := melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    manager := melodysession.NewManager(storage, melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})

    sessionInstance := mustSession(manager.NewSession(exampleTestRuntime()))
    sessionInstance.Set(SessionKeySecurityUserId, "user-1")
    sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER"})

    if err := manager.SaveSession(exampleTestRuntime(), sessionInstance); nil != err {
        t.Fatalf("the session was not saved: %v", err)
    }

    sessionId := sessionInstance.Id()

    request := plainRequest(t)
    _ = melodyhttp.SetRequestSession(request, sessionInstance)

    result, err := NewSessionLogoutHandler(nil).Logout(nil, request, melodysecuritycontract.LogoutInput{})
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if nil == result || nil != result.Response {
        t.Fatalf("expected no response of its own, so the door answers, got %+v", result)
    }

    if false != sessionInstance.IsCleared() {
        t.Fatal("expected the session left for the door to end")
    }

    if _, exists, _ := storage.Load(exampleTestRuntime(), sessionId); false == exists {
        t.Fatal("expected the entry left for the door's response path")
    }
}

func TestSessionLogoutHandlerAnswersTheErrorOfARequestCarryingNoSession(t *testing.T) {
    result, err := NewSessionLogoutHandler(nil).Logout(nil, plainRequest(t), melodysecuritycontract.LogoutInput{})
    if nil == err || nil != result {
        t.Fatalf("expected the read's error and no result, got %+v and %v", result, err)
    }
}

func TestSessionLogoutHandlerReleasesTheSessionsRow(t *testing.T) {
    request, sessionInstance := requestCarryingSession(t)
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())
    request = melodyhttp.NewRequest(request.HttpRequest(), nil, runtimeInstance, nil)
    _ = melodyhttp.SetRequestSession(request, sessionInstance)

    index := &recordingSessionIndex{}

    _, err := NewSessionLogoutHandler(index.lookup).Logout(runtimeInstance, request, melodysecuritycontract.LogoutInput{})
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if 1 != len(index.releasedList) || sessionInstance.Id() != index.releasedList[0] {
        t.Fatalf("expected the row of %q released, got %v", sessionInstance.Id(), index.releasedList)
    }
}
