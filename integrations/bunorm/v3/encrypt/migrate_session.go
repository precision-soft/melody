package encrypt

import (
    "context"
    "database/sql"

    "github.com/precision-soft/melody/v3/exception"
)

/* the pool, or one connection pinned for a run, so the sql_mode the judge reads is the one the writes run under */
type sqlSession interface {
    QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row
    ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error)
}

func (instance *Migrator) queries() sqlSession {
    if nil != instance.session {
        return instance.session
    }

    return instance.db.DB
}

func (instance *Migrator) pinSession(ctx context.Context) (*Migrator, func(), error) {
    connection, connectionErr := instance.db.DB.Conn(ctx)
    if nil != connectionErr {
        return nil, nil, exception.NewError("migrate could not take a connection for the run", nil, connectionErr)
    }

    /* a pinned connection gets no bad-connection retry from the pool, so a stale one is traded once for a fresh one before anything is judged */
    if pingErr := connection.PingContext(ctx); nil != pingErr {
        _ = connection.Close()

        connection, connectionErr = instance.db.DB.Conn(ctx)
        if nil != connectionErr {
            return nil, nil, exception.NewError("migrate could not take a connection for the run", nil, connectionErr)
        }
    }

    pinned := *instance
    pinned.session = connection

    return &pinned, func() { _ = connection.Close() }, nil
}

func (instance *Migrator) judged(ctx context.Context, spec TableSpec, mode string) (*Migrator, TableSpec, func(), error) {
    pinned, release, pinErr := instance.pinSession(ctx)
    if nil != pinErr {
        return nil, spec, nil, instance.classifyRunError(spec, 0, "", pinErr)
    }

    /* released on every way out but success, a panic inside a judge query included, where the caller's own defer is not registered yet */
    succeeded := false
    defer func() {
        if false == succeeded {
            release()
        }
    }()

    resolved, judgeErr := pinned.judgeSpecAgainstTheSchema(ctx, spec, mode)
    if nil != judgeErr {
        return nil, spec, nil, pinned.classifyRunError(spec, 0, "", judgeErr)
    }

    succeeded = true

    return pinned, resolved, release, nil
}
