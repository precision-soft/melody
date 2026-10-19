package session

import (
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* Snapshot answers a session's values, modified flag and cleared flag. A session implementing sessioncontract.SnapshotSession, as the framework's does, answers them under one critical section; another is read through All, IsModified and IsCleared, in that order, so a Clear landing between the reads is answered as cleared, since the flag it latches is read last. A nil session is refused. */
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

    /* the cleared flag is read last: Clear latches it, so a Clear landing between these reads is seen here, where read first it would be missed and the session saved as modified under its id */
    values := sessionInstance.All()
    modified := sessionInstance.IsModified()
    cleared := sessionInstance.IsCleared()

    return values, modified, cleared
}
