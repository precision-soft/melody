package repository

import (
    "context"
    "sync"
    "time"
)

func newInMemoryUserSessionRepository() UserSessionRepository {
    return &inMemoryUserSessionRepository{sessionsByUser: map[string][]string{}, userBySession: map[string]string{}}
}

/* inMemoryUserSessionRepository keeps each account's sessions in the order they were admitted, oldest first; the mutex is the lock Admit holds on the account. */
type inMemoryUserSessionRepository struct {
    mutex          sync.Mutex
    sessionsByUser map[string][]string
    userBySession  map[string]string
}

func (instance *inMemoryUserSessionRepository) Admit(
    ctx context.Context,
    userId string,
    previousSessionId string,
    sessionId string,
    createdAt time.Time,
    sessionLive func(sessionId string) (bool, error),
    release func(sessionId string) error,
) error {
    validationErr := validateSessionAdmission(userId, sessionId, release, sessionLive)
    if nil != validationErr {
        return validationErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.removeLocked(previousSessionId)

    live, ended, liveErr := liveSessionsOldestFirst(instance.sessionsByUser[userId], sessionLive)
    if nil != liveErr {
        return liveErr
    }

    for _, endedSessionId := range ended {
        instance.removeLocked(endedSessionId)
    }

    for _, pastCap := range sessionsPastCap(live) {
        if releaseErr := release(pastCap); nil != releaseErr {
            return releaseErr
        }

        instance.removeLocked(pastCap)
    }

    instance.sessionsByUser[userId] = append(append([]string{}, instance.sessionsByUser[userId]...), sessionId)
    instance.userBySession[sessionId] = userId

    return nil
}

func (instance *inMemoryUserSessionRepository) Release(ctx context.Context, sessionId string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.removeLocked(sessionId)

    return nil
}

func (instance *inMemoryUserSessionRepository) removeLocked(sessionId string) {
    userId, held := instance.userBySession[sessionId]
    if false == held {
        return
    }

    delete(instance.userBySession, sessionId)

    remaining := make([]string, 0, len(instance.sessionsByUser[userId]))
    for _, heldSessionId := range instance.sessionsByUser[userId] {
        if heldSessionId != sessionId {
            remaining = append(remaining, heldSessionId)
        }
    }

    if 0 == len(remaining) {
        delete(instance.sessionsByUser, userId)

        return
    }

    instance.sessionsByUser[userId] = remaining
}
