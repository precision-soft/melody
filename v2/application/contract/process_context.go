package contract

import (
    "time"
)

/* ProcessContext is the console counterpart of the http request context: the identity of one cli run — the generated process id every log record of the run is correlated under, and the moment the run started. It lives on the run's scope, installed by the cli entry point, so a scoped service resolves it exactly the way a request-scoped service resolves the request context; the root container never carries it, and an http process carries the request context instead. */
type ProcessContext interface {
    ProcessId() string

    StartedAt() time.Time
}
