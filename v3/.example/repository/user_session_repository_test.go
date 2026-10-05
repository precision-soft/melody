package repository

import (
    "context"
    "slices"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/persistence"
)

func TestSessionsPastCap_AnswersEverySessionButTheNewestCapMinusOne(t *testing.T) {
    held := []string{"s1", "s2", "s3", "s4", "s5", "s6"}

    if pastCap := sessionsPastCap(held); false == slices.Equal([]string{"s1", "s2"}, pastCap) {
        t.Fatalf("expected s1 and s2 past the cap, got %v", pastCap)
    }

    if pastCap := sessionsPastCap(held[:UserSessionCap-1]); 0 != len(pastCap) {
        t.Fatalf("expected none past the cap below it, got %v", pastCap)
    }
}

func TestUserSessionRepositoryAdmit_RefusesAnAdmissionWithoutAccountOrSession(t *testing.T) {
    repositoryInstance, newErr := NewUserSessionRepository(persistence.NewCatalogStorage(nil))
    if nil != newErr {
        t.Fatalf("new session index: %v", newErr)
    }

    release := func(sessionId string) error { return nil }

    for _, admission := range []struct {
        userId      string
        sessionId   string
        sessionLive func(sessionId string) (bool, error)
        release     func(sessionId string) error
    }{
        {userId: " ", sessionId: "s1", sessionLive: everySessionLive, release: release},
        {userId: "user-1", sessionId: "", sessionLive: everySessionLive, release: release},
        {userId: "user-1", sessionId: "s1", sessionLive: everySessionLive, release: nil},
        {userId: "user-1", sessionId: "s1", sessionLive: nil, release: release},
    } {
        if nil == repositoryInstance.Admit(context.Background(), admission.userId, "", admission.sessionId, time.Now(), admission.sessionLive, admission.release) {
            t.Fatalf("expected %+v refused", admission)
        }
    }
}
