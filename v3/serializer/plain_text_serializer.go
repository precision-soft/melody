package serializer

import (
    "fmt"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
    serializercontract "github.com/precision-soft/melody/v3/serializer/contract"
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
        /* Serialized bytes must outlive the caller’s buffer. */
        copied := make([]byte, len(typedValue))
        copy(copied, typedValue)

        return copied, nil
    default:
        return []byte(fmt.Sprintf("%v", value)), nil
    }
}

func (instance *PlainTextSerializer) Deserialize(payload []byte, target any) error {

    if nil == target || true == internal.IsNilInterface(target) {
        return exception.NewError("deserialize target is nil", nil, nil)
    }

    switch typedTarget := target.(type) {
    case *string:
        *typedTarget = string(payload)
        return nil
    case *[]byte:
        /* The target owns its bytes independently of the input buffer. */
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
