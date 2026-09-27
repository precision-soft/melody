package contract

import (
    "time"
)

type RequestContext interface {
    RequestId() string

    StartedAt() time.Time
}
