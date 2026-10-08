package session

import (
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

/* Snapshot answers a session's values, modified flag and cleared flag. A session implementing sessioncontract.SnapshotSession, as the framework's does, answers them under one critical section; another is read through IsCleared, IsModified and All, three reads a concurrent Clear can interleave. */
func Snapshot(sessionInstance sessioncontract.Session) (map[string]any, bool, bool) {
    snapshotSession, isSnapshotSession := sessionInstance.(sessioncontract.SnapshotSession)
    if true == isSnapshotSession {
        return snapshotSession.Snapshot()
    }

    cleared := sessionInstance.IsCleared()
    modified := sessionInstance.IsModified()

    return sessionInstance.All(), modified, cleared
}
