package repository

import (
    "context"
    "slices"
    "testing"
    "time"
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
    repositoryInstance, newErr := UserSessionRepositoryProvider("")(nil)
    if nil != newErr {
        t.Fatalf("new session index: %v", newErr)
    }

    release := func(sessionId string) error { return nil }

    for _, admission := range []struct {
        userId    string
        sessionId string
        release   func(sessionId string) error
    }{
        {userId: " ", sessionId: "s1", release: release},
        {userId: "user-1", sessionId: "", release: release},
        {userId: "user-1", sessionId: "s1", release: nil},
    } {
        if nil == repositoryInstance.Admit(context.Background(), admission.userId, "", admission.sessionId, time.Now(), admission.release) {
            t.Fatalf("expected %+v refused", admission)
        }
    }
}
