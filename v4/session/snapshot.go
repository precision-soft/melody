package session

import (
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    sessioncontract "github.com/precision-soft/melody/v4/session/contract"
)

/* Snapshot answers a session's values, modified flag and cleared flag through the session's own Snapshot, read under one critical section. A nil session is refused. */
func Snapshot(sessionInstance sessioncontract.Session) (map[string]any, bool, bool) {
    if true == internal.IsNilInterface(sessionInstance) {
        exception.Panic(
            exception.NewError("session is nil in snapshot", nil, nil),
        )
    }

    return sessionInstance.Snapshot()
}
