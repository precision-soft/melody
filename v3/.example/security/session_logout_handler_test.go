package security

import (
    nethttp "net/http"
    "testing"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodysecuritycontract "github.com/precision-soft/melody/v3/security/contract"
    melodysession "github.com/precision-soft/melody/v3/session"
)

/* The firewall's logout door is asked what the STORAGE holds afterwards, not what the session object says: deleting the two identity keys leaves the entry modified, so the response path saves it back under the same id and re-issues the cookie, and only a cleared session routes that path to DeleteSession. The response path is run here exactly as the kernel runs it, through SaveSession. */
func TestSessionLogoutHandlerEndsTheSessionRatherThanEmptyingIt(t *testing.T) {
    storage := melodysession.NewInMemoryStorage()
    defer storage.Close()

    manager := melodysession.NewManager(storage, time.Hour)

    sessionInstance := manager.NewSession()
    sessionInstance.Set(SessionKeySecurityUserId, "user-1")
    sessionInstance.Set(SessionKeySecurityRoles, []string{"ROLE_USER"})

    if err := manager.SaveSession(sessionInstance); nil != err {
        t.Fatalf("the session was not saved: %v", err)
    }

    sessionId := sessionInstance.Id()

    request := plainRequest(t)
    request.Attributes().Set(melodyhttp.RequestAttributeSession, sessionInstance)

    result, err := NewSessionLogoutHandler().Logout(nil, request, melodysecuritycontract.LogoutInput{})
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if nil == result || nil == result.Response {
        t.Fatal("the logout door answered no response")
    }

    if nethttp.StatusOK != result.Response.StatusCode() {
        t.Fatalf("unexpected status: %d", result.Response.StatusCode())
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

/* a logout that arrives with no session is refused rather than answered as a success, because the door cannot end what it cannot reach */
func TestSessionLogoutHandlerRefusesARequestCarryingNoSession(t *testing.T) {
    result, err := NewSessionLogoutHandler().Logout(nil, plainRequest(t), melodysecuritycontract.LogoutInput{})
    if nil != err {
        t.Fatalf("the logout door failed: %v", err)
    }

    if nil == result || nil == result.Response {
        t.Fatal("the logout door answered no response")
    }

    if nethttp.StatusInternalServerError != result.Response.StatusCode() {
        t.Fatalf("unexpected status: %d", result.Response.StatusCode())
    }
}
