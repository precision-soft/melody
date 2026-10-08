package session

import (
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    sessioncontract "github.com/precision-soft/melody/v3/session/contract"
)

/* Snapshot answers a session's values, modified flag and cleared flag. A session implementing sessioncontract.SnapshotSession, as the framework's does, answers them under one critical section; another is read through IsCleared, IsModified and All, three reads a concurrent Clear can interleave. A nil session is refused. */
func Snapshot(sessionInstance sessioncontract.Session) (map[string]any, bool, bool) {
    if true == internal.IsNilInterface(sessionInstance) {
        exception.Panic(
            exception.NewError("session is nil in snapshot", nil, nil),
        )
    }

    snapshotSession, isSnapshotSession := sessionInstance.(sessioncontract.SnapshotSession)
    if true == isSnapshotSession {
        return snapshotSession.Snapshot()
    }

    cleared := sessionInstance.IsCleared()
    modified := sessionInstance.IsModified()

    return sessionInstance.All(), modified, cleared
}
