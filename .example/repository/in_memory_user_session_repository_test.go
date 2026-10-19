package repository

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "testing"
    "time"
)

/* admitWithoutRelease admits a session before the grace, so the next admission asks the storage whether it still holds it */
func admitWithoutRelease(t *testing.T, repositoryInstance UserSessionRepository, userId string, previousSessionId string, sessionId string) {
    t.Helper()

    if admitErr := repositoryInstance.Admit(context.Background(), userId, previousSessionId, sessionId, time.Now().Add(-2*UserSessionAdmissionGrace), everySessionLive, func(sessionId string) error { return nil }); nil != admitErr {
        t.Fatalf("admit %s: %v", sessionId, admitErr)
    }
}

/* past the cap the oldest session of the account is released, oldest first, and the sessions of another account are not */
func TestInMemoryUserSessionRepositoryAdmit_ReleasesTheOldestPastTheCap(t *testing.T) {
    repositoryInstance := NewInMemoryUserSessionRepository()

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
    repositoryInstance := NewInMemoryUserSessionRepository()

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
    repositoryInstance := NewInMemoryUserSessionRepository()
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
    repositoryInstance := NewInMemoryUserSessionRepository()
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
    repositoryInstance := NewInMemoryUserSessionRepository()

    var waitGroup sync.WaitGroup
    for index := range 500 {
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
    repositoryInstance := NewInMemoryUserSessionRepository()

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
    repositoryInstance := NewInMemoryUserSessionRepository()
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

/* noSessionStored answers every session as absent from the storage, as it reads while the kernel has not yet saved the sessions the sign-ins rotated to */
func noSessionStored(sessionId string) (bool, error) {
    return false, nil
}

/* concurrent sign-ins whose sessions the kernel has not stored yet keep their places: a row within the grace counts as live whatever the storage answers, so the cap releases the oldest instead of dropping the newer rows unreleased */
func TestInMemoryUserSessionRepositoryAdmit_ARowWithinTheGraceCountsWhateverTheStorageAnswers(t *testing.T) {
    repositoryInstance := NewInMemoryUserSessionRepository()
    now := time.Now()

    var releasedList []string
    release := func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    }

    for index := range UserSessionCap + 2 {
        if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", fmt.Sprintf("s%d", index+1), now, noSessionStored, release); nil != admitErr {
            t.Fatalf("admit s%d: %v", index+1, admitErr)
        }
    }

    if "[s1 s2]" != fmt.Sprint(releasedList) {
        t.Fatalf("expected the two oldest released past the cap, got %v", releasedList)
    }

    if held := repositoryInstance.(*inMemoryUserSessionRepository).sessionsByUser["user-1"]; "[s3 s4 s5 s6 s7]" != fmt.Sprint(held) {
        t.Fatalf("expected the newest %d held, got %v", UserSessionCap, held)
    }
}

/* a row admitted the grace ago or earlier is read from the storage again: an absent session's row is dropped without a release */
func TestInMemoryUserSessionRepositoryAdmit_ARowAtTheGraceIsReadFromTheStorage(t *testing.T) {
    repositoryInstance := NewInMemoryUserSessionRepository()
    now := time.Now()

    if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s1", now.Add(-UserSessionAdmissionGrace), everySessionLive, func(sessionId string) error { return nil }); nil != admitErr {
        t.Fatalf("admit s1: %v", admitErr)
    }

    if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s2", now.Add(-UserSessionAdmissionGrace+time.Millisecond), everySessionLive, func(sessionId string) error { return nil }); nil != admitErr {
        t.Fatalf("admit s2: %v", admitErr)
    }

    var askedList []string
    sessionLive := func(sessionId string) (bool, error) {
        askedList = append(askedList, sessionId)

        return false, nil
    }

    if admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s3", now, sessionLive, func(sessionId string) error { return nil }); nil != admitErr {
        t.Fatalf("admit s3: %v", admitErr)
    }

    if "[s1]" != fmt.Sprint(askedList) {
        t.Fatalf("expected the storage asked about the row at the grace alone, got %v", askedList)
    }

    if held := repositoryInstance.(*inMemoryUserSessionRepository).sessionsByUser["user-1"]; "[s2 s3]" != fmt.Sprint(held) {
        t.Fatalf("expected the row at the grace dropped and the one within it kept, got %v", held)
    }
}
