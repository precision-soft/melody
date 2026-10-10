package repository

import (
    "context"
    "database/sql/driver"
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"
)

/* heldSessionRows answers the account's locked row when the account exists, and its held sessions, oldest first and admitted long before the grace, to the index read */
func heldSessionRows(accountExists bool, heldOldestFirst ...string) func(query string) ([]string, [][]driver.Value, error) {
    return heldSessionRowsAdmittedAt(accountExists, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), heldOldestFirst...)
}

/* heldSessionRowsAdmittedAt answers the held sessions as admitted at admittedAt */
func heldSessionRowsAdmittedAt(accountExists bool, admittedAt time.Time, heldOldestFirst ...string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "FOR UPDATE") {
            if false == accountExists {
                return []string{"id"}, nil, nil
            }

            return []string{"id", "username", "password", "roles"}, [][]driver.Value{{"user-1", "user", "H1", "ROLE_USER"}}, nil
        }

        if true == strings.Contains(query, "melody_example_v1_user_session") && true == strings.HasPrefix(query, "SELECT") {
            rows := make([][]driver.Value, 0, len(heldOldestFirst))
            for _, sessionId := range heldOldestFirst {
                rows = append(rows, []driver.Value{sessionId, admittedAt})
            }

            return []string{"session_id", "created_at"}, rows, nil
        }

        return []string{}, nil, nil
    }
}

func isSessionStatement(prefix string, sessionId string) func(query string) bool {
    return func(query string) bool {
        return true == strings.HasPrefix(query, prefix) && true == strings.Contains(query, "melody_example_v1_user_session") && true == strings.Contains(query, "'"+sessionId+"'")
    }
}

/* the admission past the cap locks the account first, releases the oldest session and drops its row, and writes the admitted one, all inside one transaction */
func TestBunUserSessionRepositoryAdmit_ReleasesTheOldestPastTheCapUnderTheAccountsLock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = heldSessionRows(true, "s1", "s2", "s3", "s4", "s5")
    repositoryInstance := NewBunUserSessionRepository(database)

    var releasedList []string
    admitErr := repositoryInstance.Admit(context.Background(), "user-1", "s0", "s6", time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC), everySessionLive, func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    })
    if nil != admitErr {
        t.Fatalf("the admission failed: %v", admitErr)
    }

    if 1 != len(releasedList) || "s1" != releasedList[0] {
        t.Fatalf("expected the oldest session alone released, got %v", releasedList)
    }

    queries := recorder.recordedQueries()
    lockAt, previousAt, readAt, dropAt, insertAt, commitAt := -1, -1, -1, -1, -1, -1
    for index, query := range queries {
        switch {
        case true == strings.Contains(query, "FOR UPDATE") && true == strings.Contains(query, "'user-1'"):
            lockAt = index
        case true == isSessionStatement("DELETE", "s0")(query):
            previousAt = index
        case true == strings.HasPrefix(query, "SELECT") && true == strings.Contains(query, "melody_example_v1_user_session") && true == strings.Contains(query, "ORDER BY created_at ASC"):
            readAt = index
        case true == isSessionStatement("DELETE", "s1")(query):
            dropAt = index
        case true == isSessionStatement("INSERT", "s6")(query):
            insertAt = index
        case "COMMIT" == query:
            commitAt = index
        }
    }

    if false == (0 <= lockAt && lockAt < previousAt && previousAt < readAt && readAt < dropAt && dropAt < insertAt && insertAt < commitAt) {
        t.Fatalf("expected lock, retired row, read, drop, insert and commit in that order inside one transaction: %q", queries)
    }

    if 1 != recorder.countMatching(func(query string) bool { return true == strings.HasPrefix(query, "DELETE") && true == strings.Contains(query, "melody_example_v1_user_session") && false == strings.Contains(query, "'s0'") }) {
        t.Fatalf("expected one row dropped past the cap: %q", queries)
    }
}

/* a release that fails refuses the admission and rolls the transaction back, so the admitted session is recorded nowhere */
func TestBunUserSessionRepositoryAdmit_AReleaseFailureRecordsNothing(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = heldSessionRows(true, "s1", "s2", "s3", "s4", "s5")
    repositoryInstance := NewBunUserSessionRepository(database)

    releaseErr := errors.New("the session storage is down")
    admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s6", time.Now(), everySessionLive, func(sessionId string) error {
        return releaseErr
    })
    if false == errors.Is(admitErr, releaseErr) {
        t.Fatalf("expected the release's failure, got %v", admitErr)
    }

    if 0 != recorder.countMatching(isSessionStatement("INSERT", "s6")) || 0 != recorder.countMatching(isSessionStatement("DELETE", "s1")) {
        t.Fatalf("expected nothing written past a failed release: %q", recorder.recordedQueries())
    }

    if 1 != recorder.countMatching(func(query string) bool { return "ROLLBACK" == query }) {
        t.Fatalf("expected the transaction rolled back: %q", recorder.recordedQueries())
    }
}

