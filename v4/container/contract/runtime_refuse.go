package contract

import (
    "strings"

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

/* RefuseRuntimeKind answers nil for a runtime of one of the kinds required and otherwise a refusal under ErrRuntimeKindRefused naming the lookup, the kinds required and the runtime's kind; a nil runtime is refused as RefuseNilRuntime refuses it. */
func RefuseRuntimeKind(runtimeInstance Runtime, lookup string, required []Kind) error {
    if nilErr := RefuseNilRuntime(runtimeInstance); nil != nilErr {
        return nilErr
    }

    given := runtimeInstance.Kind()
    requiredNames := make([]string, 0, len(required))
    for _, kind := range required {
        if kind == given {
            return nil
        }

        requiredNames = append(requiredNames, kind.String())
    }

    return exception.NewError(
        "the runtime's kind cannot answer the lookup",
        exceptioncontract.Context{
            "lookup":   lookup,
            "required": strings.Join(requiredNames, ", "),
            "given":    given.String(),
        },
        ErrRuntimeKindRefused,
    )
}
