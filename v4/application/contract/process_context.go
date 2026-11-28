package contract

import (
    "time"
)

/* ProcessContext is the console counterpart of the http request context: the identity of one cli run, the process id every log record of the run is correlated under and the moment the run started. It lives on the run's scope, installed by the cli entry point; the root container never carries it. */
type ProcessContext interface {
    ProcessId() string

    StartedAt() time.Time
}
