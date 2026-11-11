package security

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodysecuritycontract "github.com/precision-soft/melody/v4/security/contract"
    melodysession "github.com/precision-soft/melody/v4/session"
)

/* The handler releases the session's place and leaves ending it to the firewall's door, whose pin asks the storage. */
func TestSessionLogoutHandlerLeavesEndingTheSessionToTheDoor(t *testing.T) {
    storage := melodysession.NewInMemoryStorage(melodysession.InMemoryStorageConfig{})
    defer storage.CloseWithContext(context.Background())

    manager := melodysession.NewManager(storage, melodysession.ManagerConfig{Ttl: bound.Of(time.Hour)})

    sessionInstance := mustSession(manager.NewSession())
    sessionInstance.Set(SessionKeySecurityUserId, "user-1")
    sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER"})

    if err := manager.SaveSession(sessionInstance); nil != err {
        t.Fatalf("the session was not saved: %v", err)
    }

    sessionId := sessionInstance.Id()

    request := plainRequest(t)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

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

    if _, exists, _ := storage.Load(sessionId); false == exists {
        t.Fatal("expected the entry left for the door's response path")
    }
}

/* a logout that arrives with no session has nothing to release */
func TestSessionLogoutHandlerToleratesARequestCarryingNoSession(t *testing.T) {
    result, err := NewSessionLogoutHandler(nil).Logout(nil, plainRequest(t), melodysecuritycontract.LogoutInput{})
    if nil != err || nil == result {
        t.Fatalf("expected an empty result, got %+v and %v", result, err)
    }
}

/* the sign-out releases the row of the session it ends, so the account's place is free for its next sign-in */
func TestSessionLogoutHandlerReleasesTheSessionsRow(t *testing.T) {
    request, sessionInstance := requestCarryingSession(t)
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    request = melodyhttp.NewRequest(request.HttpRequest(), nil, melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance), nil)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    index := &recordingSessionIndex{}

    _, err := NewSessionLogoutHandler(index.lookup).Logout(nil, request, melodysecuritycontract.LogoutInput{})
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if 1 != len(index.releasedList) || sessionInstance.Id() != index.releasedList[0] {
        t.Fatalf("expected the row of %q released, got %v", sessionInstance.Id(), index.releasedList)
    }
}
