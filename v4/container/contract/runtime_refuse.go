package contract

import (
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
)

/* RefuseNilRuntime answers the refusal of a door handed no runtime: nil, a typed nil, or one whose Context is nil; nil otherwise. */
func RefuseNilRuntime(runtimeInstance Runtime) error {
    if true == internal.IsNilInterface(runtimeInstance) || true == internal.IsNilInterface(runtimeInstance.Context()) {
        return exception.NewError(
            "the runtime is nil or carries no context",
            exceptioncontract.Context{"argument": "runtimeInstance"},
            nil,
        )
    }

    return nil
}
