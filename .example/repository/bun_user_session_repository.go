package repository

import (
    "context"
    "time"

    "github.com/uptrace/bun"
)

type userSessionRow struct {
    bun.BaseModel `bun:"table:melody_example_v1_user_session,alias:user_session"`

    SessionId      string    `bun:"session_id,pk"`
    UserIdentifier string    `bun:"user_identifier,notnull"`
    CreatedAt      time.Time `bun:"created_at,notnull,type:datetime(6)"`
}

func NewBunUserSessionRepository(database *bun.DB) *bunUserSessionRepository {
    return &bunUserSessionRepository{database: database}
}

type bunUserSessionRepository struct {
    database *bun.DB
}

/* Admit locks the account's row FOR UPDATE, the lock the admin doors and GrantRole take, so the sign-ins of one account admit one at a time; the rows are read, released past the cap, and written inside that one transaction. An account deleted before the lock answers ErrSessionAccountAbsent, and one deleted after it waits for the commit, whose rows its cascade then removes. A failure after a release rolls back the row deletes while the released sessions stay ended; those rows are dropped as ended at the next admission. */
func (instance *bunUserSessionRepository) Admit(
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

    return instance.database.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
        _, found, lockErr := lockUserRow(ctx, tx, userId)
        if nil != lockErr {
            return lockErr
        }

        if false == found {
            return ErrSessionAccountAbsent
        }

        if "" != previousSessionId {
            if _, deleteErr := tx.NewDelete().Model((*userSessionRow)(nil)).Where("session_id = ?", previousSessionId).Exec(ctx); nil != deleteErr {
                return deleteErr
            }
        }

        var heldRows []userSessionRow
        selectErr := tx.NewSelect().
            Model(&heldRows).
            Column("session_id", "created_at").
            Where("user_identifier = ?", userId).
            OrderExpr("created_at ASC, session_id ASC").
            Scan(ctx)
        if nil != selectErr {
            return selectErr
        }

        heldOldestFirst := make([]heldSession, 0, len(heldRows))
        for _, heldRow := range heldRows {
            heldOldestFirst = append(heldOldestFirst, heldSession{sessionId: heldRow.SessionId, admittedAt: heldRow.CreatedAt})
        }

        live, ended, liveErr := liveSessionsOldestFirst(heldOldestFirst, createdAt, sessionLive)
        if nil != liveErr {
            return liveErr
        }

        for _, endedSessionId := range ended {
            if _, deleteErr := tx.NewDelete().Model((*userSessionRow)(nil)).Where("session_id = ?", endedSessionId).Exec(ctx); nil != deleteErr {
                return deleteErr
            }
        }

        for _, pastCap := range sessionsPastCap(live) {
            if releaseErr := release(pastCap); nil != releaseErr {
                return releaseErr
            }

            if _, deleteErr := tx.NewDelete().Model((*userSessionRow)(nil)).Where("session_id = ?", pastCap).Exec(ctx); nil != deleteErr {
                return deleteErr
            }
        }

        row := &userSessionRow{SessionId: sessionId, UserIdentifier: userId, CreatedAt: createdAt.UTC()}

        _, insertErr := tx.NewInsert().Model(row).Exec(ctx)

        return insertErr
    })
}

func (instance *bunUserSessionRepository) Release(ctx context.Context, sessionId string) error {
    if "" == sessionId {
        return nil
    }

    _, deleteErr := instance.database.NewDelete().Model((*userSessionRow)(nil)).Where("session_id = ?", sessionId).Exec(ctx)

    return deleteErr
}
