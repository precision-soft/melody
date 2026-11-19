package contract

import (
    "context"
)

/* EnvironmentSource loads the process's environment values under the context it is handed, which ends a read of a source that waits on something. */
type EnvironmentSource interface {
    Load(ctx context.Context) (map[string]string, error)
}
