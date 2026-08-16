package serializer

import (
    "fmt"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
    serializercontract "github.com/precision-soft/melody/v2/serializer/contract"
)

func NewPlainTextSerializer() *PlainTextSerializer {
    return &PlainTextSerializer{}
}

type PlainTextSerializer struct {
}

func (instance *PlainTextSerializer) Serialize(value any) ([]byte, error) {
    switch typedValue := value.(type) {
    case string:
        return []byte(typedValue), nil
    case []byte:
        /* the serialized payload is a copy, not the caller's backing array: the string and default branches both allocate, and a caller returning a pooled buffer to its pool after Serialize would otherwise overwrite the bytes it believes it snapshotted */
        copied := make([]byte, len(typedValue))
        copy(copied, typedValue)

        return copied, nil
    default:
        return []byte(fmt.Sprintf("%v", value)), nil
    }
}

func (instance *PlainTextSerializer) Deserialize(payload []byte, target any) error {
    /* a typed-nil pointer target passes the plain comparison, matches its case below and dereferences nil on the assignment — it is refused here with the same error the untyped nil gets, which is how the json serializer answers the identical misuse */
    if nil == target || true == internal.IsNilInterface(target) {
        return exception.NewError("deserialize target is nil", nil, nil)
    }

    switch typedTarget := target.(type) {
    case *string:
        *typedTarget = string(payload)
        return nil
    case *[]byte:
        /* the deserialized value owns its bytes: storing the payload slice itself would alias the caller's read buffer into the target, where the string case one branch above copies */
        copied := make([]byte, len(payload))
        copy(copied, payload)
        *typedTarget = copied
        return nil
    default:
        return exception.NewError(
            "unsupported plain text deserialize target type",
            exceptioncontract.Context{
                "targetType": fmt.Sprintf("%T", target),
            },
            nil,
        )
    }
}

func (instance *PlainTextSerializer) ContentType() string {
    return MimeTextPlain + "; charset=utf-8"
}

var _ serializercontract.Serializer = (*PlainTextSerializer)(nil)
