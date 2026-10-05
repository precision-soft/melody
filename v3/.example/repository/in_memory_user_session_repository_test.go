package repository

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "testing"
    "time"
)

func admitWithoutRelease(t *testing.T, repositoryInstance UserSessionRepository, userId string, previousSessionId string, sessionId string) {
    t.Helper()

    if admitErr := repositoryInstance.Admit(context.Background(), userId, previousSessionId, sessionId, time.Now(), everySessionLive, func(sessionId string) error { return nil }); nil != admitErr {
        t.Fatalf("admit %s: %v", sessionId, admitErr)
    }
}

/* past the cap the oldest session of the account is released, oldest first, and the sessions of another account are not */
func TestInMemoryUserSessionRepositoryAdmit_ReleasesTheOldestPastTheCap(t *testing.T) {
    repositoryInstance := newInMemoryUserSessionRepository()

    admitWithoutRelease(t, repositoryInstance, "user-2", "", "other")
    for index := range UserSessionCap {
        admitWithoutRelease(t, repositoryInstance, "user-1", "", fmt.Sprintf("s%d", index+1))
    }

    var releasedList []string
    release := func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    }

    for _, sessionId := range []string{"s6", "s7"} {
        if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", sessionId, time.Now(), everySessionLive, release); nil != admitErr {
            t.Fatalf("admit %s: %v", sessionId, admitErr)
        }
    }

    if 2 != len(releasedList) || "s1" != releasedList[0] || "s2" != releasedList[1] {
        t.Fatalf("expected s1 then s2 released, got %v", releasedList)
    }
}

/* the id the rotation retired leaves the index, so it holds no place of the account */
func TestInMemoryUserSessionRepositoryAdmit_DropsTheRetiredId(t *testing.T) {
    repositoryInstance := newInMemoryUserSessionRepository()

    admitWithoutRelease(t, repositoryInstance, "user-1", "", "retired")
    for index := range UserSessionCap - 1 {
        admitWithoutRelease(t, repositoryInstance, "user-1", "", fmt.Sprintf("s%d", index+1))
    }

    var releasedList []string
    if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "retired", "rotated", time.Now(), everySessionLive, func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    }); nil != admitErr {
        t.Fatalf("admit: %v", admitErr)
    }

    if 0 != len(releasedList) {
        t.Fatalf("expected the retired id to free its place rather than an older session released, got %v", releasedList)
    }
}

/* a release that fails refuses the admission with the index unchanged */
func TestInMemoryUserSessionRepositoryAdmit_AReleaseFailureRecordsNothing(t *testing.T) {
    repositoryInstance := newInMemoryUserSessionRepository()
    for index := range UserSessionCap {
        admitWithoutRelease(t, repositoryInstance, "user-1", "", fmt.Sprintf("s%d", index+1))
    }

    releaseErr := errors.New("the session storage is down")
    if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s6", time.Now(), everySessionLive, func(sessionId string) error { return releaseErr }); false == errors.Is(admitErr, releaseErr) {
        t.Fatalf("expected the release's failure, got %v", admitErr)
    }

    held := repositoryInstance.(*inMemoryUserSessionRepository).sessionsByUser["user-1"]
    if UserSessionCap != len(held) || "s1" != held[0] {
        t.Fatalf("expected the index unchanged by the refused admission, got %v", held)
    }
}

/* a release frees the session's place; a session the index never held is not an error */
func TestInMemoryUserSessionRepositoryRelease_FreesThePlace(t *testing.T) {
    repositoryInstance := newInMemoryUserSessionRepository()
    for index := range UserSessionCap {
        admitWithoutRelease(t, repositoryInstance, "user-1", "", fmt.Sprintf("s%d", index+1))
    }

    if releaseErr := repositoryInstance.Release(context.Background(), "s3"); nil != releaseErr {
        t.Fatalf("release: %v", releaseErr)
    }

    if releaseErr := repositoryInstance.Release(context.Background(), "never-held"); nil != releaseErr {
        t.Fatalf("release of an unknown session: %v", releaseErr)
    }

    var releasedList []string
    if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s6", time.Now(), everySessionLive, func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    }); nil != admitErr {
        t.Fatalf("admit: %v", admitErr)
    }

    if 0 != len(releasedList) {
        t.Fatalf("expected the released place reused, got %v released", releasedList)
    }
}

/* concurrent sign-ins of one account never leave it more than the cap */
func TestInMemoryUserSessionRepositoryAdmit_ConcurrentAdmissionsKeepTheCap(t *testing.T) {
    repositoryInstance := newInMemoryUserSessionRepository()

    var waitGroup sync.WaitGroup
    for index := range concurrentRounds {
        waitGroup.Add(1)
        go func() {
            defer waitGroup.Done()

            _ = repositoryInstance.Admit(context.Background(), "user-1", "", fmt.Sprintf("s%d", index), time.Now(), everySessionLive, func(sessionId string) error { return nil })
        }()
    }
    waitGroup.Wait()

    if held := repositoryInstance.(*inMemoryUserSessionRepository).sessionsByUser["user-1"]; UserSessionCap != len(held) {
        t.Fatalf("expected %d sessions held, got %d", UserSessionCap, len(held))
    }
}

/* sessionsEndedElsewhere answers every session but the ended ones as still stored */
func sessionsEndedElsewhere(endedSessionIds ...string) func(sessionId string) (bool, error) {
    return func(sessionId string) (bool, error) {
        for _, endedSessionId := range endedSessionIds {
            if endedSessionId == sessionId {
                return false, nil
            }
        }

        return true, nil
    }
}

/* a session that ended elsewhere keeps no place: its row goes at the next sign-in without a release, and the cap counts the live sessions, so no live one is ended for it */
func TestInMemoryUserSessionRepositoryAdmit_DropsTheRowsOfEndedSessionsBeforeTheCap(t *testing.T) {
    repositoryInstance := newInMemoryUserSessionRepository()

    for index := range UserSessionCap {
        admitWithoutRelease(t, repositoryInstance, "user-1", "", fmt.Sprintf("s%d", index+1))
    }

    var releasedList []string
    if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s6", time.Now(), sessionsEndedElsewhere("s3"), func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    }); nil != admitErr {
        t.Fatalf("admit s6: %v", admitErr)
    }

    if 0 != len(releasedList) {
        t.Fatalf("expected no live session ended while an ended one held a place, got %v", releasedList)
    }

    held := repositoryInstance.(*inMemoryUserSessionRepository).sessionsByUser["user-1"]
    if "[s1 s2 s4 s5 s6]" != fmt.Sprint(held) {
        t.Fatalf("expected the ended session's row dropped, got %v", held)
    }
}

/* a liveness read that fails refuses the admission with nothing recorded, as a failed release does */
func TestInMemoryUserSessionRepositoryAdmit_AFailedLivenessReadRefusesTheAdmission(t *testing.T) {
    repositoryInstance := newInMemoryUserSessionRepository()
    admitWithoutRelease(t, repositoryInstance, "user-1", "", "s1")

    readErr := errors.New("the session storage is down")
    admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s2", time.Now(), func(sessionId string) (bool, error) { return false, readErr }, func(sessionId string) error { return nil })
    if false == errors.Is(admitErr, readErr) {
        t.Fatalf("expected the read's failure, got %v", admitErr)
    }

    held := repositoryInstance.(*inMemoryUserSessionRepository).sessionsByUser["user-1"]
    if "[s1]" != fmt.Sprint(held) {
        t.Fatalf("expected nothing recorded past a failed read, got %v", held)
    }
}