/* an account the lock does not find is refused, and no session is recorded for it */
func TestBunUserSessionRepositoryAdmit_RefusesAnAbsentAccount(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = heldSessionRows(false)
    repositoryInstance := NewBunUserSessionRepository(database)

    admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s1", time.Now(), everySessionLive, func(sessionId string) error { return nil })
    if false == errors.Is(admitErr, ErrSessionAccountAbsent) {
        t.Fatalf("expected ErrSessionAccountAbsent, got %v", admitErr)
    }

    if 0 != recorder.countMatching(isSessionStatement("INSERT", "s1")) {
        t.Fatalf("expected no row for an absent account: %q", recorder.recordedQueries())
    }
}

/* the release drops the session's row alone */
func TestBunUserSessionRepositoryRelease_DropsTheSessionsRow(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    repositoryInstance := NewBunUserSessionRepository(database)

    if releaseErr := repositoryInstance.Release(context.Background(), "s1"); nil != releaseErr {
        t.Fatalf("the release failed: %v", releaseErr)
    }

    if 1 != recorder.countMatching(isSessionStatement("DELETE", "s1")) {
        t.Fatalf("expected the row of s1 dropped: %q", recorder.recordedQueries())
    }
}

/* the rows of sessions that ended elsewhere are dropped inside the transaction before the cap is counted, so no live session is released for them */
func TestBunUserSessionRepositoryAdmit_DropsTheRowsOfEndedSessionsBeforeTheCap(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = heldSessionRows(true, "s1", "s2", "s3", "s4", "s5")
    repositoryInstance := NewBunUserSessionRepository(database)

    var releasedList []string
    endedElsewhere := func(sessionId string) (bool, error) { return "s3" != sessionId, nil }
    admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s6", time.Now(), endedElsewhere, func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    })
    if nil != admitErr {
        t.Fatalf("the admission failed: %v", admitErr)
    }

    if 0 != len(releasedList) {
        t.Fatalf("expected no live session released, got %v", releasedList)
    }

    if 1 != recorder.countMatching(isSessionStatement("DELETE", "s3")) || 0 != recorder.countMatching(isSessionStatement("DELETE", "s1")) || 1 != recorder.countMatching(isSessionStatement("INSERT", "s6")) {
        t.Fatalf("expected the ended row dropped, the oldest live one kept and the admitted one written: %q", recorder.recordedQueries())
    }
}

/* a liveness read that fails rolls the admission back */
func TestBunUserSessionRepositoryAdmit_AFailedLivenessReadRecordsNothing(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = heldSessionRows(true, "s1")
    repositoryInstance := NewBunUserSessionRepository(database)

    readErr := errors.New("the session storage is down")
    admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s2", time.Now(), func(sessionId string) (bool, error) { return false, readErr }, func(sessionId string) error { return nil })
    if false == errors.Is(admitErr, readErr) {
        t.Fatalf("expected the read's failure, got %v", admitErr)
    }

    if 0 != recorder.countMatching(isSessionStatement("INSERT", "s2")) || 1 != recorder.countMatching(func(query string) bool { return "ROLLBACK" == query }) {
        t.Fatalf("expected nothing written and the transaction rolled back: %q", recorder.recordedQueries())
    }
}


/* a held row admitted within the grace counts as live without the storage read, so a concurrent sign-in whose session is not stored yet keeps its place and the cap releases the oldest */
func TestBunUserSessionRepositoryAdmit_ARowWithinTheGraceCountsWhateverTheStorageAnswers(t *testing.T) {
    now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = heldSessionRowsAdmittedAt(true, now.Add(-time.Second), "s1", "s2", "s3", "s4", "s5")
    repositoryInstance := NewBunUserSessionRepository(database)

    var releasedList []string
    admitErr := repositoryInstance.Admit(context.Background(), "user-1", "", "s6", now, func(sessionId string) (bool, error) { return false, nil }, func(sessionId string) error {
        releasedList = append(releasedList, sessionId)

        return nil
    })
    if nil != admitErr {
        t.Fatalf("the admission failed: %v", admitErr)
    }

    if "[s1]" != fmt.Sprint(releasedList) {
        t.Fatalf("expected the oldest released past the cap, got %v", releasedList)
    }

    if 0 != recorder.countMatching(func(query string) bool { return true == strings.HasPrefix(query, "DELETE") && false == strings.Contains(query, "'s1'") }) {
        t.Fatalf("expected no row within the grace dropped as ended: %q", recorder.recordedQueries())
    }
}
