package contract

import (
    "context"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
)

/* RefuseNilContext answers the refusal every backend gives an operation handed no context, nil otherwise; a typed-nil context is refused too, since its first method call would panic. */
func RefuseNilContext(ctx context.Context) error {
    if true == internal.IsNilInterface(ctx) {
        return exception.NewError("cache context is nil", nil, nil)
    }

    return nil
}
