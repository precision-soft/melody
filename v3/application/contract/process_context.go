package contract

import (
    "time"
)

/* ProcessContext identifies one CLI run and its start time. It is installed in the run’s scope, not in the root container. */
type ProcessContext interface {
    ProcessId() string

    StartedAt() time.Time
}
