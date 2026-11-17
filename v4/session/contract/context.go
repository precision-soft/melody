package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

/* RefuseNilContext answers the refusal of a nil or typed-nil context, nil otherwise. */
func RefuseNilContext(ctx context.Context) error {
    if true == internal.IsNilInterface(ctx) {
        return exception.NewError("session context is nil", nil, nil)
    }

    return nil
}
