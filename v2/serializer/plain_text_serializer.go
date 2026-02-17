package serializer

import (
	"fmt"

	"github.com/precision-soft/melody/v2/exception"
	exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
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
		return typedValue, nil
	default:
		return []byte(fmt.Sprintf("%v", value)), nil
	}
}

func (instance *PlainTextSerializer) Deserialize(payload []byte, target any) error {
	if nil == target {
		return exception.NewError("deserialize target is nil", nil, nil)
	}

	switch typedTarget := target.(type) {
	case *string:
		*typedTarget = string(payload)
		return nil
	case *[]byte:
		*typedTarget = payload
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
