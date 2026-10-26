package container

import (
    "context"
    "fmt"
    "reflect"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
)

type RegisterOptions = containercontract.RegisterOptions

type Closer = containercontract.Closer

/* NewCloser builds the Closer a registration declares, closing the service through closer in place of the Close and CloseWithContext doors its value carries. The closer replaces the value's doors at the container's teardown, for an instance an override evicted and for a value built after the container closed, and closes an override installed under the name when it takes the override's type; an override of a type it does not take closes through its own doors. A registration whose provider declares a type the closer does not take is refused with ErrCloserTypeMismatch, and so is a Closer built by hand with one of its two fields left out; the scoped registrations refuse a Closer with ErrScopedCloserUnsupported, and a nil closer panics. */
func NewCloser[T any](closer func(closeContext context.Context, value T) error) containercontract.Closer {
    if nil == closer {
        exception.Panic(exception.NewError("a closer is required", nil, nil))
    }

    return containercontract.Closer{
        ValueType: reflect.TypeFor[T](),
        Close: func(closeContext context.Context, value any) error {
            typedValue, takesValue := value.(T)
            if false == takesValue {
                return exception.NewError(
                    "the closer does not take the value it was handed",
                    map[string]any{
                        "closerValueType": reflect.TypeFor[T]().String(),
                        "valueType":       fmt.Sprintf("%T", value),
                    },
                    ErrCloserTypeMismatch,
                )
            }

            return closer(closeContext, typedValue)
        },
    }
}

func closerIsDeclared(closer containercontract.Closer) bool {
    return nil != closer.Close || nil != closer.ValueType
}

func closerIsIncomplete(closer containercontract.Closer) bool {
    return (nil == closer.Close) != (nil == closer.ValueType)
}
