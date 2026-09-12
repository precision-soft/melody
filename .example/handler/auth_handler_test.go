package handler

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/security"
    melodyhttp "github.com/precision-soft/melody/http"
    melodysession "github.com/precision-soft/melody/session"
)

/* The logout door is asked what the STORAGE holds afterwards, not what the session object says: deleting the two identity keys leaves the entry modified, so the response path saves it back under the same id and re-issues the cookie, and only a cleared session routes that path to DeleteSession. The response path is run here exactly as the kernel runs it, through SaveSession. */
func TestLogoutHandlerEndsTheSessionRatherThanEmptyingIt(t *testing.T) {
    storage := melodysession.NewInMemoryStorage()
    defer storage.Close()

    manager := melodysession.NewManager(storage, time.Hour)

    sessionInstance := manager.NewSession()
    sessionInstance.Set(security.SessionKeySecurityUserId, "user-1")
    sessionInstance.Set(security.SessionKeySecurityRoles, []string{"ROLE_USER"})

    if err := manager.SaveSession(sessionInstance); nil != err {
        t.Fatalf("the session was not saved: %v", err)
    }

    sessionId := sessionInstance.Id()

    if _, exists, _ := storage.Load(sessionId); false == exists {
        t.Fatal("the storage did not hold the session the test is about to end")
    }

    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/logout/", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, nil, nil)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    response, err := LogoutHandler()(nil, httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if nil == response {
        t.Fatal("the logout door answered no response")
    }

    if false == sessionInstance.IsCleared() {
        t.Fatal("the session was not marked cleared, so the response path will save it back under the same id")
    }

    if err := manager.SaveSession(sessionInstance); nil != err {
        t.Fatalf("the response path refused the cleared session: %v", err)
    }

    data, exists, loadErr := storage.Load(sessionId)
    if nil != loadErr {
        t.Fatalf("the storage refused the read: %v", loadErr)
    }

    if true == exists {
        t.Fatalf("the session entry outlived the logout that ended it, holding %v", data)
    }
}

/* a logout that arrives without a session is not an error: the door answers the same redirect, and nothing is left behind to end */
func TestLogoutHandlerToleratesARequestCarryingNoSession(t *testing.T) {
    httpRequest := httptest.NewRequest(nethttp.MethodGet, "/logout/", nil)
    request := melodyhttp.NewRequest(httpRequest, nil, nil, nil)

    response, err := LogoutHandler()(nil, httptest.NewRecorder(), request)
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if nil == response {
        t.Fatal("the logout door answered no response")
    }
}
